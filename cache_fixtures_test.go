package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCacheFixtures(t *testing.T) {
	f := newCacheFixtures()
	for _, method := range []string{"tools/list", "prompts/list", "resources/list", "resources/templates/list"} {
		for _, transport := range []string{"json", "sse"} {
			for _, hints := range []string{"public", "missing"} {
				cursor := ""
				pages := 0
				for {
					rec := httptest.NewRecorder()
					body := fmt.Sprintf(`{"jsonrpc":"2.0","id":9007199254740993,"method":%q,"params":{"cursor":%q}}`, method, cursor)
					f.ServeHTTP(rec, httptest.NewRequest("POST", "/fixtures/mcp?transport="+transport+"&hints="+hints, strings.NewReader(body)))
					if rec.Code != 200 {
						t.Fatal(rec.Body.String())
					}
					payload := rec.Body.String()
					if transport == "sse" {
						if rec.Header().Get("Content-Type") != "text/event-stream" {
							t.Fatal("not SSE")
						}
						_, payload, _ = strings.Cut(payload, "data: ")
					}
					var response struct {
						ID     json.RawMessage
						Result map[string]json.RawMessage
					}
					if err := json.Unmarshal([]byte(payload), &response); err != nil {
						t.Fatal(err)
					}
					if string(response.ID) != "9007199254740993" {
						t.Fatal("rounded id")
					}
					if hints == "missing" && response.Result["cacheScope"] != nil {
						t.Fatal("unexpected hints")
					}
					pages++
					if response.Result["nextCursor"] == nil {
						break
					}
					if err := json.Unmarshal(response.Result["nextCursor"], &cursor); err != nil {
						t.Fatal(err)
					}
					if pages > 4 {
						t.Fatal("cursor cycle")
					}
				}
				if pages != 4 {
					t.Fatalf("got %d pages", pages)
				}
			}
		}
	}
}

func TestCacheFixtureCountersConcurrent(t *testing.T) {
	f := newCacheFixtures()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/fixtures/mcp", strings.NewReader(`{"id":1,"method":"tools/list"}`)))
		}()
	}
	wg.Wait()
	rec := httptest.NewRecorder()
	f.counters(rec, httptest.NewRequest("GET", "/fixtures/counters", nil))
	if rec.Body.String() != "{\"tools/list\":20}\n" {
		t.Fatal(rec.Body.String())
	}
}
