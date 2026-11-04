package providerutils

import (
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ResolveOpenAICompatibleProviderOptions mirrors the TypeScript
// openai-compatible provider option merge order:
//
//	providerOptions["openai-compatible"] (deprecated)
//	providerOptions["openaiCompatible"]
//	providerOptions[rawProviderName]
//	providerOptions[camelCase(rawProviderName)]
//
// Later entries override earlier entries. Deprecated kebab/snake-case option
// keys are accepted and return warnings instead of being silently ignored.
func ResolveOpenAICompatibleProviderOptions(providerName string, providerOptions map[string]interface{}) (map[string]interface{}, []types.Warning) {
	resolved := map[string]interface{}{}
	if providerOptions == nil {
		return resolved, nil
	}

	var warnings []types.Warning
	mergeProviderOptions(resolved, providerOptions["openai-compatible"])
	if providerOptions["openai-compatible"] != nil {
		warnings = append(warnings, deprecatedProviderOptionsKeyWarning("openai-compatible", "openaiCompatible"))
	}

	mergeProviderOptions(resolved, providerOptions["openaiCompatible"])
	mergeProviderOptions(resolved, providerOptions[providerName])

	camelProviderName := ToOpenAICompatibleCamelCase(providerName)
	if providerName != camelProviderName {
		if providerOptions[providerName] != nil {
			warnings = append(warnings, deprecatedProviderOptionsKeyWarning(providerName, camelProviderName))
		}
		mergeProviderOptions(resolved, providerOptions[camelProviderName])
	}

	return resolved, warnings
}

// ToOpenAICompatibleCamelCase matches the TS helper that converts hyphen and
// underscore followed by a lower-case character to camelCase.
func ToOpenAICompatibleCamelCase(value string) string {
	// JS: value.replace(/[_-]([a-z])/g, g => g[1].toUpperCase()). A hyphen or
	// underscore that is not followed by a-z (digit, another separator, end of
	// string) is kept as is.
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c == '-' || c == '_') && i+1 < len(value) && value[i+1] >= 'a' && value[i+1] <= 'z' {
			b.WriteByte(value[i+1] - ('a' - 'A'))
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func OpenAICompatibleStringOption(options map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := options[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func OpenAICompatibleBoolOption(options map[string]interface{}, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := options[key].(bool); ok {
			return value, true
		}
	}
	return false, false
}

func OpenAICompatibleIntOption(options map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		switch value := options[key].(type) {
		case int:
			return value, true
		case int8:
			return int(value), true
		case int16:
			return int(value), true
		case int32:
			return int(value), true
		case int64:
			return int(value), true
		case uint:
			return int(value), true
		case uint8:
			return int(value), true
		case uint16:
			return int(value), true
		case uint32:
			return int(value), true
		case uint64:
			return int(value), true
		case float64:
			return int(value), true
		case float32:
			return int(value), true
		}
	}
	return 0, false
}

// OpenAICompatibleReasoningEffortOption reads the reasoningEffort provider
// option like TS openai-compatible (5006d83c4e: `reasoningEffort:
// z.string().nullish()`): a string, or an explicit null (returned as a nil
// value with ok=true, sent as "reasoning_effort": null). The deprecated
// v0.5 spelling reasoning-effort is accepted as a string. A raw
// reasoning_effort is not an option: it is a request body passthrough (TS
// 5006d83c4e), sent by ApplyOpenAICompatibleCallSettings and overwritten
// only when the effort resolves.
func OpenAICompatibleReasoningEffortOption(options map[string]interface{}) (interface{}, bool) {
	if v, ok := options["reasoningEffort"]; ok {
		switch v := v.(type) {
		case nil:
			return nil, true
		case string:
			return v, true
		}
	}
	if v, ok := OpenAICompatibleStringOption(options, "reasoning-effort"); ok {
		return v, true
	}
	return nil, false
}

// ResolveOpenAICompatibleReasoningEffort ports TS
// OpenAICompatibleChatLanguageModel's reasoning effort (5006d83c4e,
// 61a315e333): the provider option when it is set (an explicit null
// included, returned as a nil value), otherwise the top-level reasoning when
// it is custom, through mapReasoning when one is given (TS
// config.mapReasoning; it may report ok=false to send nothing), else as is.
// ok=false means no reasoning_effort is written.
func ResolveOpenAICompatibleReasoningEffort(options map[string]interface{}, reasoning *types.ReasoningLevel, mapReasoning func(reasoning *types.ReasoningLevel) (string, bool)) (interface{}, bool) {
	if v, ok := OpenAICompatibleReasoningEffortOption(options); ok {
		return v, true
	}
	if !IsCustomReasoning(reasoning) {
		return nil, false
	}
	if mapReasoning != nil {
		effort, ok := mapReasoning(reasoning)
		if !ok {
			return nil, false
		}
		return effort, true
	}
	return string(*reasoning), true
}

// ApplyOpenAICompatibleCommonRequestOptions applies the generic request fields
// that the TS openai-compatible provider maps from provider options. An
// explicit null reasoningEffort is sent as "reasoning_effort": null and wins
// over a reasoning_effort the caller set from the top-level reasoning before
// (TS 5006d83c4e).
func ApplyOpenAICompatibleCommonRequestOptions(body map[string]interface{}, options map[string]interface{}) {
	if reasoningEffort, ok := OpenAICompatibleReasoningEffortOption(options); ok {
		body["reasoning_effort"] = reasoningEffort
	}
	if textVerbosity, ok := OpenAICompatibleStringOption(options, "textVerbosity", "text_verbosity", "text-verbosity"); ok {
		body["verbosity"] = textVerbosity
	}
	if user, ok := OpenAICompatibleStringOption(options, "user"); ok {
		body["user"] = user
	}
}

// ApplyOpenAICompatibleCallSettings applies what TS
// OpenAICompatibleChatLanguageModel.getArgs sends beyond the model, messages,
// tools and max_tokens / temperature / top_p / stop: frequency_penalty,
// presence_penalty and seed, an `unsupported` warning for topK, and the
// provider options that are not one of the chat option fields (user,
// reasoningEffort, textVerbosity, strictJsonSchema), which TS spreads straight
// into the request body from providerOptions[name] and providerOptions[camel]
// (so they can override the standardized settings). Keys TS always overwrites
// afterwards (messages, tools, tool_choice) and the extra `exclude` keys a
// provider handles itself are skipped. stream and stream_options are skipped
// only for a streaming body (body["stream"] is true, set by the caller before
// this call): TS doStream writes them after the spread, while doGenerate
// sends a raw stream / stream_options as given. A raw
// reasoning_effort is passed through (null included): TS writes
// reasoning_effort afterwards only when it has a value (5006d83c4e), so
// callers set it after this call only when the effort resolves. A raw
// verbosity is passed through the same way: TS writes verbosity afterwards
// only when textVerbosity is set (add01abd2c).
func ApplyOpenAICompatibleCallSettings(body map[string]interface{}, frequencyPenalty, presencePenalty *float64, seed, topK *int, providerName string, providerOptions map[string]interface{}, exclude ...string) []types.Warning {
	var warnings []types.Warning
	if topK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if frequencyPenalty != nil {
		body["frequency_penalty"] = *frequencyPenalty
	}
	if presencePenalty != nil {
		body["presence_penalty"] = *presencePenalty
	}
	if seed != nil {
		body["seed"] = *seed
	}
	skip := map[string]bool{
		"user": true, "reasoningEffort": true, "textVerbosity": true, "strictJsonSchema": true,
		"messages": true, "tools": true, "tool_choice": true,
	}
	if streaming, _ := body["stream"].(bool); streaming {
		skip["stream"], skip["stream_options"] = true, true
	}
	for _, k := range exclude {
		skip[k] = true
	}
	passthrough := map[string]interface{}{}
	mergeProviderOptions(passthrough, providerOptions[providerName])
	if camel := ToOpenAICompatibleCamelCase(providerName); camel != providerName {
		mergeProviderOptions(passthrough, providerOptions[camel])
	}
	for k, v := range passthrough {
		if !skip[k] {
			body[k] = v
		}
	}
	return warnings
}

func OpenAICompatibleCommonOptionWarnings(options map[string]interface{}) []types.Warning {
	return DeprecatedOpenAICompatibleOptionWarnings(options, map[string]string{
		"reasoning-effort": "reasoningEffort",
		"text-verbosity":   "textVerbosity",
		"text_verbosity":   "textVerbosity",
	})
}

// DeprecatedOpenAICompatibleOptionWarnings returns a deprecation warning for
// each deprecated option key present, in key order (deterministic, unlike
// map iteration).
func DeprecatedOpenAICompatibleOptionWarnings(options map[string]interface{}, replacements map[string]string) []types.Warning {
	rawNames := make([]string, 0, len(replacements))
	for rawName := range replacements {
		rawNames = append(rawNames, rawName)
	}
	sort.Strings(rawNames)
	var warnings []types.Warning
	for _, rawName := range rawNames {
		if _, ok := options[rawName]; ok {
			warnings = append(warnings, deprecatedProviderOptionKeyWarning(rawName, replacements[rawName]))
		}
	}
	return warnings
}

func mergeProviderOptions(target map[string]interface{}, raw interface{}) {
	options, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	for key, value := range options {
		target[key] = value
	}
}

func deprecatedProviderOptionsKeyWarning(rawName, camelName string) types.Warning {
	message := "Use '" + camelName + "' instead."
	return types.Warning{
		Type: "deprecated",
		// TS: { type: 'deprecated', setting: "providerOptions key '<raw>'", message }.
		// Feature is kept for callers of v0.5.2, which only set it.
		Setting: "providerOptions key '" + rawName + "'",
		Feature: "providerOptions key '" + rawName + "'",
		Details: message,
		Message: message,
	}
}

func deprecatedProviderOptionKeyWarning(rawName, camelName string) types.Warning {
	message := "Use '" + camelName + "' instead."
	return types.Warning{
		Type:    "deprecated",
		Setting: "provider option key '" + rawName + "'",
		Feature: "provider option key '" + rawName + "'",
		Details: message,
		Message: message,
	}
}

// ResolveOpenAICompatibleMetadataKey mirrors TS resolveProviderOptionsKey:
// the providerMetadata namespace is the camelCase provider name only when the
// caller supplied provider options under that camelCase key, else the raw
// provider name.
func ResolveOpenAICompatibleMetadataKey(rawName string, providerOptions map[string]interface{}) string {
	camel := ToOpenAICompatibleCamelCase(rawName)
	if camel != rawName && providerOptions[camel] != nil {
		return camel
	}
	return rawName
}
