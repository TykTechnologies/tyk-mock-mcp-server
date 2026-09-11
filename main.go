package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/handlers/prompts"
	"github.com/TykTechnologies/tyk-mock-mcp-server/handlers/resources"
	ssehandler "github.com/TykTechnologies/tyk-mock-mcp-server/handlers/streaming"
	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultPort     = "7878"
	serverName      = "tyk-mock-mcp-server"
	serverVersion   = "1.0.0"
	shutdownTimeout = 10 * time.Second
)

var debugMode bool

// HTTPRequestMetadata stores information from the underlying HTTP request
type HTTPRequestMetadata struct {
	RemoteAddr string
	Headers    map[string]string
	RealIP     string
}

var (
	currentRequestMeta *HTTPRequestMetadata
	requestMetaMutex   sync.RWMutex
)

func setCurrentRequestMetadata(meta *HTTPRequestMetadata) {
	requestMetaMutex.Lock()
	defer requestMetaMutex.Unlock()
	currentRequestMeta = meta
}

func getCurrentRequestMetadata() *HTTPRequestMetadata {
	requestMetaMutex.RLock()
	defer requestMetaMutex.RUnlock()
	if currentRequestMeta == nil {
		return &HTTPRequestMetadata{
			RemoteAddr: "127.0.0.1",
			Headers:    make(map[string]string),
			RealIP:     "127.0.0.1",
		}
	}
	return currentRequestMeta
}

func extractRealIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func main() {
	// Enable debug mode if DEBUG=true or DEBUG=1
	if os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1" {
		debugMode = true
		log.Println("🐛 Debug mode enabled")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	server := setupServer()

	httpServer := startHTTPServer(server, port)

	waitForShutdown(httpServer)
}

func setupServer() *mcp.Server {
	dataStore := store.NewStore()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	registerTools(server, dataStore)
	registerPrompts(server)
	registerResources(server, dataStore)
	registerOASTools(server)

	log.Printf("Initialized %s v%s", serverName, serverVersion)
	log.Println("Registered 15 tools, 4 prompts, and 3 resources")

	return server
}

