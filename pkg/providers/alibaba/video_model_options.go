package alibaba

// AlibabaVideoMediaItem is one entry of the explicit `media` array
// (wan2.7 and wan3 models). It overrides the automatic mapping from
// InputReferences and FrameImages (TS AlibabaVideoModelOptions.media).
type AlibabaVideoMediaItem struct {
	// Type is one of: reference_image, reference_video, reference_audio,
	// first_frame, last_frame, file, link.
	//
	// reference_audio, file, and link are wan3-only and have no top-level
	// call option, so they can only be set here.
	Type string

	// URL is a public URL, or a `data:{mime};base64,{data}` URI for images.
	URL string

	// ReferenceVoice is a URL to an audio file used as voice reference for
	// this media item.
	ReferenceVoice *string
}

// AlibabaVideoModelOptions carries Alibaba-specific video generation
// settings, read from VideoModelV3CallOptions.ProviderOptions["alibaba"]
// (TS AlibabaVideoModelOptions).
type AlibabaVideoModelOptions struct {
	// NegativePrompt specifies what to avoid (max 500 chars).
	NegativePrompt *string
	// AudioURL is a URL to an audio file for audio-video sync (WAV/MP3,
	// 3-30s, max 15MB).
	AudioURL *string
	// PromptExtend enables prompt extension/rewriting. Defaults to true.
	PromptExtend *bool
	// ShotType is "single" or "multi" (wan2.6 and earlier only).
	ShotType *string
	// Watermark adds a watermark to the generated video. Defaults to false.
	Watermark *bool
	// Audio enables audio generation (wan2.6 I2V/R2V and wan3 models).
	// Defaults to true on wan3.
	Audio *bool
	// ReferenceURLs are URLs for reference-to-video mode (wan2.6 models).
	ReferenceURLs []string
	// Media is the explicit media array override (wan2.7 and wan3 models).
	Media []AlibabaVideoMediaItem
	// Ratio is the aspect ratio: adaptive, 16:9, 9:16, 1:1, 4:3, or 3:4.
	// "adaptive" is wan3-only and is its default.
	Ratio *string
	// PollIntervalMs is the polling interval in milliseconds.
	// Defaults to 5000 (5 seconds).
	PollIntervalMs *int
	// PollTimeoutMs is the maximum wait time in milliseconds for video
	// generation. Defaults to 600000 (10 minutes).
	PollTimeoutMs *int
}

// parseAlibabaVideoModelOptions extracts AlibabaVideoModelOptions from the
// generic ProviderOptions["alibaba"] map produced by SDK callers.
func parseAlibabaVideoModelOptions(providerOptions map[string]interface{}) *AlibabaVideoModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["alibaba"].(map[string]interface{})
	if !ok {
		return nil
	}

	opts := &AlibabaVideoModelOptions{}
	if v, ok := stringFromInterface(raw["negativePrompt"]); ok {
		opts.NegativePrompt = &v
	}
	if v, ok := stringFromInterface(raw["audioUrl"]); ok {
		opts.AudioURL = &v
	}
	if v, ok := raw["promptExtend"].(bool); ok {
		opts.PromptExtend = &v
	}
	if v, ok := stringFromInterface(raw["shotType"]); ok {
		opts.ShotType = &v
	}
	if v, ok := raw["watermark"].(bool); ok {
		opts.Watermark = &v
	}
	if v, ok := raw["audio"].(bool); ok {
		opts.Audio = &v
	}
	if v, ok := raw["referenceUrls"]; ok {
		opts.ReferenceURLs = stringSliceFromInterface(v)
	}
	if v, ok := raw["media"]; ok {
		opts.Media = mediaSliceFromInterface(v)
	}
	if v, ok := stringFromInterface(raw["ratio"]); ok {
		opts.Ratio = &v
	}
	if v, ok := intFromInterfaceAlibaba(raw["pollIntervalMs"]); ok {
		opts.PollIntervalMs = &v
	}
	if v, ok := intFromInterfaceAlibaba(raw["pollTimeoutMs"]); ok {
		opts.PollTimeoutMs = &v
	}

	return opts
}

func stringFromInterface(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func stringSliceFromInterface(v interface{}) []string {
	switch vv := v.(type) {
	case []string:
		return vv
	case []interface{}:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func mediaSliceFromInterface(v interface{}) []AlibabaVideoMediaItem {
	items, ok := v.([]interface{})
	if !ok {
		if typed, ok := v.([]AlibabaVideoMediaItem); ok {
			return typed
		}
		if typed, ok := v.([]map[string]interface{}); ok {
			out := make([]AlibabaVideoMediaItem, 0, len(typed))
			for _, m := range typed {
				out = append(out, mediaItemFromMap(m))
			}
			return out
		}
		return nil
	}
	out := make([]AlibabaVideoMediaItem, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, mediaItemFromMap(m))
		}
	}
	return out
}

func mediaItemFromMap(m map[string]interface{}) AlibabaVideoMediaItem {
	item := AlibabaVideoMediaItem{}
	if t, ok := m["type"].(string); ok {
		item.Type = t
	}
	if u, ok := m["url"].(string); ok {
		item.URL = u
	}
	if rv, ok := stringFromInterface(m["referenceVoice"]); ok {
		item.ReferenceVoice = &rv
	}
	return item
}

func intFromInterfaceAlibaba(v interface{}) (int, bool) {
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
