package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// DefaultSettingsMiddleware creates a language model middleware that applies default settings
// to all generate/stream calls. Settings provided in the call will override these defaults.
func DefaultSettingsMiddleware(settings *provider.GenerateOptions) *LanguageModelMiddleware {
	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",
		TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
			// Merge settings with params, params take precedence
			merged := mergeGenerateOptions(settings, params)
			return merged, nil
		},
	}
}

// mergeGenerateOptions merges defaults into overrides, with overrides (the
// caller's own call params) taking precedence. This mirrors the TypeScript
// SDK's `mergeObjects(settings, params)`, which starts from a copy of the
// full `params` object (so every field the caller set survives, even ones
// `settings` doesn't know about) and only pulls in `settings` keys the
// caller left unset.
//
// The previous implementation built a fresh, empty GenerateOptions and only
// copied a hand-picked subset of fields from both defaults and overrides,
// which silently dropped every other field (ProviderOptions, Prompt.System,
// Reasoning, SendReasoning, RuntimeContext, ToolsContext, IncludeRawChunks,
// AllowSystemMessages/AllowSystemInMessages, Telemetry) from the caller's
// own params, not just from defaults. Starting from a full copy of
// overrides fixes that: any field not explicitly handled below already
// survives via the struct copy.
func mergeGenerateOptions(defaults, overrides *provider.GenerateOptions) *provider.GenerateOptions {
	if defaults == nil {
		return overrides
	}
	if overrides == nil {
		return defaults
	}

	// Copy every field from overrides first, so nothing the caller passed is
	// ever dropped -- including fields not explicitly handled below (e.g.
	// AllowSystemMessages/AllowSystemInMessages/IncludeRawChunks, which are
	// plain bools in Go and so have no way to represent "unset" vs.
	// "explicitly false"; they always come from the caller's own params).
	result := *overrides

	// Prompt.* is deliberately never pulled from defaults. TS's
	// defaultSettingsMiddleware types its `settings` parameter as a
	// `Partial<{...}>` that excludes `prompt` entirely (see
	// default-settings-middleware.ts) -- a prompt/messages/system default is
	// not a representable defaultSettingsMiddleware setting in TS at all, so
	// mergeObjects(settings, params) never has a prompt field on `settings`
	// to merge in. result.Prompt therefore always stays exactly what the
	// caller passed in overrides.Prompt, regardless of what defaults.Prompt
	// contains.
	if result.Temperature == nil {
		result.Temperature = defaults.Temperature
	}
	if result.MaxTokens == nil {
		result.MaxTokens = defaults.MaxTokens
	}
	if result.TopP == nil {
		result.TopP = defaults.TopP
	}
	if result.TopK == nil {
		result.TopK = defaults.TopK
	}
	if result.PresencePenalty == nil {
		result.PresencePenalty = defaults.PresencePenalty
	}
	if result.FrequencyPenalty == nil {
		result.FrequencyPenalty = defaults.FrequencyPenalty
	}
	if result.StopSequences == nil {
		result.StopSequences = defaults.StopSequences
	}
	if result.Seed == nil {
		result.Seed = defaults.Seed
	}
	if result.Tools == nil {
		result.Tools = defaults.Tools
	}
	if result.ToolChoice.Type == "" {
		result.ToolChoice = defaults.ToolChoice
	}
	if result.ResponseFormat == nil {
		result.ResponseFormat = defaults.ResponseFormat
	}
	if result.MaxSteps == nil {
		result.MaxSteps = defaults.MaxSteps
	}
	if result.Reasoning == nil {
		result.Reasoning = defaults.Reasoning
	}
	if result.SendReasoning == nil {
		result.SendReasoning = defaults.SendReasoning
	}
	if result.RuntimeContext == nil {
		result.RuntimeContext = defaults.RuntimeContext
	}
	if result.ToolsContext == nil {
		result.ToolsContext = defaults.ToolsContext
	}
	if result.Telemetry == nil {
		result.Telemetry = defaults.Telemetry
	}

	// Headers: merge maps, overrides win per-key.
	if defaults.Headers != nil || overrides.Headers != nil {
		merged := make(map[string]string, len(defaults.Headers)+len(overrides.Headers))
		for k, v := range defaults.Headers {
			merged[k] = v
		}
		for k, v := range overrides.Headers {
			merged[k] = v
		}
		result.Headers = merged
	}

	// ProviderOptions: deep merge (recursively for nested maps), overrides
	// win per-key. Matches TS mergeObjects's recursive-object behavior.
	result.ProviderOptions = deepMergeProviderOptions(defaults.ProviderOptions, overrides.ProviderOptions)

	return &result
}

// deepMergeProviderOptions recursively merges overrides into base, with
// overrides winning on key collisions. Nested map[string]interface{} values
// present on both sides are merged recursively; everything else (including
// arrays and primitives) is replaced outright by the overrides value. This
// mirrors TS's util/merge-objects.ts.
func deepMergeProviderOptions(base, overrides map[string]interface{}) map[string]interface{} {
	if base == nil && overrides == nil {
		return nil
	}
	if base == nil {
		return overrides
	}
	if overrides == nil {
		return base
	}

	result := make(map[string]interface{}, len(base)+len(overrides))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overrides {
		baseVal, baseHas := result[k]
		if overrideMap, ok := v.(map[string]interface{}); ok {
			if baseMap, ok := baseVal.(map[string]interface{}); ok && baseHas {
				result[k] = deepMergeProviderOptions(baseMap, overrideMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}
