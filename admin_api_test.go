package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rawMCPClient speaks legacy streamable HTTP JSON-RPC so tests can assert on
// the exact wire JSON (absent vs explicit-false annotation fields).
type rawMCPClient struct {
	t         *testing.T
	url       string
	sessionID string
	nextID    int
}

func newRawMCPClient(t *testing.T, baseURL string) *rawMCPClient {
	t.Helper()
	c := &rawMCPClient{t: t, url: baseURL + "/mcp"}
	c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "admin-test", "version": "1"},
	}, nil)
	c.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	return c
}

func (c *rawMCPClient) post(body string, headers map[string]string) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodPost, c.url, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		c.sessionID = id
	}
	return resp, raw
}

// call sends a JSON-RPC request and returns its "result" member.
func (c *rawMCPClient) call(method string, params any, headers map[string]string) json.RawMessage {
	c.t.Helper()
	c.nextID++
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	resp, raw := c.post(string(body), headers)
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("%s: status %d body %s", method, resp.StatusCode, raw)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data:") {
				raw = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				break
			}
		}
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		c.t.Fatalf("%s: decode %s: %v", method, raw, err)
	}
	if len(envelope.Error) != 0 {
		c.t.Fatalf("%s: JSON-RPC error %s", method, envelope.Error)
	}
	return envelope.Result
}

// listToolsRaw returns tools/list keyed by name, each tool as a generic JSON map.
func (c *rawMCPClient) listToolsRaw() map[string]map[string]any {
	c.t.Helper()
	var res struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(c.call("tools/list", map[string]any{}, nil), &res); err != nil {
		c.t.Fatal(err)
	}
	out := make(map[string]map[string]any, len(res.Tools))
	for _, tool := range res.Tools {
		out[tool["name"].(string)] = tool
	}
	return out
}

func newAdminTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("MCP_ADMIN_ENABLED", "true")
	srv := httptest.NewServer(newHTTPMux(setupServer()))
	t.Cleanup(srv.Close)
	return srv
}

func adminRequest(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestAdminAPI_DisabledByDefault(t *testing.T) {
	t.Setenv("MCP_ADMIN_ENABLED", "")
	srv := httptest.NewServer(newHTTPMux(setupServer()))
	defer srv.Close()

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/admin/tools"},
		{http.MethodDelete, "/admin/tools"},
		{http.MethodGet, "/admin/calls"},
		{http.MethodPost, "/admin/fixtures/github"},
	} {
		status, _ := adminRequest(t, tc.method, srv.URL+tc.path, `{"name":"x"}`)
		if status != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404", tc.method, tc.path, status)
		}
	}
}

func TestAdminAPI_RegisteredToolAppearsInToolsListWithAnnotations(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)

	status, raw := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{
		"name": "create_issue",
		"description": "Creates an issue",
		"title": "Create issue",
		"inputSchema": {"type":"object","properties":{"repo":{"type":"string"}}},
		"annotations": {"title":"Create","readOnlyHint":false,"destructiveHint":false,"idempotentHint":true}
	}`)
	if status != http.StatusCreated {
		t.Fatalf("register status = %d body %s", status, raw)
	}

	tools := client.listToolsRaw()
	tool, ok := tools["create_issue"]
	if !ok {
		t.Fatalf("create_issue missing from tools/list")
	}
	if _, ok := tools["get_users"]; !ok {
		t.Fatalf("built-in tools must stay listed")
	}
	if tool["description"] != "Creates an issue" || tool["title"] != "Create issue" {
		t.Fatalf("unexpected tool metadata: %v", tool)
	}
	ann, ok := tool["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("annotations missing: %v", tool)
	}
	if v, ok := ann["destructiveHint"]; !ok || v != false {
		t.Errorf("destructiveHint = %v (present %v), want explicit false", v, ok)
	}
	if _, ok := ann["openWorldHint"]; ok {
		t.Errorf("openWorldHint must be absent when not supplied: %v", ann)
	}
	if ann["idempotentHint"] != true || ann["title"] != "Create" {
		t.Errorf("unexpected annotations: %v", ann)
	}
	props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["repo"]; !ok {
		t.Errorf("inputSchema not preserved: %v", tool["inputSchema"])
	}
}

func TestAdminAPI_ToolWithoutAnnotationsHasNoAnnotationsField(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)

	if status, raw := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"delete_repo","description":"Deletes a repo"}`); status != http.StatusCreated {
		t.Fatalf("register status = %d body %s", status, raw)
	}
	tool := client.listToolsRaw()["delete_repo"]
	if tool == nil {
		t.Fatal("delete_repo missing")
	}
	if _, ok := tool["annotations"]; ok {
		t.Errorf("annotations must be absent: %v", tool)
	}
	if tool["inputSchema"].(map[string]any)["type"] != "object" {
		t.Errorf("default inputSchema must be an object schema: %v", tool["inputSchema"])
	}
}

func TestAdminAPI_RejectsInvalidRegistration(t *testing.T) {
	srv := newAdminTestServer(t)
	for _, body := range []string{
		`{"description":"no name"}`,
		`{"name":"x","inputSchema":{"type":"string"}}`,
		`not json`,
	} {
		if status, _ := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", body); status != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, status)
		}
	}
}

