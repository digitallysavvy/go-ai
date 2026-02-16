package deepinfra

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFixUsageForGeminiModels(t *testing.T) {
	tests := []struct {
		name     string
		usage    types.Usage
		expected types.Usage
	}{
		{
			name: "fixes incorrect Gemini/Gemma token counts",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(84), // Incorrect: doesn't include reasoning
				TotalTokens:  int64Ptr(184),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 84,
					"total_tokens":      184,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 1081, // Should be included in completion_tokens
					},
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(1165), // 84 + 1081
				TotalTokens:  int64Ptr(1265),  // 184 + 1081
				OutputDetails: &types.OutputTokenDetails{
					TextTokens:      int64Ptr(84),
					ReasoningTokens: int64Ptr(1081),
				},
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 1165,
					"total_tokens":      1265,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 1081,
					},
				},
			},
		},
		{
			name: "does not fix when reasoning_tokens < completion_tokens",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(500), // Already correct
				TotalTokens:  int64Ptr(600),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 500,
					"total_tokens":      600,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 50, // Less than completion_tokens
					},
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(500),
				TotalTokens:  int64Ptr(600),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 500,
					"total_tokens":      600,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 50,
					},
				},
			},
		},
		{
			name: "handles zero reasoning_tokens",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 200,
					"total_tokens":      300,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 0,
					},
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 200,
					"total_tokens":      300,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 0,
					},
				},
			},
		},
		{
			name: "handles missing completion_tokens_details",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 200,
					"total_tokens":      300,
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 200,
					"total_tokens":      300,
				},
			},
		},
		{
			name: "handles nil raw usage",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw:          nil,
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(200),
				TotalTokens:  int64Ptr(300),
				Raw:          nil,
			},
		},
		{
			name: "handles reasoning_tokens as float64",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(50),
				TotalTokens:  int64Ptr(150),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 50,
					"total_tokens":      150,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": float64(1000),
					},
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(1050), // 50 + 1000
				TotalTokens:  int64Ptr(1150),  // 150 + 1000
				OutputDetails: &types.OutputTokenDetails{
					TextTokens:      int64Ptr(50),
					ReasoningTokens: int64Ptr(1000),
				},
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 1050,
					"total_tokens":      1150,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": float64(1000),
					},
				},
			},
		},
		{
			name: "handles reasoning_tokens as int",
			usage: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(60),
				TotalTokens:  int64Ptr(160),
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 60,
					"total_tokens":      160,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 800,
					},
				},
			},
			expected: types.Usage{
				InputTokens:  int64Ptr(100),
				OutputTokens: int64Ptr(860), // 60 + 800
				TotalTokens:  int64Ptr(960),  // 160 + 800
				OutputDetails: &types.OutputTokenDetails{
					TextTokens:      int64Ptr(60),
					ReasoningTokens: int64Ptr(800),
				},
				Raw: map[string]interface{}{
					"prompt_tokens":     100,
					"completion_tokens": 860,
					"total_tokens":      960,
					"completion_tokens_details": map[string]interface{}{
						"reasoning_tokens": 800,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fixUsageForGeminiModels(tt.usage)

			// Compare basic fields
			if !int64PtrEqual(result.InputTokens, tt.expected.InputTokens) {
				t.Errorf("InputTokens = %v, want %v", ptrValue(result.InputTokens), ptrValue(tt.expected.InputTokens))
			}
			if !int64PtrEqual(result.OutputTokens, tt.expected.OutputTokens) {
				t.Errorf("OutputTokens = %v, want %v", ptrValue(result.OutputTokens), ptrValue(tt.expected.OutputTokens))
			}
			if !int64PtrEqual(result.TotalTokens, tt.expected.TotalTokens) {
				t.Errorf("TotalTokens = %v, want %v", ptrValue(result.TotalTokens), ptrValue(tt.expected.TotalTokens))
			}

			// Compare OutputDetails
			if tt.expected.OutputDetails != nil {
				if result.OutputDetails == nil {
					t.Error("OutputDetails is nil, want non-nil")
				} else {
					if !int64PtrEqual(result.OutputDetails.TextTokens, tt.expected.OutputDetails.TextTokens) {
						t.Errorf("OutputDetails.TextTokens = %v, want %v",
							ptrValue(result.OutputDetails.TextTokens),
							ptrValue(tt.expected.OutputDetails.TextTokens))
					}
					if !int64PtrEqual(result.OutputDetails.ReasoningTokens, tt.expected.OutputDetails.ReasoningTokens) {
						t.Errorf("OutputDetails.ReasoningTokens = %v, want %v",
							ptrValue(result.OutputDetails.ReasoningTokens),
							ptrValue(tt.expected.OutputDetails.ReasoningTokens))
					}
				}
			}

			// Verify raw usage was updated correctly
			if len(result.Raw) > 0 && len(tt.expected.Raw) > 0 {
				if result.Raw["completion_tokens"] != tt.expected.Raw["completion_tokens"] {
					t.Errorf("Raw completion_tokens = %v, want %v",
						result.Raw["completion_tokens"],
						tt.expected.Raw["completion_tokens"])
				}
				if result.Raw["total_tokens"] != tt.expected.Raw["total_tokens"] {
					t.Errorf("Raw total_tokens = %v, want %v",
						result.Raw["total_tokens"],
						tt.expected.Raw["total_tokens"])
				}
			}
		})
	}
}

// Helper functions for testing

func int64Ptr(v int64) *int64 {
	return &v
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func ptrValue(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
