package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewGetAnalyticsHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.GetAnalyticsInput) (models.AnalyticsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GetAnalyticsInput) (models.AnalyticsOutput, error) {
		data := make(map[string]interface{})

		switch input.Metric {
		case "users":
			data["total_users"] = s.GetUserCount()
			data["active_users"] = s.GetActiveUserCount()
			data["inactive_users"] = s.GetUserCount() - s.GetActiveUserCount()
		case "posts":
			data["total_posts"] = s.GetPostCount()
			data["by_status"] = s.GetPostCountByStatus()
		case "orders":
			data["total_orders"] = s.GetOrderCount()
			data["total_revenue"] = s.GetTotalRevenue()
		case "revenue":
			data["total_revenue"] = s.GetTotalRevenue()
			data["currency"] = "USD"
			data["period"] = "all_time"
		default:
			return models.AnalyticsOutput{}, fmt.Errorf("unknown metric: %s", input.Metric)
		}

		return models.AnalyticsOutput{
			Metric: input.Metric,
			Data:   data,
		}, nil
	}
}

func NewGenerateReportHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.GenerateReportInput) (models.ReportOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GenerateReportInput) (models.ReportOutput, error) {
		format := input.Format
		if format == "" {
			format = "json"
		}

		var reportData interface{}

		switch input.ReportType {
		case "sales":
			reportData = map[string]interface{}{
				"total_orders":  s.GetOrderCount(),
				"total_revenue": s.GetTotalRevenue(),
				"orders":        s.GetAllOrders(),
			}
		case "users":
			reportData = map[string]interface{}{
				"total_users": s.GetUserCount(),
				"users":       s.GetAllUsers(),
			}
		case "inventory":
			products := s.GetAllProducts()
			lowStock := []models.Product{}
			for _, product := range products {
				if product.Stock < 20 {
					lowStock = append(lowStock, product)
				}
			}
			reportData = map[string]interface{}{
				"total_products": len(products),
				"products":       products,
				"low_stock":      lowStock,
			}
		default:
			return models.ReportOutput{}, fmt.Errorf("unknown report type: %s", input.ReportType)
		}

		dataBytes, err := json.MarshalIndent(reportData, "", "  ")
		if err != nil {
			return models.ReportOutput{}, err
		}

		return models.ReportOutput{
			ReportType:  input.ReportType,
			Format:      format,
			Data:        string(dataBytes),
			GeneratedAt: time.Now(),
		}, nil
	}
}
