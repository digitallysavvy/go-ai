package zai

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// zaiChatOptionsSchema mirrors zaiLanguageModelChatOptions (TS
// zai-chat-language-model-options.ts).
var zaiChatOptionsSchema = providerutils.OptionSchema{
	"doSample": providerutils.OptBool,
	"thinking": providerutils.OptObject(providerutils.OptionSchema{
		"type":          providerutils.OptEnum("enabled", "disabled"),
		"clearThinking": providerutils.OptBool,
	}),
	"reasoningEffort": providerutils.OptEnum("none", "minimal", "low", "medium", "high", "xhigh", "max"),
	"toolStream":      providerutils.OptBool,
	"requestId":       providerutils.OptStringLen(6, 64),
	"userId":          providerutils.OptStringLen(6, 128),
}

func validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("zai", providerOptions, zaiChatOptionsSchema)
	return err
}