// registerOASTools optionally registers tools derived from one or more OpenAPI
// specs (comma-separated MCP_OAS_PATH), each proxied to MCP_OAS_UPSTREAM_URL.
// Disabled when MCP_OAS_PATH is unset, so default behaviour is unchanged.
func registerOASTools(server *mcp.Server) {
	specs := os.Getenv("MCP_OAS_PATH")
	if specs == "" {
		return
	}
	cfg := UpstreamConfig{
		BaseURL:      envOr("MCP_OAS_UPSTREAM_URL", "http://localhost:8181"),
		Host:         os.Getenv("MCP_OAS_UPSTREAM_HOST"),
		ForwardAuth:  os.Getenv("MCP_OAS_FORWARD_AUTH") != "false",
		ForwardTrace: os.Getenv("MCP_OAS_FORWARD_TRACE") != "false",
	}
	for _, p := range strings.Split(specs, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		names, err := RegisterFromOAS(server, p, cfg)
		if err != nil {
			log.Printf("OAS tools: %v", err)
			continue
		}
		log.Printf("OAS tools: registered %d from %s -> %s: %v", len(names), p, cfg.BaseURL, names)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func registerTools(server *mcp.Server, s *store.Store) {
	// User Management Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_users",
		Description: "Retrieve mock users with optional filtering by role and active status",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GetUsersInput) (*mcp.CallToolResult, models.GetUsersOutput, error) {
		users := s.GetUsers(input.Role, input.Active)
		return nil, models.GetUsersOutput{Users: users, Count: len(users)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_user",
		Description: "Create a new mock user with name, email, and optional role",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.CreateUserInput) (*mcp.CallToolResult, models.UserOutput, error) {
		user := s.CreateUser(input.Name, input.Email, input.Role)
		return nil, models.UserOutput{User: *user, Message: "User created successfully"}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_user",
		Description: "Update an existing user's information (name, email, role, or active status)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.UpdateUserInput) (*mcp.CallToolResult, models.UserOutput, error) {
		user, err := s.UpdateUser(input.ID, input.Name, input.Email, input.Role, input.Active)
		if err != nil {
			return nil, models.UserOutput{}, err
		}
		return nil, models.UserOutput{User: *user, Message: "User updated successfully"}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_user",
		Description: "Delete a user by their ID",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.DeleteUserInput) (*mcp.CallToolResult, models.MessageOutput, error) {
		if err := s.DeleteUser(input.ID); err != nil {
			return nil, models.MessageOutput{}, err
		}
		return nil, models.MessageOutput{Success: true, Message: "User deleted successfully"}, nil
	})

	// Content Management Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_posts",
		Description: "Retrieve blog posts with optional filtering by author and status",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GetPostsInput) (*mcp.CallToolResult, models.GetPostsOutput, error) {
		posts := s.GetPosts(input.Author, input.Status)
		return nil, models.GetPostsOutput{Posts: posts, Count: len(posts)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_post",
		Description: "Create a new blog post with title, content, author, and optional status",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.CreatePostInput) (*mcp.CallToolResult, models.PostOutput, error) {
		post := s.CreatePost(input.Title, input.Content, input.Author, input.Status)
		return nil, models.PostOutput{Post: *post, Message: "Post created successfully"}, nil
	})

	// E-commerce Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_products",
		Description: "Browse product catalog with optional filtering by category and price range",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GetProductsInput) (*mcp.CallToolResult, models.GetProductsOutput, error) {
		products := s.GetProducts(input.Category, input.MinPrice, input.MaxPrice)
		return nil, models.GetProductsOutput{Products: products, Count: len(products)}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "process_order",
		Description: "Process a mock order for a product with specified quantity",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.ProcessOrderInput) (*mcp.CallToolResult, models.OrderOutput, error) {
		product, err := s.GetProduct(input.ProductID)
		if err != nil {
			return nil, models.OrderOutput{}, err
		}
		if product.Stock < input.Quantity {
			return nil, models.OrderOutput{}, fmt.Errorf("insufficient stock: available %d, requested %d", product.Stock, input.Quantity)
		}
		totalPrice := product.Price * float64(input.Quantity)
		order := s.CreateOrder(input.ProductID, input.Quantity, totalPrice)
		if err := s.UpdateProductStock(input.ProductID, input.Quantity); err != nil {
			return nil, models.OrderOutput{}, err
		}
		return nil, models.OrderOutput{Order: *order, Message: fmt.Sprintf("Order placed successfully. Total: $%.2f", totalPrice)}, nil
	})

	// Analytics Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_analytics",
		Description: "Generate analytics data for various metrics (users, posts, orders, revenue)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GetAnalyticsInput) (*mcp.CallToolResult, models.AnalyticsOutput, error) {
		data := make(map[string]interface{})
		switch input.Metric {
		case "users":
			data["total_users"] = s.GetUserCount()
			data["active_users"] = s.GetActiveUserCount()
			data["inactive_users"] = s.GetUserCount() - s.GetActiveUserCount()
		case "posts":
			data["total_posts"] = s.GetPostCount()
			data["by_status"] = s.GetPostCountByStatus()
		case "orders":
			data["total_orders"] = s.GetOrderCount()
			data["total_revenue"] = s.GetTotalRevenue()
		case "revenue":
			data["total_revenue"] = s.GetTotalRevenue()
			data["currency"] = "USD"
			data["period"] = "all_time"
		default:
			return nil, models.AnalyticsOutput{}, fmt.Errorf("unknown metric: %s", input.Metric)
		}
		return nil, models.AnalyticsOutput{Metric: input.Metric, Data: data}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_report",
		Description: "Generate business reports (sales, users, inventory) in specified format",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GenerateReportInput) (*mcp.CallToolResult, models.ReportOutput, error) {
		format := input.Format
		if format == "" {
			format = "json"
		}
		var reportData interface{}
		switch input.ReportType {
		case "sales":
			reportData = map[string]interface{}{
				"total_orders":  s.GetOrderCount(),
				"total_revenue": s.GetTotalRevenue(),
				"orders":        s.GetAllOrders(),
			}
		case "users":
			reportData = map[string]interface{}{
				"total_users": s.GetUserCount(),
				"users":       s.GetAllUsers(),
			}
		case "inventory":
			products := s.GetAllProducts()
			lowStock := []models.Product{}
			for _, product := range products {
				if product.Stock < 20 {
					lowStock = append(lowStock, product)
				}
			}
			reportData = map[string]interface{}{
				"total_products": len(products),
				"products":       products,
				"low_stock":      lowStock,
			}
		default:
			return nil, models.ReportOutput{}, fmt.Errorf("unknown report type: %s", input.ReportType)
		}
		dataBytes, err := json.MarshalIndent(reportData, "", "  ")
		if err != nil {
			return nil, models.ReportOutput{}, err
		}
		return nil, models.ReportOutput{ReportType: input.ReportType, Format: format, Data: string(dataBytes), GeneratedAt: time.Now()}, nil
	})

	// Utility Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "validate_email",
		Description: "Validate email address format using regex",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.ValidateEmailInput) (*mcp.CallToolResult, models.ValidateEmailOutput, error) {
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		valid := emailRegex.MatchString(input.Email)
		message := "Valid email address"
		if !valid {
			message = "Invalid email address format"
		}
		return nil, models.ValidateEmailOutput{Email: input.Email, Valid: valid, Message: message}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "generate_uuid",
		Description: "Generate a new UUID v4",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, models.GenerateUUIDOutput, error) {
		return nil, models.GenerateUUIDOutput{UUID: uuid.New().String()}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "format_date",
		Description: "Format dates in various formats (iso, rfc3339, unix, custom)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.FormatDateInput) (*mcp.CallToolResult, models.FormatDateOutput, error) {
		var parsedTime time.Time
		var err error
		parsedTime, err = time.Parse(time.RFC3339, input.Date)
		if err != nil {
			parsedTime, err = time.Parse("2006-01-02", input.Date)
			if err != nil {
				return nil, models.FormatDateOutput{}, fmt.Errorf("unable to parse date: %s", input.Date)
			}
		}
		format := input.Format
		if format == "" {
			format = "iso"
		}
		var formatted string
		switch format {
		case "iso":
			formatted = parsedTime.Format("2006-01-02")
		case "rfc3339":
			formatted = parsedTime.Format(time.RFC3339)
		case "unix":
			formatted = fmt.Sprintf("%d", parsedTime.Unix())
		default:
			formatted = parsedTime.Format(format)
		}
		return nil, models.FormatDateOutput{Original: input.Date, Formatted: formatted, Format: format}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_anything",
		Description: "Echo back request data in httpbin.org/anything format with real HTTP request metadata",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.GetAnythingInput) (*mcp.CallToolResult, models.GetAnythingOutput, error) {
		// Get real HTTP request metadata
		requestMeta := getCurrentRequestMetadata()

		// Set defaults
		method := input.Method
		if method == "" {
			method = "GET"
		}

		path := input.Path
		if path == "" {
			path = "/anything"
		}

		// Start with real HTTP headers from the underlying request
		headers := make(map[string]string)
		for k, v := range requestMeta.Headers {
			headers[k] = v
		}

		// Merge/override with user-provided headers from the tool input
		if input.Headers != nil {
			for k, v := range input.Headers {
				headers[k] = v
			}
		}

		// Query parameters (args in httpbin)
		args := make(map[string]string)
		if input.Query != nil {
			args = input.Query
		}

		// Build URL with query string
		url := "https://mock-api.example.com" + path
		if len(args) > 0 {
			url += "?"
			first := true
			for k, v := range args {
				if !first {
					url += "&"
				}
				url += k + "=" + v
				first = false
			}
		}

		// Handle body data
		var data string
		jsonData := make(map[string]interface{}) // Initialize to empty map, not nil
		if input.Body != nil && len(input.Body) > 0 {
			bodyBytes, err := json.Marshal(input.Body)
			if err != nil {
				return nil, models.GetAnythingOutput{}, fmt.Errorf("failed to marshal body: %v", err)
			}
			data = string(bodyBytes)
			jsonData = input.Body
			// Add Content-Type if body is present and not already set
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		}

		// Use real client IP from the HTTP request
		origin := requestMeta.RealIP

		return nil, models.GetAnythingOutput{
			Args:    args,
			Data:    data,
			Files:   make(map[string]interface{}),
			Form:    make(map[string]interface{}),
			Headers: headers,
			JSON:    jsonData,
			Method:  method,
			Origin:  origin,
			URL:     url,
		}, nil
	})

	// Testing Tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "slow_response",
		Description: "Respond after a configurable delay. Useful for testing gateway timeout behaviour with MCP tool calls.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input models.SlowResponseInput) (*mcp.CallToolResult, models.SlowResponseOutput, error) {
		if input.DelaySeconds <= 0 {
			return nil, models.SlowResponseOutput{}, fmt.Errorf("delay_seconds must be positive")
		}
		if input.DelaySeconds > 600 {
			return nil, models.SlowResponseOutput{}, fmt.Errorf("delay_seconds must not exceed 600")
		}

		startedAt := time.Now().UTC().Format(time.RFC3339)

		select {
		case <-time.After(time.Duration(input.DelaySeconds) * time.Second):
			// completed normally
		case <-ctx.Done():
			return nil, models.SlowResponseOutput{}, ctx.Err()
		}

		msg := input.Message
		if msg == "" {
			msg = fmt.Sprintf("Response delivered after %d second delay", input.DelaySeconds)
		}

		return nil, models.SlowResponseOutput{
			Message:      msg,
			DelaySeconds: input.DelaySeconds,
			StartedAt:    startedAt,
			CompletedAt:  time.Now().UTC().Format(time.RFC3339),
		}, nil
	})
}

