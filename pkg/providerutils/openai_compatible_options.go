package providerutils

import (
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
	if _, ok := providerOptions["openai-compatible"]; ok {
		warnings = append(warnings, deprecatedProviderOptionsKeyWarning("openai-compatible", "openaiCompatible"))
	}

	mergeProviderOptions(resolved, providerOptions["openaiCompatible"])
	mergeProviderOptions(resolved, providerOptions[providerName])

	camelProviderName := ToOpenAICompatibleCamelCase(providerName)
	if providerName != camelProviderName {
		if _, ok := providerOptions[providerName]; ok {
			warnings = append(warnings, deprecatedProviderOptionsKeyWarning(providerName, camelProviderName))
		}
		mergeProviderOptions(resolved, providerOptions[camelProviderName])
	}

	return resolved, warnings
}

// ToOpenAICompatibleCamelCase matches the TS helper that converts hyphen and
// underscore followed by a lower-case character to camelCase.
func ToOpenAICompatibleCamelCase(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	upperNext := false
	for _, r := range value {
		if r == '-' || r == '_' {
			upperNext = true
			continue
		}
		if upperNext && r >= 'a' && r <= 'z' {
			r = r - ('a' - 'A')
		}
		upperNext = false
		b.WriteRune(r)
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

// ApplyOpenAICompatibleCommonRequestOptions applies the generic request fields
// that the TS openai-compatible provider maps from provider options.
func ApplyOpenAICompatibleCommonRequestOptions(body map[string]interface{}, options map[string]interface{}) {
	if reasoningEffort, ok := OpenAICompatibleStringOption(options, "reasoningEffort", "reasoning_effort", "reasoning-effort"); ok {
		body["reasoning_effort"] = reasoningEffort
	}
	if textVerbosity, ok := OpenAICompatibleStringOption(options, "textVerbosity", "text_verbosity", "text-verbosity"); ok {
		body["verbosity"] = textVerbosity
	}
	if user, ok := OpenAICompatibleStringOption(options, "user"); ok {
		body["user"] = user
	}
}

func OpenAICompatibleCommonOptionWarnings(options map[string]interface{}) []types.Warning {
	return DeprecatedOpenAICompatibleOptionWarnings(options, map[string]string{
		"reasoning-effort": "reasoningEffort",
		"reasoning_effort": "reasoningEffort",
		"text-verbosity":   "textVerbosity",
		"text_verbosity":   "textVerbosity",
	})
}

func DeprecatedOpenAICompatibleOptionWarnings(options map[string]interface{}, replacements map[string]string) []types.Warning {
	var warnings []types.Warning
	for rawName, camelName := range replacements {
		if _, ok := options[rawName]; ok {
			warnings = append(warnings, deprecatedProviderOptionKeyWarning(rawName, camelName))
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
		Type:    "deprecated",
		Feature: "providerOptions key '" + rawName + "'",
		Details: message,
		Message: message,
	}
}

func deprecatedProviderOptionKeyWarning(rawName, camelName string) types.Warning {
	message := "Use '" + camelName + "' instead."
	return types.Warning{
		Type:    "deprecated",
		Feature: "provider option key '" + rawName + "'",
		Details: message,
		Message: message,
	}
}
