package prompts

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromptsHandler struct{}

func NewPromptsHandler() *PromptsHandler {
	return &PromptsHandler{}
}

func (h *PromptsHandler) UserManagement(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := "You are helping with user management tasks. Available operations:\n\n" +
		"1. List users with optional filters (role, active status)\n" +
		"2. Create new users with name, email, and role\n" +
		"3. Update existing users (name, email, role, active status)\n" +
		"4. Delete users by ID\n\n" +
		"What user management task would you like to perform?"

	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: text,
				},
			},
		},
	}, nil
}

func (h *PromptsHandler) Ecommerce(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := "You are helping with e-commerce operations. Available features:\n\n" +
		"1. Browse products with filters (category, price range)\n" +
		"2. Process orders for products\n" +
		"3. Check inventory and stock levels\n\n" +
		"How can I assist you with e-commerce tasks today?"

	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: text,
				},
			},
		},
	}, nil
}

func (h *PromptsHandler) ContentManagement(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := "You are managing blog content. Available operations:\n\n" +
		"1. List posts with filters (author, status)\n" +
		"2. Create new posts (title, content, author)\n" +
		"3. Posts can have status: draft, published, or archived\n\n" +
		"What would you like to do with your blog content?"

	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: text,
				},
			},
		},
	}, nil
}

func (h *PromptsHandler) Analytics(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	text := "You are analyzing business metrics. Available analytics:\n\n" +
		"1. User metrics (total, active, inactive)\n" +
		"2. Post metrics (total, by status)\n" +
		"3. Order metrics (total, revenue)\n" +
		"4. Generate reports (sales, users, inventory)\n\n" +
		"What insights would you like to explore?"

	return &mcp.GetPromptResult{
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{
					Text: text,
				},
			},
		},
	}, nil
}
