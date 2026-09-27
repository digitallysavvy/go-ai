package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file implements per-call provider options resolution, mirroring TS
// AnthropicLanguageModel.prepareRequest (anthropic-language-model.ts at
// ai@7.0.113): every call parses providerOptions[providerOptionsName] (and
// the canonical "anthropic" key) against the anthropicLanguageModelOptions
// schema, merges the two (custom key wins field-by-field over canonical),
// and validates the result.
//
// The Go SDK additionally supports construction-time ModelOptions (passed to
// NewLanguageModel/LanguageModelWithOptions) as defaults; TS has no
// construction-time concept since AnthropicLanguageModelConfig carries no
// model options at all. Here, construction-time ModelOptions are the
// defaults and the per-call merged result wins field-by-field, per the
// implementation instructions for this slice.

// providerOptionsName mirrors TS getProviderOptionsName(config.provider):
// the provider label up to (but not including) the first '.'. TS's
// config.provider strings look like "anthropic.messages",
// "minimax.messages", or "bedrock.anthropic.messages" (Bedrock-Anthropic).
// Go's Provider.Name() already omits the ".messages" suffix (e.g.
// "anthropic", "minimax", "bedrock.anthropic.messages" for Bedrock), so
// splitting on the first '.' here reproduces the same result:
// "anthropic" -> "anthropic", "minimax" -> "minimax",
// "bedrock.anthropic.messages" -> "bedrock".
func (m *LanguageModel) providerOptionsName() string {
	name := m.provider.Name()
	if idx := strings.IndexByte(name, '.'); idx != -1 {
		return name[:idx]
	}
	return name
}

// resolveCallOptions computes the effective ModelOptions for a single call:
// construction-time m.options as defaults, overridden field-by-field by the
// merged (canonical "anthropic" + custom providerOptionsName, custom wins)
// per-call provider options. Returns a validation error shaped like TS's
// parseProviderOptions InvalidArgumentError when either raw options object
// fails to decode.
//
// There is deliberately no provider-specific narrowing hook here (e.g. for
// MiniMax's thinking.type): TS's AnthropicLanguageModel.prepareRequest always
// parses providerOptions.<name> against the single shared
// anthropicLanguageModelOptions schema, regardless of which wrapper
// constructed it. MiniMax exports a narrower MiniMaxLanguageModelOptions type
// (thinking.type: "adaptive" | "disabled") purely for compile-time
// TypeScript ergonomics — minimax-provider.ts never imports or validates
// against it at runtime, and minimax-reasoning.test.ts never asserts
// rejection of `thinking: { type: "enabled" }`. Matching that, Go accepts
// whatever the shared anthropicLanguageModelOptions-equivalent schema allows
// for every wrapper.
//
// The second return value mirrors TS's usedCustomProviderKey: true when
// providerOptions[providerOptionsName] was present (non-nil) for a
// providerOptionsName other than "anthropic" — i.e. the caller explicitly
// used the wrapper's own key (e.g. providerOptions.minimax), not just the
// canonical "anthropic" key. doGenerate/doStream use it to decide whether to
// duplicate the response providerMetadata under providerOptionsName.
func (m *LanguageModel) resolveCallOptions(opts *provider.GenerateOptions) (*ModelOptions, bool, error) {
	base := ModelOptions{}
	if m.options != nil {
		base = *m.options
	}

	if opts == nil || len(opts.ProviderOptions) == 0 {
		return &base, false, nil
	}

	providerOptionsName := m.providerOptionsName()

	canonicalOverlay, err := decodeAnthropicCallOptionsAt(opts.ProviderOptions, "anthropic")
	if err != nil {
		return nil, false, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: fmt.Sprintf("invalid anthropic provider options: %s", err.Error()),
		}
	}

	var customOverlay *ModelOptions
	usedCustomProviderKey := false
	if providerOptionsName != "anthropic" {
		customOverlay, err = decodeAnthropicCallOptionsAt(opts.ProviderOptions, providerOptionsName)
		if err != nil {
			return nil, false, &providererrors.InvalidArgumentError{
				Field:   "providerOptions",
				Message: fmt.Sprintf("invalid %s provider options: %s", providerOptionsName, err.Error()),
			}
		}
		usedCustomProviderKey = customOverlay != nil
	}

	applyModelOptionsOverlay(&base, canonicalOverlay)
	applyModelOptionsOverlay(&base, customOverlay)

	return &base, usedCustomProviderKey, nil
}

