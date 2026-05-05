package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with API key",
			config: Config{
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name:    "missing API key",
			config:  Config{},
			wantErr: false,
		},
		{
			name: "zero data retention enabled",
			config: Config{
				APIKey:            "test-api-key",
				ZeroDataRetention: true,
			},
			wantErr: false,
		},
		{
			name: "custom base URL",
			config: Config{
				APIKey:  "test-api-key",
				BaseURL: "https://custom.gateway.example.com",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AI_GATEWAY_API_KEY", "")
			t.Setenv("VERCEL_OIDC_TOKEN", "")
			provider, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && provider == nil {
				t.Error("New() returned nil provider")
			}
		})
	}
}

func TestProvider_Name(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	if got := provider.Name(); got != "gateway" {
		t.Errorf("Name() = %v, want %v", got, "gateway")
	}
}

func TestNew_UsesOIDCTokenWhenNoAPIKeyProvided(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "oidc-token")

	provider, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.headers["Authorization"] != "" {
		t.Fatalf("Authorization should not be set statically, got %q", provider.headers["Authorization"])
	}
	if provider.headers["ai-gateway-auth-method"] != "" {
		t.Fatalf("auth method should not be set statically, got %q", provider.headers["ai-gateway-auth-method"])
	}
}

func TestNew_PrefersAPIKeyOverOIDCToken(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "env-api-key")
	t.Setenv("VERCEL_OIDC_TOKEN", "oidc-token")

	provider, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if provider.headers["Authorization"] != "" {
		t.Fatalf("Authorization should not be set statically, got %q", provider.headers["Authorization"])
	}
	if provider.headers["ai-gateway-auth-method"] != "" {
		t.Fatalf("auth method should not be set statically, got %q", provider.headers["ai-gateway-auth-method"])
	}
}

func TestNew_RequiresAPIKeyOrOIDCToken(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")

	provider, err := New(Config{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.GetSpendReport(context.Background(), SpendReportParams{
		StartDate: "2026-03-01",
		EndDate:   "2026-03-25",
	})
	if err == nil {
		t.Fatal("expected request error")
	}
	var authErr *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected GatewayAuthenticationError, got %T: %v", err, err)
	}
}

func TestProvider_AuthResolvedPerRequest(t *testing.T) {
	var authHeader string
	var authMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		authMethod = r.Header.Get("ai-gateway-auth-method")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"balance":"1000","total_used":"250"}`))
	}))
	defer server.Close()

	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "first-token")

	provider, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Setenv("VERCEL_OIDC_TOKEN", "second-token")

	_, err = provider.GetCredits(context.Background())
	if err != nil {
		t.Fatalf("GetCredits() error = %v", err)
	}
	if authHeader != "Bearer second-token" {
		t.Fatalf("Authorization = %q", authHeader)
	}
	if authMethod != "oidc" {
		t.Fatalf("auth method = %q", authMethod)
	}
}

func TestProvider_LanguageModel(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name    string
		modelID string
		wantErr bool
	}{
		{
			name:    "valid model ID",
			modelID: "openai/gpt-4",
			wantErr: false,
		},
		{
			name:    "empty model ID",
			modelID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := provider.LanguageModel(tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Errorf("LanguageModel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && model == nil {
				t.Error("LanguageModel() returned nil model")
			}
			if !tt.wantErr && model.ModelID() != tt.modelID {
				t.Errorf("LanguageModel().ModelID() = %v, want %v", model.ModelID(), tt.modelID)
			}
		})
	}
}

func TestProvider_GetAvailableModels(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config" {
			t.Errorf("Expected path /config, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": [
				{
					"id": "openai/gpt-4",
					"name": "GPT-4",
					"description": "flagship",
					"pricing": {
						"input": "0.00001",
						"output": "0.00003",
						"input_cache_read": "0.000005",
						"input_cache_write": "0.000015"
					},
					"specification": {
						"specificationVersion": "v4",
						"provider": "openai.chat",
						"modelId": "gpt-4"
					},
					"modelType": "language"
				}
			]
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	metadata, err := provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	if len(metadata.Models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(metadata.Models))
	}
	if metadata.Models[0].Name != "GPT-4" {
		t.Errorf("Expected model name GPT-4, got %s", metadata.Models[0].Name)
	}
	if metadata.Models[0].Specification.Provider != "openai.chat" {
		t.Errorf("Expected provider openai.chat, got %s", metadata.Models[0].Specification.Provider)
	}
	if metadata.Models[0].Pricing == nil || metadata.Models[0].Pricing.CachedInputTokens != "0.000005" {
		t.Fatalf("unexpected pricing: %#v", metadata.Models[0].Pricing)
	}
}

func TestProvider_GetCredits(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/credits" {
			t.Errorf("Expected path /v1/credits, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"balance": "1000",
			"total_used": "250"
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL + "/v3/ai",
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	credits, err := provider.GetCredits(context.Background())
	if err != nil {
		t.Fatalf("GetCredits() error = %v", err)
	}

	if credits.Balance != "1000" {
		t.Errorf("Expected balance 1000, got %s", credits.Balance)
	}

	if credits.TotalUsed != "250" {
		t.Errorf("Expected total used 250, got %s", credits.TotalUsed)
	}

	payload, err := json.Marshal(credits)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(payload) != `{"balance":"1000","totalUsed":"250"}` {
		t.Fatalf("Marshal payload = %s", payload)
	}
}

func TestProvider_MetadataCache(t *testing.T) {
	callCount := 0

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models": []}`))
	}))
	defer server.Close()

	provider, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// First call should hit the server
	_, err = provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	// Second call should use cache
	_, err = provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}

	// Should only have called the server once (second call used cache)
	if callCount != 1 {
		t.Errorf("Expected 1 server call (cached second call), got %d", callCount)
	}
}

