# Testserver Package - Using Mock MCP Server in Tests

This package allows you to import and use the mock MCP server programmatically in your integration tests, similar to how Tyk's `gateway/testutil.go` provides test utilities.

## Quick Start

### 1. Import in your Tyk tests

```go
import "github.com/TykTechnologies/tyk-mock-mcp-server/testserver"
```

### 2. Start the server in your test

```go
func TestMyJSONRPCFeature(t *testing.T) {
    // Start MCP server on random port
    mcpServer, cleanup := testserver.StartServer(0)
    defer cleanup()

    // mcpServer.URL contains "http://127.0.0.1:XXXXX/mcp"
    // mcpServer.Port contains the port number
    // mcpServer.Store gives access to the data for assertions
}
```

### 3. Use in Tyk API configuration

Following the pattern described in `coprocess/python/coprocess_python_test.go`:

```go
func TestJSONRPCProxy(t *testing.T) {
    // Start mock MCP server
    mcpServer, cleanup := testserver.StartServer(0)
    defer cleanup()

    // Start Tyk test gateway (following Tyk's test pattern)
    ts := gateway.StartTest()
    defer ts.Close()

    // Define API that proxies to the MCP server
    ts.Gw.BuildAndLoadAPI(func(spec *gateway.APISpec) {
        spec.Proxy.ListenPath = "/mcp-proxy/"
        spec.JSONRPC.TargetURL = mcpServer.URL  // Point to mock MCP server
        spec.JSONRPC.EnableRateLimiting = true
        spec.JSONRPC.MethodACL = map[string]string{
            "get_users": "allow",
            "create_user": "allow",
        }
    })

    // Create session key for authentication
    keyID := gateway.CreateSession(ts.Gw, func(s *user.SessionState) {
        // Configure session as needed
    })

    // Execute test request
    jsonRPCRequest := `{"jsonrpc":"2.0","method":"get_users","params":{},"id":1}`

    ts.Run(t, test.TestCase{
        Path:      "/mcp-proxy/",
        Method:    http.MethodPost,
        Headers:   map[string]string{
            "Authorization": keyID,
            "Content-Type": "application/json",
        },
        Data:      jsonRPCRequest,
        Code:      http.StatusOK,
        BodyMatch: `"result"`,  // Verify JSON-RPC response
    })

    // Assert on the mock server's state
    users := mcpServer.Store.GetAllUsers()
    t.Logf("After test, MCP server store has %d users", len(users))
}
```

## API Reference

### `StartServer(port int) (*ServerInstance, func())`

Starts a mock MCP server on the specified port.

**Parameters:**
- `port int` - Port to listen on (0 for random available port)

**Returns:**
- `*ServerInstance` - The server instance with URL, Port, Store, etc.
- `func()` - Cleanup function to stop the server (use with `defer`)

**Example:**
```go
mcpServer, cleanup := testserver.StartServer(0)  // Random port
defer cleanup()
```

### `StartServerWithDebug(port int, debug bool) (*ServerInstance, func())`

Same as `StartServer` but with optional debug logging.

**Example:**
```go
mcpServer, cleanup := testserver.StartServerWithDebug(16502, true)
defer cleanup()
```

### ServerInstance

The `ServerInstance` type provides access to:

```go
type ServerInstance struct {
    // URL is the full MCP endpoint (e.g., "http://127.0.0.1:45123/mcp")
    URL string

    // Port is the listening port
    Port int

    // Store provides access to data for assertions
    Store *store.Store

    // Server is the underlying MCP server instance
    Server *mcp.Server
}
```

### Accessing the Store for Assertions

The `Store` field allows you to inspect the server's state:

```go
// Get all users
users := mcpServer.Store.GetAllUsers()

// Get user count
count := mcpServer.Store.GetUserCount()

// Get all products
products := mcpServer.Store.GetAllProducts()

// Manipulate data for test setup
mcpServer.Store.CreateUser("Test User", "test@example.com", "admin")
```

## Usage Patterns

### Pattern 1: Random Port (Recommended)

Use port `0` to automatically assign an available port. This prevents port conflicts when running tests in parallel.

```go
mcpServer, cleanup := testserver.StartServer(0)
defer cleanup()
```

### Pattern 2: Fixed Port (For Debugging)

Use a specific port for easier debugging with tools like curl or Postman.

```go
// Similar to Tyk's TestHttpAny at 127.0.0.1:16500
mcpServer, cleanup := testserver.StartServer(16502)
defer cleanup()

// Now you can manually test: curl http://127.0.0.1:16502/health
```

