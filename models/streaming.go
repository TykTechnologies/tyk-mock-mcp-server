package models

type SlowResponseInput struct {
	DelaySeconds int    `json:"delay_seconds" jsonschema:"required,description:Number of seconds to wait before responding (simulates a slow upstream)"`
	Message      string `json:"message,omitempty" jsonschema:"description:Optional message to include in the response"`
}

type SlowResponseOutput struct {
	Message      string `json:"message"`
	DelaySeconds int    `json:"delay_seconds"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
}
