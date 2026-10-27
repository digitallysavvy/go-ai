package minimax

import (
	"context"
	"regexp"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModel implements the provider.LanguageModel interface for MiniMax
// chat completions over the Anthropic Messages protocol.
//
// TS's MiniMax provider reuses AnthropicLanguageModel directly, constructed
// with provider: "minimax.messages" (see minimax-provider.ts). Every call
// then resolves providerOptions.minimax (falling back to the canonical
// providerOptions.anthropic key) fresh via AnthropicLanguageModel's own
// prepareRequest, validated against the same shared
// anthropicLanguageModelOptions schema used for every other Anthropic
// wrapper — MiniMax's own MiniMaxLanguageModelOptions type (which narrows
// thinking.type to "adaptive"|"disabled") is a compile-time-only TypeScript
// export never consulted at runtime. Go mirrors this the same way:
// pkg/providers/anthropic's shared per-call provider-options merge
// (call_options.go) derives the "minimax" providerOptionsName from
// Provider.Name() (set to "minimax" in provider.go) and reads
// providerOptions.minimax / providerOptions.anthropic on every call with no
// MiniMax-specific narrowing, so this wrapper only needs to hold a single,
// shared *anthropic.LanguageModel and delegate to it.
type LanguageModel struct {
	provider *Provider
	modelID  string
	inner    *anthropic.LanguageModel

	// supportsM31Features is true for MiniMax M3.1 and later models, which
	// accept effort-based reasoning, image and video input, temperatures up to
	// 2 (also with thinking) and prompt caching through providerOptions.minimax.
	supportsM31Features bool
}

// minimaxVersionPattern matches the version of a MiniMax M-series model ID,
// like the regular expression in TS MiniMaxChatLanguageModel.
var minimaxVersionPattern = regexp.MustCompile(`(?i)^minimax-m(\d+)(?:\.(\d+))?(?:\.\d+)*(?:-|$)`)

// modelSupportsM31Features compares the version components as numbers
// (M3.123 is newer than M3.1) and assumes later M-series models retain the
// M3.1 capabilities.
func modelSupportsM31Features(modelID string) bool {
	version := minimaxVersionPattern.FindStringSubmatch(modelID)
	if version == nil {
		return false
	}
	major, _ := strconv.ParseFloat(version[1], 64)
	minor := 0.0
	if version[2] != "" {
		minor, _ = strconv.ParseFloat(version[2], 64)
	}
	return major > 3 || (major == 3 && minor >= 1)
}

// newM31AnthropicProvider derives the Anthropic transport of M3.1 and later
// models from the provider's: custom media conversion, a maximum temperature
// of 2, sampling kept with thinking, the minimax cache namespace and native
// image and video URLs (the TS MiniMaxChatLanguageModel config overrides).
func (p *Provider) newM31AnthropicProvider() *anthropic.Provider {
	cfg := p.anthropicConfig
	maxTemperature := 2.0
	cfg.ConvertMediaPart = convertToMiniMaxMediaPart
	cfg.MaxTemperature = &maxTemperature
	cfg.SupportsTemperatureAndTopPWithThinking = true
	cfg.CacheControlProviderOptionsName = "minimax"
	cfg.SupportedURLs = func(string) map[string][]string {
		return map[string][]string{
			"image/*": {`^https?://`},
			"video/*": {`^https?://`},
		}
	}
	return anthropic.New(cfg)
}

// NewLanguageModel creates a new MiniMax language model.
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	supportsM31Features := modelSupportsM31Features(modelID)
	transport := provider.anthropicProvider
	if supportsM31Features {
		transport = provider.newM31AnthropicProvider()
	}
	return &LanguageModel{
		provider:            provider,
		modelID:             modelID,
		inner:               anthropic.NewLanguageModel(transport, modelID, nil),
		supportsM31Features: supportsM31Features,
	}
}