func registerPrompts(server *mcp.Server) {
	promptsHandler := prompts.NewPromptsHandler()

	server.AddPrompt(&mcp.Prompt{
		Name:        "user_management",
		Description: "Help with user management operations (list, create, update, delete)",
	}, promptsHandler.UserManagement)

	server.AddPrompt(&mcp.Prompt{
		Name:        "ecommerce_assistant",
		Description: "Assist with e-commerce operations (products, orders, inventory)",
	}, promptsHandler.Ecommerce)

	server.AddPrompt(&mcp.Prompt{
		Name:        "content_management",
		Description: "Help with blog content management (posts creation and retrieval)",
	}, promptsHandler.ContentManagement)

	server.AddPrompt(&mcp.Prompt{
		Name:        "analytics_dashboard",
		Description: "Provide insights and generate reports from business data",
	}, promptsHandler.Analytics)
}

func registerResources(server *mcp.Server, dataStore *store.Store) {
	resourcesHandler := resources.NewResourcesHandler(dataStore)

	server.AddResource(&mcp.Resource{
		URI:         "users://list",
		Name:        "Users List",
		Description: "Complete list of all mock users in the system",
		MIMEType:    "application/json",
	}, resourcesHandler.Users)

	server.AddResource(&mcp.Resource{
		URI:         "products://catalog",
		Name:        "Product Catalog",
		Description: "Complete product catalog with prices and inventory",
		MIMEType:    "application/json",
	}, resourcesHandler.Products)

	server.AddResource(&mcp.Resource{
		URI:         "posts://list",
		Name:        "Blog Posts",
		Description: "All blog posts with their content and status",
		MIMEType:    "application/json",
	}, resourcesHandler.Posts)

	// Resource templates
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "file://{path}",
		Name:        "File Resource",
		Description: "Access a file by path",
		MIMEType:    "application/octet-stream",
	}, resourcesHandler.FileTemplate)

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "db://{schema}/{table}",
		Name:        "Database Table",
		Description: "Access a database table by schema and table name",
		MIMEType:    "application/json",
	}, resourcesHandler.DBTemplate)
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if debugMode {
			log.Printf("📥 [%s] %s %s", r.RemoteAddr, r.Method, r.URL.Path)
			log.Printf("   Headers:")
			for name, values := range r.Header {
				for _, value := range values {
					log.Printf("     %s: %s", name, value)
				}
			}
			if r.Method == "POST" || r.Method == "PUT" {
				log.Printf("   Content-Type: %s", r.Header.Get("Content-Type"))
				log.Printf("   Content-Length: %s", r.Header.Get("Content-Length"))
			}
		}

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		if debugMode {
			statusEmoji := "✅"
			if rw.statusCode >= 400 {
				statusEmoji = "❌"
			} else if rw.statusCode >= 300 {
				statusEmoji = "↪️"
			}
			log.Printf("%s Response: %d %s", statusEmoji, rw.statusCode, http.StatusText(rw.statusCode))
		}
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if debugMode {
			log.Printf("🌐 CORS: Processing %s request from origin: %s", r.Method, r.Header.Get("Origin"))
		}

		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight OPTIONS request
		if r.Method == "OPTIONS" {
			if debugMode {
				log.Printf("✓ CORS: Handling OPTIONS preflight request")
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func captureRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture HTTP request metadata
		headers := make(map[string]string)
		for name, values := range r.Header {
			if len(values) > 0 {
				headers[name] = values[0]
			}
		}

		meta := &HTTPRequestMetadata{
			RemoteAddr: r.RemoteAddr,
			Headers:    headers,
			RealIP:     extractRealIP(r),
		}

		setCurrentRequestMetadata(meta)

		if debugMode {
			log.Printf("📍 Captured request metadata: IP=%s, Headers=%d", meta.RealIP, len(headers))
		}

		next.ServeHTTP(w, r)
	})
}

