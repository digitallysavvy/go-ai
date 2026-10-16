package topaz

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

func intRange(min, max int) providerutils.OptionCheck { return providerutils.OptInt(&min, &max) }

// topazImageOptionsSchema is topazImageModelOptionsSchema
// (topaz-image-model-options.ts).
var topazImageOptionsSchema = providerutils.OptionSchema{
	"enhancementStrength": providerutils.OptEnum("low", "medium", "high"),
	"grain":               providerutils.OptBool,
	"grainDensity":        providerutils.OptRange(0, 1),
	"grainModel":          providerutils.OptEnum("silver", "gaussian", "grey"),
	"grainSize":           providerutils.OptRange(1, 5),
	"grainStrength":       providerutils.OptRange(0, 1),
	"inputWidth":          providerutils.OptPositive(true),
	"inputHeight":         providerutils.OptPositive(true),
	"outputWidth":         intRange(1, 32000),
	"outputHeight":        intRange(1, 32000),
	"outputFormat":        providerutils.OptEnum("jpeg", "jpg", "png", "tiff", "tif"),
	"cropToFill":          providerutils.OptBool,
	"webhookUrl":          providerutils.OptString,
	"pollIntervalMillis":  providerutils.OptPositive(true),
	"pollTimeoutMillis":   providerutils.OptPositive(true),
}

// topazVideoOptionsSchema is topazVideoModelOptionsSchema
// (topaz-video-model-options.ts).
var topazVideoOptionsSchema = func() providerutils.OptionSchema {
	positiveInt := providerutils.OptPositive(true)
	positive := providerutils.OptPositive(false)
	rng := providerutils.OptRange
	anyRecord := providerutils.OptRecord(nil, nil)
	source := providerutils.OptObject(providerutils.OptionSchema{
		"width": positiveInt, "height": positiveInt, "duration": positive, "frameRate": positive, "frameCount": positiveInt,
		"container": providerutils.OptEnum("3gp", "avi", "dv", "flv", "m1v", "m2t", "m2ts", "m2v", "m4v", "mkv", "mov",
			"mp4", "mpeg", "mpg", "mts", "mxf", "ser", "ts", "vob", "webm", "wmv"),
	})
	output := providerutils.OptObject(providerutils.OptionSchema{
		"width": positiveInt, "height": positiveInt, "frameRate": positive,
		"audioCodec":              providerutils.OptEnum("AAC", "AC3", "PCM"),
		"audioBitrate":            providerutils.OptString,
		"audioTransfer":           providerutils.OptEnum("Copy", "Convert", "None"),
		"videoEncoder":            providerutils.OptEnum("AV1", "H264", "H265", "ProRes", "VP9"),
		"videoProfile":            providerutils.OptString,
		"videoBitrate":            providerutils.OptString,
		"dynamicCompressionLevel": providerutils.OptEnum("Low", "Mid", "High"),
		"cropToFit":               providerutils.OptBool,
		"container":               providerutils.OptEnum("mp4", "mov", "mkv", "avi", "webm"),
	})
	return providerutils.OptionSchema{
		"source":            source,
		"output":            output,
		"additionalFilters": providerutils.OptArray(anyRecord, 0, -1),
		"filter":            anyRecord,

		"videoType":                  providerutils.OptEnum("Progressive", "Interlaced", "ProgressiveInterlaced"),
		"auto":                       providerutils.OptEnum("Auto", "Manual", "Relative"),
		"fieldOrder":                 providerutils.OptEnum("TopFirst", "BottomFirst", "Auto"),
		"focusFixLevel":              providerutils.OptEnum("None", "Normal", "Strong"),
		"compression":                rng(-1, 1),
		"details":                    rng(-1, 1),
		"prenoise":                   rng(0, 0.1),
		"noise":                      rng(-1, 1),
		"halo":                       rng(-1, 1),
		"preblur":                    rng(-1, 1),
		"blur":                       rng(-1, 1),
		"grain":                      rng(0, 0.1),
		"grainSigma":                 rng(0, 1),
		"grainSize":                  rng(0, 5),
		"grainType":                  providerutils.OptEnum("silver_rich", "gaussian", "grey"),
		"recoverOriginalDetailValue": rng(0, 1),

		"sharpness":     rng(1, 5),
		"videoBitDepth": positiveInt,
		"videoCodec":    providerutils.OptEnum("ffv1", "prores", "vp9"),
		"videoProfile":  providerutils.OptEnum("420", "422", "444"),
		"watermark":     providerutils.OptBool,
	}
}()

func validateImageOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("topaz", providerOptions, topazImageOptionsSchema)
}

func validateVideoOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("topaz", providerOptions, topazVideoOptionsSchema)
}
