package models

type ValidateEmailInput struct {
	Email string `json:"email" jsonschema:"required,description:Email address to validate"`
}

type ValidateEmailOutput struct {
	Email   string `json:"email"`
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
}

type GenerateUUIDOutput struct {
	UUID string `json:"uuid"`
}

type FormatDateInput struct {
	Date   string `json:"date" jsonschema:"required,description:Date to format (RFC3339 or YYYY-MM-DD)"`
	Format string `json:"format,omitempty" jsonschema:"description:Output format (iso/rfc3339/unix/custom)"`
}

type FormatDateOutput struct {
	Original  string `json:"original"`
	Formatted string `json:"formatted"`
	Format    string `json:"format"`
}

type GetAnythingInput struct {
	Method  string                 `json:"method,omitempty" jsonschema:"description:HTTP method (GET/POST/PUT/DELETE)"`
	Headers map[string]string      `json:"headers,omitempty" jsonschema:"description:Request headers"`
	Query   map[string]string      `json:"query,omitempty" jsonschema:"description:Query parameters"`
	Body    map[string]interface{} `json:"body,omitempty" jsonschema:"description:Request body data"`
	Path    string                 `json:"path,omitempty" jsonschema:"description:Request path (defaults to /anything)"`
}

type GetAnythingOutput struct {
	Args    map[string]string      `json:"args"`
	Data    string                 `json:"data"`
	Files   map[string]interface{} `json:"files"`
	Form    map[string]interface{} `json:"form"`
	Headers map[string]string      `json:"headers"`
	JSON    map[string]interface{} `json:"json"`
	Method  string                 `json:"method"`
	Origin  string                 `json:"origin"`
	URL     string                 `json:"url"`
}
