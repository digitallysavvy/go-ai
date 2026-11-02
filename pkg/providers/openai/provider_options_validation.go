package openai

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Schemas mirror the zod objects the TS providers pass to parseProviderOptions
// (openai-chat-language-model-options.ts, openai-completion-language-model-
// options.ts, openai-embedding-model-options.ts and the openai-compatible
// counterparts). Invalid options fail the call with an InvalidArgumentError.

var openAIChatOptionsSchema = providerutils.OptionSchema{
	"logitBias":           providerutils.OptRecord(providerutils.OptKeyNumeric, providerutils.OptNumber),
	"logprobs":            providerutils.OptBoolOrNumber,
	"parallelToolCalls":   providerutils.OptBool,
	"user":                providerutils.OptString,
	"reasoningEffort":     providerutils.OptEnum("none", "minimal", "low", "medium", "high", "xhigh", "max"),
	"maxCompletionTokens": providerutils.OptNumber,
	"store":               providerutils.OptBool,
	"metadata":            providerutils.OptRecord(providerutils.OptKeyMax(64), providerutils.OptStringMax(512)),
	"prediction":          providerutils.OptRecord(nil, nil),
	"serviceTier":         providerutils.OptEnum("auto", "flex", "priority", "fast", "ultrafast", "default"),
	"strictJsonSchema":    providerutils.OptBool,
	"textVerbosity":       providerutils.OptEnum("low", "medium", "high"),
	"promptCacheKey":      providerutils.OptString,
	"promptCacheOptions": providerutils.OptObject(providerutils.OptionSchema{
		"mode": providerutils.OptEnum("implicit", "explicit"),
		"ttl":  providerutils.OptLiteral("30m"),
	}),
	"promptCacheRetention": providerutils.OptEnum("in_memory", "24h"),
	"safetyIdentifier":     providerutils.OptString,
	"systemMessageMode":    providerutils.OptEnum("system", "developer", "remove"),
	"forceReasoning":       providerutils.OptBool,
}

var openAICompletionOptionsSchema = providerutils.OptionSchema{
	"echo":      providerutils.OptBool,
	"logitBias": providerutils.OptRecord(nil, providerutils.OptNumber),
	"suffix":    providerutils.OptString,
	"user":      providerutils.OptString,
	"logprobs":  providerutils.OptBoolOrNumber,
}

var openAIEmbeddingOptionsSchema = providerutils.OptionSchema{
	"dimensions": providerutils.OptNumber,
	"user":       providerutils.OptString,
}

// validateProviderOptionKeys parses each key in order (TS awaits them in
// sequence, so the first invalid key decides the error).
func validateProviderOptionKeys(providerOptions map[string]interface{}, schema providerutils.OptionSchema, keys ...string) error {
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := providerutils.ParseProviderOptions(key, providerOptions, schema); err != nil {
			return err
		}
	}
	return nil
}

// validateChatProviderOptions mirrors the parseProviderOptions calls in
// OpenAIChatLanguageModel (key "openai") and OpenAICompatibleChatLanguageModel
// (keys openai-compatible, openaiCompatible, <name>, camelCase(<name>)).
func (m *LanguageModel) validateChatProviderOptions(providerOptions map[string]interface{}) error {
	if !m.provider.config.OpenAICompatible {
		return validateProviderOptionKeys(providerOptions, openAIChatOptionsSchema, "openai")
	}
	name := m.fileReferenceProvider()
	return providerutils.ValidateOpenAICompatibleChatOptions(providerOptions, name)
}

// validateCompletionProviderOptions mirrors OpenAICompletionLanguageModel
// (keys openai and <providerOptionsName>) and the openai-compatible
// completion model (<name> and camelCase(<name>)).
func (m *CompletionModel) validateCompletionProviderOptions(providerOptions map[string]interface{}) error {
	name := m.provider.completionProviderOptionsName()
	if !m.provider.config.OpenAICompatible {
		return validateProviderOptionKeys(providerOptions, openAICompletionOptionsSchema, "openai", name)
	}
	return providerutils.ValidateOpenAICompatibleCompletionOptions(providerOptions, name)
}