func TestGetO11yHeaders(t *testing.T) {
	// Set environment variables
	t.Setenv("VERCEL_DEPLOYMENT_ID", "test-deployment")
	t.Setenv("VERCEL_ENV", "production")
	t.Setenv("VERCEL_REGION", "us-east-1")
	t.Setenv("VERCEL_PROJECT_ID", "proj-abc123")

	headers := GetO11yHeaders(context.Background())

	if headers.DeploymentID != "test-deployment" {
		t.Errorf("Expected DeploymentID test-deployment, got %s", headers.DeploymentID)
	}

	if headers.Environment != "production" {
		t.Errorf("Expected Environment production, got %s", headers.Environment)
	}

	if headers.Region != "us-east-1" {
		t.Errorf("Expected Region us-east-1, got %s", headers.Region)
	}

	if headers.ProjectID != "proj-abc123" {
		t.Errorf("Expected ProjectID proj-abc123, got %s", headers.ProjectID)
	}
}

func TestProvider_GetSpendReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/report" {
			t.Fatalf("path = %s, want /v1/report", r.URL.Path)
		}
		if got := r.URL.Query().Get("start_date"); got != "2026-03-01" {
			t.Fatalf("start_date = %q", got)
		}
		if got := r.URL.Query().Get("end_date"); got != "2026-03-25" {
			t.Fatalf("end_date = %q", got)
		}
		if got := r.URL.Query().Get("tags"); got != "production,api" {
			t.Fatalf("tags = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"day":"2026-03-01","total_cost":12.5,"request_count":42}]}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v3/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	report, err := provider.GetSpendReport(context.Background(), SpendReportParams{
		StartDate: "2026-03-01",
		EndDate:   "2026-03-25",
		Tags:      []string{"production", "api"},
	})
	if err != nil {
		t.Fatalf("GetSpendReport error = %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(report.Results))
	}
	if report.Results[0].TotalCost != 12.5 {
		t.Fatalf("total cost = %v", report.Results[0].TotalCost)
	}
	if report.Results[0].RequestCount != 42 {
		t.Fatalf("request count = %d", report.Results[0].RequestCount)
	}

	payload, err := json.Marshal(report.Results[0])
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(payload) != `{"day":"2026-03-01","totalCost":12.5,"requestCount":42}` {
		t.Fatalf("Marshal payload = %s", payload)
	}
}

