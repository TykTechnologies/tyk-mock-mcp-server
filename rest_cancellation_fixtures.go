package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const restCancellationPrefix = "/fixtures/rest/cancellation"

type cancellationOperation struct {
	ID          string    `json:"id"`
	Started     bool      `json:"started"`
	Cancelled   bool      `json:"cancelled"`
	Completed   bool      `json:"completed"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	release     chan struct{}
	releaseOnce sync.Once
}

type restCancellationFixtures struct {
	mu         sync.Mutex
	operations map[string]*cancellationOperation
}

func newRESTCancellationFixtures() *restCancellationFixtures {
	return &restCancellationFixtures{operations: make(map[string]*cancellationOperation)}
}

func (f *restCancellationFixtures) register(mux *http.ServeMux) {
	mux.HandleFunc(restCancellationPrefix+"/wait/", f.wait)
	mux.HandleFunc(restCancellationPrefix+"/release/", f.release)
	mux.HandleFunc(restCancellationPrefix+"/operations/", f.operation)
	mux.HandleFunc(restCancellationPrefix+"/reset", f.reset)
}

func (f *restCancellationFixtures) wait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, restCancellationPrefix+"/wait/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "operation id is required", http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	if _, exists := f.operations[id]; exists {
		f.mu.Unlock()
		http.Error(w, "operation already exists", http.StatusConflict)
		return
	}
	op := &cancellationOperation{ID: id, Started: true, StartedAt: time.Now().UTC(), release: make(chan struct{})}
	f.operations[id] = op
	f.mu.Unlock()

	select {
	case <-r.Context().Done():
		f.finish(id, true)
		return
	case <-op.release:
		f.finish(id, false)
		writeCancellationJSON(w, http.StatusOK, map[string]any{"id": id, "completed": true})
	}
}

func (f *restCancellationFixtures) release(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, restCancellationPrefix+"/release/")
	f.mu.Lock()
	op := f.operations[id]
	f.mu.Unlock()
	if op == nil {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	op.releaseOnce.Do(func() { close(op.release) })
	w.WriteHeader(http.StatusNoContent)
}

func (f *restCancellationFixtures) operation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, restCancellationPrefix+"/operations/")
	f.mu.Lock()
	op := f.operations[id]
	if op == nil {
		f.mu.Unlock()
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	snapshot := map[string]any{
		"id": op.ID, "started": op.Started, "cancelled": op.Cancelled,
		"completed": op.Completed, "started_at": op.StartedAt, "finished_at": op.FinishedAt,
	}
	f.mu.Unlock()
	writeCancellationJSON(w, http.StatusOK, snapshot)
}

func (f *restCancellationFixtures) reset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	for _, op := range f.operations {
		op.releaseOnce.Do(func() { close(op.release) })
	}
	f.operations = make(map[string]*cancellationOperation)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *restCancellationFixtures) finish(id string, cancelled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if op := f.operations[id]; op != nil {
		op.Cancelled = cancelled
		op.Completed = !cancelled
		op.FinishedAt = time.Now().UTC()
	}
}

func writeCancellationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
