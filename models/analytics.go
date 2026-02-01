package models

import "time"

type GetAnalyticsInput struct {
	Metric    string `json:"metric" jsonschema:"required,description:Metric type (users/posts/orders/revenue)"`
	StartDate string `json:"start_date,omitempty" jsonschema:"description:Start date (YYYY-MM-DD)"`
	EndDate   string `json:"end_date,omitempty" jsonschema:"description:End date (YYYY-MM-DD)"`
}

type AnalyticsOutput struct {
	Metric string                 `json:"metric"`
	Data   map[string]interface{} `json:"data"`
}

type GenerateReportInput struct {
	ReportType string `json:"report_type" jsonschema:"required,description:Report type (sales/users/inventory)"`
	Format     string `json:"format,omitempty" jsonschema:"description:Output format (json/csv/pdf)"`
}

type ReportOutput struct {
	ReportType  string    `json:"report_type"`
	Format      string    `json:"format"`
	Data        string    `json:"data"`
	GeneratedAt time.Time `json:"generated_at"`
}
