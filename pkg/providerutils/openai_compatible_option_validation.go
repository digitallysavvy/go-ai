package providerutils

// Schemas mirror the zod objects TS @ai-sdk/openai-compatible passes to
// parseProviderOptions (openaiCompatibleLanguageModelChatOptions,
// openaiCompatibleLanguageModelCompletionOptions,
// openaiCompatibleEmbeddingModelOptions). The wrapper providers built on it
// (togetherai, fireworks, gmicloud, baseten, ...) share them.

var openAICompatChatOptionsSchema = OptionSchema{
	"user":             OptString,
	"reasoningEffort":  OptString,
	"textVerbosity":    OptString,
	"strictJsonSchema": OptBool,
}

var openAICompatCompletionOptionsSchema = OptionSchema{
	"echo":      OptBool,
	"logitBias": OptRecord(nil, OptNumber),
	"suffix":    OptString,
	"user":      OptString,
}

var openAICompatEmbeddingOptionsSchema = OptionSchema{
	"dimensions": OptNumber,
	"user":       OptString,
}

// ParseProviderOptionKeys parses each key in order (TS awaits them in
// sequence, so the first invalid key decides the error).
func ParseProviderOptionKeys(providerOptions map[string]interface{}, schema OptionSchema, keys ...string) error {
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := ParseProviderOptions(key, providerOptions, schema); err != nil {
			return err
		}
	}
	return nil
}

// ValidateOpenAICompatibleChatOptions is the option parsing of
// OpenAICompatibleChatLanguageModel (keys openai-compatible, openaiCompatible,
// <providerOptionsName>, camelCase(<providerOptionsName>)).
func ValidateOpenAICompatibleChatOptions(providerOptions map[string]interface{}, name string) error {
	return ParseProviderOptionKeys(providerOptions, openAICompatChatOptionsSchema,
		"openai-compatible", "openaiCompatible", name, ToOpenAICompatibleCamelCase(name))
}

// ValidateOpenAICompatibleCompletionOptions is the option parsing of the
// openai-compatible completion model (<name> and camelCase(<name>)).
func ValidateOpenAICompatibleCompletionOptions(providerOptions map[string]interface{}, name string) error {
	return ParseProviderOptionKeys(providerOptions, openAICompatCompletionOptionsSchema,
		name, ToOpenAICompatibleCamelCase(name))
}

// ValidateOpenAICompatibleEmbeddingOptions is the option parsing of
// OpenAICompatibleEmbeddingModel (keys openai-compatible, openaiCompatible,
// <name>).
func ValidateOpenAICompatibleEmbeddingOptions(providerOptions map[string]interface{}, name string) error {
	return ParseProviderOptionKeys(providerOptions, openAICompatEmbeddingOptionsSchema,
		"openai-compatible", "openaiCompatible", name)
}
