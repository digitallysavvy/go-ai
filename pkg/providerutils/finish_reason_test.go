package providerutils

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapOpenAIFinishReason(t *testing.T) {
	tests := []struct {
		input    string
		expected types.FinishReason
	}{
		{"stop", types.FinishReasonStop},
		{"length", types.FinishReasonLength},
		{"tool_calls", types.FinishReasonToolCalls},
		{"function_call", types.FinishReasonToolCalls},
		{"content_filter", types.FinishReasonContentFilter},
		{"unknown_value", types.FinishReasonOther},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := MapOpenAIFinishReason(tt.input)
			if got != tt.expected {
				t.Errorf("MapOpenAIFinishReason(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