func TestAdminAPI_CallEchoesToolNameAndRecordsCallWithMcpNameHeader(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"create_issue","description":"Creates an issue"}`)

	result := client.call("tools/call",
		map[string]any{"name": "create_issue", "arguments": map[string]any{"repo": "tyk", "title": "bug"}},
		map[string]string{"Mcp-Name": "create_issue"})

	var res struct {
		IsError           bool `json:"isError"`
		StructuredContent struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		} `json:"structuredContent"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		t.Fatal(err)
	}
	if res.IsError || res.StructuredContent.Tool != "create_issue" || res.StructuredContent.Arguments["repo"] != "tyk" {
		t.Fatalf("unexpected echo: %s", result)
	}
	if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, `"tool":"create_issue"`) {
		t.Fatalf("text content must carry the echo: %s", result)
	}

	// A built-in tool call without the header is recorded too.
	client.call("tools/call", map[string]any{"name": "generate_uuid", "arguments": map[string]any{}}, nil)

	status, raw := adminRequest(t, http.MethodGet, srv.URL+"/admin/calls", "")
	if status != http.StatusOK {
		t.Fatalf("calls status = %d", status)
	}
	var calls struct {
		Calls []recordedCall `json:"calls"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls.Calls) != 2 {
		t.Fatalf("recorded calls = %s", raw)
	}
	first := calls.Calls[0]
	if first.Tool != "create_issue" || first.McpNameHeader != "create_issue" || !strings.Contains(string(first.Arguments), `"title":"bug"`) {
		t.Errorf("unexpected first call: %s", raw)
	}
	if calls.Calls[1].Tool != "generate_uuid" || calls.Calls[1].McpNameHeader != "" {
		t.Errorf("unexpected second call: %s", raw)
	}
	if strings.Count(string(raw), `"mcpNameHeader"`) != 1 {
		t.Errorf("absent Mcp-Name header must be omitted: %s", raw)
	}

	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/calls", ""); status != http.StatusNoContent {
		t.Fatalf("clear calls status = %d", status)
	}
	_, raw = adminRequest(t, http.MethodGet, srv.URL+"/admin/calls", "")
	if strings.TrimSpace(string(raw)) != `{"calls":[]}` {
		t.Errorf("calls after clear = %s", raw)
	}
}

func TestAdminAPI_DeleteRemovesToolsFromToolsList(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"alpha","description":"a"}`)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"beta","description":"b"}`)

	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools/alpha", ""); status != http.StatusNoContent {
		t.Fatalf("delete alpha status = %d", status)
	}
	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools/alpha", ""); status != http.StatusNotFound {
		t.Fatalf("deleting an unknown dynamic tool status = %d, want 404", status)
	}
	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools/get_users", ""); status != http.StatusNotFound {
		t.Fatalf("built-in tools must not be deletable, status = %d", status)
	}
	tools := client.listToolsRaw()
	if _, ok := tools["alpha"]; ok {
		t.Fatal("alpha still listed after delete")
	}
	if _, ok := tools["beta"]; !ok {
		t.Fatal("beta must remain listed")
	}

	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools", ""); status != http.StatusNoContent {
		t.Fatalf("delete all status = %d", status)
	}
	tools = client.listToolsRaw()
	if _, ok := tools["beta"]; ok {
		t.Fatal("beta still listed after reset")
	}
	if len(tools) != 15 {
		t.Fatalf("reset must leave exactly the 15 built-in tools, got %d", len(tools))
	}
}

func TestAdminAPI_GitHubFixtureRegistersRepoTools(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)

	if status, raw := adminRequest(t, http.MethodPost, srv.URL+"/admin/fixtures/github", ""); status != http.StatusCreated {
		t.Fatalf("fixture status = %d body %s", status, raw)
	}
	tools := client.listToolsRaw()
	for _, name := range []string{"create_issue", "list_issues", "remove_repo", "update_repo", "delete_repo"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("%s missing from tools/list", name)
		}
	}
	create := tools["create_issue"]
	if create["description"] != "Creates an issue" {
		t.Errorf("create_issue description = %v", create["description"])
	}
	props := create["inputSchema"].(map[string]any)["properties"].(map[string]any)
	for _, p := range []string{"repo", "title"} {
		if prop, ok := props[p].(map[string]any); !ok || prop["type"] != "string" {
			t.Errorf("create_issue.%s = %v, want string", p, props[p])
		}
	}
	if _, ok := tools["delete_repo"]["annotations"]; ok {
		t.Errorf("delete_repo must have no annotations: %v", tools["delete_repo"])
	}

	// Fixture tools are dynamic, so a reset removes them.
	adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools", "")
	if _, ok := client.listToolsRaw()["create_issue"]; ok {
		t.Error("fixture tools must be removed by reset")
	}
}

func TestAdminAPI_RejectsBuiltInToolNamesWithConflict(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)

	status, raw := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"get_users","description":"hijacked"}`)
	if status != http.StatusConflict {
		t.Fatalf("status = %d body %s, want 409", status, raw)
	}
	tool, ok := client.listToolsRaw()["get_users"]
	if !ok || tool["description"] == "hijacked" {
		t.Fatalf("built-in get_users must stay listed unchanged: %v", tool)
	}
	if status, _ := adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools", ""); status != http.StatusNoContent {
		t.Fatalf("reset status = %d", status)
	}
	if _, ok := client.listToolsRaw()["get_users"]; !ok {
		t.Fatal("reset must not remove built-in tools")
	}
}

