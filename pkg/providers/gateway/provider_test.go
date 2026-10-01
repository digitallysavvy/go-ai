package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

func TestResolveGatewayAuthTokenDoesNotUseVercelAccessTokenEnv(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_ACCESS_TOKEN", "vercel-access-token")
	t.Setenv("VERCEL_OIDC_TOKEN", "oidc-token")

	token, authMethod, err := resolveGatewayAuthToken(context.Background(), Config{})
	if err != nil {
		t.Fatalf("resolveGatewayAuthToken error = %v", err)
	}
	if token != "oidc-token" || authMethod != "oidc" {
		t.Fatalf("auth = (%q, %q), want OIDC token", token, authMethod)
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

func TestProvider_GetAvailableModels_FiltersUnknownModelTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": [
				{
					"id": "openai/gpt-4",
					"name": "GPT-4",
					"specification": {"specificationVersion":"v4","provider":"openai.chat","modelId":"gpt-4"},
					"modelType": "language"
				},
				{
					"id": "future/model",
					"name": "Future",
					"specification": {"specificationVersion":"v4","provider":"future.chat","modelId":"f1"},
					"modelType": "hologram"
				}
			]
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	metadata, err := provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}
	if len(metadata.Models) != 1 {
		t.Fatalf("expected only known model types to remain, got %d models", len(metadata.Models))
	}
	if metadata.Models[0].ModelType != "language" {
		t.Fatalf("modelType = %q, want language", metadata.Models[0].ModelType)
	}
}

// TestProvider_GetAvailableModels_KeepsRealtimeAndEvaluationTypes mirrors the
// TS test "should include realtime models in getAvailableModels" (9dce0a7)
// plus the evaluation model type added by 6982e9d5c6's catalog work.
func TestProvider_GetAvailableModels_KeepsRealtimeAndEvaluationTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"models": [
				{
					"id": "openai/gpt-realtime-2",
					"name": "GPT Realtime",
					"specification": {"specificationVersion":"v4","provider":"openai.realtime","modelId":"gpt-realtime-2"},
					"modelType": "realtime"
				},
				{
					"id": "typesafe-ai/jev",
					"name": "JEV",
					"specification": {"specificationVersion":"v4","provider":"typesafe.evaluation","modelId":"jev"},
					"modelType": "evaluation"
				}
			]
		}`))
	}))
	defer server.Close()

	provider, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	metadata, err := provider.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatalf("GetAvailableModels() error = %v", err)
	}
	if len(metadata.Models) != 2 {
		t.Fatalf("expected realtime and evaluation entries to be kept, got %d models: %#v", len(metadata.Models), metadata.Models)
	}
	types := map[string]bool{}
	for _, m := range metadata.Models {
		types[m.ModelType] = true
	}
	if !types["realtime"] || !types["evaluation"] {
		t.Fatalf("expected realtime and evaluation model types present, got %#v", types)
	}
}

func TestProvider_SpeechAndTranscriptionModels(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	speech, err := provider.SpeechModel("x")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	if speech.Provider() != "gateway" || speech.ModelID() != "x" {
		t.Fatalf("SpeechModel metadata = %s/%s", speech.Provider(), speech.ModelID())
	}
	transcription, err := provider.TranscriptionModel("x")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	if transcription.Provider() != "gateway" || transcription.ModelID() != "x" {
		t.Fatalf("TranscriptionModel metadata = %s/%s", transcription.Provider(), transcription.ModelID())
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
	ctx := context.WithValue(context.Background(), "x-vercel-id", "req_123") //nolint:staticcheck // "x-vercel-id" is a raw-string context key by design: it's the external Vercel platform integration contract GetO11yHeaders/DoGenerate read via ctx.Value("x-vercel-id") in provider.go, not an internal key we control
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

	ctx := context.WithValue(context.Background(), "x-vercel-id", "req_from_ctx") //nolint:staticcheck // "x-vercel-id" is a raw-string context key by design: it's the external Vercel platform integration contract GetO11yHeaders/DoGenerate read via ctx.Value("x-vercel-id") in provider.go, not an internal key we control
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

func TestProvider_LanguageModel_DoGenerate_HTTPStatusErrorPreservesGatewayErrorMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After-Ms", "25")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down","type":"rate_limit_exceeded"},"generationId":"gen_123"}`))
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

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil {
		t.Fatal("expected gateway error")
	}

	var gatewayErr gatewayerrors.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gatewayErr.GetStatusCode() != http.StatusTooManyRequests || gatewayErr.GetType() != "rate_limit_exceeded" || !gatewayErr.IsRetryable() {
		t.Fatalf("unexpected gateway error metadata: status=%d type=%q retryable=%v", gatewayErr.GetStatusCode(), gatewayErr.GetType(), gatewayErr.IsRetryable())
	}
	if gatewayErr.GetGenerationID() != "gen_123" {
		t.Fatalf("generation ID = %q, want gen_123", gatewayErr.GetGenerationID())
	}

	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error cause with headers, got %T: %v", err, err)
	}
	if providerErr.ResponseHeaders["Retry-After-Ms"] != "25" {
		t.Fatalf("response headers = %+v, want Retry-After-Ms", providerErr.ResponseHeaders)
	}
}

