package topaz

import "encoding/json"

// ImageModelOptions contains provider options for Topaz generative image
// models (Wonder 3.5).
//
// The documented request schema fields are sent in snake_case
// (output_width, output_format, ...); model-specific settings are sent in
// camelCase, matching the Topaz model reference. These Go field names and
// their JSON tags are the user-facing surface and stay camelCase, matching
// the TS provider options.
//
// https://developer.topazlabs.com/image-models/wonder/wonder-3.5-new
type ImageModelOptions struct {
	// EnhancementStrength controls how aggressively the model enhances the
	// image: "low", "medium", or "high". Defaults to "high".
	EnhancementStrength *string `json:"enhancementStrength,omitempty"`

	// Grain adds grain to the output. Defaults to false.
	Grain *bool `json:"grain,omitempty"`

	// GrainDensity is the grain intensity, 0.0 to 1.0. Defaults to 0.5.
	GrainDensity *float64 `json:"grainDensity,omitempty"`

	// GrainModel is the grain model: "silver", "gaussian", or "grey".
	// Defaults to "silver".
	GrainModel *string `json:"grainModel,omitempty"`

	// GrainSize is the grain particle size, 1 to 5. Defaults to 1.
	GrainSize *float64 `json:"grainSize,omitempty"`

	// GrainStrength is the grain effect strength, 0.0 to 1.0. Defaults to 0.5.
	GrainStrength *float64 `json:"grainStrength,omitempty"`

	// InputWidth is the width of the input image in pixels. Topaz infers
	// this from the upload when omitted.
	InputWidth *int `json:"inputWidth,omitempty"`

	// InputHeight is the height of the input image in pixels. Topaz infers
	// this from the upload when omitted.
	InputHeight *int `json:"inputHeight,omitempty"`

	// OutputWidth is the width of the output image in pixels, 1 to 32000.
	// Takes precedence over the width derived from the Size call option.
	OutputWidth *int `json:"outputWidth,omitempty"`

	// OutputHeight is the height of the output image in pixels, 1 to 32000.
	// Takes precedence over the height derived from the Size call option.
	OutputHeight *int `json:"outputHeight,omitempty"`

	// OutputFormat is the output image format: "jpeg", "jpg", "png",
	// "tiff", or "tif". Defaults to the Topaz API default.
	OutputFormat *string `json:"outputFormat,omitempty"`

	// CropToFill crops the output to fill the requested dimensions.
	// Defaults to false.
	CropToFill *bool `json:"cropToFill,omitempty"`

	// WebhookURL receives job-status webhooks.
	WebhookURL *string `json:"webhookUrl,omitempty"`

	// PollIntervalMillis is how often to poll the Topaz status endpoint, in
	// milliseconds. Defaults to 2000.
	PollIntervalMillis *int `json:"pollIntervalMillis,omitempty"`

	// PollTimeoutMillis is how long to wait for the job to finish before
	// failing, in milliseconds. Defaults to 600000 (10 minutes).
	PollTimeoutMillis *int `json:"pollTimeoutMillis,omitempty"`
}

// parseImageModelOptions extracts ImageModelOptions from the generic
// ProviderOptions["topaz"] map produced by SDK callers. A missing or
// malformed entry returns nil, matching TS parseProviderOptions returning
// undefined for an absent key.
func parseImageModelOptions(providerOptions map[string]interface{}) *ImageModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["topaz"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts ImageModelOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil
	}
	return &opts
}
