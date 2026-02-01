package models

import "time"

type Order struct {
	ID         string    `json:"id"`
	ProductID  string    `json:"product_id"`
	Quantity   int       `json:"quantity"`
	TotalPrice float64   `json:"total_price"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type ProcessOrderInput struct {
	ProductID string `json:"product_id" jsonschema:"required,description:Product ID to order"`
	Quantity  int    `json:"quantity" jsonschema:"required,description:Order quantity"`
}

type OrderOutput struct {
	Order   Order  `json:"order"`
	Message string `json:"message"`
}
