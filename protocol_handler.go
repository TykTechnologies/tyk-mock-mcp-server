package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const modernProtocolVersion = "2026-07-28"

// protocolSwitchHandler keeps one stateful and one stateless SDK transport.
// Both transports resolve the same server, so tool registrations and state are
// shared across protocol eras without allocating a handler per request.
type protocolSwitchHandler struct {
	stateful  http.Handler
	stateless http.Handler
}

func newProtocolSwitchHandler(server *mcp.Server) *protocolSwitchHandler {
	serverForRequest := func(*http.Request) *mcp.Server { return server }

	// MCP_JSON_RESPONSE makes the SDK answer POST /mcp with application/json
	// instead of text/event-stream. This matters for recording: when the
	// upstream streams SSE, Tyk Pump records the request but not the response,
	// so the analytics record lands in Elasticsearch/OpenSearch with an empty
	// raw_response. Answering in JSON keeps the body observable and the full
	// MCP exchange is captured. It is off by default so streaming semantics are
	// unchanged for clients that expect SSE; when it is unset an empty
	// (zero-value) option set is equivalent to passing nil.
	jsonResponse := os.Getenv("MCP_JSON_RESPONSE") == "true" || os.Getenv("MCP_JSON_RESPONSE") == "1"

	return &protocolSwitchHandler{
		stateful:  mcp.NewStreamableHTTPHandler(serverForRequest, &mcp.StreamableHTTPOptions{JSONResponse: jsonResponse}),
		stateless: mcp.NewStreamableHTTPHandler(serverForRequest, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse}),
	}
}

func (h *protocolSwitchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if requestDeclaresModernProtocol(r) {
		h.stateless.ServeHTTP(w, r)
		return
	}
	h.stateful.ServeHTTP(w, r)
}

// requestDeclaresModernProtocol selects stateless routing only when the HTTP
// and per-request protocol declarations agree. The SDK remains responsible for
// validating all other modern metadata and mirrored MCP headers.
func requestDeclaresModernProtocol(r *http.Request) bool {
	if r.Method != http.MethodPost || r.Header.Get("Mcp-Protocol-Version") != modernProtocolVersion || r.Body == nil {
		return false
	}

	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return false
	}

	var envelope struct {
		Params struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}

	var bodyVersion string
	if err := json.Unmarshal(envelope.Params.Meta[mcp.MetaKeyProtocolVersion], &bodyVersion); err != nil {
		return false
	}
	return bodyVersion == modernProtocolVersion
}
