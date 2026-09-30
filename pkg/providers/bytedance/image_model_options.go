package bytedance

import (
	"encoding/json"
	"fmt"
)

// ImageModelOptions contains ByteDance-specific options for image generation
// (TS ByteDanceImageModelOptions).
type ImageModelOptions struct {
	// Watermark controls whether to add an "AI generated" watermark to the
	// bottom-right corner of the output image.
	Watermark *bool `json:"watermark,omitempty"`

	// OutputFormat is the format of the generated image file ("png" or
	// "jpeg"). Supported by seedream-5-0 and dola-seedream-5-0-pro;
	// seedream-4-5 / seedream-4-0 always return jpeg.
	OutputFormat *string `json:"outputFormat,omitempty"`

	// Size is a resolution level (e.g. "1K", "2K", "3K", "4K") as an
	// alternative to passing pixel dimensions via the top-level Size call
	// option. When set, this overrides the top-level Size. Available levels
	// vary by model.
	Size *string `json:"size,omitempty"`

	// SequentialImageGeneration set to "auto" generates a batch of related
	// images (e.g. storyboards or brand visuals). Defaults to "disabled"
	// (single image).
	SequentialImageGeneration *string `json:"sequentialImageGeneration,omitempty"`

	// MaxImages is the maximum number of images to generate when
	// SequentialImageGeneration is "auto". The number of input reference
	// images plus generated images must not exceed the model's limit.
	MaxImages *int `json:"maxImages,omitempty"`

	// OptimizePromptMode is the prompt optimization mode. seedream-4-0
	// supports both "standard" and "fast"; other models support "standard"
	// only.
	OptimizePromptMode *string `json:"optimizePromptMode,omitempty"`

	// Additional carries passthrough options not explicitly handled above
	// (TS byteDanceImageModelOptionsSchema is a z.looseObject: unknown keys
	// are preserved and forwarded verbatim to the request body).
	Additional map[string]interface{} `json:"-"`
}

// handledImageProviderOptionKeys are the JSON keys ImageModelOptions handles
// directly; any other key present in providerOptions.bytedance is captured
// into Additional for passthrough to the API body (TS
// HANDLED_PROVIDER_OPTIONS).
var handledImageProviderOptionKeys = map[string]bool{
	"watermark":                 true,
	"outputFormat":              true,
	"size":                      true,
	"sequentialImageGeneration": true,
	"maxImages":                 true,
	"optimizePromptMode":        true,
}

// extractImageProviderOptions extracts ByteDance image provider options from
// the generic options map.
func extractImageProviderOptions(opts map[string]interface{}) (*ImageModelOptions, error) {
	if opts == nil {
		return &ImageModelOptions{}, nil
	}

	bdOpts, ok := opts["bytedance"]
	if !ok {
		return &ImageModelOptions{}, nil
	}

	jsonData, err := json.Marshal(bdOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bytedance image provider options: %w", err)
	}

	var provOpts ImageModelOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bytedance image provider options: %w", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err == nil {
		additional := map[string]interface{}{}
		for k, v := range rawMap {
			if !handledImageProviderOptionKeys[k] {
				additional[k] = v
			}
		}
		if len(additional) > 0 {
			provOpts.Additional = additional
		}
	}

	return &provOpts, nil
}
