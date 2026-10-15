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

var openAICompatChatOptionsSchema = providerutils.OptionSchema{
	"user":             providerutils.OptString,
	"reasoningEffort":  providerutils.OptString,
	"textVerbosity":    providerutils.OptString,
	"strictJsonSchema": providerutils.OptBool,
}

var openAICompletionOptionsSchema = providerutils.OptionSchema{
	"echo":      providerutils.OptBool,
	"logitBias": providerutils.OptRecord(nil, providerutils.OptNumber),
	"suffix":    providerutils.OptString,
	"user":      providerutils.OptString,
	"logprobs":  providerutils.OptBoolOrNumber,
}

var openAICompatCompletionOptionsSchema = providerutils.OptionSchema{
	"echo":      providerutils.OptBool,
	"logitBias": providerutils.OptRecord(nil, providerutils.OptNumber),
	"suffix":    providerutils.OptString,
	"user":      providerutils.OptString,
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
	return validateProviderOptionKeys(providerOptions, openAICompatChatOptionsSchema,
		"openai-compatible", "openaiCompatible", name, providerutils.ToOpenAICompatibleCamelCase(name))
}

// validateCompletionProviderOptions mirrors OpenAICompletionLanguageModel
// (keys openai and <providerOptionsName>) and the openai-compatible
// completion model (<name> and camelCase(<name>)).
func (m *CompletionModel) validateCompletionProviderOptions(providerOptions map[string]interface{}) error {
	name := m.provider.completionProviderOptionsName()
	if !m.provider.config.OpenAICompatible {
		return validateProviderOptionKeys(providerOptions, openAICompletionOptionsSchema, "openai", name)
	}
	return validateProviderOptionKeys(providerOptions, openAICompatCompletionOptionsSchema,
		name, providerutils.ToOpenAICompatibleCamelCase(name))
}

// validateEmbeddingProviderOptions mirrors OpenAIEmbeddingModel (key
// "openai") and OpenAICompatibleEmbeddingModel (keys openai-compatible,
// openaiCompatible, <name>).
func (m *EmbeddingModel) validateEmbeddingProviderOptions(providerOptions map[string]interface{}) error {
	if !m.provider.config.OpenAICompatible {
		return validateProviderOptionKeys(providerOptions, openAIEmbeddingOptionsSchema, "openai")
	}
	name, _, _ := strings.Cut(m.provider.Name(), ".")
	return validateProviderOptionKeys(providerOptions, openAIEmbeddingOptionsSchema,
		"openai-compatible", "openaiCompatible", name)
}
