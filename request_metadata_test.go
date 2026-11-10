package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerTransport struct {
	name, value string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.name, h.value)
	return http.DefaultTransport.RoundTrip(r)
}

// Concurrent clients must each see their own request headers in get_anything, not the
// headers of whichever request the shared capture middleware saw last.
func TestGetAnything_ConcurrentClientsSeeOwnHeaders(t *testing.T) {
	httpServer := httptest.NewServer(captureRequestMiddleware(newProtocolSwitchHandler(setupServer())))
	defer httpServer.Close()

	const clients, calls = 4, 25
	var wg sync.WaitGroup
	errs := make(chan error, clients*calls)
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(marker string) {
			defer wg.Done()
			ctx := context.Background()
			transport := &mcp.StreamableClientTransport{
				Endpoint:             httpServer.URL,
				HTTPClient:           &http.Client{Transport: headerTransport{"X-Client-Marker", marker}},
				DisableStandaloneSSE: true,
			}
			session, err := mcp.NewClient(&mcp.Implementation{Name: marker, Version: "1"}, nil).Connect(ctx, transport, nil)
			if err != nil {
				errs <- err
				return
			}
			defer session.Close()
			for j := 0; j < calls; j++ {
				res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_anything", Arguments: map[string]any{}})
				if err != nil {
					errs <- err
					return
				}
				raw, _ := json.Marshal(res.StructuredContent)
				var out struct {
					Headers map[string]string `json:"headers"`
				}
				_ = json.Unmarshal(raw, &out)
				if got := out.Headers["X-Client-Marker"]; got != marker {
					errs <- fmt.Errorf("client %s saw X-Client-Marker=%q", marker, got)
				}
			}
		}(fmt.Sprintf("client-%d", i))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
