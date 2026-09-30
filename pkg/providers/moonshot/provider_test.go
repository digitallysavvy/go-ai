package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestNewProvider tests provider creation
func TestNewProvider(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name: "valid config with API key",
			config: Config{
				APIKey: "test-key",
			},
			expectErr: false,
		},
		{
			name: "valid config with custom base URL",
			config: Config{
				APIKey:  "test-key",
				BaseURL: "https://custom.api.moonshot.cn/v1",
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := New(tt.config)
			if provider == nil {
				t.Fatal("expected provider to be created")
			}

			if provider.Name() != "moonshot" {
				t.Errorf("expected provider name 'moonshot', got %s", provider.Name())
			}

			if provider.Client() == nil {
				t.Error("expected HTTP client to be initialized")
			}
		})
	}
}

// TestLanguageModelValidation tests model ID validation
func TestLanguageModelValidation(t *testing.T) {
	provider := New(Config{APIKey: "test-key"})

	tests := []struct {
		name      string
		modelID   string
		expectErr bool
	}{
		{"moonshot-v1-8k", "moonshot-v1-8k", false},
		{"moonshot-v1-32k", "moonshot-v1-32k", false},
		{"moonshot-v1-128k", "moonshot-v1-128k", false},
		{"kimi-k2", "kimi-k2", false},
		{"kimi-k2.5", "kimi-k2.5", false},
		{"kimi-k2-thinking", "kimi-k2-thinking", false},
		{"kimi-k2-thinking-turbo", "kimi-k2-thinking-turbo", false},
		{"kimi-k2-turbo", "kimi-k2-turbo", false},
		// TS accepts any model ID (MoonshotAIChatModelId includes string).
		{"unlisted-model", "kimi-k9-preview", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, err := provider.LanguageModel(tt.modelID)

			if tt.expectErr {
				if err == nil {
					t.Error("expected error for invalid model ID")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if model == nil {
				t.Fatal("expected model to be created")
			}

			if model.ModelID() != tt.modelID {
				t.Errorf("expected model ID %s, got %s", tt.modelID, model.ModelID())
			}

			if model.Provider() != "moonshot" {
				t.Errorf("expected provider 'moonshot', got %s", model.Provider())
			}

			if model.SpecificationVersion() != "v3" {
				t.Errorf("expected specification version 'v3', got %s", model.SpecificationVersion())
			}

			if !model.SupportsTools() {
				t.Error("expected model to support tools")
			}

			if !model.SupportsStructuredOutput() {
				t.Error("expected model to support structured output")
			}
		})
	}
}

// TestConfigValidation tests configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name: "valid config",
			config: Config{
				APIKey: "test-key",
			},
			expectErr: false,
		},
		{
			name: "missing API key",
			config: Config{
				APIKey: "",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectErr && err == nil {
				t.Error("expected validation error")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// TestMoonshotUsageConversion tests basic usage conversion
func TestMoonshotUsageConversion(t *testing.T) {
	usage := MoonshotUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	result := ConvertMoonshotUsage(usage)

	if result.InputTokens == nil || *result.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %v", result.InputTokens)
	}

	if result.OutputTokens == nil || *result.OutputTokens != 50 {
		t.Errorf("expected output tokens 50, got %v", result.OutputTokens)
	}

	if result.TotalTokens == nil || *result.TotalTokens != 150 {
		t.Errorf("expected total tokens 150, got %v", result.TotalTokens)
	}
}

// TestMoonshotUsageWithCaching tests usage conversion with cache tokens
func TestMoonshotUsageWithCaching(t *testing.T) {
	tests := []struct {
		name              string
		usage             MoonshotUsage
		expectedCacheRead int64
		expectedNoCache   int64
	}{
		{
			name: "top-level cached_tokens",
			usage: MoonshotUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
				CachedTokens:     intPtr(30),
			},
			expectedCacheRead: 30,
			expectedNoCache:   70,
		},
		{
			name: "nested cached_tokens in prompt_tokens_details",
			usage: MoonshotUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: intPtr(40),
				},
			},
			expectedCacheRead: 40,
			expectedNoCache:   60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConvertMoonshotUsage(tt.usage)

			if result.InputDetails == nil {
				t.Fatal("expected input details to be set")
			}

			if result.InputDetails.CacheReadTokens == nil || *result.InputDetails.CacheReadTokens != tt.expectedCacheRead {
				t.Errorf("expected cache read tokens %d, got %v", tt.expectedCacheRead, result.InputDetails.CacheReadTokens)
			}

			if result.InputDetails.NoCacheTokens == nil || *result.InputDetails.NoCacheTokens != tt.expectedNoCache {
				t.Errorf("expected no-cache tokens %d, got %v", tt.expectedNoCache, result.InputDetails.NoCacheTokens)
			}

			if result.Raw == nil {
				t.Fatal("expected full decoded usage object in Raw")
			}
			if result.Raw["prompt_tokens"] != float64(100) {
				t.Errorf("expected raw prompt_tokens 100, got %v", result.Raw["prompt_tokens"])
			}
		})
	}
}

