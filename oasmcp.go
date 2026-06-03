package main

// OpenAPI → MCP tools.
//
// RegisterFromOAS reads an OpenAPI 3 spec and registers one MCP tool per
// operation: the tool's input schema is built from the operation's path/query
// parameters and its application/json request body, and at call time a generic
// handler rebuilds the HTTP request, replays the inbound Authorization bearer,
// forwards the W3C trace context, and returns the upstream response.
//
// This lets a spec define the MCP tool surface (names, inputs, scopes) without
// writing Go — useful for the demo and for integration tests that need an MCP
// backend whose tools mirror a real API contract. Authorization/scope is NOT
// enforced here; that is the gateway's job. The server only forwards.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UpstreamConfig is the runtime binding the OpenAPI spec does not carry.
type UpstreamConfig struct {
	BaseURL      string // base URL to call (e.g. the gateway)
	Host         string // Host header to send (custom domain), empty to leave as-is
	ForwardAuth  bool   // replay the inbound Authorization bearer
	ForwardTrace bool   // forward inbound W3C trace context (traceparent/tracestate)
}

// UpstreamCallOutput is the structured tool result surfaced back to the client.
type UpstreamCallOutput struct {
	Status          int                    `json:"status"`
	URL             string                 `json:"url"`
	ResponseHeaders map[string][]string    `json:"responseHeaders,omitempty"`
	Upstream        map[string]interface{} `json:"upstream,omitempty"`
	Error           string                 `json:"error,omitempty"`
}

// binding is everything needed to rebuild one operation's HTTP request.
type binding struct {
	method       string
	pathTemplate string
	pathParams   []string
	queryParams  []string
	bodyProps    []string
}

// RegisterFromOAS loads the spec at specPath and registers one MCP tool per
// operation on server. It returns the registered tool names.
func RegisterFromOAS(server *mcp.Server, specPath string, cfg UpstreamConfig) ([]string, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("load spec %q: %w", specPath, err)
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("spec %q has no paths", specPath)
	}

	var names []string
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			name := toolName(op.OperationID, method, path)
			schema, b := buildToolFor(method, path, op)
			tool := &mcp.Tool{Name: name, Description: toolDescription(op), InputSchema: schema}
			server.AddTool(tool, makeHandler(b, cfg))
			names = append(names, name)
		}
	}
	return names, nil
}

// buildToolFor produces the input schema (a JSON Schema object) and the request
// binding for one operation.
func buildToolFor(method, path string, op *openapi3.Operation) (map[string]any, binding) {
	props := map[string]any{}
	var required []string
	b := binding{method: strings.ToUpper(method), pathTemplate: path}

	for _, pref := range op.Parameters {
		p := pref.Value
		if p == nil {
			continue
		}
		switch p.In {
		case "path":
			b.pathParams = append(b.pathParams, p.Name)
		case "query":
			b.queryParams = append(b.queryParams, p.Name)
		default:
			continue // header/cookie params are not exposed as tool inputs
		}
		m := schemaToMap(p.Schema)
		if p.Description != "" {
			if _, ok := m["description"]; !ok {
				m["description"] = p.Description
			}
		}
		props[p.Name] = m
		if p.Required {
			required = append(required, p.Name)
		}
	}

	if op.RequestBody != nil && op.RequestBody.Value != nil {
		if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil && mt.Schema != nil && mt.Schema.Value != nil {
			bs := mt.Schema.Value
			for pname, pref := range bs.Properties {
				props[pname] = schemaToMap(pref)
				b.bodyProps = append(b.bodyProps, pname)
				if contains(bs.Required, pname) {
					required = append(required, pname)
				}
			}
		}
	}

	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, b
}

// makeHandler returns the generic tool handler for one binding.
func makeHandler(b binding, cfg UpstreamConfig) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &args)
		}

		p := b.pathTemplate
		for _, name := range b.pathParams {
			p = strings.ReplaceAll(p, "{"+name+"}", url.PathEscape(strval(args[name])))
		}
		if len(b.queryParams) > 0 {
			q := url.Values{}
			for _, name := range b.queryParams {
				if v, ok := args[name]; ok {
					q.Set(name, strval(v))
				}
			}
			if enc := q.Encode(); enc != "" {
				p += "?" + enc
			}
		}
		var body []byte
		if len(b.bodyProps) > 0 {
			obj := map[string]any{}
			for _, name := range b.bodyProps {
				if v, ok := args[name]; ok {
					obj[name] = v
				}
			}
			body, _ = json.Marshal(obj)
		}

		out := doUpstreamCall(ctx, cfg, b.method, p, body)
		js, _ := json.Marshal(out)
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: string(js)}},
			StructuredContent: out,
			IsError:           out.Status == 0 && out.Error != "", // transport failure only; a 4xx is a real result
		}, nil
	}
}

// doUpstreamCall issues the rebuilt request to the upstream, replaying auth and
// trace context read from the current request's captured metadata.
func doUpstreamCall(ctx context.Context, cfg UpstreamConfig, method, path string, body []byte) UpstreamCallOutput {
	out := UpstreamCallOutput{URL: cfg.Host + path}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, rdr)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if cfg.Host != "" {
		req.Host = cfg.Host
		req.Header.Set("Host", cfg.Host)
	}
	meta := getCurrentRequestMetadata()
	if cfg.ForwardAuth && meta != nil {
		if b := meta.Headers["Authorization"]; b != "" {
			req.Header.Set("Authorization", b)
		}
	}
	if cfg.ForwardTrace && meta != nil {
		for _, h := range []string{"Traceparent", "Tracestate"} {
			if v := meta.Headers[h]; v != "" {
				req.Header.Set(h, v)
			}
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	out.ResponseHeaders = resp.Header

	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]interface{}
	if json.Unmarshal(raw, &parsed) == nil {
		out.Upstream = parsed
	} else if len(raw) > 0 {
		out.Upstream = map[string]interface{}{"raw": string(raw)}
	}
	if resp.StatusCode >= 400 {
		out.Error = fmt.Sprintf("downstream returned %d", resp.StatusCode)
	}
	return out
}

// ---- helpers ----------------------------------------------------------------

func schemaToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{"type": "string"}
	}
	b, err := ref.Value.MarshalJSON()
	if err != nil {
		return map[string]any{"type": "string"}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]any{"type": "string"}
	}
	return m
}

func toolDescription(op *openapi3.Operation) string {
	if op.Description != "" {
		return op.Description
	}
	return op.Summary
}

// toolName derives an MCP tool name: snake_case(operationId), or a slug of
// method+path when no operationId is present.
func toolName(opID, method, path string) string {
	if opID != "" {
		return camelToSnake(opID)
	}
	slug := strings.Trim(strings.NewReplacer("/", "_", "{", "", "}", "").Replace(path), "_")
	return strings.ToLower(method) + "_" + slug
}

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func strval(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
