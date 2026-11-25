package prodia

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// prodiaImageOptionsSchema is prodiaImageModelOptionsSchema
// (prodia-image-model-options.ts).
var prodiaImageOptionsSchema = func() providerutils.OptionSchema {
	one, four, min, max := 1, 4, 256, 1920
	return providerutils.OptionSchema{
		"steps":  providerutils.OptInt(&one, &four),
		"width":  providerutils.OptInt(&min, &max),
		"height": providerutils.OptInt(&min, &max),
		"stylePreset": providerutils.OptEnum("3d-model", "analog-film", "anime", "cinematic", "comic-book",
			"digital-art", "enhance", "fantasy-art", "isometric", "line-art", "low-poly", "neon-punk", "origami",
			"photographic", "pixel-art", "texture", "craft-clay"),
		"loras":       providerutils.OptArray(providerutils.OptString, 0, 3),
		"progressive": providerutils.OptBool,
	}
}()

// prodiaLanguageOptionsSchema is prodiaLanguageModelOptionsSchema.
var prodiaLanguageOptionsSchema = providerutils.OptionSchema{
	"aspectRatio": providerutils.OptEnum("1:1", "2:3", "3:2", "4:5", "5:4", "4:7", "7:4", "9:16", "16:9", "9:21", "21:9"),
}

// prodiaVideoOptionsSchema is prodiaVideoModelOptionsSchema.
var prodiaVideoOptionsSchema = providerutils.OptionSchema{
	"resolution": providerutils.OptString,
}

func validateImageOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("prodia", providerOptions, prodiaImageOptionsSchema)
}

func validateLanguageOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("prodia", providerOptions, prodiaLanguageOptionsSchema)
}

func validateVideoOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("prodia", providerOptions, prodiaVideoOptionsSchema)
}
