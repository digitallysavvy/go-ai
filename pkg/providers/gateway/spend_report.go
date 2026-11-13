package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	Day            string  `json:"day,omitempty"`
	Hour           string  `json:"hour,omitempty"`
	User           string  `json:"user,omitempty"`
	Model          string  `json:"model,omitempty"`
	Tag            string  `json:"tag,omitempty"`
	Provider       string  `json:"provider,omitempty"`
	CredentialType string  `json:"credential_type,omitempty"`
	TotalCost      float64 `json:"total_cost"`
	MarketCost     float64 `json:"market_cost,omitempty"`
	// The counts are z.number() in TS: decoded as float64 so a fractional
	// value parses, then truncated into the v0.5.2 integer fields (S1).
	InputTokens              float64 `json:"input_tokens,omitempty"`
	OutputTokens             float64 `json:"output_tokens,omitempty"`
	CachedInputTokens        float64 `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens,omitempty"`
	ReasoningTokens          float64 `json:"reasoning_tokens,omitempty"`
	RequestCount             float64 `json:"request_count,omitempty"`
}

// validateSpendReportRows applies the TS row schema's required and enum
// members: total_cost must be a number and credential_type, when present,
// must be "byok" or "system".
func validateSpendReportRows(body []byte) error {
	var raw struct {
		Results []map[string]json.RawMessage `json:"results"`
	}
	if err := jsonexact.Unmarshal(body, &raw); err != nil {
		return nil // reported by the typed decode
	}
	for i, row := range raw.Results {
		// z.optional fields: present means the right JSON type; null fails.
		for _, key := range []string{"day", "hour", "user", "model", "tag", "provider"} {
			if v, ok := row[key]; ok && (len(v) == 0 || v[0] != '"') {
				return fmt.Errorf("Invalid JSON response: results[%d].%s must be a string", i, key) //nolint:staticcheck // mirrors TS wording
			}
		}
		for _, key := range []string{"market_cost", "input_tokens", "output_tokens", "cached_input_tokens", "cache_creation_input_tokens", "reasoning_tokens", "request_count"} {
			if v, ok := row[key]; ok {
				var f float64
				if json.Unmarshal(v, &f) != nil || string(v) == "null" {
					return fmt.Errorf("Invalid JSON response: results[%d].%s must be a number", i, key) //nolint:staticcheck // mirrors TS wording
				}
			}
		}
		cost, ok := row["total_cost"]
		if !ok || len(cost) == 0 || cost[0] == '"' || string(cost) == "null" {
			return fmt.Errorf("Invalid JSON response: results[%d].total_cost must be a number", i) //nolint:staticcheck // mirrors TS wording
		}
		if ct, ok := row["credential_type"]; ok {
			var s string
			if json.Unmarshal(ct, &s) != nil || (s != "byok" && s != "system") {
				return fmt.Errorf("Invalid JSON response: results[%d].credential_type must be byok or system", i) //nolint:staticcheck // mirrors TS wording
			}
		}
	}
	return nil
}

func (p *Provider) GetSpendReport(ctx context.Context, params SpendReportParams) (*SpendReportResponse, error) {
	// Same order and escaping as the TS URLSearchParams.set calls.
	param := func(key, value string) string {
		return internalhttp.EncodeQuery(map[string]string{key: value})
	}
	query := []string{
		param("start_date", params.StartDate),
		param("end_date", params.EndDate),
	}
	for _, kv := range [][2]string{
		{"group_by", params.GroupBy},
		{"date_part", params.DatePart},
		{"user_id", params.UserID},
		{"model", params.Model},
		{"provider", params.Provider},
		{"credential_type", params.CredentialType},
	} {
		if kv[1] != "" {
			query = append(query, param(kv[0], kv[1]))
		}
	}
	if len(params.Tags) > 0 {
		query = append(query, param("tags", strings.Join(params.Tags, ",")))
	}

	body, err := p.doOriginRequest(ctx, "/v1/report?"+strings.Join(query, "&"))
	if err != nil {
		return nil, err
	}

	// TS createJsonResponseHandler(gatewaySpendReportResponseSchema): a body
	// that is not JSON or fails the schema is the APICallError "Invalid JSON
	// response", which asGatewayError turns into a GatewayResponseError.
	invalid := func() error {
		return p.handleErrorWithContext(ctx, providerutils.InvalidJSONResponseError("gateway", http.StatusOK, http.Header{}, body, nil))
	}
	var wire spendReportResponseWire
	if err := jsonexact.Unmarshal(body, &wire); err != nil {
		return nil, invalid()
	}
	// TS schema: `results` must be an array.
	if wire.Results == nil {
		return nil, invalid()
	}
	if err := validateSpendReportRows(body); err != nil {
		return nil, invalid()
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
			InputTokens:              int64(row.InputTokens),
			OutputTokens:             int64(row.OutputTokens),
			CachedInputTokens:        int64(row.CachedInputTokens),
			CacheCreationInputTokens: int64(row.CacheCreationInputTokens),
			ReasoningTokens:          int64(row.ReasoningTokens),
			RequestCount:             int64(row.RequestCount),
		})
	}

	return response, nil
}
