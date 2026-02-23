# Tyk Mock MCP Server

A comprehensive mock MCP (Model Context Protocol) server built with Go, implementing the November 2025 MCP specification. This server provides a rich set of tools, prompts, and resources for testing and development purposes.

## Features

### 15 Tools Across 6 Categories

#### 👥 User Management
- `get_users` - Retrieve mock users with filtering options (role, active status)
- `create_user` - Create new mock users
- `update_user` - Update existing user data
- `delete_user` - Delete users by ID

#### 📝 Content Management
- `get_posts` - Retrieve blog posts with filters (author, status)
- `create_post` - Create new blog posts

#### 🛒 E-commerce
- `get_products` - Browse product catalog with filters (category, price range)
- `process_order` - Process mock orders with inventory management

#### 📊 Analytics & Reports
- `get_analytics` - Generate analytics data (users, posts, orders, revenue)
- `generate_report` - Create business reports (sales, users, inventory)

#### 🔧 Utilities
- `validate_email` - Email validation using regex
- `generate_uuid` - UUID v4 generation
- `format_date` - Date formatting (iso, rfc3339, unix, custom)
- `get_anything` - Echo back request data with **real HTTP metadata** (client IP, headers) in httpbin.org/anything format

#### Testing
- `slow_response` - Respond after a configurable delay (for gateway timeout testing)

### SSE Test Endpoints (non-MCP)
- `GET /sse/stream` - Configurable SSE event stream (event count, pacing, charset)
- `GET /sse/crash` - SSE stream that crashes after N events (upstream failure simulation)

### 4 Contextual Prompts
- `user_management` - User operations guidance
- `ecommerce_assistant` - E-commerce operations help
- `content_management` - Blog content management
- `analytics_dashboard` - Business insights and reporting

### 3 Data Resources
- `users://list` - Complete users list (JSON)
- `products://catalog` - Product catalog (JSON)
- `posts://list` - Blog posts collection (JSON)

## Architecture

The server follows clean architecture principles with clear separation of concerns:

```
tyk-mock-mcp-server/
├── main.go                    # Server initialization and HTTP setup
├── models/                    # Data models and DTOs
│   ├── user.go
│   ├── post.go
│   ├── product.go
│   ├── order.go
│   ├── analytics.go
│   ├── utilities.go
│   └── common.go
├── store/                     # Data storage layer
│   └── store.go
└── handlers/                  # Business logic handlers
    ├── tools/                 # Tool implementations
    │   ├── users.go
    │   ├── posts.go
    │   ├── ecommerce.go
    │   ├── analytics.go
    │   └── utilities.go
    ├── prompts/              # Prompt handlers
    │   └── prompts.go
    └── resources/            # Resource handlers
        └── resources.go
```

## Requirements