// TestProvider_LanguageModel_DoGenerate_NestedCauseSerializesErrorBody mirrors
// ea75787 (errorToMessage: data => getErrorMessage(data)): the nested
// ProviderError cause's Message must be the full serialized error body
// instead of the generic "Gateway request failed" placeholder, and
// ResponseBody/Data must be populated from the same body.
func TestProvider_LanguageModel_DoGenerate_NestedCauseSerializesErrorBody(t *testing.T) {
	const rawBody = `{"error":{"message":"slow down","type":"rate_limit_exceeded"},"generationId":"gen_123"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(rawBody))
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

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil {
		t.Fatal("expected gateway error")
	}

	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected provider error cause, got %T: %v", err, err)
	}

	// Re-serialize the raw body the same way gatewayErrorMessage does
	// (parse then re-marshal) so the assertion isn't order-sensitive.
	var parsed interface{}
	if jsonErr := json.Unmarshal([]byte(rawBody), &parsed); jsonErr != nil {
		t.Fatalf("failed to parse fixture body: %v", jsonErr)
	}
	wantMessage, err2 := json.Marshal(parsed)
	if err2 != nil {
		t.Fatalf("failed to re-marshal fixture body: %v", err2)
	}
	if providerErr.Message != string(wantMessage) {
		t.Fatalf("nested cause Message = %q, want serialized body %q", providerErr.Message, string(wantMessage))
	}
	if providerErr.ResponseBody != rawBody {
		t.Fatalf("ResponseBody = %q, want %q", providerErr.ResponseBody, rawBody)
	}
	if providerErr.Data == nil {
		t.Fatal("expected Data to be populated with the parsed error body")
	}
}

// TestGatewayErrorMessage mirrors TS provider-utils
// createJsonErrorResponseHandler + getErrorMessage (@ai-sdk/provider): an
// empty or non-JSON body falls back to the HTTP status text
// (response.statusText), a JSON string body is used as-is, a JSON `null`
// body yields "unknown error", and any other JSON value is re-serialized.
// Regression test for a bug where a literal JSON `null` body unmarshaled
// into a Go string as "" (no error) and slipped past the empty-body check.
func TestGatewayErrorMessage(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		want       string
	}{
		{"empty body falls back to status text", "", http.StatusTooManyRequests, "Too Many Requests"},
		{"empty body with unknown status falls back to unknown error", "", 0, "unknown error"},
		{"non-JSON body falls back to status text", "not json", http.StatusBadRequest, "Bad Request"},
		{"literal null", "null", http.StatusInternalServerError, "unknown error"},
		{"whitespace-only null", " null \n", http.StatusInternalServerError, "unknown error"},
		{"JSON string body used as-is", `"boom"`, http.StatusInternalServerError, "boom"},
		{"JSON empty string body", `""`, http.StatusInternalServerError, ""},
		{"JSON object body re-serialized", `{"b":2,"a":1}`, http.StatusInternalServerError, `{"a":1,"b":2}`},
		{"JSON number body re-serialized", `42`, http.StatusInternalServerError, "42"},
		{"JSON boolean body re-serialized", `false`, http.StatusInternalServerError, "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayErrorMessage([]byte(tt.body), tt.statusCode); got != tt.want {
				t.Fatalf("gatewayErrorMessage(%q, %d) = %q, want %q", tt.body, tt.statusCode, got, tt.want)
			}
		})
	}
}

func TestLanguageModel_DoGenerate_ForwardsGatewayProviderOptions(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
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

	disallowTraining := true
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: GatewayProviderOptions{
			QuotaEntityID:          "tenant-123",
			DisallowPromptTraining: &disallowTraining,
		}.ToProviderOptions(),
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}

	providerOptions, ok := capturedBody["providerOptions"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerOptions missing or wrong type: %#v", capturedBody["providerOptions"])
	}
	gatewayOptions, ok := providerOptions["gateway"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerOptions.gateway missing or wrong type: %#v", providerOptions["gateway"])
	}
	if gatewayOptions["quotaEntityId"] != "tenant-123" {
		t.Fatalf("quotaEntityId = %#v, want tenant-123", gatewayOptions["quotaEntityId"])
	}
	if gatewayOptions["disallowPromptTraining"] != true {
		t.Fatalf("disallowPromptTraining = %#v, want true", gatewayOptions["disallowPromptTraining"])
	}
}

func TestLanguageModel_DoGenerate_MergesGatewayConfigProviderOptions(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"ok","finishReason":"stop","usage":{}}`))
	}))
	defer server.Close()

	p, err := New(Config{
		APIKey:                 "test-key",
		BaseURL:                server.URL,
		DisallowPromptTraining: true,
		QuotaEntityID:          "config-tenant",
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"gateway": map[string]interface{}{
				"quotaEntityId": "request-tenant",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}

	gatewayOptions := capturedBody["providerOptions"].(map[string]interface{})["gateway"].(map[string]interface{})
	if gatewayOptions["disallowPromptTraining"] != true {
		t.Fatalf("disallowPromptTraining = %#v, want true", gatewayOptions["disallowPromptTraining"])
	}
	if gatewayOptions["quotaEntityId"] != "request-tenant" {
		t.Fatalf("quotaEntityId = %#v, want request override", gatewayOptions["quotaEntityId"])
	}
}

// TestLanguageModel_DoGenerate_ForwardsWarnings mirrors the TS test "should
// forward warnings returned by the gateway" (gateway-language-model.test.ts).
func TestLanguageModel_DoGenerate_ForwardsWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"ok","finishReason":"stop","usage":{},"warnings":[{"type":"other","message":"from provider"}]}`))
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

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "from provider" {
		t.Fatalf("Warnings = %#v, want one warning with message 'from provider'", result.Warnings)
	}
}

// TestLanguageModel_DoGenerate_DefaultsWarningsToEmptySlice mirrors the TS
// test "should default warnings to an empty array when absent".
func TestLanguageModel_DoGenerate_DefaultsWarningsToEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if result.Warnings == nil {
		t.Fatalf("Warnings = nil, want empty (non-nil) slice")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want empty", result.Warnings)
	}
}

// TestLanguageModel_DoGenerate_MalformedWarningsFallBackToEmptySlice covers
// the Go-specific tolerant-decode requirement: a warnings field that does not
// parse as []types.Warning must not fail the whole response decode.
func TestLanguageModel_DoGenerate_MalformedWarningsFallBackToEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"ok","finishReason":"stop","usage":{},"warnings":"not-an-array"}`))
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

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v, want call to still succeed", err)
	}
	if result.Warnings == nil || len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want empty (non-nil) slice", result.Warnings)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q, want ok (rest of decode unaffected)", result.Text)
	}
}

