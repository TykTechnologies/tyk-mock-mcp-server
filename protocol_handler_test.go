package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	httpServer := httptest.NewServer(newProtocolSwitchHandler(server))
	defer httpServer.Close()

	t.Run("modern discovery is stateless", func(t *testing.T) {
		body := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "server/discover",
			"params": map[string]any{
				"_meta": map[string]any{
					mcp.MetaKeyProtocolVersion:    modernProtocolVersion,
					mcp.MetaKeyClientInfo:         map[string]any{"name": "test", "version": "1"},
					mcp.MetaKeyClientCapabilities: map[string]any{},
				},
			},
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, httpServer.URL, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Protocol-Version", modernProtocolVersion)
		req.Header.Set("Mcp-Method", "server/discover")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		responseBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body = %s", resp.StatusCode, responseBody)
		}
		if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
			t.Fatalf("stateless response set session ID %q", sessionID)
		}
		if !bytes.Contains(responseBody, []byte(modernProtocolVersion)) {
			t.Fatalf("discovery response does not advertise %s: %s", modernProtocolVersion, responseBody)
		}
	})

	t.Run("legacy initialize remains stateful", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"legacy","version":"1"}}}`
		req, err := http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		responseBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body = %s", resp.StatusCode, responseBody)
		}
		if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID == "" {
			t.Fatalf("legacy initialize did not establish a session: %s", responseBody)
		}
	})
}