func TestProvider_GetSpendReport_OmitsEmptyOptionalParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("group_by") || r.URL.Query().Has("tags") {
			t.Fatal("unexpected optional params present")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v3/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	_, err = provider.GetSpendReport(context.Background(), SpendReportParams{
		StartDate: "2026-03-01",
		EndDate:   "2026-03-25",
	})
	if err != nil {
		t.Fatalf("GetSpendReport error = %v", err)
	}
}

func TestProvider_GetGenerationInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/generation" {
			t.Fatalf("path = %s, want /v1/generation", r.URL.Path)
		}
		if got := r.URL.Query().Get("id"); got != "gen_123" {
			t.Fatalf("id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"gen_123","model":"gpt-4","provider_name":"openai","finish_reason":"stop","total_cost":1.25,"upstream_inference_cost":1.1,"usage":1.25,"is_byok":false,"streamed":true,"created_at":"2024-01-01T00:00:00.000Z","native_tokens_prompt":100,"native_tokens_completion":50,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"latency":200,"generation_time":1500,"billable_web_search_calls":0}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v3/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	info, err := provider.GetGenerationInfo(context.Background(), GenerationInfoParams{ID: "gen_123"})
	if err != nil {
		t.Fatalf("GetGenerationInfo error = %v", err)
	}
	if info.ID != "gen_123" || info.Model != "gpt-4" || info.ProviderName != "openai" {
		t.Fatalf("unexpected info: %#v", info)
	}
	if info.PromptTokens != 100 || info.CompletionTokens != 50 {
		t.Fatalf("unexpected token counts: %#v", info)
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if string(payload) != `{"id":"gen_123","model":"gpt-4","providerName":"openai","finishReason":"stop","totalCost":1.25,"upstreamInferenceCost":1.1,"usage":1.25,"isByok":false,"streamed":true,"createdAt":"2024-01-01T00:00:00.000Z","promptTokens":100,"completionTokens":50,"reasoningTokens":0,"cachedTokens":0,"cacheCreationTokens":0,"latency":200,"generationTime":1500,"billableWebSearchCalls":0}` {
		t.Fatalf("Marshal payload = %s", payload)
	}
}

func TestProvider_GetGenerationInfo_RequiresID(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	_, err = provider.GetGenerationInfo(context.Background(), GenerationInfoParams{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProvider_GetSpendReport_PropagatesGatewayErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_exceeded"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v3/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	_, err = provider.GetSpendReport(context.Background(), SpendReportParams{
		StartDate: "2026-03-01",
		EndDate:   "2026-03-25",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var rateErr *gatewayerrors.GatewayRateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected GatewayRateLimitError, got %T: %v", err, err)
	}
	if rateErr.GetStatusCode() != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rateErr.GetStatusCode())
	}
	if rateErr.GetType() != "rate_limit_exceeded" {
		t.Fatalf("error type = %q", rateErr.GetType())
	}
	if rateErr.Error() != "Rate limit exceeded" {
		t.Fatalf("message = %q", rateErr.Error())
	}
}

func TestProvider_GetGenerationInfo_PropagatesGatewayErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Model not found","type":"model_not_found"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v3/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	_, err = provider.GetGenerationInfo(context.Background(), GenerationInfoParams{ID: "gen_missing"})
	if err == nil {
		t.Fatal("expected error")
	}

	var modelErr *gatewayerrors.GatewayModelNotFoundError
	if !errors.As(err, &modelErr) {
		t.Fatalf("expected GatewayModelNotFoundError, got %T: %v", err, err)
	}
	if modelErr.GetStatusCode() != http.StatusNotFound {
		t.Fatalf("status = %d", modelErr.GetStatusCode())
	}
	if modelErr.GetType() != "model_not_found" {
		t.Fatalf("error type = %q", modelErr.GetType())
	}
	if modelErr.Error() != "Model not found" {
		t.Fatalf("message = %q", modelErr.Error())
	}
}

func TestProvider_GetAvailableModels_PropagatesGatewayErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_exceeded"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	_, err = provider.GetAvailableModels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	var rateErr *gatewayerrors.GatewayRateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected GatewayRateLimitError, got %T: %v", err, err)
	}
}