// TestGatewayProviderOptionsHasSerializes mirrors the TS gateway-provider.test-d.ts
// `has` typing coverage: implicit-caching/reasoning/tool-use/vision plus the
// quantization helpers all serialize verbatim and in order.
func TestGatewayProviderOptionsHasSerializes(t *testing.T) {
	opts := GatewayProviderOptions{
		Has: []string{
			GatewayHasImplicitCaching,
			GatewayHasReasoning,
			GatewayHasStructuredOutput,
			GatewayHasToolUse,
			GatewayHasVision,
			GatewayHasQuantization("fp8"),
			GatewayHasNotQuantization("fp8"),
		},
	}
	got := opts.toMap()
	has, ok := got["has"].([]string)
	if !ok {
		t.Fatalf("has type = %T, want []string", got["has"])
	}
	want := []string{"implicit-caching", "reasoning", "structured-output", "tool-use", "vision", "quantization:fp8", "!quantization:fp8"}
	if len(has) != len(want) {
		t.Fatalf("has = %#v, want %#v", has, want)
	}
	for i := range want {
		if has[i] != want[i] {
			t.Fatalf("has[%d] = %q, want %q", i, has[i], want[i])
		}
	}
}

// TestGatewayProviderOptionsModelsSerializesPlainAndConditionalFallbacks
// mirrors TS gateway-provider-options d3cc6ae28d/b67b1b7463: a plain
// GatewayModel entry serializes to a bare string, and a
// GatewayConditionalModelFallback entry serializes to {model, when}.
func TestGatewayProviderOptionsModelsSerializesPlainAndConditionalFallbacks(t *testing.T) {
	confidenceBelow := 0.6
	opts := GatewayProviderOptions{
		Models: []GatewayModelFallback{
			GatewayConditionalModelFallback("openai/gpt-5.6-sol", EvaluationFallbackCondition{
				Question:        "intent",
				ConfidenceBelow: &confidenceBelow,
			}),
			GatewayModel("anthropic/claude-sonnet-5"),
		},
	}
	got := opts.toMap()
	models, ok := got["models"].([]interface{})
	if !ok || len(models) != 2 {
		t.Fatalf("models = %#v, want 2-entry []interface{}", got["models"])
	}
	first, ok := models[0].(map[string]interface{})
	if !ok || first["model"] != "openai/gpt-5.6-sol" {
		t.Fatalf("models[0] = %#v", models[0])
	}
	when, ok := first["when"].(map[string]interface{})
	if !ok || when["question"] != "intent" || when["confidenceBelow"] != 0.6 {
		t.Fatalf("models[0].when = %#v", first["when"])
	}
	if models[1] != "anthropic/claude-sonnet-5" {
		t.Fatalf("models[1] = %#v, want plain string", models[1])
	}
}

