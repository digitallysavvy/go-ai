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

// alibabaVideoOptionsSchema mirrors alibabaVideoModelOptionsSchema (TS
// alibaba-video-model-options.ts; a looseObject, so unknown keys pass).
var alibabaVideoOptionsSchema = providerutils.OptionSchema{
	"negativePrompt": providerutils.OptNullable(providerutils.OptString),
	"audioUrl":       providerutils.OptNullable(providerutils.OptString),
	"promptExtend":   providerutils.OptNullable(providerutils.OptBool),
	"shotType":       providerutils.OptNullable(providerutils.OptEnum("single", "multi")),
	"watermark":      providerutils.OptNullable(providerutils.OptBool),
	"audio":          providerutils.OptNullable(providerutils.OptBool),
	"referenceUrls":  providerutils.OptNullable(providerutils.OptArray(providerutils.OptString, 0, -1)),
	"media": providerutils.OptNullable(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"type": providerutils.OptEnum("reference_image", "reference_video", "reference_audio",
			"first_frame", "last_frame", "file", "link"),
		"url":            providerutils.OptString,
		"referenceVoice": providerutils.OptNullable(providerutils.OptString),
	}, "type", "url"), 0, -1)),
	"ratio":          providerutils.OptNullable(providerutils.OptEnum("adaptive", "16:9", "9:16", "1:1", "4:3", "3:4")),
	"pollIntervalMs": providerutils.OptNullable(providerutils.OptPositive(false)),
	"pollTimeoutMs":  providerutils.OptNullable(providerutils.OptPositive(false)),
}

func validateVideoProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("alibaba", providerOptions, alibabaVideoOptionsSchema)
	return err
}
