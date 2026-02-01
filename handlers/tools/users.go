package tools

import (
	"context"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewGetUsersHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.GetUsersInput) (models.GetUsersOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GetUsersInput) (models.GetUsersOutput, error) {
		users := s.GetUsers(input.Role, input.Active)
		return models.GetUsersOutput{
			Users: users,
			Count: len(users),
		}, nil
	}
}

func NewCreateUserHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.CreateUserInput) (models.UserOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.CreateUserInput) (models.UserOutput, error) {
		user := s.CreateUser(input.Name, input.Email, input.Role)
		return models.UserOutput{
			User:    *user,
			Message: "User created successfully",
		}, nil
	}
}

func NewUpdateUserHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.UpdateUserInput) (models.UserOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.UpdateUserInput) (models.UserOutput, error) {
		user, err := s.UpdateUser(input.ID, input.Name, input.Email, input.Role, input.Active)
		if err != nil {
			return models.UserOutput{}, err
		}
		return models.UserOutput{
			User:    *user,
			Message: "User updated successfully",
		}, nil
	}
}

func NewDeleteUserHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.DeleteUserInput) (models.MessageOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.DeleteUserInput) (models.MessageOutput, error) {
		if err := s.DeleteUser(input.ID); err != nil {
			return models.MessageOutput{}, err
		}
		return models.MessageOutput{
			Success: true,
			Message: "User deleted successfully",
		}, nil
	}
}