// SpecificationVersion returns the specification version.
func (m *LanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *LanguageModel) Provider() string { return "minimax.messages" }

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

// SupportedURLs returns the URL patterns the model accepts without a
// download: image and video URLs for M3.1 and later models, none otherwise.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return m.inner.SupportedURLs()
}

// minimaxEffortMap maps the portable reasoning levels to MiniMax effort
// (TS MiniMaxChatLanguageModel prepareCallOptions effortMap).
var minimaxEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "xhigh",
	types.ReasoningMax:     "max",
}

// prepareCallOptions maps the top-level reasoning of M3.1 and later models to
// providerOptions.minimax.effort. An explicit minimax or anthropic effort wins.
// The input options are not modified.
func (m *LanguageModel) prepareCallOptions(opts *provider.GenerateOptions) (*provider.GenerateOptions, []types.Warning) {
	var warnings []types.Warning
	if opts == nil || !m.supportsM31Features || opts.Reasoning == nil || *opts.Reasoning == types.ReasoningDefault {
		return opts, nil
	}
	for _, namespace := range []string{"minimax", "anthropic"} {
		if ns, ok := opts.ProviderOptions[namespace].(map[string]interface{}); ok && ns["effort"] != nil {
			return opts, nil
		}
	}

	reasoning := *opts.Reasoning
	var effort string
	if reasoning == types.ReasoningNone {
		warnings = append(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "reasoning",
			Details: "reasoning 'none' is not supported by " + m.modelID + "; it always reasons. Using effort 'low' to minimize thinking instead.",
		})
		effort = "low"
	} else {
		var ok bool
		effort, ok = providerutils.MapReasoningToProviderEffort(reasoning, minimaxEffortMap, &warnings)
		if !ok {
			effort = ""
		}
	}

	prepared := *opts
	// M3.1 and later use effort instead of Anthropic thinking budgets.
	prepared.Reasoning = nil
	providerOptions := make(map[string]interface{}, len(opts.ProviderOptions)+1)
	for k, v := range opts.ProviderOptions {
		providerOptions[k] = v
	}
	minimaxOptions := map[string]interface{}{}
	if existing, ok := opts.ProviderOptions["minimax"].(map[string]interface{}); ok {
		for k, v := range existing {
			minimaxOptions[k] = v
		}
	}
	if effort != "" {
		minimaxOptions["effort"] = effort
	}
	providerOptions["minimax"] = minimaxOptions
	prepared.ProviderOptions = providerOptions
	return &prepared, warnings
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	prepared, warnings := m.prepareCallOptions(opts)
	result, err := m.inner.DoGenerate(ctx, prepared)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(result.Warnings, warnings...)
	return result, nil
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	prepared, warnings := m.prepareCallOptions(opts)
	stream, err := m.inner.DoStream(ctx, prepared)
	if err != nil {
		return nil, err
	}
	if len(warnings) == 0 {
		return stream, nil
	}
	return &warningsAppendStream{inner: stream, warnings: warnings}, nil
}

// warningsAppendStream appends warnings to the stream-start chunk of the
// wrapped stream (TS adds them in a TransformStream).
type warningsAppendStream struct {
	inner    provider.TextStream
	warnings []types.Warning
}

func (s *warningsAppendStream) Next() (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next()
	if err == nil && chunk != nil && chunk.Type == provider.ChunkTypeStreamStart {
		merged := *chunk
		merged.Warnings = append(append([]types.Warning{}, chunk.Warnings...), s.warnings...)
		return &merged, nil
	}
	return chunk, err
}

func (s *warningsAppendStream) Err() error   { return s.inner.Err() }
func (s *warningsAppendStream) Close() error { return s.inner.Close() }

// RequestBody implements provider.StreamRequestBody by delegating to the
// wrapped stream.
func (s *warningsAppendStream) RequestBody() interface{} {
	if rb, ok := s.inner.(provider.StreamRequestBody); ok {
		return rb.RequestBody()
	}
	return nil
}
