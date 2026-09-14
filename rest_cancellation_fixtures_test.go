package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRESTCancellationFixturesObserveCancellationAndRelease(t *testing.T) {
	fixture := newRESTCancellationFixtures()
	mux := http.NewServeMux()
	fixture.register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancelDone := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+restCancellationPrefix+"/wait/cancel-me", nil)
		if err != nil {
			cancelDone <- err
			return
		}
		response, err := http.DefaultClient.Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		cancelDone <- err
	}()
	waitCancellationState(t, server.URL, "cancel-me", func(state map[string]any) bool { return state["started"] == true })
	cancel()
	select {
	case <-cancelDone:
	case <-time.After(time.Second):
		t.Fatal("cancelled fixture request did not return")
	}
	waitCancellationState(t, server.URL, "cancel-me", func(state map[string]any) bool { return state["cancelled"] == true })

	successDone := make(chan error, 1)
	go func() {
		response, err := http.Get(server.URL + restCancellationPrefix + "/wait/complete-me")
		if response != nil {
			_ = response.Body.Close()
		}
		successDone <- err
	}()
	waitCancellationState(t, server.URL, "complete-me", func(state map[string]any) bool { return state["started"] == true })
	response, err := http.Post(server.URL+restCancellationPrefix+"/release/complete-me", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("release status = %d", response.StatusCode)
	}
	select {
	case err := <-successDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("released fixture request did not return")
	}
	waitCancellationState(t, server.URL, "complete-me", func(state map[string]any) bool { return state["completed"] == true })
}

func waitCancellationState(t *testing.T, baseURL, id string, ready func(map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + restCancellationPrefix + "/operations/" + id)
		if err == nil {
			var state map[string]any
			decodeErr := json.NewDecoder(response.Body).Decode(&state)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && ready(state) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("operation %q did not reach expected state", id)
}
