package anthropic

import "testing"

// TestGetModelCapabilities ports the model-capability branches of
// getModelCapabilities (anthropic-language-model.ts:3114-3172, verified
// against ai@7.0.113) as a table-driven test, one row per branch in TS
// source order.
func TestGetModelCapabilities(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    ModelCapabilities
	}{
		{
			name:    "claude-sonnet-5-5",
			modelID: "claude-sonnet-5-5",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabledAboveHighEffort: true, RejectsThinkingDisabled: true,
				RejectsForcedToolUse: true, SupportsBetweenToolsThinking: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-opus-5-5",
			modelID: "claude-opus-5-5",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabledAboveHighEffort: true, RejectsThinkingDisabled: true,
				RejectsForcedToolUse: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-opus-5",
			modelID: "claude-opus-5",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabledAboveHighEffort: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-fable-5-1",
			modelID: "claude-fable-5-1",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabled: true, RejectsForcedToolUse: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-fable-5",
			modelID: "claude-fable-5",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabled: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-opus-4-8",
			modelID: "claude-opus-4-8",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-opus-4-7",
			modelID: "claude-opus-4-7",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-sonnet-5",
			modelID: "claude-sonnet-5",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true, IsKnownModel: true,
			},
		},
		{
			name:    "claude-sonnet-4-6",
			modelID: "claude-sonnet-4-6",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				IsKnownModel: true,
			},
		},
		{
			name:    "claude-opus-4-6",
			modelID: "claude-opus-4-6",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				IsKnownModel: true,
			},
		},
		{
			name:    "claude-sonnet-4-5",
			modelID: "claude-sonnet-4-5-20250929",
			want:    ModelCapabilities{MaxOutputTokens: 64000, SupportsStructuredOutput: true, IsKnownModel: true},
		},
		{
			name:    "claude-opus-4-5",
			modelID: "claude-opus-4-5-20251101",
			want:    ModelCapabilities{MaxOutputTokens: 64000, SupportsStructuredOutput: true, IsKnownModel: true},
		},
		{
			name:    "claude-haiku-4-5",
			modelID: "claude-haiku-4-5-20251001",
			want:    ModelCapabilities{MaxOutputTokens: 64000, SupportsStructuredOutput: true, IsKnownModel: true},
		},
		{
			name:    "claude-opus-4-1",
			modelID: "claude-opus-4-1-20250805",
			want:    ModelCapabilities{MaxOutputTokens: 32000, SupportsStructuredOutput: true, IsKnownModel: true},
		},
		{
			name:    "claude-sonnet-4 dated (regex)",
			modelID: "claude-sonnet-4-20250514",
			want:    ModelCapabilities{MaxOutputTokens: 64000, IsKnownModel: true},
		},
		{
			name:    "claude-sonnet-4 vertex @ id (regex)",
			modelID: "claude-sonnet-4@20250514",
			want:    ModelCapabilities{MaxOutputTokens: 64000, IsKnownModel: true},
		},
		{
			name:    "claude-opus-4 dated (regex)",
			modelID: "claude-opus-4-20250514",
			want:    ModelCapabilities{MaxOutputTokens: 32000, IsKnownModel: true},
		},
		{
			name:    "claude-opus-4 vertex @ id (regex)",
			modelID: "claude-opus-4@20250514",
			want:    ModelCapabilities{MaxOutputTokens: 32000, IsKnownModel: true},
		},
		{
			name:    "claude-3-haiku",
			modelID: "claude-3-haiku-20240307",
			want:    ModelCapabilities{MaxOutputTokens: 4096, IsKnownModel: true},
		},
		{
			name:    "legacy claude-instant",
			modelID: "claude-instant-1.2",
			want:    ModelCapabilities{MaxOutputTokens: 4096},
		},
		{
			name:    "legacy claude-2",
			modelID: "claude-2.1",
			want:    ModelCapabilities{MaxOutputTokens: 4096},
		},
		{
			name:    "legacy claude-3-sonnet",
			modelID: "claude-3-sonnet-20240229",
			want:    ModelCapabilities{MaxOutputTokens: 4096},
		},
		{
			name:    "unknown future claude model",
			modelID: "claude-future-9",
			want: ModelCapabilities{
				MaxOutputTokens: 128000, SupportsStructuredOutput: true, SupportsAdaptiveThinking: true,
				RejectsSamplingParameters: true, SupportsXHighEffort: true,
				RejectsThinkingDisabledAboveHighEffort: true,
			},
		},
		{
			name:    "unknown non-claude model",
			modelID: "future-model",
			want:    ModelCapabilities{MaxOutputTokens: 4096},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetModelCapabilities(tt.modelID)
			if got != tt.want {
				t.Errorf("GetModelCapabilities(%q) = %+v, want %+v", tt.modelID, got, tt.want)
			}
		})
	}
}
