package testserver

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/handlers/prompts"
	"github.com/TykTechnologies/tyk-mock-mcp-server/handlers/resources"
	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "tyk-mock-mcp-server"
	serverVersion = "1.0.0"
)

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

func extractRealIP(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

func setupMCPServer(dataStore *store.Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	registerTools(server, dataStore)
	registerPrompts(server)
	registerResources(server, dataStore)

	return server
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
}
