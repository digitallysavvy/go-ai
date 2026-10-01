package luma

// ImageConfig is per-image configuration for a Luma image reference (TS
// LumaImageConfig). Each entry corresponds to an image in Files, in order.
type ImageConfig struct {
	// Weight is this image's influence on the generation.
	//   - For "image": higher weight = closer to reference (default: 0.85)
	//   - For "style": higher weight = stronger style influence (default: 0.8)
	//   - For "modify_image": higher weight = closer to input, lower = more
	//     creative (default: 1.0)
	//
	// Not applicable to "character".
	Weight *float64
	// ID is the identity name for character references. Used with
	// ReferenceType "character" to specify which identity group the image
	// belongs to. Default: "identity0".
	ID *string
}

// ImageModelOptions carries Luma-specific image generation settings, read
// from ImageGenerateOptions.ProviderOptions["luma"] (TS LumaImageSettings /
// lumaImageModelOptionsSchema).
type ImageModelOptions struct {
	// ReferenceType is the type of image reference to use when providing
	// input images via Files. Default is "image".
	ReferenceType *string
	// Images is per-image configuration; each entry corresponds to an image
	// in Files, in order.
	Images []ImageConfig
	// PollIntervalMillis overrides the polling interval in milliseconds
	// (default 500).
	PollIntervalMillis *int
	// MaxPollAttempts overrides the maximum number of polling attempts
	// (default 120).
	MaxPollAttempts *int
	// Additional carries any passthrough fields not explicitly modeled here
	// (the TS schema is a looseObject, and every unrecognized field is
	// spread directly into the request body).
	Additional map[string]interface{}
}

var lumaHandledOptionKeys = map[string]bool{
	"referenceType": true, "images": true,
	"pollIntervalMillis": true, "maxPollAttempts": true,
}

// parseImageModelOptions extracts ImageModelOptions from the generic
// ProviderOptions["luma"] map produced by SDK callers.
func parseImageModelOptions(providerOptions map[string]interface{}) *ImageModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["luma"].(map[string]interface{})
	if !ok {
		return nil
	}

	opts := &ImageModelOptions{}
	if v, ok := raw["referenceType"].(string); ok {
		opts.ReferenceType = &v
	}
	if v, ok := raw["images"].([]interface{}); ok {
		opts.Images = make([]ImageConfig, 0, len(v))
		for _, item := range v {
			cfg := ImageConfig{}
			if m, ok := item.(map[string]interface{}); ok {
				if w, ok := floatFromInterface(m["weight"]); ok {
					cfg.Weight = &w
				}
				if id, ok := m["id"].(string); ok {
					cfg.ID = &id
				}
			}
			opts.Images = append(opts.Images, cfg)
		}
	}
	if v, ok := intFromInterface(raw["pollIntervalMillis"]); ok {
		opts.PollIntervalMillis = &v
	}
	if v, ok := intFromInterface(raw["maxPollAttempts"]); ok {
		opts.MaxPollAttempts = &v
	}

	additional := map[string]interface{}{}
	for k, v := range raw {
		if !lumaHandledOptionKeys[k] {
			additional[k] = v
		}
	}
	if len(additional) > 0 {
		opts.Additional = additional
	}

	return opts
}

func (o *ImageModelOptions) imageConfig(index int) ImageConfig {
	if o == nil || index >= len(o.Images) {
		return ImageConfig{}
	}
	return o.Images[index]
}

func floatFromInterface(v interface{}) (float64, bool) {
	switch vv := v.(type) {
	case float64:
		return vv, true
	case float32:
		return float64(vv), true
	case int:
		return float64(vv), true
	case int64:
		return float64(vv), true
	default:
		return 0, false
	}
}

func intFromInterface(v interface{}) (int, bool) {
	switch vv := v.(type) {
	case int:
		return vv, true
	case int64:
		return int(vv), true
	case float64:
		return int(vv), true
	default:
		return 0, false
	}
}
