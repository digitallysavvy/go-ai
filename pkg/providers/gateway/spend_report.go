package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// SpendReportParams configures a spend report query.
type SpendReportParams struct {
	StartDate      string
	EndDate        string
	GroupBy        string
	DatePart       string
	UserID         string
	Model          string
	Provider       string
	CredentialType string
	Tags           []string
}

// SpendReportResponse contains the query results.
type SpendReportResponse struct {
	Results []SpendReportRow `json:"results"`
}

// SpendReportRow represents one aggregated row.
type SpendReportRow struct {
	Day                      string  `json:"day,omitempty"`
	Hour                     string  `json:"hour,omitempty"`
	User                     string  `json:"user,omitempty"`
	Model                    string  `json:"model,omitempty"`
	Tag                      string  `json:"tag,omitempty"`
	Provider                 string  `json:"provider,omitempty"`
	CredentialType           string  `json:"credentialType,omitempty"`
	TotalCost                float64 `json:"totalCost"`
	MarketCost               float64 `json:"marketCost,omitempty"`
	InputTokens              int64   `json:"inputTokens,omitempty"`
	OutputTokens             int64   `json:"outputTokens,omitempty"`
	CachedInputTokens        int64   `json:"cachedInputTokens,omitempty"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens,omitempty"`
	ReasoningTokens          int64   `json:"reasoningTokens,omitempty"`
	RequestCount             int64   `json:"requestCount,omitempty"`
}

type spendReportResponseWire struct {
	Results []spendReportRowWire `json:"results"`
}

type spendReportRowWire struct {
	Day                      string  `json:"day,omitempty"`
	Hour                     string  `json:"hour,omitempty"`
	User                     string  `json:"user,omitempty"`
	Model                    string  `json:"model,omitempty"`
	Tag                      string  `json:"tag,omitempty"`
	Provider                 string  `json:"provider,omitempty"`
	CredentialType           string  `json:"credential_type,omitempty"`
	TotalCost                float64 `json:"total_cost"`
	MarketCost               float64 `json:"market_cost,omitempty"`
	InputTokens              int64   `json:"input_tokens,omitempty"`
	OutputTokens             int64   `json:"output_tokens,omitempty"`
	CachedInputTokens        int64   `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens,omitempty"`
	ReasoningTokens          int64   `json:"reasoning_tokens,omitempty"`
	RequestCount             int64   `json:"request_count,omitempty"`
}

func (p *Provider) GetSpendReport(ctx context.Context, params SpendReportParams) (*SpendReportResponse, error) {
	if params.StartDate == "" {
		return nil, fmt.Errorf("start date is required")
	}
	if params.EndDate == "" {
		return nil, fmt.Errorf("end date is required")
	}

	values := url.Values{}
	values.Set("start_date", params.StartDate)
	values.Set("end_date", params.EndDate)
	if params.GroupBy != "" {
		values.Set("group_by", params.GroupBy)
	}
	if params.DatePart != "" {
		values.Set("date_part", params.DatePart)
	}
	if params.UserID != "" {
		values.Set("user_id", params.UserID)
	}
	if params.Model != "" {
		values.Set("model", params.Model)
	}
	if params.Provider != "" {
		values.Set("provider", params.Provider)
	}
	if params.CredentialType != "" {
		values.Set("credential_type", params.CredentialType)
	}
	if len(params.Tags) > 0 {
		values.Set("tags", strings.Join(params.Tags, ","))
	}

	body, err := p.doOriginRequest(ctx, "/v1/report?"+values.Encode())
	if err != nil {
		return nil, err
	}

	var wire spendReportResponseWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to decode spend report response: %w", err)
	}

	response := &SpendReportResponse{Results: make([]SpendReportRow, 0, len(wire.Results))}
	for _, row := range wire.Results {
		response.Results = append(response.Results, SpendReportRow{
			Day:                      row.Day,
			Hour:                     row.Hour,
			User:                     row.User,
			Model:                    row.Model,
			Tag:                      row.Tag,
			Provider:                 row.Provider,
			CredentialType:           row.CredentialType,
			TotalCost:                row.TotalCost,
			MarketCost:               row.MarketCost,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CachedInputTokens:        row.CachedInputTokens,
			CacheCreationInputTokens: row.CacheCreationInputTokens,
			ReasoningTokens:          row.ReasoningTokens,
			RequestCount:             row.RequestCount,
		})
	}

	return response, nil
}
