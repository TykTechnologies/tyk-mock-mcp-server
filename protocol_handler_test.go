package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type statusHandler int

func (s statusHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(int(s))
}

func TestProtocolSwitchHandler_SelectsOnlyMatchingModernDeclarations(t *testing.T) {
	modernBody := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	tests := []struct {
		name       string
		method     string
		header     string
		body       string
		wantStatus int
	}{
		{name: "matching modern declarations", method: http.MethodPost, header: modernProtocolVersion, body: modernBody, wantStatus: http.StatusNoContent},
		{name: "legacy without declarations", method: http.MethodPost, body: `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, wantStatus: http.StatusAccepted},
		{name: "header only", method: http.MethodPost, header: modernProtocolVersion, body: `{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`, wantStatus: http.StatusAccepted},
		{name: "body only", method: http.MethodPost, body: modernBody, wantStatus: http.StatusAccepted},
		{name: "mismatched body", method: http.MethodPost, header: modernProtocolVersion, body: strings.Replace(modernBody, modernProtocolVersion, "2025-11-25", 1), wantStatus: http.StatusAccepted},
		{name: "malformed body", method: http.MethodPost, header: modernProtocolVersion, body: `{`, wantStatus: http.StatusAccepted},
		{name: "modern get stays stateful", method: http.MethodGet, header: modernProtocolVersion, body: modernBody, wantStatus: http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &protocolSwitchHandler{
				stateful:  statusHandler(http.StatusAccepted),
				stateless: statusHandler(http.StatusNoContent),
			}
			req := httptest.NewRequest(tt.method, "/mcp", strings.NewReader(tt.body))
			if tt.header != "" {
				req.Header.Set("Mcp-Protocol-Version", tt.header)
			}
			got := httptest.NewRecorder()
			handler.ServeHTTP(got, req)
			if got.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", got.Code, tt.wantStatus)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.body {
				t.Fatalf("downstream body = %q, want %q", body, tt.body)
			}
		})
	}
}

func TestProtocolSwitchHandler_ModernDiscoveryAndLegacySession(t *testing.T) {
	type echoInput struct {
		Value int `json:"value"`
	}
	type echoOutput struct {
		Echo  int   `json:"echo"`
		Calls int64 `json:"calls"`
	}
	var calls atomic.Int64
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "shared_echo", Description: "Echo through the shared registration"},
		func(_ context.Context, _ *mcp.CallToolRequest, input echoInput) (*mcp.CallToolResult, echoOutput, error) {
			return nil, echoOutput{Echo: input.Value, Calls: calls.Add(1)}, nil
		})
	httpServer := httptest.NewServer(newProtocolSwitchHandler(server))
	defer httpServer.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	type binding struct {
		modern           bool
		session, version string
	}
	modern := binding{modern: true, version: modernProtocolVersion}
	type envelope struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	request := func(b binding, method, id string, params map[string]any) (envelope, http.Header, error) {
		var result envelope
		copied := make(map[string]any, len(params)+1)
		for k, v := range params {
			copied[k] = v
		}
		if b.modern {
			copied["_meta"] = map[string]any{
				mcp.MetaKeyProtocolVersion:    modernProtocolVersion,
				mcp.MetaKeyClientInfo:         map[string]any{"name": "test", "version": "1"},
				mcp.MetaKeyClientCapabilities: map[string]any{},
			}
		}
		body := map[string]any{"jsonrpc": "2.0", "method": method, "params": copied}
		if id != "" {
			body["id"] = id
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			return result, nil, err
		}
		req, err := http.NewRequest(http.MethodPost, httpServer.URL, bytes.NewReader(encoded))
		if err != nil {
			return result, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if b.version != "" {
			req.Header.Set("Mcp-Protocol-Version", b.version)
		}
		if b.session != "" {
			req.Header.Set("Mcp-Session-Id", b.session)
		}
		if b.modern {
			req.Header.Set("Mcp-Method", method)
			if method == "tools/call" {
				req.Header.Set("Mcp-Name", "shared_echo")
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return result, nil, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return result, nil, err
		}
		expected := http.StatusOK
		if id == "" {
			expected = http.StatusAccepted
		}
		if resp.StatusCode != expected {
			return result, nil, fmt.Errorf("%s status=%d body=%s", method, resp.StatusCode, raw)
		}
		if b.modern && resp.Header.Get("Mcp-Session-Id") != "" {
			return result, nil, fmt.Errorf("modern %s acquired a session", method)
		}
		if id == "" {
			return result, resp.Header, nil
		}
		contentType := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/json") && !strings.HasPrefix(contentType, "text/event-stream") {
			return result, nil, fmt.Errorf("%s unexpected content type %q", method, contentType)
		}
		if strings.HasPrefix(contentType, "text/event-stream") {
			var data []string
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "data:") {
					data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				}
			}
			raw = []byte(strings.Join(data, "\n"))
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			return result, nil, err
		}
		if result.ID != id || len(result.Error) != 0 || len(result.Result) == 0 {
			return result, nil, fmt.Errorf("%s response=%s", method, raw)
		}
		return result, resp.Header, nil
	}
	discovery, _, err := request(modern, "server/discover", "modern-discover", nil)
	if err != nil {
		t.Fatal(err)
	}
	var advertised mcp.DiscoverResult
	if err := json.Unmarshal(discovery.Result, &advertised); err != nil {
		t.Fatal(err)
	}
	supported := false
	for _, version := range advertised.SupportedVersions {
		supported = supported || version == modernProtocolVersion
	}
	if !supported {
		t.Fatalf("modern version missing: %s", discovery.Result)
	}

	legacy := make([]binding, 2)
	for i := range legacy {
		initialized, headers, err := request(binding{}, "initialize", fmt.Sprintf("legacy-init-%d", i), map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "legacy", "version": "1"},
		})
		if err != nil {
			t.Fatal(err)
		}
		var result mcp.InitializeResult
		if err := json.Unmarshal(initialized.Result, &result); err != nil {
			t.Fatal(err)
		}
		legacy[i] = binding{session: headers.Get("Mcp-Session-Id"), version: result.ProtocolVersion}
		if legacy[i].session == "" || legacy[i].version != "2025-06-18" {
			t.Fatalf("invalid legacy initialize: %s", initialized.Result)
		}
		if _, _, err := request(legacy[i], "notifications/initialized", "", nil); err != nil {
			t.Fatal(err)
		}
		b := legacy[i]
		defer func() {
			req, err := http.NewRequest(http.MethodDelete, httpServer.URL, nil)
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Mcp-Session-Id", b.session)
			req.Header.Set("Mcp-Protocol-Version", b.version)
			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("session cleanup status=%d", resp.StatusCode)
			}
		}()
	}
	if legacy[0].session == legacy[1].session {
		t.Fatal("distinct legacy clients shared a session")
	}

	call := func(b binding, id string, value int) (int64, error) {
		response, _, err := request(b, "tools/call", id, map[string]any{"name": "shared_echo", "arguments": echoInput{Value: value}})
		if err != nil {
			return 0, err
		}
		var result struct {
			IsError bool       `json:"isError"`
			Output  echoOutput `json:"structuredContent"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil {
			return 0, err
		}
		if result.IsError || result.Output.Echo != value || result.Output.Calls <= 0 {
			return 0, fmt.Errorf("call output=%s", response.Result)
		}
		return result.Output.Calls, nil
	}
	seen := map[int64]bool{}
	for i, b := range []binding{modern, legacy[0], modern, legacy[1]} {
		listed, _, err := request(b, "tools/list", fmt.Sprintf("interleaved-list-%d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		var catalog mcp.ListToolsResult
		if err := json.Unmarshal(listed.Result, &catalog); err != nil {
			t.Fatal(err)
		}
		if len(catalog.Tools) != 1 || catalog.Tools[0].Name != "shared_echo" {
			t.Fatalf("shared registration missing: %s", listed.Result)
		}
		ordinal, err := call(b, fmt.Sprintf("interleaved-call-%d", i), i+10)
		if err != nil {
			t.Fatal(err)
		}
		if seen[ordinal] {
			t.Fatal("duplicate shared counter ordinal")
		}
		seen[ordinal] = true
	}
	type outcome struct {
		ordinal int64
		err     error
	}
	const concurrent = 16
	outcomes := make(chan outcome, concurrent)
	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := modern
			if i%2 != 0 {
				b = legacy[(i/2)%len(legacy)]
			}
			ordinal, err := call(b, fmt.Sprintf("concurrent-call-%d", i), i+100)
			outcomes <- outcome{ordinal: ordinal, err: err}
		}()
	}
	wg.Wait()
	close(outcomes)
	for result := range outcomes {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if seen[result.ordinal] {
			t.Errorf("duplicate shared counter ordinal %d", result.ordinal)
		}
		seen[result.ordinal] = true
	}
	if got := calls.Load(); got != concurrent+4 {
		t.Errorf("shared operation calls=%d, want %d", got, concurrent+4)
	}
	for ordinal := int64(1); ordinal <= concurrent+4; ordinal++ {
		if !seen[ordinal] {
			t.Errorf("missing shared operation ordinal %d", ordinal)
		}
	}
}
