package main

// Acme Support Copilot demo tools.
//
// These tools act as a delegated agent: each reads the inbound Authorization
// bearer (the token Tyk forwarded after the hop-1 delegated exchange) and
// replays it on an outbound call to the downstream "acme-api" published on Tyk
// at a custom domain. The downstream echoes the bearer back (httpbin /anything),
// so the caller can decode it and see sub=alice preserved and the act claim
// recording the delegation. The per-tool scope (customers:read vs refunds:write)
// is enforced by Tyk, not here.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LookupCustomerInput / RecentOrdersInput / IssueRefundInput are the tool args.
type LookupCustomerInput struct {
	CustomerID string `json:"customer_id" jsonschema:"the customer id to look up"`
}

type RecentOrdersInput struct {
	CustomerID string `json:"customer_id" jsonschema:"the customer id whose orders to list"`
}

type IssueRefundInput struct {
	CustomerID string  `json:"customer_id" jsonschema:"the customer id to refund"`
	Amount     float64 `json:"amount" jsonschema:"the refund amount"`
}

// AcmeCallOutput is the downstream result surfaced back to the chat. Upstream
// holds the httpbin echo, whose headers.Authorization is the exchanged token
// the chat decodes to reveal sub + act.
type AcmeCallOutput struct {
	Status   int                    `json:"status"`
	URL      string                 `json:"url"`
	Upstream map[string]interface{} `json:"upstream,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

func acmeBaseURL() string {
	if v := os.Getenv("ACME_API_URL"); v != "" {
		return v
	}
	return "http://localhost:8181"
}

func acmeHost() string {
	if v := os.Getenv("ACME_API_HOST"); v != "" {
		return v
	}
	return "api.acme.internal"
}

// inboundBearer returns the Authorization header value Tyk forwarded to us.
func inboundBearer() string {
	meta := getCurrentRequestMetadata()
	if meta == nil {
		return ""
	}
	return meta.Headers["Authorization"]
}

// callAcmeAPI replays the inbound bearer on a request to the downstream API
// (through Tyk, addressed by Host header for the custom domain).
func callAcmeAPI(ctx context.Context, method, path string, body []byte) AcmeCallOutput {
	url := acmeBaseURL() + path
	out := AcmeCallOutput{URL: acmeHost() + path}

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	req.Host = acmeHost()
	req.Header.Set("Host", acmeHost())
	if b := inboundBearer(); b != "" {
		req.Header.Set("Authorization", b)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode

	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]interface{}
	if json.Unmarshal(raw, &parsed) == nil {
		out.Upstream = parsed
	} else if len(raw) > 0 {
		out.Upstream = map[string]interface{}{"raw": string(raw)}
	}
	if resp.StatusCode >= 400 {
		out.Error = fmt.Sprintf("downstream returned %d", resp.StatusCode)
	}
	return out
}

// registerAcmeTools registers the three on-behalf-of demo tools.
func registerAcmeTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "lookup_customer",
		Description: "Look up an Acme customer account. Calls the downstream Orders/Customers API on the rep's behalf (requires customers:read).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input LookupCustomerInput) (*mcp.CallToolResult, AcmeCallOutput, error) {
		id := input.CustomerID
		if id == "" {
			id = "unknown"
		}
		return nil, callAcmeAPI(ctx, http.MethodGet, "/anything/customers/"+id, nil), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "recent_orders",
		Description: "List a customer's recent orders. Calls the downstream API on the rep's behalf (requires customers:read).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RecentOrdersInput) (*mcp.CallToolResult, AcmeCallOutput, error) {
		id := input.CustomerID
		if id == "" {
			id = "unknown"
		}
		return nil, callAcmeAPI(ctx, http.MethodGet, "/anything/customers/"+id+"/orders", nil), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "issue_refund",
		Description: "Issue a refund for a customer. Sensitive write — the downstream API requires refunds:write, so only supervisors can complete it.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input IssueRefundInput) (*mcp.CallToolResult, AcmeCallOutput, error) {
		body, _ := json.Marshal(map[string]interface{}{
			"customer_id": input.CustomerID,
			"amount":      input.Amount,
		})
		return nil, callAcmeAPI(ctx, http.MethodPost, "/anything/refunds", body), nil
	})
}