func TestGatewayProviderOptionsModelsOmittedWhenEmpty(t *testing.T) {
	got := GatewayProviderOptions{}.toMap()
	if _, ok := got["models"]; ok {
		t.Fatalf("models should be omitted when empty, got %#v", got["models"])
	}
}

// TestGatewayProviderOptionsHasOmittedWhenEmpty mirrors TS `has` being
// optional: an unset Has field must not appear in the serialized map.
func TestGatewayProviderOptionsHasOmittedWhenEmpty(t *testing.T) {
	got := GatewayProviderOptions{}.toMap()
	if _, ok := got["has"]; ok {
		t.Fatalf("has should be omitted when empty, got %#v", got["has"])
	}
}

// TestGatewayProviderOptionsIdempotencyKeySerializes covers
// providerOptions.gateway.idempotencyKey used by experimental_startBatch.
func TestGatewayProviderOptionsIdempotencyKeySerializes(t *testing.T) {
	got := GatewayProviderOptions{IdempotencyKey: "idem-abc"}.toMap()
	if got["idempotencyKey"] != "idem-abc" {
		t.Fatalf("idempotencyKey = %#v, want idem-abc", got["idempotencyKey"])
	}
	empty := GatewayProviderOptions{}.toMap()
	if _, ok := empty["idempotencyKey"]; ok {
		t.Fatalf("idempotencyKey should be omitted when empty")
	}
}

