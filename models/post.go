package models

import "time"

type Post struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Author    string    `json:"author"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type GetPostsInput struct {
	Author string `json:"author,omitempty" jsonschema:"description:Filter by author"`
	Status string `json:"status,omitempty" jsonschema:"description:Filter by status (draft/published/archived)"`
}

type GetPostsOutput struct {
	Posts []Post `json:"posts"`
	Count int    `json:"count"`
}

type CreatePostInput struct {
	Title   string `json:"title" jsonschema:"required,description:Post title"`
	Content string `json:"content" jsonschema:"required,description:Post content"`
	Author  string `json:"author" jsonschema:"required,description:Author name"`
	Status  string `json:"status,omitempty" jsonschema:"description:Post status (draft/published)"`
}

type PostOutput struct {
	Post    Post   `json:"post"`
	Message string `json:"message"`
}