// decodeAnthropicCallOptionsAt reads providerOptions[key] and decodes it into
// a *ModelOptions overlay. Returns (nil, nil) when the key is absent, mirrors
// TS parseProviderOptions: `if (providerOptions?.[provider] == null) return
// undefined;`.
func decodeAnthropicCallOptionsAt(raw map[string]interface{}, key string) (*ModelOptions, error) {
	value, ok := raw[key]
	if !ok || value == nil {
		return nil, nil
	}
	asMap, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected an object")
	}
	return decodeAnthropicCallOptions(asMap)
}

// decodeAnthropicCallOptions decodes a raw providerOptions.<key> map into a
// *ModelOptions overlay, matching the anthropicLanguageModelOptions zod
// schema (anthropic-language-model-options.ts). Fields not present in raw
// are left at their zero value in the returned overlay, which
// applyModelOptionsOverlay treats as "not provided" (see its doc comment for
// the one exception, Fallbacks/FallbacksDefault, which needs raw's presence
// captured separately since "fallbacks" is a TS union type that doesn't
// round-trip through a single Go field).
func decodeAnthropicCallOptions(raw map[string]interface{}) (*ModelOptions, error) {
	if raw == nil {
		return nil, nil
	}

	// "fallbacks" is a TS union (the literal "default" or an array of
	// FallbackConfig); pull it out before the generic unmarshal, which
	// cannot decode a union into a single Go field.
	fallbacksRaw, hasFallbacks := raw["fallbacks"]
	working := raw
	if hasFallbacks {
		working = make(map[string]interface{}, len(raw))
		for k, v := range raw {
			working[k] = v
		}
		delete(working, "fallbacks")
	}

	data, err := json.Marshal(working)
	if err != nil {
		return nil, err
	}

	var overlay ModelOptions
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, err
	}

	if hasFallbacks {
		switch v := fallbacksRaw.(type) {
		case string:
			if v != "default" {
				return nil, fmt.Errorf("fallbacks: invalid literal %q, expected \"default\" or an array", v)
			}
			overlay.FallbacksDefault = true
		case []interface{}:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			var fbs []FallbackConfig
			if err := json.Unmarshal(b, &fbs); err != nil {
				return nil, fmt.Errorf("fallbacks: %w", err)
			}
			overlay.Fallbacks = fbs
		case nil:
			// fallbacks: null -> treated as not provided.
		default:
			return nil, fmt.Errorf(`fallbacks: must be "default" or an array`)
		}
	}

	if err := validateAnthropicCallOptions(&overlay); err != nil {
		return nil, err
	}

	return &overlay, nil
}

// validateAnthropicCallOptions performs light structural validation of the
// decoded overlay, matching the zod enum/discriminator checks in
// anthropicLanguageModelOptions where practical in Go's weaker type system.
func validateAnthropicCallOptions(o *ModelOptions) error {
	if o.Thinking != nil {
		switch o.Thinking.Type {
		case "", ThinkingTypeAdaptive, ThinkingTypeEnabled, ThinkingTypeDisabled:
		default:
			return fmt.Errorf("thinking.type: invalid value %q", o.Thinking.Type)
		}
	}
	if o.Effort != "" {
		switch o.Effort {
		case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		default:
			return fmt.Errorf("effort: invalid value %q", o.Effort)
		}
	}
	if o.Speed != "" {
		switch o.Speed {
		case SpeedFast, SpeedStandard:
		default:
			return fmt.Errorf("speed: invalid value %q", o.Speed)
		}
	}
	if o.ServiceTier != "" && o.ServiceTier != "auto" && o.ServiceTier != "standard_only" {
		return fmt.Errorf("serviceTier: invalid value %q", o.ServiceTier)
	}
	if o.InferenceGeo != "" && o.InferenceGeo != "us" && o.InferenceGeo != "global" {
		return fmt.Errorf("inferenceGeo: invalid value %q", o.InferenceGeo)
	}
	if o.StructuredOutputMode != "" {
		switch o.StructuredOutputMode {
		case StructuredOutputAuto, StructuredOutputFormat, StructuredOutputJSONTool:
		default:
			return fmt.Errorf("structuredOutputMode: invalid value %q", o.StructuredOutputMode)
		}
	}
	if o.TaskBudget != nil {
		if o.TaskBudget.Type != "tokens" {
			return fmt.Errorf("taskBudget.type: invalid value %q", o.TaskBudget.Type)
		}
		if o.TaskBudget.Total < 20000 {
			return fmt.Errorf("taskBudget.total: must be >= 20000, got %d", o.TaskBudget.Total)
		}
	}
	for _, sg := range o.Safeguards {
		if sg.Type != "dangerous_tool_use" {
			return fmt.Errorf("safeguards[].type: invalid value %q", sg.Type)
		}
	}
	if o.Compaction != nil && o.Compaction.Type != "summarize" {
		return fmt.Errorf("compaction.type: invalid value %q", o.Compaction.Type)
	}
	for _, s := range o.MCPServers {
		if s.Type != "url" {
			return fmt.Errorf("mcpServers[].type: invalid value %q", s.Type)
		}
	}
	if o.Container != nil {
		for _, sk := range o.Container.Skills {
			if sk.Type != "anthropic" && sk.Type != "custom" {
				return fmt.Errorf("container.skills[].type: invalid value %q", sk.Type)
			}
		}
	}
	for _, fb := range o.Fallbacks {
		if fb.Model == "" {
			return fmt.Errorf("fallbacks[].model: required")
		}
	}
	return nil
}

