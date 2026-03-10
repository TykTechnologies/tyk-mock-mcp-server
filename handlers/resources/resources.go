package resources

import (
	"context"
	"encoding/json"

	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ResourcesHandler struct {
	store *store.Store
}

func NewResourcesHandler(s *store.Store) *ResourcesHandler {
	return &ResourcesHandler{store: s}
}

func (h *ResourcesHandler) Users(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	users := h.store.GetAllUsers()

	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "users://list",
				MIMEType: "application/json",
				Text:     string(data),
			},
		},
	}, nil
}

func (h *ResourcesHandler) Products(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	products := h.store.GetAllProducts()

	data, err := json.MarshalIndent(products, "", "  ")
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "products://catalog",
				MIMEType: "application/json",
				Text:     string(data),
			},
		},
	}, nil
}

func (h *ResourcesHandler) Posts(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	posts := h.store.GetAllPosts()

	data, err := json.MarshalIndent(posts, "", "  ")
	if err != nil {
		return nil, err
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      "posts://list",
				MIMEType: "application/json",
				Text:     string(data),
			},
		},
	}, nil
}

func (h *ResourcesHandler) FileTemplate(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/octet-stream",
				Text:     "mock file content",
			},
		},
	}, nil
}

func (h *ResourcesHandler) DBTemplate(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     `{"rows": []}`,
			},
		},
	}, nil
}
