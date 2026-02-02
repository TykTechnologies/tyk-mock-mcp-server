package testserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerInstance represents a running mock MCP server
type ServerInstance struct {
	// URL is the full URL to the MCP endpoint (e.g., "http://localhost:45123/mcp")
	URL string

	// Port is the port the server is listening on
	Port int

	// Store is the data store (exposed for test assertions)
	Store *store.Store

	// Server is the MCP server instance
	Server *mcp.Server

	httpServer *http.Server
	listener   net.Listener
}

// StartServer starts a mock MCP server on the specified port (0 for random port)
// Returns a ServerInstance and a cleanup function
// This is designed to be used in tests similar to Tyk's StartTest() pattern
func StartServer(port int) (*ServerInstance, func()) {
	return StartServerWithDebug(port, false)
}

// StartServerWithDebug starts a mock MCP server with optional debug logging
func StartServerWithDebug(port int, debug bool) (*ServerInstance, func()) {
	instance := &ServerInstance{}

	// Initialize store with mock data
	instance.Store = store.NewStore()

	// Create MCP server
	instance.Server = setupMCPServer(instance.Store)

	// Start HTTP server
	if err := instance.startHTTP(port, debug); err != nil {
		log.Fatalf("Failed to start MCP server: %v", err)
	}

	cleanup := func() {
		instance.Stop()
	}

	return instance, cleanup
}

// Stop gracefully stops the server
func (si *ServerInstance) Stop() error {
	if si.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := si.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	if si.listener != nil {
		si.listener.Close()
	}

	return nil
}

func (si *ServerInstance) startHTTP(port int, debug bool) error {
	// Create listener with specified port (0 for random)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	si.listener = listener
	si.Port = listener.Addr().(*net.TCPAddr).Port
	si.URL = fmt.Sprintf("http://127.0.0.1:%d/mcp", si.Port)

	mux := http.NewServeMux()

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if debug {
			log.Printf("🔧 MCP Handler: Returning server instance for request")
		}
		return si.Server
	}, nil)

	// Add CORS middleware
	wrappedHandler := corsMiddleware(handler)

	mux.Handle("/mcp", wrappedHandler)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","version":"1.0.0"}`)
	})

	si.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		if debug {
			log.Printf("Starting MCP server on port %d", si.Port)
			log.Printf("MCP endpoint: %s", si.URL)
		}

		if err := si.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			if debug {
				log.Printf("HTTP server error: %v", err)
			}
		}
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
