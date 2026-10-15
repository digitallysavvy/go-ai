package deepseek

import (
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var zero, twenty = 0, 20

// deepseekChatOptionsSchema mirrors deepseekLanguageModelChatOptions (TS
// chat/deepseek-chat-language-model-options.ts).
var deepseekChatOptionsSchema = providerutils.OptionSchema{
	"logprobs":    providerutils.OptBool,
	"topLogprobs": providerutils.OptInt(&zero, &twenty),
	"userId": providerutils.OptAnd(
		providerutils.OptMatch(deepseekUserIDPattern, "userId must match /^[a-zA-Z0-9_-]+$/"),
		providerutils.OptStringLen(0, 512),
	),
	"thinking": providerutils.OptObject(providerutils.OptionSchema{
		"type": providerutils.OptEnum("adaptive", "enabled", "disabled"),
	}),
	"reasoningEffort":  providerutils.OptEnum("low", "medium", "high", "xhigh", "max"),
	"strictJsonSchema": providerutils.OptBool,
}

func (m *LanguageModel) validateChatProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions(m.provider.providerOptionsName(), providerOptions, deepseekChatOptionsSchema)
	return err
}
