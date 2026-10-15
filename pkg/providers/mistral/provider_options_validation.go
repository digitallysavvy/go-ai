package mistral

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// mistralChatOptionsSchema mirrors mistralLanguageModelChatOptions (TS
// mistral-chat-language-model-options.ts); the conversation model reuses it
// (mistralLanguageModelConversationOptions).
var mistralChatOptionsSchema = providerutils.OptionSchema{
	"safePrompt":         providerutils.OptBool,
	"documentImageLimit": providerutils.OptNumber,
	"documentPageLimit":  providerutils.OptNumber,
	"structuredOutputs":  providerutils.OptBool,
	"strictJsonSchema":   providerutils.OptBool,
	"parallelToolCalls":  providerutils.OptBool,
	"promptCacheKey":     providerutils.OptString,
	"reasoningEffort":    providerutils.OptEnum("high", "none"),
}

// mistralEmbeddingOptionsSchema mirrors mistralEmbeddingModelOptions.
var mistralEmbeddingOptionsSchema = providerutils.OptionSchema{
	"metadata":        providerutils.OptRecord(nil, nil),
	"outputDimension": providerutils.OptPositive(true),
	"outputDtype":     providerutils.OptEnum("float", "int8", "uint8", "binary", "ubinary"),
}

func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("mistral", providerOptions, mistralChatOptionsSchema)
	return err
}

func validateEmbeddingProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("mistral", providerOptions, mistralEmbeddingOptionsSchema)
	return err
}