// TestMoonshotUsageWithThinking tests usage conversion with reasoning tokens
func TestMoonshotUsageWithThinking(t *testing.T) {
	usage := MoonshotUsage{
		PromptTokens:     100,
		CompletionTokens: 100,
		TotalTokens:      200,
		CompletionTokensDetails: &CompletionTokensDetails{
			ReasoningTokens: intPtr(60),
		},
	}

	result := ConvertMoonshotUsage(usage)

	if result.OutputDetails == nil {
		t.Fatal("expected output details to be set")
	}

	if result.OutputDetails.ReasoningTokens == nil || *result.OutputDetails.ReasoningTokens != 60 {
		t.Errorf("expected reasoning tokens 60, got %v", result.OutputDetails.ReasoningTokens)
	}

	if result.OutputDetails.TextTokens == nil || *result.OutputDetails.TextTokens != 40 {
		t.Errorf("expected text tokens 40, got %v", result.OutputDetails.TextTokens)
	}

	details, ok := result.Raw["completion_tokens_details"].(map[string]interface{})
	if !ok || details["reasoning_tokens"] != float64(60) {
		t.Errorf("expected raw completion_tokens_details.reasoning_tokens 60, got %v", result.Raw["completion_tokens_details"])
	}
}

// TestMoonshotUsageClampsNegativeTextTokens verifies that when reasoning
// tokens exceed completion tokens (a provider-side inconsistency), text
// tokens are clamped to zero rather than going negative.
func TestMoonshotUsageClampsNegativeTextTokens(t *testing.T) {
	usage := MoonshotUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		CompletionTokensDetails: &CompletionTokensDetails{
			ReasoningTokens: intPtr(9),
		},
	}

	result := ConvertMoonshotUsage(usage)

	if result.OutputDetails == nil || result.OutputDetails.TextTokens == nil {
		t.Fatal("expected output details with text tokens")
	}
	if *result.OutputDetails.TextTokens != 0 {
		t.Errorf("expected clamped text tokens 0, got %d", *result.OutputDetails.TextTokens)
	}
}