func TestProvider_GetCredits_PropagatesGatewayErrorDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication failed","type":"authentication_error"}}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	_, err = provider.GetCredits(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	var authErr *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected GatewayAuthenticationError, got %T: %v", err, err)
	}
}

func TestGetO11yHeaders_RequestIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "x-vercel-id", "req_123")
	headers := GetO11yHeaders(ctx)
	if headers.RequestID != "req_123" {
		t.Fatalf("RequestID = %q", headers.RequestID)
	}
}

func TestProvider_LanguageModel_DoGenerate_IncludesRequestIDHeader(t *testing.T) {
	var requestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID = r.Header.Get("ai-o11y-request-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"ok","finishReason":"stop","usage":{}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}

	model, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	ctx := context.WithValue(context.Background(), "x-vercel-id", "req_from_ctx")
	_, err = model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{
				Role: "user",
				Content: []types.ContentPart{
					types.TextContent{Text: "hello"},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if requestID != "req_from_ctx" {
		t.Fatalf("request ID header = %q", requestID)
	}
}

func TestAddO11yHeaders(t *testing.T) {
	headers := make(map[string]string)
	o11y := O11yHeaders{
		DeploymentID: "test-deployment",
		Environment:  "production",
		Region:       "us-east-1",
		RequestID:    "req-123",
		ProjectID:    "proj-xyz",
	}

	AddO11yHeaders(headers, o11y)

	expected := map[string]string{
		"ai-o11y-deployment-id": "test-deployment",
		"ai-o11y-environment":   "production",
		"ai-o11y-region":        "us-east-1",
		"ai-o11y-request-id":    "req-123",
		"ai-o11y-project-id":    "proj-xyz",
	}

	for k, v := range expected {
		if headers[k] != v {
			t.Errorf("Expected header %s = %s, got %s", k, v, headers[k])
		}
	}
}

// TestNew_WithProjectID verifies that WithProjectID option sets the project ID config field.
func TestNew_WithProjectID(t *testing.T) {
	projectID := "my-project-123"
	p, err := New(Config{APIKey: "test-key"}, WithProjectID(projectID))
	if err != nil {
		t.Fatalf("New() with WithProjectID error: %v", err)
	}
	if p == nil {
		t.Fatal("New() returned nil provider")
	}
	if p.config.ProjectID == nil {
		t.Fatal("Expected ProjectID to be set, got nil")
	}
	if *p.config.ProjectID != projectID {
		t.Errorf("Expected ProjectID %q, got %q", projectID, *p.config.ProjectID)
	}
}

// TestProjectIDHeader_PresentWhenSet verifies that the ai-o11y-project-id header is
// injected on all requests when ProjectID is configured. (GW-T15)
func TestProjectIDHeader_PresentWhenSet(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	projectID := "proj-test-456"
	p, err := New(Config{
		APIKey:    "test-key",
		BaseURL:   server.URL,
		ProjectID: &projectID,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != projectID {
		t.Errorf("Expected ai-o11y-project-id header %q, got %q", projectID, capturedHeader)
	}
}

// TestProjectIDHeader_PresentWhenSetViaOption verifies that WithProjectID() injects the
// header just as setting Config.ProjectID does. (GW-T15 via functional option)
func TestProjectIDHeader_PresentWhenSetViaOption(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	projectID := "proj-option-789"
	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	}, WithProjectID(projectID))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != projectID {
		t.Errorf("Expected ai-o11y-project-id header %q, got %q", projectID, capturedHeader)
	}
}

// TestProjectIDHeader_AbsentWhenNotSet verifies that the ai-o11y-project-id header is
// NOT present when ProjectID is not configured. (GW-T16)
func TestProjectIDHeader_AbsentWhenNotSet(t *testing.T) {
	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("ai-o11y-project-id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	// Ensure VERCEL_PROJECT_ID is not set in the environment
	t.Setenv("VERCEL_PROJECT_ID", "")

	p, err := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		// ProjectID intentionally not set
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, _ = p.GetAvailableModels(context.Background())

	if capturedHeader != "" {
		t.Errorf("Expected ai-o11y-project-id header to be absent, got %q", capturedHeader)
	}
}
