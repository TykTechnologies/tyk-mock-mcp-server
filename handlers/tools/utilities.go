package tools

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewValidateEmailHandler() func(context.Context, *mcp.CallToolRequest, models.ValidateEmailInput) (models.ValidateEmailOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.ValidateEmailInput) (models.ValidateEmailOutput, error) {
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		valid := emailRegex.MatchString(input.Email)

		message := "Valid email address"
		if !valid {
			message = "Invalid email address format"
		}

		return models.ValidateEmailOutput{
			Email:   input.Email,
			Valid:   valid,
			Message: message,
		}, nil
	}
}

func NewGenerateUUIDHandler() func(context.Context, *mcp.CallToolRequest, struct{}) (models.GenerateUUIDOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (models.GenerateUUIDOutput, error) {
		return models.GenerateUUIDOutput{
			UUID: uuid.New().String(),
		}, nil
	}
}

func NewFormatDateHandler() func(context.Context, *mcp.CallToolRequest, models.FormatDateInput) (models.FormatDateOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.FormatDateInput) (models.FormatDateOutput, error) {
		var parsedTime time.Time
		var err error

		parsedTime, err = time.Parse(time.RFC3339, input.Date)
		if err != nil {
			parsedTime, err = time.Parse("2006-01-02", input.Date)
			if err != nil {
				return models.FormatDateOutput{}, fmt.Errorf("unable to parse date: %s", input.Date)
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

		return models.FormatDateOutput{
			Original:  input.Date,
			Formatted: formatted,
			Format:    format,
		}, nil
	}
}

func NewGetAnythingHandler() func(context.Context, *mcp.CallToolRequest, models.GetAnythingInput) (models.GetAnythingOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GetAnythingInput) (models.GetAnythingOutput, error) {
		method := input.Method
		if method == "" {
			method = "GET"
		}

		headers := input.Headers
		if headers == nil {
			headers = make(map[string]string)
		}
		headers["content-type"] = "application/json"
		headers["user-agent"] = "MCP-Mock-Server/1.0"

		query := input.Query
		if query == nil {
			query = make(map[string]string)
		}

		body := input.Body
		if body == nil {
			body = make(map[string]interface{})
		}

		url := "https://mock-api.example.com/anything"
		if len(query) > 0 {
			url += "?"
			first := true
			for k, v := range query {
				if !first {
					url += "&"
				}
				url += k + "=" + v
				first = false
			}
		}

		return models.GetAnythingOutput{
			Method:  method,
			Headers: headers,
			Query:   query,
			Body:    body,
			URL:     url,
			Origin:  "127.0.0.1",
			Args:    query,
		}, nil
	}
}