func startHTTPServer(mcpServer *mcp.Server, port string) *http.Server {
	mux := http.NewServeMux()

	handler := newProtocolSwitchHandler(mcpServer)

	// Wrap handler with middleware (order matters: logging -> CORS -> capture -> handler)
	wrappedHandler := loggingMiddleware(corsMiddleware(captureRequestMiddleware(handler)))

	mux.Handle("/mcp", wrappedHandler)
	fixtures := newCacheFixtures()
	mux.Handle("/fixtures/mcp", fixtures)
	mux.HandleFunc("/fixtures/counters", fixtures.counters)

	// SSE test endpoints for gateway SSE proxy testing.
	mux.HandleFunc("/sse/stream", ssehandler.StreamHandler)
	mux.HandleFunc("/sse/crash", ssehandler.CrashHandler)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if debugMode {
			log.Printf("💚 Health check from %s", r.RemoteAddr)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","version":"%s"}`, serverVersion)
	})

	writeTimeout := parseEnvDuration("WRITE_TIMEOUT", 0)

	httpServer := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Starting HTTP server on port %s", port)
		log.Printf("MCP endpoint: http://localhost:%s/mcp", port)
		log.Printf("SSE stream endpoint: http://localhost:%s/sse/stream", port)
		log.Printf("SSE crash endpoint: http://localhost:%s/sse/crash", port)
		log.Printf("Health endpoint: http://localhost:%s/health", port)

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	return httpServer
}

// parseEnvDuration reads a duration in seconds from an environment variable.
// Returns defaultVal when the variable is unset or unparseable.
func parseEnvDuration(envKey string, defaultVal time.Duration) time.Duration {
	s := os.Getenv(envKey)
	if s == "" {
		return defaultVal
	}
	secs, err := strconv.Atoi(s)
	if err != nil || secs < 0 {
		log.Printf("Invalid %s value %q, using default %v", envKey, s, defaultVal)
		return defaultVal
	}
	return time.Duration(secs) * time.Second
}

func waitForShutdown(httpServer *http.Server) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutdown signal received, gracefully stopping server...")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
