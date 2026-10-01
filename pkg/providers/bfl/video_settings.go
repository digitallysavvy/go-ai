package bfl

// VideoModelID identifies a Black Forest Labs video generation model.
// https://docs.bfl.ai/api-reference/utility/generate-a-video-with-flux-3
const VideoModelFlux3Video = "flux-3-video"

// videoAspectRatios are the aspect ratios FLUX 3 video accepts. "auto" lets
// the model infer the ratio from the prompt and any conditioning media, and
// is the API default.
var videoAspectRatios = map[string]bool{
	"21:9": true, "2:1": true, "16:9": true, "4:3": true,
	"1:1": true, "3:4": true, "9:16": true, "auto": true,
}

// videoResolutions are the output resolution tiers FLUX 3 video accepts.
var videoResolutions = map[string]bool{"hd": true, "fhd": true}

const (
	defaultVideoPollIntervalMillis = 2000
	defaultVideoPollTimeoutMillis  = 600000
	minVideoDurationSeconds        = 5
	maxVideoDurationSeconds        = 20
	// untimedKeyframesNeedingDuration is the keyframe count above which an
	// explicit duration is required for untimed (plain string) keyframes.
	untimedKeyframesNeedingDuration = 3
)

// videoTerminalFailureStatuses are poll statuses that end polling without a
// video.
var videoTerminalFailureStatuses = map[string]bool{
	"Error": true, "Failed": true, "Request Moderated": true,
	"Content Moderated": true, "Task not found": true,
}

// VideoModelOptions carries Black Forest Labs FLUX 3 video-specific
// settings, read from VideoModelV3CallOptions.ProviderOptions["blackForestLabs"]
// (TS BlackForestLabsVideoModelOptions).
type VideoModelOptions struct {
	// Resolution is the output resolution tier ("hd" or "fhd"). Takes
	// precedence over the top-level Resolution, which is "{width}x{height}"
	// and has to be mapped onto a tier.
	Resolution *string
	// AspectRatio takes precedence over the top-level AspectRatio, and
	// unlike it can be set to "auto".
	AspectRatio *string
	// Keyframes are for image-to-video generation: each entry is either a
	// plain URL/base64 string, or a [seconds, url] pair. Takes precedence
	// over both Image and FrameImages.
	Keyframes []interface{}
	// SafetyTolerance is moderation strictness from 0 (strictest) to 4.
	// Defaults to 2.
	SafetyTolerance *int
	// Draft renders a fast, lower-quality preview instead of the finished
	// video. Defaults to false.
	Draft *bool
	// DraftCache is an encrypted draft-cache bundle from a prior Draft
	// generation, which switches the request to draft-enhance mode.
	DraftCache *string
	// Version pins the model version. Only "latest" is available today.
	Version *string
}

// parseVideoModelOptions extracts VideoModelOptions from the generic
// ProviderOptions["blackForestLabs"] map produced by SDK callers.
func parseVideoModelOptions(providerOptions map[string]interface{}) *VideoModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["blackForestLabs"].(map[string]interface{})
	if !ok {
		return nil
	}

	opts := &VideoModelOptions{}
	if v, ok := raw["resolution"].(string); ok {
		opts.Resolution = &v
	}
	if v, ok := raw["aspectRatio"].(string); ok {
		opts.AspectRatio = &v
	}
	if v, ok := raw["keyframes"].([]interface{}); ok {
		opts.Keyframes = v
	}
	if v, ok := intFromInterface(raw["safetyTolerance"]); ok {
		opts.SafetyTolerance = &v
	}
	if v, ok := raw["draft"].(bool); ok {
		opts.Draft = &v
	}
	if v, ok := raw["draftCache"].(string); ok {
		opts.DraftCache = &v
	}
	if v, ok := raw["version"].(string); ok {
		opts.Version = &v
	}
	return opts
}