### Pattern 3: Server State Assertions

Test that Tyk correctly forwarded requests to the MCP server:

```go
// Before the request
initialUserCount := mcpServer.Store.GetUserCount()

// Make request through Tyk that creates a user via MCP
ts.Run(t, test.TestCase{
    Path: "/mcp-proxy/",
    Data: `{"jsonrpc":"2.0","method":"create_user","params":{"name":"Alice","email":"alice@example.com"},"id":1}`,
    // ...
})

// Verify the MCP server processed it
newUserCount := mcpServer.Store.GetUserCount()
if newUserCount != initialUserCount + 1 {
    t.Errorf("Expected user to be created in MCP server")
}
```

## Available MCP Tools

The mock server provides 14 tools you can test against:

### User Management
- `get_users` - List users with filters
- `create_user` - Create new user
- `update_user` - Update user
- `delete_user` - Delete user

### Content Management
- `get_posts` - List blog posts
- `create_post` - Create blog post

### E-commerce
- `get_products` - Browse products
- `process_order` - Process orders

### Analytics
- `get_analytics` - Get analytics data
- `generate_report` - Generate reports

### Utilities
- `validate_email` - Email validation
- `generate_uuid` - UUID generation
- `format_date` - Date formatting
- `get_anything` - Echo request (like httpbin.org/anything)

## Testing Different Scenarios

### Test Rate Limiting

```go
// Configure API with rate limit
ts.Gw.BuildAndLoadAPI(func(spec *gateway.APISpec) {
    spec.JSONRPC.TargetURL = mcpServer.URL
    spec.JSONRPC.RateLimit = 5  // 5 requests per minute
})

// Send 6 requests, expect the 6th to be rate limited
for i := 0; i < 6; i++ {
    expectedCode := http.StatusOK
    if i == 5 {
        expectedCode = http.StatusTooManyRequests
    }

    ts.Run(t, test.TestCase{
        Path: "/mcp-proxy/",
        Data: `{"jsonrpc":"2.0","method":"get_users","id":1}`,
        Code: expectedCode,
    })
}

// Verify MCP server only received 5 requests, not 6
// (You'd need to add request counting to ServerInstance for this)
```

### Test Method ACL

```go
// Configure API with method ACL
ts.Gw.BuildAndLoadAPI(func(spec *gateway.APISpec) {
    spec.JSONRPC.TargetURL = mcpServer.URL
    spec.JSONRPC.MethodACL = map[string]string{
        "get_users": "allow",
        "delete_user": "deny",
    }
})

// Allowed method should work
ts.Run(t, test.TestCase{
    Path: "/mcp-proxy/",
    Data: `{"jsonrpc":"2.0","method":"get_users","id":1}`,
    Code: http.StatusOK,
})

// Denied method should be blocked
ts.Run(t, test.TestCase{
    Path: "/mcp-proxy/",
    Data: `{"jsonrpc":"2.0","method":"delete_user","params":{"id":"user-1"},"id":2}`,
    Code: http.StatusForbidden,
})

// Verify delete never reached the MCP server
users := mcpServer.Store.GetAllUsers()
// Assert user still exists
```

## Health Check

The server includes a health endpoint at `/health`:

```go
resp, err := http.Get(mcpServer.URL[:len(mcpServer.URL)-4] + "/health")
// Returns: {"status":"healthy","version":"1.0.0"}
```

## Comparison with Tyk's Test Pattern

This package mirrors Tyk's testing approach:

| Tyk Pattern | MCP Test Pattern |
|-------------|------------------|
| `gateway.StartTest()` | `testserver.StartServer(0)` |
| `gateway.TestHttpAny` (127.0.0.1:16500) | `mcpServer.URL` |
| `ts.Gw.BuildAndLoadAPI()` | Same (configure target to MCP) |
| `gateway.CreateSession()` | Same |
| `ts.Run(t, test.TestCase{})` | Same |
| Mock upstream in `testHttpHandler` | This MCP server |

## Running the Examples

```bash
cd testserver
go test -v
```

This will run the example tests in `example_test.go`.

## Tips

1. **Always use `defer cleanup()`** to ensure the server is stopped after tests
2. **Use port 0** for parallel test execution
3. **Access `mcpServer.Store`** for state verification
4. **Check `mcpServer.URL`** to get the full endpoint URL
5. **Use debug mode** when troubleshooting: `StartServerWithDebug(port, true)`
