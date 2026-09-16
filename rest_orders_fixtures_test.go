package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRESTOrdersFixturesCaptureHeadersAndErrors(t *testing.T) {
	fixture := newRESTOrdersFixtures()
	mux := http.NewServeMux()
	fixture.register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodGet, server.URL+restOrdersPrefix+"/order-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Trace-ID", "trace-1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("success status = %d", response.StatusCode)
	}
	_ = response.Body.Close()

	for id, status := range map[string]int{"missing": http.StatusNotFound, "unavailable": http.StatusServiceUnavailable} {
		response, err = http.Get(server.URL + restOrdersPrefix + "/" + id)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("%s status = %d, want %d", id, response.StatusCode, status)
		}
	}

	response, err = http.Get(server.URL + restOrdersPrefix + "/records")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Records []restOrderRecord `json:"records"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(body.Records) != 3 || body.Records[0].Headers.Get("X-Trace-ID") != "trace-1" {
		t.Fatalf("unexpected records: %#v", body.Records)
	}

	request, _ = http.NewRequest(http.MethodDelete, server.URL+restOrdersPrefix+"/reset", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("reset status = %d", response.StatusCode)
	}
}