// TestGatewayProviderOptionsCachingSerializes covers providerOptions.gateway.caching.
func TestGatewayProviderOptionsCachingSerializes(t *testing.T) {
	got := GatewayProviderOptions{Caching: GatewayCachingAuto}.toMap()
	if got["caching"] != "auto" {
		t.Fatalf("caching = %#v, want auto", got["caching"])
	}
	empty := GatewayProviderOptions{}.toMap()
	if _, ok := empty["caching"]; ok {
		t.Fatalf("caching should be omitted when empty")
	}
}

// TestGatewayProviderOptionsHIPAACompliantRemoved documents that the removed
// hipaaCompliant option (cefa3b1) no longer round-trips through toMap, even
// via the raw providerOptions passthrough shape callers might still send.
func TestGatewayProviderOptionsHIPAACompliantRemoved(t *testing.T) {
	got := GatewayProviderOptions{}.toMap()
	if _, ok := got["hipaaCompliant"]; ok {
		t.Fatalf("hipaaCompliant should no longer be a typed option, got %#v", got["hipaaCompliant"])
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

// TestIsGatewayJSONDecodeError covers the classifier used by gatewayUnknownError
// to distinguish permanent JSON decode failures from transient errors (90192f1).
func TestIsGatewayJSONDecodeError(t *testing.T) {
	var syntaxErr error
	if err := json.Unmarshal([]byte("{"), &struct{}{}); err != nil {
		syntaxErr = err
	}
	if syntaxErr == nil {
		t.Fatal("expected a JSON syntax error from malformed input")
	}
	wrapped := fmt.Errorf("failed to decode JSON response: %w", syntaxErr)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrapped decode error", err: wrapped, want: true},
		{name: "raw syntax error", err: syntaxErr, want: true},
		{name: "generic network error", err: errors.New("connection reset by peer"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGatewayJSONDecodeError(tt.err); got != tt.want {
				t.Fatalf("isGatewayJSONDecodeError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestLanguageModel_DoGenerate_MalformedJSONBodyNotRetryable covers the
// "JSON decode failures must NOT be retryable" half of 90192f1: a 200
// response whose body is not valid JSON at all (not just a malformed
// "warnings" field) must surface a non-retryable gateway error.
func TestLanguageModel_DoGenerate_MalformedJSONBodyNotRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
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

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil {
		t.Fatal("expected an error for a non-JSON response body")
	}
	var gatewayErr gatewayerrors.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gatewayErr.IsRetryable() {
		t.Fatal("a JSON decode failure must not be retryable")
	}
}

// TestLanguageModel_DoGenerate_TruncatedBodyIsRetryable covers the
// "transient network errors while reading successful response bodies are
// retryable" half of 90192f1: a connection that closes before delivering
// the promised Content-Length must surface a retryable gateway error.
func TestLanguageModel_DoGenerate_TruncatedBodyIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter does not support hijacking")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack error: %v", err)
		}
		defer conn.Close() //nolint:errcheck
		// Declare a Content-Length far larger than the bytes actually sent,
		// then close the connection: the client's body read fails with a
		// truncated-body error even though the status line was 200 OK.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 4096\r\n\r\n")
		_, _ = buf.WriteString(`{"text":"partial`)
		_ = buf.Flush()
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

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err == nil {
		t.Fatal("expected an error for a truncated response body")
	}
	var gatewayErr gatewayerrors.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if !gatewayErr.IsRetryable() {
		t.Fatal("a truncated body read error should be retryable")
	}
}