func TestAdminAPI_RejectsSchemaTheSDKRefusesWithoutRegistering(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)

	status, raw := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools",
		`{"name":"bad_headers","inputSchema":{"type":"object","properties":{"p":{"type":"object","x-mcp-header":"P"}}}}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d body %s, want 400", status, raw)
	}
	if _, ok := client.listToolsRaw()["bad_headers"]; ok {
		t.Fatal("rejected tool must not be listed")
	}
	_, raw = adminRequest(t, http.MethodGet, srv.URL+"/admin/tools", "")
	if strings.Contains(string(raw), "bad_headers") {
		t.Fatalf("rejected tool must not be in the admin registry: %s", raw)
	}
}

func TestAdminAPI_RejectsUnknownFields(t *testing.T) {
	srv := newAdminTestServer(t)
	for _, body := range []string{
		`{"name":"x","descripton":"typo"}`,
		`{"name":"x","annotations":{"readOnly":true}}`,
	} {
		if status, _ := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", body); status != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, status)
		}
	}
}

func TestAdminAPI_ExplicitOpenWorldHintFalseIsOnTheWire(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"closed","annotations":{"openWorldHint":false}}`)

	ann := client.listToolsRaw()["closed"]["annotations"].(map[string]any)
	if v, ok := ann["openWorldHint"]; !ok || v != false {
		t.Errorf("openWorldHint = %v (present %v), want explicit false", v, ok)
	}
	if _, ok := ann["destructiveHint"]; ok {
		t.Errorf("destructiveHint must be absent: %v", ann)
	}
}

func TestAdminAPI_RepostReplacesDynamicTool(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"create_issue","description":"old"}`)
	if status, _ := adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"create_issue","description":"new"}`); status != http.StatusCreated {
		t.Fatalf("replace status = %d", status)
	}
	if got := client.listToolsRaw()["create_issue"]["description"]; got != "new" {
		t.Fatalf("description = %v, want new", got)
	}
}

func TestAdminAPI_ListToolsReturnsServedShape(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"create_issue","annotations":{"destructiveHint":false}}`)

	_, raw := adminRequest(t, http.MethodGet, srv.URL+"/admin/tools", "")
	var admin struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(raw, &admin); err != nil || len(admin.Tools) != 1 {
		t.Fatalf("admin tools = %s (%v)", raw, err)
	}
	served, _ := json.Marshal(client.listToolsRaw()["create_issue"])
	got, _ := json.Marshal(admin.Tools[0])
	if string(served) != string(got) {
		t.Fatalf("GET /admin/tools = %s, tools/list = %s", got, served)
	}
}

func TestAdminAPI_RecordsCallsToUnknownToolsAndMismatchedLegacyMcpName(t *testing.T) {
	srv := newAdminTestServer(t)
	client := newRawMCPClient(t, srv.URL)
	adminRequest(t, http.MethodPost, srv.URL+"/admin/tools", `{"name":"gone"}`)
	adminRequest(t, http.MethodDelete, srv.URL+"/admin/tools/gone", "")

	// Errors are irrelevant here; only what the server received matters.
	client.post(`{"jsonrpc":"2.0","id":90,"method":"tools/call","params":{"name":"gone","arguments":{}}}`, nil)
	client.post(`{"jsonrpc":"2.0","id":91,"method":"tools/call","params":{"name":"never_existed","arguments":{}}}`, nil)
	client.call("tools/call", map[string]any{"name": "generate_uuid", "arguments": map[string]any{}},
		map[string]string{"Mcp-Name": "renamed_uuid"})

	_, raw := adminRequest(t, http.MethodGet, srv.URL+"/admin/calls", "")
	var calls struct {
		Calls []recordedCall `json:"calls"`
	}
	if err := json.Unmarshal(raw, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls.Calls) != 3 || calls.Calls[0].Tool != "gone" || calls.Calls[1].Tool != "never_existed" {
		t.Fatalf("unexpected calls: %s", raw)
	}
	if c := calls.Calls[2]; c.Tool != "generate_uuid" || c.McpNameHeader != "renamed_uuid" {
		t.Fatalf("legacy session must record the Mcp-Name header as sent: %s", raw)
	}
}

func TestAdminAPI_OnlyExactTrueEnablesIt(t *testing.T) {
	for _, value := range []string{"1", "TRUE", "True", "yes"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MCP_ADMIN_ENABLED", value)
			srv := httptest.NewServer(newHTTPMux(setupServer()))
			defer srv.Close()
			if status, _ := adminRequest(t, http.MethodGet, srv.URL+"/admin/calls", ""); status != http.StatusNotFound {
				t.Errorf("status = %d, want 404", status)
			}
		})
	}
}
