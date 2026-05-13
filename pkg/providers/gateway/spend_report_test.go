package gateway

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGetSpendReportValidatesRequiredFields(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://example.com/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, err := p.GetSpendReport(context.Background(), SpendReportParams{EndDate: "2026-05-01"}); err == nil || err.Error() != "start date is required" {
		t.Fatalf("expected start date error, got %v", err)
	}
	if _, err := p.GetSpendReport(context.Background(), SpendReportParams{StartDate: "2026-05-01"}); err == nil || err.Error() != "end date is required" {
		t.Fatalf("expected end date error, got %v", err)
	}
}

func TestGetSpendReportBuildsQueryAndMapsSnakeCase(t *testing.T) {
	var seenPath string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"day":"2026-05-10","credential_type":"api-key","total_cost":1.25,"market_cost":0.95,"input_tokens":11,"output_tokens":12,"cached_input_tokens":3,"cache_creation_input_tokens":4,"reasoning_tokens":2,"request_count":5}]}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	report, err := p.GetSpendReport(context.Background(), SpendReportParams{
		StartDate:      "2026-05-01",
		EndDate:        "2026-05-11",
		GroupBy:        "day",
		DatePart:       "day",
		UserID:         "u1",
		Model:          "gpt-4.1",
		Provider:       "openai",
		CredentialType: "api-key",
		Tags:           []string{"prod", "core"},
	})
	if err != nil {
		t.Fatalf("GetSpendReport error = %v", err)
	}

	if !strings.HasPrefix(seenPath, "/v1/report?") {
		t.Fatalf("path = %q", seenPath)
	}
	for _, want := range []string{
		"start_date=2026-05-01",
		"end_date=2026-05-11",
		"group_by=day",
		"date_part=day",
		"user_id=u1",
		"model=gpt-4.1",
		"provider=openai",
		"credential_type=api-key",
		"tags=prod%2Ccore",
	} {
		if !strings.Contains(seenPath, want) {
			t.Fatalf("query missing %q: %s", want, seenPath)
		}
	}

	if len(report.Results) != 1 {
		t.Fatalf("results len = %d", len(report.Results))
	}
	row := report.Results[0]
	if row.CredentialType != "api-key" || row.TotalCost != 1.25 || row.InputTokens != 11 || row.CachedInputTokens != 3 || row.CacheCreationInputTokens != 4 || row.ReasoningTokens != 2 || row.RequestCount != 5 {
		t.Fatalf("row mapping mismatch: %#v", row)
	}
}

func TestGetSpendReportDecodeError(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad-json`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, err := p.GetSpendReport(context.Background(), SpendReportParams{StartDate: "2026-05-01", EndDate: "2026-05-02"}); err == nil || !strings.Contains(err.Error(), "failed to decode spend report response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}
