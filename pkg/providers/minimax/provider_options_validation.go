package minimax

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// minimaxVideoOptionsSchema is minimaxVideoModelOptionsSchema
// (minimax-video-model-options.ts); every member is .optional(), so null
// fails.
var minimaxVideoOptionsSchema = providerutils.OptionSchema{
	"resolution":         providerutils.OptEnum("480P", "768P", "2K"),
	"ratio":              providerutils.OptEnum("adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16"),
	"referenceAudioUrls": providerutils.OptArray(providerutils.OptString, 0, -1),
	"aigcWatermark":      providerutils.OptBool,
	"pollIntervalMs":     providerutils.OptPositive(true),
	"pollTimeoutMs":      providerutils.OptPositive(true),
}

func validateVideoOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("minimax", providerOptions, minimaxVideoOptionsSchema)
}
