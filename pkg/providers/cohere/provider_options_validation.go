package cohere

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// cohereChatOptionsSchema mirrors cohereLanguageModelChatOptions (TS
// cohere-chat-language-model-options.ts).
var cohereChatOptionsSchema = providerutils.OptionSchema{
	"thinking": providerutils.OptObject(providerutils.OptionSchema{
		"type":        providerutils.OptEnum("enabled", "disabled"),
		"tokenBudget": providerutils.OptNumber,
	}),
}

func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("cohere", providerOptions, cohereChatOptionsSchema)
	return err
}
