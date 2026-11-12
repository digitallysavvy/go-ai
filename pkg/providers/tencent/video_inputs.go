package tencent

import (
	"encoding/base64"
	"math"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// getFileBase64 is TS getFileBase64 for a file input.
func getFileBase64(file *provider.VideoModelV3File) string {
	return base64.StdEncoding.EncodeToString(file.Data)
}

// toURLOrDataURI is TS toUrlOrDataUri.
func toURLOrDataURI(file *provider.VideoModelV3File) string {
	if file.Type == "url" {
		return file.URL
	}
	return "data:" + file.MediaType + ";base64," + getFileBase64(file)
}

func mediaKind(file *provider.VideoModelV3File) string {
	if file.MediaType == "" {
		return ""
	}
	top, _, _ := strings.Cut(file.MediaType, "/")
	return top
}

func isVideoFile(file *provider.VideoModelV3File) bool { return mediaKind(file) == "video" }
func isAudioFile(file *provider.VideoModelV3File) bool { return mediaKind(file) == "audio" }

// rejectNonImageFrame is TS rejectNonImageFrame: a video or audio frame is
// ignored with a warning.
func rejectNonImageFrame(file *provider.VideoModelV3File, warnings *[]types.Warning) *provider.VideoModelV3File {
	if file != nil && (isVideoFile(file) || isAudioFile(file)) {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frameImages",
			Details: "Frame images must be images; the frame was ignored.",
		})
		return nil
	}
	return file
}

func findFrame(opts *provider.VideoModelV3CallOptions, frameType string) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == frameType {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// resolveFirstFrameImage is TS resolveFirstFrameImage: the first_frame frame
// image, else the legacy Image.
func resolveFirstFrameImage(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) *provider.VideoModelV3File {
	frame := findFrame(opts, provider.VideoFrameTypeFirstFrame)
	if frame == nil {
		frame = opts.Image
	}
	return rejectNonImageFrame(frame, warnings)
}

// resolveLastFrameImage is TS resolveLastFrameImage.
func resolveLastFrameImage(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) *provider.VideoModelV3File {
	return rejectNonImageFrame(findFrame(opts, provider.VideoFrameTypeLastFrame), warnings)
}

// splitInputReferences is TS splitInputReferences.
func splitInputReferences(opts *provider.VideoModelV3CallOptions) (images, videos, audios []*provider.VideoModelV3File) {
	for i := range opts.InputReferences {
		ref := &opts.InputReferences[i]
		switch {
		case isVideoFile(ref):
			videos = append(videos, ref)
		case isAudioFile(ref):
			audios = append(audios, ref)
		default:
			images = append(images, ref)
		}
	}
	return images, videos, audios
}

// mapResolutionToTier is TS mapResolutionToTier: the tier of the shorter side
// of a {width}x{height} resolution (JS Number of each side).
func mapResolutionToTier(resolution string, tiers map[float64]string) (string, bool) {
	parts := strings.Split(resolution, "x")
	nums := make([]float64, len(parts))
	for i, part := range parts {
		nums[i] = jsNumber(part)
	}
	if len(nums) < 2 {
		return "", false // TS Math.min(width, undefined) is NaN
	}
	shorter := math.Min(nums[0], nums[1])
	tier, ok := tiers[shorter]
	return tier, ok
}

// jsNumber is JS Number(s) for a resolution side (NaN when not numeric).
func jsNumber(s string) float64 {
	f, ok := providerutils.JSNumber(s)
	if !ok {
		return math.NaN()
	}
	return f
}

// tierValues lists the tier names in ascending key order (TS
// Object.values of an integer-keyed record).
func tierValues(tiers map[float64]string) string {
	keys := make([]float64, 0, len(tiers))
	for k := range tiers {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = tiers[k]
	}
	return strings.Join(values, ", ")
}

// resolveResolution is TS resolveResolution: the provider option wins as
// is; a top-level resolution maps to a tier, with a warning when it maps to
// none.
func resolveResolution(opts *provider.VideoModelV3CallOptions, tencentOpts *parsedVideoOptions, tiers map[float64]string, warnings *[]types.Warning) (string, bool) {
	if tencentOpts != nil && tencentOpts.opts.Resolution != nil {
		return *tencentOpts.opts.Resolution, true
	}
	if opts.Resolution == "" {
		return "", false
	}
	tier, ok := mapResolutionToTier(opts.Resolution, tiers)
	if !ok {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "resolution",
			Details: "Resolution " + opts.Resolution + " does not map to a supported tier (" + tierValues(tiers) + "); the default resolution was used.",
		})
	}
	return tier, ok
}

// warnIgnoredProviderOptions is TS warnIgnoredProviderOptions.
func warnIgnoredProviderOptions(tencentOpts *parsedVideoOptions, keys []string, details string, warnings *[]types.Warning) {
	if tencentOpts == nil {
		return
	}
	for _, key := range keys {
		if tencentOpts.present[key] {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.tencent." + key,
				Details: details,
			})
		}
	}
}

// warnUnsupportedCommonOptions is TS warnUnsupportedCommonOptions.
func warnUnsupportedCommonOptions(opts *provider.VideoModelV3StartOptions, supportsSeed bool, warnings *[]types.Warning) {
	if opts.FPS != nil {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "fps", Details: "Tencent video models do not support custom FPS."})
	}
	if opts.N > 1 {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "n", Details: "Tencent video models generate one video per call. Only 1 video will be generated."})
	}
	if !supportsSeed && opts.Seed != nil {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "seed", Details: "TokenHub Kling models do not support a seed."})
	}
	if opts.WebhookURL != "" {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "webhookUrl", Details: "Tencent video tasks do not accept a per-request callback URL; the result is retrieved by polling."})
	}
}

// promptPresent is TS `options.prompt != null`.
func promptPresent(opts *provider.VideoModelV3CallOptions) bool {
	return opts.PromptSet || opts.Prompt != ""
}

// newOrderedBody builds a JSON object with the given key / value pairs in
// order.
func newOrderedBody(pairs ...interface{}) *jsonorder.Object {
	o := jsonorder.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// jsNumberString is JS String(number).
func jsNumberString(f float64) string { return providerutils.JSNumberString(f) }
