package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// adminEnabled reports whether the runtime admin API is switched on. It is off
// by default so the server's tool surface stays fixed unless a test opts in.
func adminEnabled() bool { return os.Getenv("MCP_ADMIN_ENABLED") == "true" }

// adminToolSpec is the POST /admin/tools body.
type adminToolSpec struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Title       string            `json:"title,omitempty"`
	InputSchema json.RawMessage   `json:"inputSchema,omitempty"`
	Annotations *adminAnnotations `json:"annotations,omitempty"`
}

// adminAnnotations keeps every hint as *bool so an explicit false is distinct from absent.
type adminAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// recordedCall is one tools/call request received on /mcp.
type recordedCall struct {
	Tool          string          `json:"tool"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	McpNameHeader string          `json:"mcpNameHeader,omitempty"`
}

// adminAPI registers echo tools at runtime and records every tools/call the
// MCP server receives, so tests can control and observe a remote MCP upstream.
type adminAPI struct {
	server   *mcp.Server
	reserved map[string]bool // built-in and OAS tool names, never replaceable

	// ops serialises registry changes with the matching SDK call so the two cannot drift.
	ops sync.Mutex
	mu  sync.Mutex
	// dynamic holds each tool as served, guarded by mu.
	dynamic map[string]*mcp.Tool
	calls   []recordedCall
}

// newAdminAPI must run after every static tool is registered: it snapshots their names.
func newAdminAPI(server *mcp.Server) (*adminAPI, error) {
	names, err := serverToolNames(server)
	if err != nil {
		return nil, err
	}
	reserved := make(map[string]bool, len(names))
	for _, name := range names {
		reserved[name] = true
	}
	a := &adminAPI{server: server, reserved: reserved, dynamic: map[string]*mcp.Tool{}}
	server.AddReceivingMiddleware(a.recordToolCalls)
	return a, nil
}

// serverToolNames lists the server's tools over an in-memory session, since the
// SDK has no direct accessor.
func serverToolNames(server *mcp.Server) ([]string, error) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "admin-snapshot", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, err
	}
	defer clientSession.Close()
	var names []string
	for tool, err := range clientSession.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		names = append(names, tool.Name)
	}
	return names, nil
}

func (a *adminAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /admin/tools", a.addTool)
	mux.HandleFunc("GET /admin/tools", a.listTools)
	mux.HandleFunc("DELETE /admin/tools", a.removeAllTools)
	mux.HandleFunc("DELETE /admin/tools/{name}", a.removeTool)
	mux.HandleFunc("GET /admin/calls", a.listCalls)
	mux.HandleFunc("DELETE /admin/calls", a.clearCalls)
	mux.HandleFunc("POST /admin/fixtures/github", a.addGitHubFixture)
}

// recordToolCalls runs before dispatch, so calls to unknown tools are recorded too.
func (a *adminAPI) recordToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
			rec := recordedCall{Tool: call.Params.Name, Arguments: call.Params.Arguments}
			if call.Extra != nil && call.Extra.Header != nil {
				rec.McpNameHeader = call.Extra.Header.Get("Mcp-Name")
			}
			a.mu.Lock()
			a.calls = append(a.calls, rec)
			a.mu.Unlock()
		}
		return next(ctx, method, req)
	}
}

func (a *adminAPI) addTool(w http.ResponseWriter, r *http.Request) {
	var spec adminToolSpec
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	tool, err := a.addSpec(spec)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errReservedTool) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeRESTFixtureJSON(w, http.StatusCreated, tool)
}

var errReservedTool = errors.New("name belongs to a built-in tool")

// addSpec registers spec with the SDK and records it only once the SDK accepted it.
func (a *adminAPI) addSpec(spec adminToolSpec) (*mcp.Tool, error) {
	if spec.Name == "" {
		return nil, errors.New("name is required")
	}
	if a.reserved[spec.Name] {
		return nil, fmt.Errorf("%q: %w", spec.Name, errReservedTool)
	}
	schema := map[string]any{"type": "object"}
	if len(spec.InputSchema) > 0 {
		if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("inputSchema must be a JSON object: %w", err)
		}
		if schema["type"] != "object" {
			return nil, errors.New(`inputSchema must have "type": "object"`)
		}
	}
	tool := &mcp.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		Title:       spec.Title,
		InputSchema: schema,
		Annotations: spec.Annotations.toMCP(),
	}
	a.ops.Lock()
	defer a.ops.Unlock()
	if err := addToolOrError(a.server, tool); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.dynamic[spec.Name] = tool
	a.mu.Unlock()
	return tool, nil
}

// addToolOrError turns AddTool's validation panics (bad schema, invalid
// x-mcp-header annotations) into an error. AddTool panics before changing state.
func addToolOrError(server *mcp.Server, tool *mcp.Tool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tool rejected: %v", r)
		}
	}()
	server.AddTool(tool, echoToolHandler)
	return nil
}

// toMCP maps onto the SDK type. The SDK always serialises readOnlyHint and
// idempotentHint (plain bools), so only destructiveHint and openWorldHint can
// be absent once any annotation is set.
func (an *adminAnnotations) toMCP() *mcp.ToolAnnotations {
	if an == nil {
		return nil
	}
	out := &mcp.ToolAnnotations{
		Title:           an.Title,
		DestructiveHint: an.DestructiveHint,
		OpenWorldHint:   an.OpenWorldHint,
	}
	if an.ReadOnlyHint != nil {
		out.ReadOnlyHint = *an.ReadOnlyHint
	}
	if an.IdempotentHint != nil {
		out.IdempotentHint = *an.IdempotentHint
	}
	return out
}

// echoToolHandler returns the tool name the server received and the raw arguments.
func echoToolHandler(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	echo := map[string]any{"tool": req.Params.Name, "arguments": args}
	text, err := json.Marshal(echo)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
		StructuredContent: echo,
	}, nil
}

func (a *adminAPI) listTools(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	tools := make([]*mcp.Tool, 0, len(a.dynamic))
	for _, tool := range a.dynamic {
		tools = append(tools, tool)
	}
	a.mu.Unlock()
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	writeRESTFixtureJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

// removeTool only removes dynamic tools; built-in tools return 404.
func (a *adminAPI) removeTool(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a.ops.Lock()
	defer a.ops.Unlock()
	a.mu.Lock()
	_, ok := a.dynamic[name]
	delete(a.dynamic, name)
	a.mu.Unlock()
	if !ok {
		http.Error(w, "no dynamic tool named "+name, http.StatusNotFound)
		return
	}
	a.server.RemoveTools(name)
	w.WriteHeader(http.StatusNoContent)
}

func (a *adminAPI) removeAllTools(w http.ResponseWriter, _ *http.Request) {
	a.ops.Lock()
	defer a.ops.Unlock()
	a.mu.Lock()
	names := make([]string, 0, len(a.dynamic))
	for name := range a.dynamic {
		names = append(names, name)
	}
	a.dynamic = map[string]*mcp.Tool{}
	a.mu.Unlock()
	if len(names) > 0 {
		a.server.RemoveTools(names...)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *adminAPI) listCalls(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	calls := append([]recordedCall{}, a.calls...)
	a.mu.Unlock()
	writeRESTFixtureJSON(w, http.StatusOK, map[string]any{"calls": calls})
}

func (a *adminAPI) clearCalls(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	a.calls = nil
	a.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (a *adminAPI) addGitHubFixture(w http.ResponseWriter, _ *http.Request) {
	tools := make([]*mcp.Tool, 0, len(githubFixtureTools))
	for _, spec := range githubFixtureTools {
		tool, err := a.addSpec(spec)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errReservedTool) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		tools = append(tools, tool)
	}
	writeRESTFixtureJSON(w, http.StatusCreated, map[string]any{"tools": tools})
}

func repoSchema(extra ...string) json.RawMessage {
	props := map[string]any{"repo": map[string]any{"type": "string"}}
	required := []string{"repo"}
	for _, p := range extra {
		props[p] = map[string]any{"type": "string"}
		required = append(required, p)
	}
	raw, _ := json.Marshal(map[string]any{"type": "object", "properties": props, "required": required})
	return raw
}

func boolPtr(b bool) *bool { return &b }

// githubFixtureTools is a small GitHub-like catalogue. Annotations are kept
// minimal; tests set specific upstream annotations through POST /admin/tools.
var githubFixtureTools = []adminToolSpec{
	{Name: "create_issue", Description: "Creates an issue", InputSchema: repoSchema("title")},
	{Name: "list_issues", Description: "Lists issues", InputSchema: repoSchema(), Annotations: &adminAnnotations{ReadOnlyHint: boolPtr(true)}},
	{Name: "remove_repo", Description: "Removes a repository", InputSchema: repoSchema()},
	{Name: "update_repo", Description: "Updates a repository", InputSchema: repoSchema()},
	{Name: "delete_repo", Description: "Deletes a repository", InputSchema: repoSchema()},
}
