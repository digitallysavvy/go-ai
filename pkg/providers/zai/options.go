package zai

import "fmt"

// ThinkingType controls Z.AI's thinking mode
// (providerOptions.zai.thinking.type).
type ThinkingType string

const (
	ThinkingEnabled  ThinkingType = "enabled"
	ThinkingDisabled ThinkingType = "disabled"
)

// ReasoningEffort controls reasoning effort for GLM-5.2 and later models
// (providerOptions.zai.reasoningEffort). Mirrors TS
// zaiLanguageModelChatOptions.reasoningEffort.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
)

// LanguageModelChatOptions documents the Z.AI provider options accepted
// under providerOptions.zai (camelCase, matching TS
// ZaiLanguageModelChatOptions). This type is documentation-only: options are
// read from the generic provider.GenerateOptions.ProviderOptions map at
// call time (see resolvedOptions / decodeThinking below), the same
// convention every other OpenAI-compatible provider in this SDK uses.
type LanguageModelChatOptions struct {
	// DoSample enables or disables sampling. When disabled, temperature and
	// topP do not take effect.
	DoSample *bool `json:"doSample,omitempty"`

	// Thinking controls model thinking and whether reasoning from earlier
	// turns is kept.
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// ReasoningEffort controls reasoning effort for GLM-5.2 and later models.
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty"`

	// ToolStream enables incremental function-call argument streaming on
	// supported models.
	ToolStream *bool `json:"toolStream,omitempty"`

	// RequestID is a caller-provided request identifier between 6 and 64
	// characters.
	RequestID string `json:"requestId,omitempty"`

	// UserID is a non-sensitive end-user identifier between 6 and 128
	// characters.
	UserID string `json:"userId,omitempty"`
}

// ThinkingConfig configures Z.AI's thinking mode.
type ThinkingConfig struct {
	Type          ThinkingType `json:"type,omitempty"`
	ClearThinking *bool        `json:"clearThinking,omitempty"`
}

// resolvedThinking is the decoded, wire-ready form of providerOptions.zai.thinking.
type resolvedThinking struct {
	typ           string
	clearThinking *bool
}

// resolvedOptions is the decoded form of providerOptions.zai (merged via
// providerutils.ResolveOpenAICompatibleProviderOptions, which also accepts
// the deprecated "openai-compatible"/"openaiCompatible" keys).
type resolvedOptions struct {
	doSample   *bool
	thinking   *resolvedThinking
	toolStream *bool
	requestID  string
	userID     string
}

// decodeZaiOptions extracts and validates the Z.AI-specific fields from an
// already-resolved provider options map (see
// providerutils.ResolveOpenAICompatibleProviderOptions). It mirrors TS
// zaiLanguageModelChatOptions (a zod schema): unknown keys are ignored,
// requestId/userId length constraints are enforced, and thinking.type is
// constrained to "enabled"/"disabled". On validation failure it returns an
// error whose message contains "invalid zai provider options", matching the
// TS error text asserted by zai-chat-language-model.test.ts.
func decodeZaiOptions(options map[string]interface{}) (*resolvedOptions, error) {
	resolved := &resolvedOptions{}

	if v, ok := options["doSample"].(bool); ok {
		resolved.doSample = &v
	}

	if raw, ok := options["thinking"].(map[string]interface{}); ok {
		th := &resolvedThinking{}
		if t, ok := raw["type"].(string); ok {
			if t != string(ThinkingEnabled) && t != string(ThinkingDisabled) {
				return nil, fmt.Errorf("invalid zai provider options: thinking.type must be %q or %q", ThinkingEnabled, ThinkingDisabled)
			}
			th.typ = t
		}
		if ct, ok := raw["clearThinking"].(bool); ok {
			th.clearThinking = &ct
		}
		resolved.thinking = th
	}

	if v, ok := options["toolStream"].(bool); ok {
		resolved.toolStream = &v
	}

	if v, ok := options["requestId"].(string); ok {
		if len(v) < 6 || len(v) > 64 {
			return nil, fmt.Errorf("invalid zai provider options: requestId must be between 6 and 64 characters")
		}
		resolved.requestID = v
	}

	if v, ok := options["userId"].(string); ok {
		if len(v) < 6 || len(v) > 128 {
			return nil, fmt.Errorf("invalid zai provider options: userId must be between 6 and 128 characters")
		}
		resolved.userID = v
	}

	return resolved, nil
}