// applyModelOptionsOverlay copies every field the caller actually provided in
// overlay onto dst, leaving fields dst already has untouched when overlay
// left them unset. This implements the "call wins field-by-field" merge:
// dst starts as either the construction-time defaults or the
// already-applied canonical overlay, and overlay is either the canonical or
// custom per-call parse result.
//
// "Provided" is detected via each field's own zero value (nil for pointers
// and slices, "" for strings), which works because ModelOptions already
// treats the zero value as "unset" throughout request.go — with one
// intentional exception: Fallbacks/FallbacksDefault, where an explicit empty
// per-call `fallbacks: []` is indistinguishable from "not provided" and
// therefore never clears a construction-time default (matches TS, which
// also only forwards fallbacks when non-empty or "default").
func applyModelOptionsOverlay(dst *ModelOptions, overlay *ModelOptions) {
	if overlay == nil {
		return
	}
	if overlay.ContextManagement != nil {
		dst.ContextManagement = overlay.ContextManagement
	}
	if overlay.Compaction != nil {
		dst.Compaction = overlay.Compaction
	}
	if overlay.Thinking != nil {
		dst.Thinking = overlay.Thinking
	}
	if overlay.Speed != "" {
		dst.Speed = overlay.Speed
	}
	if overlay.CacheControl != nil {
		dst.CacheControl = overlay.CacheControl
	}
	// AutomaticCaching is a plain bool (not *bool, unlike ToolStreaming /
	// DisableParallelToolUse / SendReasoning), so a per-call `false` cannot
	// be distinguished from "not provided" here; only a per-call `true` can
	// meaningfully override. This mirrors FallbacksDefault below, the
	// struct's other plain-bool shorthand.
	if overlay.AutomaticCaching {
		dst.AutomaticCaching = true
	}
	if overlay.ContainerID != "" {
		dst.ContainerID = overlay.ContainerID
	}
	if overlay.Effort != "" {
		dst.Effort = overlay.Effort
	}
	if overlay.TaskBudget != nil {
		dst.TaskBudget = overlay.TaskBudget
	}
	if overlay.InferenceGeo != "" {
		dst.InferenceGeo = overlay.InferenceGeo
	}
	if overlay.FallbacksDefault {
		dst.FallbacksDefault = true
		dst.Fallbacks = nil
	} else if len(overlay.Fallbacks) > 0 {
		dst.Fallbacks = overlay.Fallbacks
		dst.FallbacksDefault = false
	}
	if overlay.ServiceTier != "" {
		dst.ServiceTier = overlay.ServiceTier
	}
	if len(overlay.AnthropicBeta) > 0 {
		dst.AnthropicBeta = overlay.AnthropicBeta
	}
	if len(overlay.Safeguards) > 0 {
		dst.Safeguards = overlay.Safeguards
	}
	if overlay.ToolStreaming != nil {
		dst.ToolStreaming = overlay.ToolStreaming
	}
	if overlay.DisableParallelToolUse != nil {
		dst.DisableParallelToolUse = overlay.DisableParallelToolUse
	}
	if len(overlay.MCPServers) > 0 {
		dst.MCPServers = overlay.MCPServers
	}
	if overlay.Container != nil {
		dst.Container = overlay.Container
	}
	if overlay.StructuredOutputMode != "" {
		dst.StructuredOutputMode = overlay.StructuredOutputMode
	}
	if overlay.SendReasoning != nil {
		dst.SendReasoning = overlay.SendReasoning
	}
	if overlay.Metadata != nil {
		dst.Metadata = overlay.Metadata
	}
}
