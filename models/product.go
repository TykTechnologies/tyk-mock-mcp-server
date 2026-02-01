package models

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Stock       int     `json:"stock"`
	Category    string  `json:"category"`
}

type GetProductsInput struct {
	Category string  `json:"category,omitempty" jsonschema:"description:Filter by category"`
	MinPrice float64 `json:"min_price,omitempty" jsonschema:"description:Minimum price"`
	MaxPrice float64 `json:"max_price,omitempty" jsonschema:"description:Maximum price"`
}

type GetProductsOutput struct {
	Products []Product `json:"products"`
	Count    int       `json:"count"`
}
