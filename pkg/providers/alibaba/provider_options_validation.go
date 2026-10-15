package alibaba

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// alibabaChatOptionsSchema mirrors alibabaLanguageModelChatOptions (TS
// alibaba-chat-language-model-options.ts).
var alibabaChatOptionsSchema = providerutils.OptionSchema{
	"enableThinking":    providerutils.OptBool,
	"thinkingBudget":    providerutils.OptPositive(false),
	"preserveThinking":  providerutils.OptBool,
	"parallelToolCalls": providerutils.OptBool,
}

func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("alibaba", providerOptions, alibabaChatOptionsSchema)
	return err
}
