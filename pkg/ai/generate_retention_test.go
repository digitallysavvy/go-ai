package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockLanguageModelForRetention is a mock model for testing retention settings
type mockLanguageModelForRetention struct {
	generateFunc func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error)
}

func (m *mockLanguageModelForRetention) SpecificationVersion() string { return "v3" }
func (m *mockLanguageModelForRetention) Provider() string             { return "mock" }
func (m *mockLanguageModelForRetention) ModelID() string              { return "mock-model" }
func (m *mockLanguageModelForRetention) SupportsTools() bool          { return true }
func (m *mockLanguageModelForRetention) SupportsStructuredOutput() bool {
	return false
}
func (m *mockLanguageModelForRetention) SupportsImageInput() bool { return false }

func (m *mockLanguageModelForRetention) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, opts)
	}
	return &types.GenerateResult{
		Text:         "Hello",
		FinishReason: types.FinishReasonStop,
		Usage: types.Usage{
			InputTokens:  int64Ptr(10),
			OutputTokens: int64Ptr(20),
			TotalTokens:  int64Ptr(30),
		},
		RawRequest:  map[string]interface{}{"prompt": "test"},
		RawResponse: map[string]interface{}{"content": "Hello"},
	}, nil
}

func (m *mockLanguageModelForRetention) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, nil
}

// TestRetentionSettings_Default tests that nil retention retains everything
func TestRetentionSettings_Default(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		// ExperimentalRetention is nil (default)
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Both request and response should be retained by default
	if result.RawRequest == nil {
		t.Error("Expected RawRequest to be retained by default, but it was nil")
	}
	if result.RawResponse == nil {
		t.Error("Expected RawResponse to be retained by default, but it was nil")
	}
}

// TestRetentionSettings_ExcludeRequest tests request body exclusion
func TestRetentionSettings_ExcludeRequest(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalRetention: &types.RetentionSettings{
			RequestBody: types.BoolPtr(false), // Exclude request
		},
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Request should be excluded
	if result.RawRequest != nil {
		t.Error("Expected RawRequest to be excluded, but it was present")
	}

	// Response should still be retained (only request was excluded)
	if result.RawResponse == nil {
		t.Error("Expected RawResponse to be retained, but it was nil")
	}
}

// TestRetentionSettings_ExcludeResponse tests response body exclusion
func TestRetentionSettings_ExcludeResponse(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalRetention: &types.RetentionSettings{
			ResponseBody: types.BoolPtr(false), // Exclude response
		},
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Request should still be retained (only response was excluded)
	if result.RawRequest == nil {
		t.Error("Expected RawRequest to be retained, but it was nil")
	}

	// Response should be excluded
	if result.RawResponse != nil {
		t.Error("Expected RawResponse to be excluded, but it was present")
	}
}

// TestRetentionSettings_ExcludeBoth tests both bodies excluded
func TestRetentionSettings_ExcludeBoth(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalRetention: &types.RetentionSettings{
			RequestBody:  types.BoolPtr(false), // Exclude request
			ResponseBody: types.BoolPtr(false), // Exclude response
		},
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Both should be excluded
	if result.RawRequest != nil {
		t.Error("Expected RawRequest to be excluded, but it was present")
	}
	if result.RawResponse != nil {
		t.Error("Expected RawResponse to be excluded, but it was present")
	}

	// But other fields should still be present
	if result.Text == "" {
		t.Error("Expected Text to be present")
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 30 {
		t.Error("Expected Usage to be present and correct")
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Error("Expected FinishReason to be present and correct")
	}
}

// TestRetentionSettings_ExplicitRetain tests explicit retention (true values)
func TestRetentionSettings_ExplicitRetain(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  model,
		Prompt: "test",
		ExperimentalRetention: &types.RetentionSettings{
			RequestBody:  types.BoolPtr(true), // Explicitly retain request
			ResponseBody: types.BoolPtr(true), // Explicitly retain response
		},
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Both should be retained
	if result.RawRequest == nil {
		t.Error("Expected RawRequest to be retained, but it was nil")
	}
	if result.RawResponse == nil {
		t.Error("Expected RawResponse to be retained, but it was nil")
	}
}

// TestRetentionSettings_BackwardsCompatibility tests that existing code still works
func TestRetentionSettings_BackwardsCompatibility(t *testing.T) {
	ctx := context.Background()

	model := &mockLanguageModelForRetention{}

	// Old code without ExperimentalRetention should work unchanged
	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:       model,
		Prompt:      "test",
		Temperature: floatPtr(0.7),
		MaxTokens:   intPtr(100),
	})

	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}

	// Everything should be retained by default
	if result.RawRequest == nil {
		t.Error("Expected RawRequest to be retained for backwards compatibility")
	}
	if result.RawResponse == nil {
		t.Error("Expected RawResponse to be retained for backwards compatibility")
	}
	if result.Text == "" {
		t.Error("Expected Text to be present")
	}
}

// TestRetentionSettings_HelperMethods tests the RetentionSettings helper methods
func TestRetentionSettings_HelperMethods(t *testing.T) {
	tests := []struct {
		name            string
		settings        *types.RetentionSettings
		shouldRetainReq bool
		shouldRetainRes bool
	}{
		{
			name:            "nil settings",
			settings:        nil,
			shouldRetainReq: true,
			shouldRetainRes: true,
		},
		{
			name:            "empty settings",
			settings:        &types.RetentionSettings{},
			shouldRetainReq: true,
			shouldRetainRes: true,
		},
		{
			name: "exclude request only",
			settings: &types.RetentionSettings{
				RequestBody: types.BoolPtr(false),
			},
			shouldRetainReq: false,
			shouldRetainRes: true,
		},
		{
			name: "exclude response only",
			settings: &types.RetentionSettings{
				ResponseBody: types.BoolPtr(false),
			},
			shouldRetainReq: true,
			shouldRetainRes: false,
		},
		{
			name: "exclude both",
			settings: &types.RetentionSettings{
				RequestBody:  types.BoolPtr(false),
				ResponseBody: types.BoolPtr(false),
			},
			shouldRetainReq: false,
			shouldRetainRes: false,
		},
		{
			name: "explicitly retain both",
			settings: &types.RetentionSettings{
				RequestBody:  types.BoolPtr(true),
				ResponseBody: types.BoolPtr(true),
			},
			shouldRetainReq: true,
			shouldRetainRes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.ShouldRetainRequestBody(); got != tt.shouldRetainReq {
				t.Errorf("ShouldRetainRequestBody() = %v, want %v", got, tt.shouldRetainReq)
			}
			if got := tt.settings.ShouldRetainResponseBody(); got != tt.shouldRetainRes {
				t.Errorf("ShouldRetainResponseBody() = %v, want %v", got, tt.shouldRetainRes)
			}
		})
	}
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
