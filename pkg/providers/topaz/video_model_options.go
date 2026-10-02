package topaz

import "encoding/json"

// Containers Topaz accepts for the input video.
var topazSourceContainers = map[string]bool{
	"3gp": true, "avi": true, "dv": true, "flv": true, "m1v": true,
	"m2t": true, "m2ts": true, "m2v": true, "m4v": true, "mkv": true,
	"mov": true, "mp4": true, "mpeg": true, "mpg": true, "mts": true,
	"mxf": true, "ser": true, "ts": true, "vob": true, "webm": true,
	"wmv": true,
}

// Containers Topaz can produce for the enhanced video.
var topazOutputContainers = map[string]bool{
	"mp4": true, "mov": true, "mkv": true, "avi": true, "webm": true,
}

// VideoSource describes metadata about the input video.
//
// Starlight models require Width, Height, Duration and FrameRate (Topaz
// prices them from these values), and for other models they let Topaz
// estimate the cost before it has the video. The AI SDK does not inspect
// media files, so the values come from the caller. Setting any of them
// requires all four.
type VideoSource struct {
	// Width of the input video in pixels.
	Width *int `json:"width,omitempty"`

	// Height of the input video in pixels.
	Height *int `json:"height,omitempty"`

	// Duration of the input video in seconds.
	Duration *float64 `json:"duration,omitempty"`

	// FrameRate of the input video.
	FrameRate *float64 `json:"frameRate,omitempty"`

	// FrameCount is the total number of frames in the input video. Derived
	// from Duration * FrameRate when omitted, which is only correct for
	// constant-frame-rate input, so set it explicitly for
	// variable-frame-rate sources.
	FrameCount *int `json:"frameCount,omitempty"`

	// Container of the input video. Detected from the input file's media
	// type or URL extension when omitted.
	Container *string `json:"container,omitempty"`
}

// VideoOutput contains output settings for the enhanced video.
type VideoOutput struct {
	// Width of the output video in pixels. Takes precedence over the
	// Resolution call option.
	Width *int `json:"width,omitempty"`

	// Height of the output video in pixels. Takes precedence over the
	// Resolution call option.
	Height *int `json:"height,omitempty"`

	// FrameRate of the output video. Takes precedence over the FPS call
	// option. Topaz only changes the frame rate when a frame-interpolation
	// filter is present.
	FrameRate *float64 `json:"frameRate,omitempty"`

	// AudioCodec of the output video: "AAC", "AC3", or "PCM". Defaults to
	// "AAC".
	AudioCodec *string `json:"audioCodec,omitempty"`

	// AudioBitrate, e.g. "192k". Topaz uses the codec default when
	// omitted.
	AudioBitrate *string `json:"audioBitrate,omitempty"`

	// AudioTransfer controls how the input audio track is handled: "Copy",
	// "Convert", or "None". Defaults to "Copy".
	AudioTransfer *string `json:"audioTransfer,omitempty"`

	// VideoEncoder for the output: "AV1", "H264", "H265", "ProRes", or
	// "VP9". Topaz defaults to "H265". "ProRes" forces a mov container,
	// "AV1" and "VP9" force mp4.
	VideoEncoder *string `json:"videoEncoder,omitempty"`

	// VideoProfile is the encoder profile, e.g. "Main10" for H265 or
	// "422 HQ" for ProRes. Topaz uses the encoder's default profile when
	// omitted.
	VideoProfile *string `json:"videoProfile,omitempty"`

	// VideoBitrate, e.g. "20m". Required for "VP9". Mutually exclusive
	// with DynamicCompressionLevel.
	VideoBitrate *string `json:"videoBitrate,omitempty"`

	// DynamicCompressionLevel is the automatic constant-quality
	// compression level: "Low", "Mid", or "High". Topaz defaults to
	// "High" unless VideoBitrate is set.
	DynamicCompressionLevel *string `json:"dynamicCompressionLevel,omitempty"`

	// CropToFit center-crops to fit the output dimensions.
	CropToFit *bool `json:"cropToFit,omitempty"`

	// Container of the output video. Defaults to the input container when
	// Topaz can produce it, otherwise "mp4".
	Container *string `json:"container,omitempty"`
}

