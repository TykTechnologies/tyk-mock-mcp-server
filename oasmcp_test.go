package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fixtureSpec = "testdata/example-api.oas.json"

// RegisterFromOAS turns each operation into a tool, named snake_case(operationId).
func TestRegisterFromOAS_DerivesTools(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	names, err := RegisterFromOAS(srv, fixtureSpec, UpstreamConfig{BaseURL: "http://unused"})
	if err != nil {
		t.Fatalf("RegisterFromOAS: %v", err)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range []string{"get_item", "create_item"} {
		if !got[want] {
			t.Errorf("expected tool %q, got %v", want, names)
		}
	}
}

// The input schema is built from path params + the JSON request body.
func TestBuildToolFor_InputSchema(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile(fixtureSpec)
	if err != nil {
		t.Fatal(err)
	}

	getSchema, getBind := buildToolFor("get", "/items/{id}", doc.Paths.Find("/items/{id}").Get)
	if got := getSchema["required"]; !equalStrings(got, []string{"id"}) {
		t.Errorf("getItem required = %v, want [id]", got)
	}
	if !equalStrings(getBind.pathParams, []string{"id"}) {
		t.Errorf("getItem pathParams = %v, want [id]", getBind.pathParams)
	}

	postSchema, postBind := buildToolFor("post", "/items", doc.Paths.Find("/items").Post)
	props := postSchema["properties"].(map[string]any)
	if _, ok := props["name"]; !ok {
		t.Errorf("createItem missing 'name' property: %v", props)
	}
	if _, ok := props["qty"]; !ok {
		t.Errorf("createItem missing 'qty' property: %v", props)
	}
	if !contains(postBind.bodyProps, "name") || !contains(postBind.bodyProps, "qty") {
		t.Errorf("createItem bodyProps = %v", postBind.bodyProps)
	}
}

// A tool call rebuilds the request and forwards the inbound bearer + trace.
func TestOASTool_ProxiesWithAuthAndTrace(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotTrace string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.RequestURI()
		gotAuth, gotTrace = r.Header.Get("Authorization"), r.Header.Get("Traceparent")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := UpstreamConfig{BaseURL: upstream.URL, ForwardAuth: true, ForwardTrace: true}
	handler := makeHandler(binding{method: "POST", pathTemplate: "/items", bodyProps: []string{"name", "qty"}}, cfg)

	// Simulate the inbound MCP request's captured headers.
	setCurrentRequestMetadata(&HTTPRequestMetadata{Headers: map[string]string{
		"Authorization": "Bearer abc123",
		"Traceparent":   "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	}})

	req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{
		Name:      "create_item",
		Arguments: json.RawMessage(`{"name":"widget","qty":3}`),
	}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if gotMethod != "POST" || gotPath != "/items" {
		t.Errorf("upstream got %s %s, want POST /items", gotMethod, gotPath)
	}
	if gotAuth != "Bearer abc123" {
		t.Errorf("bearer not forwarded: %q", gotAuth)
	}
	if gotTrace == "" {
		t.Errorf("traceparent not forwarded")
	}
	var body map[string]any
	_ = json.Unmarshal(gotBody, &body)
	if body["name"] != "widget" {
		t.Errorf("request body = %s", gotBody)
	}
	out, ok := res.StructuredContent.(UpstreamCallOutput)
	if !ok || out.Status != 200 {
		t.Errorf("result = %+v", res.StructuredContent)
	}
}

func equalStrings(got any, want []string) bool {
	gs, ok := got.([]string)
	if !ok || len(gs) != len(want) {
		return false
	}
	for i := range want {
		if gs[i] != want[i] {
			return false
		}
	}
	return true
}
