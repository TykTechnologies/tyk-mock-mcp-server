package tools

import (
	"context"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewGetPostsHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.GetPostsInput) (models.GetPostsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GetPostsInput) (models.GetPostsOutput, error) {
		posts := s.GetPosts(input.Author, input.Status)
		return models.GetPostsOutput{
			Posts: posts,
			Count: len(posts),
		}, nil
	}
}

func NewCreatePostHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.CreatePostInput) (models.PostOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.CreatePostInput) (models.PostOutput, error) {
		post := s.CreatePost(input.Title, input.Content, input.Author, input.Status)
		return models.PostOutput{
			Post:    *post,
			Message: "Post created successfully",
		}, nil
	}
}