- Go 1.22 or higher
- Docker (optional, for containerized deployment)
- [Task](https://taskfile.dev/) (optional, for using Taskfile commands)

## Installation

### As a Library (For Integration Tests)

You can import this server in your integration tests (e.g., Tyk Gateway tests) to test JSON-RPC/MCP features:

```go
import "github.com/TykTechnologies/tyk-mock-mcp-server/testserver"

func TestMyMCPFeature(t *testing.T) {
    // Start mock MCP server on random port
    mcpServer, cleanup := testserver.StartServer(0)
    defer cleanup()

    // Use mcpServer.URL in your API configuration
    // Access mcpServer.Store for test assertions
}
```

**See [testserver/README.md](testserver/README.md) for detailed usage patterns and examples.**

### As a Standalone Server

### Using Go

```bash
# Clone the repository
git clone https://github.com/TykTechnologies/tyk-mock-mcp-server.git
cd tyk-mock-mcp-server

# Install dependencies
go mod download

# Build the server
go build -o tyk-mock-mcp-server .

# Run the server
./tyk-mock-mcp-server
```

### Using Task

```bash
# Install dependencies and build
task install

# Build the binary
task build

# Run the server
task run

# Or run directly with go run
task dev
```

### Using Docker

```bash
# Build Docker image
docker build -t tyk-mock-mcp-server:latest .

# Run container
docker run -p 8080:8080 tyk-mock-mcp-server:latest

# Or use Task
task docker-build
task docker-run
```

## Usage

### Starting the Server

The server runs on HTTP with the MCP endpoint at `/mcp` and includes a health check endpoint:

```bash
# Default port 7878
./tyk-mock-mcp-server

# Custom port
PORT=3000 ./tyk-mock-mcp-server
```

**Endpoints:**
- MCP: `http://localhost:7878/mcp`
- SSE stream: `http://localhost:7878/sse/stream`
- SSE crash: `http://localhost:7878/sse/crash`
- Health: `http://localhost:7878/health`

### Configuration

Configure the server using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `7878` |
| `DEBUG` | Enable debug logging (true/1) | `false` |
| `WRITE_TIMEOUT` | HTTP write timeout in seconds (0 = disabled) | `0` |

## MCP Client Configuration

### Registering with Claude Desktop

To use this server with Claude Desktop, add it to your MCP settings configuration file:

**Location:**
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

**Configuration:**

```json
{
  "mcpServers": {
    "tyk-mock-mcp": {
      "url": "http://localhost:7878/mcp"
    }
  }
}
```

After adding the configuration:
1. Restart Claude Desktop
2. The server will appear in the MCP servers list
3. All 14 tools, 4 prompts, and 3 resources will be available

### Registering with Claude Code

To use this server with Claude Code or other MCP clients that support streamable-http transport, add it to your MCP settings configuration file:

**Location:**
- macOS/Linux: `~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json`
- Windows: `%APPDATA%\Code\User\globalStorage\rooveterinaryinc.roo-cline\settings\mcp_settings.json`

**Configuration:**

```json
{
  "mcpServers": {
    "tyk-mock-mcp": {
      "type": "streamable-http",
      "url": "https://your-server-domain.example.com/mcp",
      "alwaysAllow": [
        "get_anything"
      ],
      "headers": {
        "Authorization": "your-auth-token-here"
      },
      "disabled": false
    }
  }
}
```

**Configuration Options:**
- `type`: Must be `"streamable-http"` for SSE-based transport
- `url`: The server endpoint (use ngrok or similar for exposing local server)
- `alwaysAllow`: Optional array of tool names to auto-approve without user confirmation
- `headers`: Optional HTTP headers to include with requests (e.g., authentication)
- `disabled`: Set to `true` to temporarily disable the server without removing configuration

**Example with ngrok:**

If you're running the server locally and want to expose it via ngrok:

```bash
# Start the MCP server
./tyk-mock-mcp-server

# In another terminal, expose via ngrok
ngrok http 7878

# Use the ngrok URL in your configuration
# https://<random-subdomain>.ngrok-free.app/mcp
```

After adding the configuration:
1. Reload VS Code/Claude Code
2. The server will appear in the MCP servers list
3. Tools in the `alwaysAllow` list won't require confirmation prompts

### Registering with Other MCP Clients

For other MCP-compatible clients (IDEs, editors, custom applications):

**HTTP/SSE Connection:**
```javascript
// JavaScript/TypeScript example
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { SSEClientTransport } from "@modelcontextprotocol/sdk/client/sse.js";

const transport = new SSEClientTransport(
  new URL("http://localhost:7878/mcp")
);

const client = new Client({
  name: "my-mcp-client",
  version: "1.0.0"
}, {
  capabilities: {}
});

await client.connect(transport);
```

**Environment-specific Configuration:**

For development environments, you can use environment variables:

```bash
# .env file
MCP_SERVER_URL=http://localhost:7878/mcp
```

### VS Code / Cursor Integration

If your IDE supports MCP servers, add this to your settings:

```json
{
  "mcp.servers": [
    {
      "name": "tyk-mock-mcp",
      "url": "http://localhost:7878/mcp",
      "enabled": true
    }
  ]
}
```

### Docker Deployment for Production

When running in Docker for team/production use:

```bash
# Run server accessible on network
docker run -p 7878:7878 \
  --name tyk-mock-mcp-server \
  tyk-mock-mcp-server:latest

# Or with custom port
docker run -p 8080:7878 \
  -e PORT=7878 \
  --name tyk-mock-mcp-server \
  tyk-mock-mcp-server:latest
```

Then configure clients to point to:
```
http://<docker-host>:7878/mcp
```

### Verifying Connection

Test the server is accessible:

```bash
# Check health endpoint
curl http://localhost:7878/health

# Expected response:
# {"status":"healthy","version":"1.0.0"}
```

### Example Tool Calls

#### Get Users
```json
{
  "name": "get_users",
  "arguments": {
    "role": "admin",
    "active": true
  }
}
```

#### Create User
```json
{
  "name": "create_user",
  "arguments": {
    "name": "Alice Johnson",
    "email": "alice@example.com",
    "role": "user"
  }
}
```

#### Process Order
```json
{
  "name": "process_order",
  "arguments": {
    "product_id": "prod-1",
    "quantity": 2
  }
}
```

#### Get Anything (HTTP Testing Tool)
Echoes back request data in httpbin.org/anything format with **real HTTP request metadata** including actual client IP and headers from the underlying HTTP transport.

**Key Features:**
- 📡 **Real Client IP**: Extracts actual client IP from `X-Forwarded-For`, `X-Real-IP`, or `RemoteAddr`
- 🔍 **Real HTTP Headers**: Includes actual headers from the underlying HTTP request
- 🔀 **Header Merging**: User-provided headers are merged with/override real headers
- 🧪 **Perfect for Testing**: Useful for debugging MCP integrations and testing HTTP flows

```json
{
  "name": "get_anything",
  "arguments": {
    "method": "POST",
    "path": "/anything",
    "headers": {
      "Authorization": "Bearer token123",
      "X-Custom": "MyValue"
    },
    "query": {
      "page": "1",
      "limit": "10"
    },
    "body": {
      "test": "data",
      "foo": "bar"
    }
  }
}
```

**Response format (matches httpbin.org/anything):**
```json
{
  "args": {"page": "1", "limit": "10"},
  "data": "{\"test\":\"data\",\"foo\":\"bar\"}",
  "files": {},
  "form": {},
  "headers": {
    "Authorization": "Bearer token123",
    "X-Custom": "MyValue",
    "Content-Type": "application/json",
    "User-Agent": "Mozilla/5.0...",
    "Accept": "application/json",
    "Host": "localhost:7878",
    ... // Real HTTP headers from the transport layer
  },
  "json": {"test": "data", "foo": "bar"},
  "method": "POST",
  "origin": "203.0.113.45",  // Real client IP (respects X-Forwarded-For, X-Real-IP)
  "url": "https://mock-api.example.com/anything?page=1&limit=10"
}

### SSE Test Endpoints

These endpoints serve raw SSE streams outside the MCP protocol. They are designed
for testing gateway SSE proxy behaviour (timeout handling, Content-Type detection,
upstream crash simulation). See [TT-16661].

#### `GET /sse/stream` — Configurable SSE stream

| Parameter | Description | Default |
|-----------|-------------|---------|
| `events` | Number of events to send | `10` |
| `delay_ms` | Milliseconds between events | `1000` |
| `charset` | Appended to Content-Type (e.g. `utf-8`) | _(none)_ |

```bash
# 30 events, one per 5 seconds (~150s total, exceeds default 120s write_timeout)
curl -N "http://localhost:7878/sse/stream?events=30&delay_ms=5000"

# SSE with charset parameter in Content-Type
curl -N "http://localhost:7878/sse/stream?events=5&delay_ms=500&charset=utf-8"
```

Sends a `done` event after the last message event. Detects client disconnects
via context cancellation and stops sending.

#### `GET /sse/crash` — Upstream crash simulation

| Parameter | Description | Default |
|-----------|-------------|---------|
| `events_before_crash` | Events to send before crashing | `3` |
| `delay_ms` | Milliseconds between events | `1000` |
| `charset` | Appended to Content-Type (e.g. `utf-8`) | _(none)_ |

```bash
# Send 3 events then abruptly close the TCP connection
curl -N "http://localhost:7878/sse/crash?events_before_crash=3&delay_ms=1000"
```

After sending the configured events, the handler hijacks the underlying TCP
connection and closes it without sending any HTTP framing or SSE termination.
This simulates an upstream server crash.

#### `slow_response` MCP Tool

An MCP tool that responds after a configurable delay. Useful for testing
gateway timeout behaviour with MCP tool calls over SSE transport.

```json
{
  "name": "slow_response",
  "arguments": {
    "delay_seconds": 150,
    "message": "optional custom message"
  }
}
```

Respects context cancellation (client disconnect stops the timer).

### Available Task Commands

```bash
task                 # Show all available tasks
task install         # Install Go dependencies
task build           # Build the server binary
task run             # Build and run the server
task dev             # Run with go run (development)
task test            # Run tests
task test-coverage   # Run tests with coverage report
task fmt             # Format Go code
task vet             # Run go vet
task lint            # Run golangci-lint (requires golangci-lint)
task clean           # Clean build artifacts
task docker-build    # Build Docker image
task docker-run      # Run Docker container
task docker-stop     # Stop Docker container
task docker-clean    # Remove Docker image
task check           # Run fmt, vet, and test
task all             # Run fmt, vet, build, and test
```

## Troubleshooting

### Running in Debug Mode

To see detailed logs of all requests and responses, enable debug mode:

```bash
# Enable debug mode
DEBUG=true ./tyk-mock-mcp-server

# Or with environment variable
export DEBUG=1
./tyk-mock-mcp-server
```

**Debug output includes:**
- 📥 All incoming requests with headers
- 🌐 CORS middleware processing
- 🔧 MCP handler invocations
- ✅/❌ Response status codes
- 💚 Health check requests

**Example debug output:**
```
🐛 Debug mode enabled
📥 [127.0.0.1:52134] POST /mcp
   Headers:
     Content-Type: application/json
     Authorization: Bearer token123
🌐 CORS: Processing POST request from origin: http://example.com
🔧 MCP Handler: Returning server instance for request
✅ Response: 200 OK
```

This is invaluable for diagnosing connection issues with IDEs or MCP clients.

### SSE Error: Non-200 status code (401 or 405)

If your IDE or MCP client shows a **401 Unauthorized**, **405 Method Not Allowed**, or other connection errors:

**Understanding the Error:**

The MCP StreamableHTTP transport uses **Server-Sent Events (SSE)** which requires:
1. An initial POST request to establish the session
2. GET requests with an active session ID
3. Proper CORS headers for browser/IDE connections

A **405 error** typically means a GET request was made without an active session. A **401 error** usually indicates authentication/CORS issues.

**Common Causes:**

1. **Direct GET Request**: The StreamableHTTP transport requires establishing a session first via POST
2. **Missing CORS Headers**: Cross-origin requests from browser-based IDEs need proper CORS support (✅ now included)
3. **Incorrect URL**: Ensure you're connecting to `http://localhost:7878/mcp` (not `/` or other paths)
4. **Client not using SSE protocol**: Some clients try direct HTTP instead of SSE transport

**Solutions:**

✅ **Server now includes CORS support** (added in latest version)

✅ **Proper configuration for Claude Desktop:**
```json
{
  "mcpServers": {
    "tyk-mock-mcp": {
      "url": "http://localhost:7878/mcp"
    }
  }
}
```

✅ **For MCP SDK clients**, ensure you're using the proper transport:
```javascript
// Correct - uses SSEClientTransport
import { SSEClientTransport } from "@modelcontextprotocol/sdk/client/sse.js";

const transport = new SSEClientTransport(
  new URL("http://localhost:7878/mcp")
);
```

✅ **Verify server is running:**
```bash
# Should return: {"status":"healthy","version":"1.0.0"}
curl http://localhost:7878/health
```

### Connection Hangs or Timeouts

If connections hang:

1. **Check firewall**: Ensure port 7878 is not blocked
2. **Test connectivity**: `curl -v http://localhost:7878/mcp` should return a response
3. **Check server logs**: The server logs all connections and errors
4. **Restart server**: Stop any old instances with `pkill tyk-mock-mcp-server`

### Tools Not Appearing in IDE

If the MCP server connects but tools don't appear:

1. **Restart the IDE/Claude Desktop** after configuration changes
2. **Check the MCP settings file** is in the correct location
3. **Verify JSON syntax** in the configuration file
4. **Check server logs** for registration messages:
   ```
   Registered 14 tools, 4 prompts, and 3 resources
   ```

### Development Mode Issues

When running in development mode (`go run main.go` or `task dev`):

- **Port already in use**: Kill old instances: `pkill tyk-mock-mcp-server`
- **Changes not reflected**: Restart the server after code changes
- **Build errors**: Run `go mod tidy` to ensure dependencies are correct

### Getting Help

If issues persist:

1. Check server logs for detailed error messages
2. Enable verbose logging in your MCP client
3. Test the health endpoint: `curl http://localhost:7878/health`
4. Review the [MCP Go SDK documentation](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp)
5. Report issues at: https://github.com/TykTechnologies/tyk-mock-mcp-server/issues

## Development

### Project Structure

The codebase follows clean code principles:
- **Small functions**: Each function has a single responsibility
- **Separation of concerns**: Models, storage, and handlers are separated
- **Dependency injection**: Store is injected into handlers
- **No database required**: All data is stored in-memory

### Adding New Tools

1. Define models in `models/` directory
2. Add store methods in `store/store.go` if needed
3. Create handler in appropriate `handlers/tools/` file
4. Register tool in `main.go` `registerTools()` function

### Mock Data

The server initializes with mock data:
- 2 users (1 admin, 1 regular user)
- 2 blog posts
- 3 products (electronics, appliances, sports)

All data is stored in-memory and resets on server restart.

## MCP Specification Compliance

This server implements the **MCP 2025-11-25 specification** using the Go SDK v1.2.0+, which includes:
- ✅ Tools with automatic schema generation
- ✅ Prompts for contextual assistance
- ✅ Resources for data access
- ✅ HTTP/Streamable transport
- ✅ Proper error handling
- ✅ Health checks

## Health Checks

The server includes a health endpoint for monitoring:

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "healthy",
  "version": "1.0.0"
}
```

## Docker Support

### Multi-stage Build

The Dockerfile uses multi-stage builds for optimized image size:
- Builder stage: Compiles the Go application
- Runtime stage: Minimal Alpine Linux with only the binary

### Health Checks

Docker health checks are configured to monitor the server:
- Interval: 30 seconds
- Timeout: 3 seconds
- Start period: 5 seconds
- Retries: 3

## Contributing

Contributions are welcome! Please follow these guidelines:
1. Follow Go best practices and code style
2. Write small, focused functions
3. Maintain separation of concerns
4. Add tests for new features
5. Update documentation

## License

Copyright © 2025 Tyk Technologies

## References

- [Model Context Protocol Specification (2025-11-25)](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- [Task - A task runner](https://taskfile.dev/)