// VideoModelOptions contains provider options for Topaz video models.
//
// https://developer.topazlabs.com/video-models/proteus/proteus-1
// https://developer.topazlabs.com/video-models/starlight/starlight-precise-2.6
type VideoModelOptions struct {
	Source *VideoSource `json:"source,omitempty"`
	Output *VideoOutput `json:"output,omitempty"`

	// AdditionalFilters are extra filters[] entries to send alongside the
	// model's own filter, e.g. a frame-interpolation filter. Each entry
	// must include a "model" key.
	AdditionalFilters []map[string]interface{} `json:"additionalFilters,omitempty"`

	// Filter is an escape hatch for filter settings this package does not
	// model yet. Merged into the model's filter entry, taking precedence
	// over the typed options below.
	Filter map[string]interface{} `json:"filter,omitempty"`

	// -------------------------------------------------------------------
	// Proteus ("proteus")
	// -------------------------------------------------------------------

	// VideoType is how the input frames are encoded: "Progressive",
	// "Interlaced", or "ProgressiveInterlaced".
	VideoType *string `json:"videoType,omitempty"`

	// Auto is the parameter estimation mode: "Auto", "Manual", or
	// "Relative".
	Auto *string `json:"auto,omitempty"`

	// FieldOrder for interlaced input: "TopFirst", "BottomFirst", or
	// "Auto".
	FieldOrder *string `json:"fieldOrder,omitempty"`

	// FocusFixLevel is the focus-fix strength: "None", "Normal", or
	// "Strong".
	FocusFixLevel *string `json:"focusFixLevel,omitempty"`

	// Compression artifact removal, -1 to 1.
	Compression *float64 `json:"compression,omitempty"`

	// Details is detail recovery, -1 to 1.
	Details *float64 `json:"details,omitempty"`

	// Prenoise is pre-processing noise reduction, 0 to 0.1.
	Prenoise *float64 `json:"prenoise,omitempty"`

	// Noise reduction, -1 to 1.
	Noise *float64 `json:"noise,omitempty"`

	// Halo suppression, -1 to 1.
	Halo *float64 `json:"halo,omitempty"`

	// Preblur is pre-processing blur, -1 to 1.
	Preblur *float64 `json:"preblur,omitempty"`

	// Blur is sharpening, -1 to 1.
	Blur *float64 `json:"blur,omitempty"`

	// Grain amount, 0 to 0.1.
	Grain *float64 `json:"grain,omitempty"`

	// GrainSigma, 0 to 1.
	GrainSigma *float64 `json:"grainSigma,omitempty"`

	// GrainSize, 0 to 5.
	GrainSize *float64 `json:"grainSize,omitempty"`

	// GrainType is the grain model: "silver_rich", "gaussian", or "grey".
	GrainType *string `json:"grainType,omitempty"`

	// RecoverOriginalDetailValue is original detail recovery, 0 to 1.
	RecoverOriginalDetailValue *float64 `json:"recoverOriginalDetailValue,omitempty"`

	// -------------------------------------------------------------------
	// Starlight Precise ("starlight-precise-2.6")
	// -------------------------------------------------------------------

	// Sharpness applied to the output, 1.0 to 5.0. Defaults to 5.0.
	Sharpness *float64 `json:"sharpness,omitempty"`

	// VideoBitDepth is the output bit depth.
	VideoBitDepth *int `json:"videoBitDepth,omitempty"`

	// VideoCodec is the output video codec: "ffv1", "prores", or "vp9".
	VideoCodec *string `json:"videoCodec,omitempty"`

	// VideoProfile is the output chroma subsampling profile: "420",
	// "422", or "444".
	VideoProfile *string `json:"videoProfile,omitempty"`

	// Watermark controls whether to watermark the output. Defaults to
	// false.
	Watermark *bool `json:"watermark,omitempty"`
}

// topazNonFilterOptionKeys are option keys that are structural rather than
// filter settings, so they are not forwarded into the filters[] entry.
var topazNonFilterOptionKeys = map[string]bool{
	"source": true, "output": true, "additionalFilters": true, "filter": true,
}

// parseVideoModelOptions extracts VideoModelOptions from the generic
// ProviderOptions["topaz"] map produced by SDK callers. A missing or
// malformed entry returns nil.
func parseVideoModelOptions(providerOptions map[string]interface{}) *VideoModelOptions {
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
	var opts VideoModelOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil
	}
	return &opts
}

// buildFilter builds the filters[] entry for the model, mirroring TS
// buildFilter: the resolved API model name, followed by every non-nil
// top-level model-setting field (marshaled using its camelCase JSON tag),
// followed by the Filter escape hatch, which takes precedence over the
// typed settings.
func buildFilter(modelID string, topazOpts *VideoModelOptions) (map[string]interface{}, error) {
	filter := map[string]interface{}{"model": resolveTopazVideoAPIModelID(modelID)}
	if topazOpts == nil {
		return filter, nil
	}

	data, err := json.Marshal(topazOpts)
	if err != nil {
		return nil, err
	}
	var all map[string]interface{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	for key := range topazNonFilterOptionKeys {
		delete(all, key)
	}
	for k, v := range all {
		filter[k] = v
	}
	for k, v := range topazOpts.Filter {
		filter[k] = v
	}
	return filter, nil
}
