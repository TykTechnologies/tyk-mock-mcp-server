package tools

import (
	"context"
	"fmt"

	"github.com/TykTechnologies/tyk-mock-mcp-server/models"
	"github.com/TykTechnologies/tyk-mock-mcp-server/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func NewGetProductsHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.GetProductsInput) (models.GetProductsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.GetProductsInput) (models.GetProductsOutput, error) {
		products := s.GetProducts(input.Category, input.MinPrice, input.MaxPrice)
		return models.GetProductsOutput{
			Products: products,
			Count:    len(products),
		}, nil
	}
}

func NewProcessOrderHandler(s *store.Store) func(context.Context, *mcp.CallToolRequest, models.ProcessOrderInput) (models.OrderOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, input models.ProcessOrderInput) (models.OrderOutput, error) {
		product, err := s.GetProduct(input.ProductID)
		if err != nil {
			return models.OrderOutput{}, err
		}

		if product.Stock < input.Quantity {
			return models.OrderOutput{}, fmt.Errorf("insufficient stock: available %d, requested %d", product.Stock, input.Quantity)
		}

		totalPrice := product.Price * float64(input.Quantity)
		order := s.CreateOrder(input.ProductID, input.Quantity, totalPrice)

		if err := s.UpdateProductStock(input.ProductID, input.Quantity); err != nil {
			return models.OrderOutput{}, err
		}

		return models.OrderOutput{
			Order:   *order,
			Message: fmt.Sprintf("Order placed successfully. Total: $%.2f", totalPrice),
		}, nil
	}
}
