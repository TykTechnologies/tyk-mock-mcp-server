package testserver_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/TykTechnologies/tyk-mock-mcp-server/testserver"
)

// Example test showing how to use the MCP test server
// This pattern mirrors how Tyk's gateway tests work
func TestMCPServerBasic(t *testing.T) {
	// Start the mock MCP server (similar to Tyk's StartTest)
	// Port 0 means use a random available port
	mcpServer, cleanup := testserver.StartServer(0)
	defer cleanup()

	t.Logf("MCP Server started at: %s", mcpServer.URL)

	// Test 1: Health check
	resp, err := http.Get(mcpServer.URL[:len(mcpServer.URL)-4] + "/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Test 2: Verify store is accessible for assertions
	users := mcpServer.Store.GetAllUsers()
	if len(users) == 0 {
		t.Error("Expected mock users in store, got empty list")
	}
	t.Logf("Store contains %d users", len(users))
}

// Example showing how this would be used in a Tyk JSON-RPC test
// This is a CONCEPTUAL example - actual implementation depends on Tyk's JSON-RPC support
func TestTykJSONRPCWithMCPServer(t *testing.T) {
	// Start mock MCP server
	mcpServer, cleanup := testserver.StartServer(0)
	defer cleanup()

	// In actual Tyk tests, you would do something like:
	//
	// ts := gateway.StartTest()
	// defer ts.Close()
	//
	// ts.Gw.BuildAndLoadAPI(func(spec *gateway.APISpec) {
	//     spec.Proxy.ListenPath = "/json-rpc/"
	//     spec.JSONRPC.TargetURL = mcpServer.URL  // Point to our mock MCP server
	//     spec.JSONRPC.EnableRateLimiting = true
	//     spec.JSONRPC.MethodACL = map[string]string{
	//         "get_users": "allow",
	//         "create_user": "allow",
	//     }
	// })
	//
	// // Create session key for auth
	// keyID := gateway.CreateSession(ts.Gw, func(s *user.SessionState) {
	//     // Configure session
	// })
	//
	// // Test JSON-RPC request proxying to MCP server
	// jsonRPCRequest := `{"jsonrpc":"2.0","method":"get_users","params":{},"id":1}`
	//
	// ts.Run(t, test.TestCase{
	//     Path:      "/json-rpc/",
	//     Method:    http.MethodPost,
	//     Headers:   map[string]string{"Authorization": keyID, "Content-Type": "application/json"},
	//     Data:      jsonRPCRequest,
	//     Code:      http.StatusOK,
	//     BodyMatch: `"result"`,  // Verify response contains result
	// })
	//
	// // Assert on the mock server's store
	// users := mcpServer.Store.GetAllUsers()
	// t.Logf("After test, store has %d users", len(users))

	// For now, just demonstrate the URL is accessible
	t.Logf("MCP server URL for Tyk configuration: %s", mcpServer.URL)
	t.Logf("In your Tyk test, set spec.JSONRPC.TargetURL = %q", mcpServer.URL)
}

// Example showing server with specific port (useful for debugging)
func TestMCPServerWithFixedPort(t *testing.T) {
	// Start server on port 16502 (similar to Tyk's TestHttpAny constant at 16500)
	mcpServer, cleanup := testserver.StartServer(16502)
	defer cleanup()

	if mcpServer.Port != 16502 {
		t.Errorf("Expected port 16502, got %d", mcpServer.Port)
	}

	t.Logf("Server listening on fixed port: %d", mcpServer.Port)
}

// Example showing store manipulation during tests
func TestMCPServerStoreAssertion(t *testing.T) {
	mcpServer, cleanup := testserver.StartServer(0)
	defer cleanup()

	// Initial state
	initialUsers := mcpServer.Store.GetAllUsers()
	t.Logf("Initial users: %d", len(initialUsers))

	// Manipulate store (simulating what would happen via MCP tool calls)
	mcpServer.Store.CreateUser("Test User", "test@example.com", "user")

	// Assert on new state
	updatedUsers := mcpServer.Store.GetAllUsers()
	if len(updatedUsers) != len(initialUsers)+1 {
		t.Errorf("Expected %d users after create, got %d",
			len(initialUsers)+1, len(updatedUsers))
	}

	// In real Tyk tests, you would:
	// 1. Make a request through Tyk gateway
	// 2. Have Tyk proxy it to the MCP server
	// 3. Assert on the store to verify the request was processed correctly
}

// Helper function showing how to verify JSON-RPC responses
func TestJSONRPCResponseParsing(t *testing.T) {
	// This shows how to parse MCP/JSON-RPC responses in tests
	sampleResponse := `{"jsonrpc":"2.0","result":{"users":[{"id":"1","name":"Test"}]},"id":1}`

	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		ID      int             `json:"id"`
	}

	if err := json.Unmarshal([]byte(sampleResponse), &response); err != nil {
		t.Fatalf("Failed to parse JSON-RPC response: %v", err)
	}

	if response.JSONRPC != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %s", response.JSONRPC)
	}

	t.Logf("Parsed JSON-RPC response successfully: %+v", response)
}