// validateEmbeddingProviderOptions mirrors OpenAIEmbeddingModel (key
// "openai") and OpenAICompatibleEmbeddingModel (keys openai-compatible,
// openaiCompatible, <name>).
func (m *EmbeddingModel) validateEmbeddingProviderOptions(providerOptions map[string]interface{}) error {
	if !m.provider.config.OpenAICompatible {
		return validateProviderOptionKeys(providerOptions, openAIEmbeddingOptionsSchema, "openai")
	}
	name, _, _ := strings.Cut(m.provider.Name(), ".")
	return providerutils.ValidateOpenAICompatibleEmbeddingOptions(providerOptions, name)
}

// openAIResponsesOptionsSchema is openaiLanguageModelResponsesOptionsSchema
// (openai-responses-language-model-options.ts). nullish members accept null;
// optional members reject it. promptCacheOptions has its own validator
// (validateResponsesPromptCacheOptions), and metadata is z.any().
var openAIResponsesOptionsSchema = providerutils.OptionSchema{
	"conversation": providerutils.OptNullable(providerutils.OptString),
	"include": providerutils.OptNullable(providerutils.OptArray(providerutils.OptEnum(
		"reasoning.encrypted_content", "file_search_call.results", "web_search_call.results", "message.output_text.logprobs"), 0, -1)),
	"includeWebSearchSources": providerutils.OptBool,
	"instructions":            providerutils.OptNullable(providerutils.OptString),
	// z.union([z.boolean(), z.number().min(1).max(TOP_LOGPROBS_MAX)])
	"logprobs": func(path string, v interface{}) error {
		if _, ok := v.(bool); ok {
			return nil
		}
		return providerutils.OptRange(1, 20)(path, v)
	},
	"maxToolCalls":                providerutils.OptNullable(providerutils.OptNumber),
	"parallelToolCalls":           providerutils.OptNullable(providerutils.OptBool),
	"previousResponseId":          providerutils.OptNullable(providerutils.OptString),
	"promptCacheKey":              providerutils.OptNullable(providerutils.OptString),
	"promptCacheRetention":        providerutils.OptNullable(providerutils.OptEnum("in_memory", "24h")),
	"reasoningEffort":             providerutils.OptNullable(providerutils.OptString),
	"reasoningEffortUpdate":       providerutils.OptEnum("none", "low", "medium", "high", "xhigh", "max"),
	"reasoningMode":               providerutils.OptEnum("standard", "pro"),
	"reasoningContext":            providerutils.OptEnum("auto", "current_turn", "all_turns"),
	"reasoningSummary":            providerutils.OptNullable(providerutils.OptString),
	"safetyIdentifier":            providerutils.OptNullable(providerutils.OptString),
	"serviceTier":                 providerutils.OptNullable(providerutils.OptEnum("auto", "flex", "priority", "fast", "ultrafast", "default")),
	"store":                       providerutils.OptNullable(providerutils.OptBool),
	"passThroughUnsupportedFiles": providerutils.OptBool,
	"strictJsonSchema":            providerutils.OptNullable(providerutils.OptBool),
	"textVerbosity":               providerutils.OptNullable(providerutils.OptEnum("low", "medium", "high")),
	"truncation":                  providerutils.OptNullable(providerutils.OptEnum("auto", "disabled")),
	"user":                        providerutils.OptNullable(providerutils.OptString),
	"systemMessageMode":           providerutils.OptEnum("system", "developer", "remove"),
	"forceReasoning":              providerutils.OptBool,
	"contextManagement": providerutils.OptNullable(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"type":             providerutils.OptLiteral("compaction"),
		"compactThreshold": providerutils.OptNumber,
	}, "type", "compactThreshold"), 0, -1)),
	"compactionTrigger": providerutils.OptBool,
	"allowedTools": providerutils.OptObjectWith(providerutils.OptionSchema{
		"toolNames": providerutils.OptArray(providerutils.OptString, 1, -1),
		"mode":      providerutils.OptEnum("auto", "required"),
	}, "toolNames"),
}

// validateResponsesProviderOptions mirrors the Responses model's
// parseProviderOptions calls: providerOptions[providerOptionsName], and
// providerOptions.openai when that is absent or null and the name is not
// "openai" (Azure). Typed option values are valid by construction.
func validateResponsesProviderOptions(providerOptions map[string]interface{}, providerOptionsName string) error {
	if raw, ok := providerOptions[providerOptionsName]; (ok && raw != nil) || providerOptionsName == "openai" {
		return providerutils.ValidateMapProviderOptions(providerOptionsName, providerOptions, openAIResponsesOptionsSchema)
	}
	return providerutils.ValidateMapProviderOptions("openai", providerOptions, openAIResponsesOptionsSchema)
}
