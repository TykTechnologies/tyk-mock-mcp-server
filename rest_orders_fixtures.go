package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

const restOrdersPrefix = "/fixtures/rest/orders"

type restOrderRecord struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Headers http.Header `json:"headers"`
}

type restOrdersFixtures struct {
	mu      sync.Mutex
	records []restOrderRecord
}

func newRESTOrdersFixtures() *restOrdersFixtures { return &restOrdersFixtures{} }

func (f *restOrdersFixtures) register(mux *http.ServeMux) {
	mux.HandleFunc(restOrdersPrefix+"/records", f.serveRecords)
	mux.HandleFunc(restOrdersPrefix+"/reset", f.reset)
	mux.HandleFunc(restOrdersPrefix+"/", f.getOrder)
}

func (f *restOrdersFixtures) getOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, restOrdersPrefix+"/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "order id is required", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.records = append(f.records, restOrderRecord{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone()})
	f.mu.Unlock()

	switch id {
	case "missing":
		writeRESTFixtureJSON(w, http.StatusNotFound, map[string]any{"error": "Order not found"})
	case "unavailable":
		writeRESTFixtureJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Service temporarily unavailable"})
	default:
		writeRESTFixtureJSON(w, http.StatusOK, map[string]any{"id": id, "headers": r.Header})
	}
}

func (f *restOrdersFixtures) serveRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	records := append([]restOrderRecord(nil), f.records...)
	f.mu.Unlock()
	writeRESTFixtureJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (f *restOrdersFixtures) reset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	f.records = nil
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func writeRESTFixtureJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
