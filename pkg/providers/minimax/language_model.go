package minimax

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// LanguageModel implements the provider.LanguageModel interface for MiniMax
// chat completions over the Anthropic Messages protocol.
//
// TS's MiniMax provider reuses AnthropicLanguageModel directly, constructed
// with provider: "minimax.messages" (see minimax-provider.ts). Every call
// then resolves providerOptions.minimax (falling back to the canonical
// providerOptions.anthropic key) fresh via AnthropicLanguageModel's own
// prepareRequest. Go mirrors this the same way: pkg/providers/anthropic's
// shared per-call provider-options merge (call_options.go) derives the
// "minimax" providerOptionsName from Provider.Name() (set to "minimax" in
// provider.go) and reads providerOptions.minimax / providerOptions.anthropic
// on every call, so this wrapper only needs to hold a single, shared
// *anthropic.LanguageModel and delegate to it — no more per-call model
// construction or manual thinking bridging. The one MiniMax-specific
// behavior left (narrowing thinking.type to "adaptive"|"disabled") is wired
// as anthropic.Config.ValidateCallOptions in provider.go.
type LanguageModel struct {
	provider *Provider
	modelID  string
	inner    *anthropic.LanguageModel
}

// NewLanguageModel creates a new MiniMax language model.
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
		inner:    anthropic.NewLanguageModel(provider.anthropicProvider, modelID, nil),
	}
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
	return m.inner.SupportsStructuredOutput()
}

// SupportsImageInput returns whether the model accepts image inputs,
// delegating to the underlying Anthropic model's capability detection.
func (m *LanguageModel) SupportsImageInput() bool {
	return m.inner.SupportsImageInput()
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.inner.DoGenerate(ctx, opts)
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return m.inner.DoStream(ctx, opts)
}
