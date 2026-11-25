package groq

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// groqChatOptionsSchema mirrors groqLanguageModelChatOptions (TS
// groq-chat-language-model-options.ts), which groq-chat-language-model.ts
// validates with parseProviderOptions({provider: 'groq'}).
var groqChatOptionsSchema = providerutils.OptionSchema{
	"reasoningFormat":   providerutils.OptEnum("parsed", "raw", "hidden"),
	"reasoningEffort":   providerutils.OptEnum("none", "default", "low", "medium", "high"),
	"parallelToolCalls": providerutils.OptBool,
	"user":              providerutils.OptString,
	"structuredOutputs": providerutils.OptBool,
	"strictJsonSchema":  providerutils.OptBool,
	"serviceTier":       providerutils.OptEnum("on_demand", "performance", "flex", "auto"),
}

func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("groq", providerOptions, groqChatOptionsSchema)
	return err
}
