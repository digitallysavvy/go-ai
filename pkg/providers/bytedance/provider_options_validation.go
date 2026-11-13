package bytedance

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// byteDanceVideoOptionsSchema mirrors byteDanceVideoModelOptionsSchema (TS
// bytedance-video-model-options.ts; a looseObject, so unknown keys pass).
var byteDanceVideoOptionsSchema = providerutils.OptionSchema{
	"watermark":       providerutils.OptNullable(providerutils.OptBool),
	"generateAudio":   providerutils.OptNullable(providerutils.OptBool),
	"cameraFixed":     providerutils.OptNullable(providerutils.OptBool),
	"returnLastFrame": providerutils.OptNullable(providerutils.OptBool),
	"serviceTier":     providerutils.OptNullable(providerutils.OptEnum("default", "flex")),
	"draft":           providerutils.OptNullable(providerutils.OptBool),
	"lastFrameImage":  providerutils.OptNullable(providerutils.OptString),
	"referenceImages": providerutils.OptNullable(providerutils.OptArray(providerutils.OptString, 0, -1)),
	"referenceVideos": providerutils.OptNullable(providerutils.OptArray(providerutils.OptString, 0, -1)),
	"referenceAudio":  providerutils.OptNullable(providerutils.OptArray(providerutils.OptString, 0, -1)),
	"pollIntervalMs":  providerutils.OptNullable(providerutils.OptPositive(false)),
	"pollTimeoutMs":   providerutils.OptNullable(providerutils.OptPositive(false)),
}

// byteDanceImageOptionsSchema mirrors byteDanceImageModelOptionsSchema (TS
// bytedance-image-model-options.ts).
var byteDanceImageOptionsSchema = providerutils.OptionSchema{
	"watermark":                 providerutils.OptNullable(providerutils.OptBool),
	"outputFormat":              providerutils.OptNullable(providerutils.OptEnum("png", "jpeg")),
	"size":                      providerutils.OptNullable(providerutils.OptString),
	"sequentialImageGeneration": providerutils.OptNullable(providerutils.OptEnum("auto", "disabled")),
	"maxImages":                 providerutils.OptNullable(providerutils.OptPositive(true)),
	"optimizePromptMode":        providerutils.OptNullable(providerutils.OptEnum("standard", "fast")),
}

// validateByteDanceOptions is parseProviderOptions for providerOptions.bytedance.
// Typed option structs are normalised to their JSON object first.
func validateByteDanceOptions(providerOptions map[string]interface{}, schema providerutils.OptionSchema) error {
	raw, ok := providerOptions["bytedance"]
	if !ok || raw == nil {
		return nil
	}
	if _, isMap := raw.(map[string]interface{}); !isMap {
		if b, err := json.Marshal(raw); err == nil {
			var m map[string]interface{}
			if json.Unmarshal(b, &m) == nil {
				raw = m
			}
		}
	}
	_, err := providerutils.ParseProviderOptions("bytedance", map[string]interface{}{"bytedance": raw}, schema)
	return err
}
