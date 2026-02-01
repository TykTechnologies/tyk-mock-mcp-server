package models

import "time"

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type GetUsersInput struct {
	Role   string `json:"role,omitempty" jsonschema:"description:Filter by user role"`
	Active *bool  `json:"active,omitempty" jsonschema:"description:Filter by active status"`
}

type GetUsersOutput struct {
	Users []User `json:"users"`
	Count int    `json:"count"`
}

type CreateUserInput struct {
	Name  string `json:"name" jsonschema:"required,description:User's full name"`
	Email string `json:"email" jsonschema:"required,description:User's email address"`
	Role  string `json:"role,omitempty" jsonschema:"description:User role (admin/user/guest)"`
}

type UserOutput struct {
	User    User   `json:"user"`
	Message string `json:"message"`
}

type UpdateUserInput struct {
	ID     string  `json:"id" jsonschema:"required,description:User ID to update"`
	Name   *string `json:"name,omitempty" jsonschema:"description:New name"`
	Email  *string `json:"email,omitempty" jsonschema:"description:New email"`
	Role   *string `json:"role,omitempty" jsonschema:"description:New role"`
	Active *bool   `json:"active,omitempty" jsonschema:"description:New active status"`
}

type DeleteUserInput struct {
	ID string `json:"id" jsonschema:"required,description:User ID to delete"`
}
