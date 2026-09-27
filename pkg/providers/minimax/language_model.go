package minimax

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// LanguageModel implements the provider.LanguageModel interface for MiniMax
// chat completions over the Anthropic Messages protocol.
//
// pkg/providers/anthropic.LanguageModel resolves its "thinking" option once,
// at construction time, from a *anthropic.ModelOptions passed to
// NewLanguageModel/LanguageModelWithOptions — it does not read
// providerOptions.anthropic.thinking dynamically per call. TS's MiniMax
// provider, in contrast, reuses AnthropicLanguageModel with
// providerOptionsName="minimax" (derived from config.provider =
// "minimax.messages"), so TS resolves thinking fresh on every doGenerate/
// doStream call from providerOptions.minimax.thinking (falling back to
// providerOptions.anthropic.thinking for canonical-key compatibility).
//
// This wrapper bridges that gap without modifying the shared Anthropic
// provider: on every call it resolves providerOptions.minimax.thinking (see
// resolveMiniMaxThinking), builds a fresh *anthropic.LanguageModel with that
// resolved ModelOptions, and delegates to it. Building the model is cheap
// (no I/O), so doing it per call has no meaningful cost.
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new MiniMax language model.
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *LanguageModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider name.
func (m *LanguageModel) Provider() string { return "minimax" }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools returns whether the model supports tool calling.
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput returns whether the model supports structured
// output, delegating to the underlying Anthropic model's capability
// detection for this model ID.
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return m.plainAnthropicModel().SupportsStructuredOutput()
}

// SupportsImageInput returns whether the model accepts image inputs,
// delegating to the underlying Anthropic model's capability detection.
func (m *LanguageModel) SupportsImageInput() bool {
	return m.plainAnthropicModel().SupportsImageInput()
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	base, err := m.baseModel(opts)
	if err != nil {
		return nil, err
	}
	return base.DoGenerate(ctx, opts)
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	base, err := m.baseModel(opts)
	if err != nil {
		return nil, err
	}
	return base.DoStream(ctx, opts)
}

func (m *LanguageModel) plainAnthropicModel() *anthropic.LanguageModel {
	return anthropic.NewLanguageModel(m.provider.anthropicProvider, m.modelID, nil)
}

// baseModel builds the Anthropic language model used for a single call,
// with ModelOptions.Thinking resolved from this call's provider options.
func (m *LanguageModel) baseModel(opts *provider.GenerateOptions) (*anthropic.LanguageModel, error) {
	thinking, err := resolveMiniMaxThinking(opts)
	if err != nil {
		return nil, err
	}
	var modelOpts *anthropic.ModelOptions
	if thinking != nil {
		modelOpts = &anthropic.ModelOptions{Thinking: thinking}
	}
	return anthropic.NewLanguageModel(m.provider.anthropicProvider, m.modelID, modelOpts), nil
}

// resolveMiniMaxThinking extracts providerOptions.minimax.thinking (or the
// canonical providerOptions.anthropic.thinking, which the custom "minimax"
// key overrides when both are set — mirrors TS's
// Object.assign({}, canonicalOptions, customProviderOptions) merge order)
// and validates it against MiniMax's own thinking.type enum
// ("adaptive"|"disabled" — mirrors minimaxLanguageModelOptions in
// minimax-chat-options.ts; note this is narrower than plain Anthropic's
// thinking.type, which also allows "enabled").
func resolveMiniMaxThinking(opts *provider.GenerateOptions) (*anthropic.ThinkingConfig, error) {
	if opts == nil || opts.ProviderOptions == nil {
		return nil, nil
	}

	var raw map[string]interface{}
	if anthropicOpts, ok := opts.ProviderOptions["anthropic"].(map[string]interface{}); ok {
		if th, ok := anthropicOpts["thinking"].(map[string]interface{}); ok {
			raw = th
		}
	}
	if minimaxOpts, ok := opts.ProviderOptions["minimax"].(map[string]interface{}); ok {
		if th, ok := minimaxOpts["thinking"].(map[string]interface{}); ok {
			raw = th
		}
	}
	if raw == nil {
		return nil, nil
	}

	typ, _ := raw["type"].(string)
	switch anthropic.ThinkingType(typ) {
	case anthropic.ThinkingTypeAdaptive, anthropic.ThinkingTypeDisabled:
		return &anthropic.ThinkingConfig{Type: anthropic.ThinkingType(typ)}, nil
	default:
		return nil, fmt.Errorf("invalid minimax provider options: thinking.type must be %q or %q", anthropic.ThinkingTypeAdaptive, anthropic.ThinkingTypeDisabled)
	}
}
