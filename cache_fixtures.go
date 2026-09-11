package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// cacheFixtures is deliberately independent of SDK pagination and transport
// choices: integration tests must traverse the same pages over JSON and SSE.
type cacheFixtures struct {
	mu     sync.Mutex
	counts map[string]int
}

func newCacheFixtures() *cacheFixtures { return &cacheFixtures{counts: make(map[string]int)} }

func (f *cacheFixtures) counters(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodDelete {
		f.counts = make(map[string]int)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(f.counts)
}

func (f *cacheFixtures) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			Cursor string `json:"cursor"`
		} `json:"params"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	f.mu.Lock()
	f.counts[request.Method]++
	f.mu.Unlock()
	if len(request.ID) == 0 {
		w.WriteHeader(202)
		return
	}
	result := map[string]any{"extension": map[string]any{"kept": true}}
	switch request.Method {
	case "initialize":
		result["protocolVersion"] = "2025-03-26"
		result["capabilities"] = map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "prompts": map[string]any{}}
		result["serverInfo"] = map[string]any{"name": "deterministic-fixtures", "version": "1.0.0"}
	case "server/discover":
		result["resultType"] = "complete"
		result["supportedVersions"] = []string{"2099-01-01", "2025-03-26", "2026-07-28", "2025-11-25", "2025-06-18"}
		result["capabilities"] = map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}, "resources": map[string]any{}, "extensionCapability": map[string]any{"kept": true}}
		result["serverInfo"] = map[string]any{"name": "deterministic-fixtures", "version": "2.0.0"}
		result["instructions"] = "Keep this upstream instruction unchanged."
		result["_meta"] = map[string]any{"extension": true}
		result["cacheScope"] = "public"
		result["ttlMs"] = 60000
	case "tools/list", "prompts/list", "resources/list", "resources/templates/list":
		key, field := "tools", "name"
		switch request.Method {
		case "prompts/list":
			key = "prompts"
		case "resources/list":
			key, field = "resources", "uri"
		case "resources/templates/list":
			key, field = "resourceTemplates", "uriTemplate"
		}
		names := []string{"visible"}
		switch request.Params.Cursor {
		case "":
			result["nextCursor"] = "mixed-page"
		case "mixed-page":
			names = []string{"visible2", "hidden", "visible3"}
			result["nextCursor"] = "empty-page"
		case "empty-page":
			names = []string{}
			result["nextCursor"] = "last-page"
		case "last-page":
			names = []string{"hidden"}
		default:
			http.Error(w, "unknown fixture cursor", 400)
			return
		}
		items := make([]map[string]any, 0, len(names))
		for _, name := range names {
			item := map[string]any{field: name, "name": name, "description": "deterministic fixture", "extension": true}
			if key == "tools" {
				item["inputSchema"] = map[string]any{"type": "object"}
			}
			items = append(items, item)
		}
		result[key] = items
		if r.URL.Query().Get("hints") != "missing" {
			result["cacheScope"] = "public"
			result["ttlMs"] = 60000
		}
	default:
		result["ok"] = true
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result, "fixtureExtension": true})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if r.URL.Query().Get("transport") == "sse" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "id: fixture-event\nevent: message\ndata: %s\n\n", body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}