// TestConvertMoonshotUsageRawPreservesUnknownFields verifies Raw contains the
// full decoded usage object, including fields the SDK doesn't model.
func TestConvertMoonshotUsageRawPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 100,
		"completion_tokens": 50,
		"total_tokens": 150,
		"cached_tokens": 20,
		"provider_usage_id": "usage-123",
		"prompt_tokens_details": {"cached_tokens": 20, "cache_type": "ephemeral"},
		"completion_tokens_details": {"reasoning_tokens": 10, "billable_tokens": 42}
	}`)

	result, err := ConvertMoonshotUsageRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Raw["provider_usage_id"] != "usage-123" {
		t.Errorf("expected provider_usage_id preserved, got %v", result.Raw["provider_usage_id"])
	}
	details, ok := result.Raw["prompt_tokens_details"].(map[string]interface{})
	if !ok || details["cache_type"] != "ephemeral" {
		t.Errorf("expected prompt_tokens_details.cache_type preserved, got %v", result.Raw["prompt_tokens_details"])
	}
	completionDetails, ok := result.Raw["completion_tokens_details"].(map[string]interface{})
	if !ok || completionDetails["billable_tokens"] != float64(42) {
		t.Errorf("expected completion_tokens_details.billable_tokens preserved, got %v", result.Raw["completion_tokens_details"])
	}
	if result.InputDetails == nil || result.InputDetails.CacheReadTokens == nil || *result.InputDetails.CacheReadTokens != 20 {
		t.Errorf("expected cache read tokens 20, got %v", result.InputDetails)
	}
}

// TestConvertMoonshotUsageRawRejectsWrongFieldType verifies that a known
// numeric usage field with the wrong JSON type produces an error, mirroring
// TS's Zod-validated tokenUsageSchema rejecting malformed usage payloads.
func TestConvertMoonshotUsageRawRejectsWrongFieldType(t *testing.T) {
	raw := []byte(`{"prompt_tokens": "100", "completion_tokens": 50, "total_tokens": 150}`)
	if _, err := ConvertMoonshotUsageRaw(raw); err == nil {
		t.Fatal("expected an error for a string prompt_tokens value")
	}
}

// TestMoonshotUsageComplete tests usage conversion with all fields
func TestMoonshotUsageComplete(t *testing.T) {
	usage := MoonshotUsage{
		PromptTokens:     100,
		CompletionTokens: 100,
		TotalTokens:      200,
		CachedTokens:     intPtr(30),
		CompletionTokensDetails: &CompletionTokensDetails{
			ReasoningTokens: intPtr(60),
		},
	}

	result := ConvertMoonshotUsage(usage)

	// Check input tokens and details
	if result.InputTokens == nil || *result.InputTokens != 100 {
		t.Errorf("expected input tokens 100, got %v", result.InputTokens)
	}

	if result.InputDetails == nil {
		t.Fatal("expected input details to be set")
	}

	if result.InputDetails.CacheReadTokens == nil || *result.InputDetails.CacheReadTokens != 30 {
		t.Errorf("expected cache read tokens 30, got %v", result.InputDetails.CacheReadTokens)
	}

	if result.InputDetails.NoCacheTokens == nil || *result.InputDetails.NoCacheTokens != 70 {
		t.Errorf("expected no-cache tokens 70, got %v", result.InputDetails.NoCacheTokens)
	}

	// Check output tokens and details
	if result.OutputTokens == nil || *result.OutputTokens != 100 {
		t.Errorf("expected output tokens 100, got %v", result.OutputTokens)
	}

	if result.OutputDetails == nil {
		t.Fatal("expected output details to be set")
	}

	if result.OutputDetails.ReasoningTokens == nil || *result.OutputDetails.ReasoningTokens != 60 {
		t.Errorf("expected reasoning tokens 60, got %v", result.OutputDetails.ReasoningTokens)
	}

	if result.OutputDetails.TextTokens == nil || *result.OutputDetails.TextTokens != 40 {
		t.Errorf("expected text tokens 40, got %v", result.OutputDetails.TextTokens)
	}

	// Check total tokens
	if result.TotalTokens == nil || *result.TotalTokens != 200 {
		t.Errorf("expected total tokens 200, got %v", result.TotalTokens)
	}
}

// TestFinishReasonMapping tests finish reason mapping
func TestFinishReasonMapping(t *testing.T) {
	tests := []struct {
		name           string
		moonshotReason string
		expected       types.FinishReason
	}{
		{"stop", "stop", types.FinishReasonStop},
		{"length", "length", types.FinishReasonLength},
		{"tool_calls", "tool_calls", types.FinishReasonToolCalls},
		{"content_filter", "content_filter", types.FinishReasonContentFilter},
		{"unknown", "unknown", types.FinishReasonOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := providerutils.MapOpenAIFinishReason(tt.moonshotReason)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// Helper function to create int pointers
func intPtr(i int) *int {
	return &i
}
