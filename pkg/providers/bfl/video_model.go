package bfl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Black
// Forest Labs' FLUX 3 video model.
type VideoModel struct {
	provider *Provider
	modelID  string
}

// NewVideoModel creates a new BFL video generation model
func NewVideoModel(provider *Provider, modelID string) *VideoModel {
	return &VideoModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name
func (m *VideoModel) Provider() string { return "bfl.video" }

// ModelID returns the model ID
func (m *VideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 1 (FLUX 3 video generates a single video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// bflVideoOperation is the opaque operation reference returned by DoStart
// and passed back into DoStatus (TS BlackForestLabsVideoOperation).
type bflVideoOperation struct {
	RequestID  string   `json:"requestId"`
	PollingURL string   `json:"pollingUrl"`
	Cost       *float64 `json:"cost,omitempty"`
	InputMP    *float64 `json:"inputMegapixels,omitempty"`
	OutputMP   *float64 `json:"outputMegapixels,omitempty"`
}

var resolutionDimensionPattern = regexp.MustCompile(`^(\d+)x(\d+)$`)

// resolveTopLevelResolution maps the top-level "{width}x{height}" resolution
// onto a named tier, since the API takes a named tier ("hd"/"fhd") while the
// top-level Resolution option is dimensions.
func resolveTopLevelResolution(resolution string) (tier string, derived bool, ok bool) {
	named := strings.ToLower(resolution)
	if videoResolutions[named] {
		return named, false, true
	}

	match := resolutionDimensionPattern.FindStringSubmatch(resolution)
	if match == nil {
		return "", false, false
	}
	var w, h int
	fmt.Sscanf(match[1], "%d", &w) //nolint:errcheck
	fmt.Sscanf(match[2], "%d", &h) //nolint:errcheck
	shorterSide := w
	if h < w {
		shorterSide = h
	}
	tier = "fhd"
	if shorterSide <= 720 {
		tier = "hd"
	}
	derived = shorterSide != 720 && shorterSide != 1080
	return tier, derived, true
}

func videoTopLevelMediaType(mediaType string) string {
	if idx := strings.Index(mediaType, "/"); idx >= 0 {
		return mediaType[:idx]
	}
	return mediaType
}

// nonImageFrameMediaType returns the file's top-level media type when it is
// not an image (empty string otherwise), mirroring TS nonImageFrameMediaType.
func nonImageFrameMediaType(file *provider.VideoModelV3File) string {
	if file.MediaType == "" {
		return ""
	}
	top := videoTopLevelMediaType(file.MediaType)
	if top == "image" {
		return ""
	}
	return top
}

// toBlackForestLabsFile encodes conditioning media as an http(s) URL or a
// bare base64 string (not a data URI), matching TS toBlackForestLabsFile.
func toBlackForestLabsFile(file *provider.VideoModelV3File) string {
	if file.Type == "url" {
		return file.URL
	}
	return base64.StdEncoding.EncodeToString(file.Data)
}

func getFrameImage(opts *provider.VideoModelV3CallOptions, frameType string) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == frameType {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// getDraftEnhanceArgs builds the draft-enhance request body: draft-enhance
// replays an encrypted bundle from a prior draft generation at full quality,
// so it accepts nothing alongside it but safety_tolerance -- everything else
// the caller set is reported as dropped (TS getDraftEnhanceArgs).
func getDraftEnhanceArgs(opts *provider.VideoModelV3CallOptions, bflOpts *VideoModelOptions) (map[string]interface{}, []types.Warning) {
	var warnings []types.Warning

	pinned := []struct {
		feature string
		isSet   bool
	}{
		{"prompt", strings.TrimSpace(opts.Prompt) != ""},
		{"aspectRatio", opts.AspectRatio != "" || bflOpts.AspectRatio != nil},
		{"resolution", opts.Resolution != "" || bflOpts.Resolution != nil},
		{"duration", opts.Duration != nil},
		{"fps", opts.FPS != nil},
		{"seed", opts.Seed != nil},
		{"generateAudio", opts.GenerateAudio != nil},
		{"image", opts.Image != nil},
		{"frameImages", len(opts.FrameImages) > 0},
		{"inputReferences", len(opts.InputReferences) > 0},
		{"keyframes", len(bflOpts.Keyframes) > 0},
		{"version", bflOpts.Version != nil},
	}
	for _, p := range pinned {
		if p.isSet {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: p.feature,
				Details: fmt.Sprintf("FLUX 3 draft enhance replays the draft bundle as it was generated, so %q was ignored. Set it on the original draft request instead.", p.feature),
			})
		}
	}

	if bflOpts.Draft != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "draft",
			Details: "FLUX 3 draft enhance always renders at full quality. The draft option was ignored.",
		})
	}

	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "FLUX 3 video generates a single video per call. Only 1 video will be generated.",
		})
	}

	body := map[string]interface{}{"mode": "draft_enhance"}
	if bflOpts.DraftCache != nil {
		body["draft_cache"] = *bflOpts.DraftCache
	}
	if bflOpts.SafetyTolerance != nil {
		body["safety_tolerance"] = *bflOpts.SafetyTolerance
	}

	return body, warnings
}

// buildVideoArgs builds the FLUX 3 video request body (TS
// BlackForestLabsVideoModel#getArgs).
func (m *VideoModel) buildVideoArgs(opts *provider.VideoModelV3CallOptions) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	bflOpts := parseVideoModelOptions(opts.ProviderOptions)

	if bflOpts != nil && bflOpts.DraftCache != nil {
		body, w := getDraftEnhanceArgs(opts, bflOpts)
		return body, w, nil
	}
	if bflOpts == nil {
		bflOpts = &VideoModelOptions{}
	}

	if opts.FPS != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "fps", Details: "FLUX 3 video does not support a custom frame rate."})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed", Details: "FLUX 3 video does not accept a seed."})
	}
	if opts.N > 1 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "n", Details: "FLUX 3 video generates a single video per call. Only 1 video will be generated."})
	}

	// Resolution: an explicit provider option is already a tier, so it wins
	// and a top-level value only has to be resolved when it is the sole
	// source.
	resolution := ""
	resolutionSet := false
	if bflOpts.Resolution != nil {
		resolution = *bflOpts.Resolution
		resolutionSet = true
	}
	if opts.Resolution != "" {
		tier, derived, ok := resolveTopLevelResolution(opts.Resolution)
		if resolutionSet {
			if !ok {
				warnings = append(warnings, types.Warning{
					Type: "unsupported", Feature: "resolution",
					Details: fmt.Sprintf("Unrecognized resolution %q. FLUX 3 video supports \"hd\" and \"fhd\", so providerOptions.blackForestLabs.resolution (%q) was used instead.", opts.Resolution, resolution),
				})
			}
		} else if !ok {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "resolution",
				Details: fmt.Sprintf("Unrecognized resolution %q. FLUX 3 video supports \"hd\" and \"fhd\", or a {width}x{height} value to map onto one.", opts.Resolution),
			})
		} else {
			resolution = tier
			resolutionSet = true
			if derived {
				warnings = append(warnings, types.Warning{
					Type: "compatibility", Feature: "resolution",
					Details: fmt.Sprintf("FLUX 3 video renders at \"hd\" or \"fhd\"; the requested resolution %q was mapped to %q.", opts.Resolution, tier),
				})
			}
		}
	}

	// Aspect ratio: the provider option can also express "auto", so it wins.
	aspectRatio := ""
	aspectRatioSet := false
	if bflOpts.AspectRatio != nil {
		aspectRatio = *bflOpts.AspectRatio
		aspectRatioSet = true
	}
	if !aspectRatioSet && opts.AspectRatio != "" {
		if videoAspectRatios[opts.AspectRatio] {
			aspectRatio = opts.AspectRatio
			aspectRatioSet = true
		} else {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "aspectRatio",
				Details: fmt.Sprintf("FLUX 3 video does not support the aspect ratio %q. Using the provider default (auto).", opts.AspectRatio),
			})
		}
	}

	// Resolve first/last frame inputs. A standalone image is treated as the
	// first frame (image-to-video).
	firstFrameImage := getFrameImage(opts, provider.VideoFrameTypeFirstFrame)
	firstFrame := firstFrameImage
	if firstFrame == nil {
		firstFrame = opts.Image
	}
	lastFrame := getFrameImage(opts, provider.VideoFrameTypeLastFrame)

	if firstFrame != nil {
		if mt := nonImageFrameMediaType(firstFrame); mt != "" {
			feature := "image"
			if firstFrameImage != nil {
				feature = "frameImages"
			}
			var details string
			if mt == "video" {
				details = "FLUX 3 video does not accept a video as a keyframe. Pass it as an inputReference to continue from it instead."
			} else {
				details = fmt.Sprintf("FLUX 3 video only accepts an image as a keyframe; the %q file was ignored.", firstFrame.MediaType)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature, Details: details})
			firstFrame = nil
		}
	}

	if lastFrame != nil {
		if firstFrame == nil {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "frameImages",
				Details: "FLUX 3 video requires a first_frame when a last_frame is provided. The last_frame was ignored.",
			})
			lastFrame = nil
		} else if mt := nonImageFrameMediaType(lastFrame); mt != "" {
			var details string
			if mt == "video" {
				details = "FLUX 3 video does not accept a video as a keyframe. The last_frame video was ignored."
			} else {
				details = fmt.Sprintf("FLUX 3 video only accepts an image as a keyframe; the %q last_frame was ignored.", lastFrame.MediaType)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frameImages", Details: details})
			lastFrame = nil
		}
	}

	// Keyframes: the provider option covers shapes the top-level fields
	// cannot express, so it wins.
	var keyframes []interface{}
	if len(bflOpts.Keyframes) > 0 {
		keyframes = append(keyframes, bflOpts.Keyframes...)
	}

	if keyframes != nil {
		if firstFrame != nil || lastFrame != nil {
			feature := "image"
			if len(opts.FrameImages) > 0 {
				feature = "frameImages"
			}
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: feature,
				Details: "FLUX 3 video takes a single keyframe list. providerOptions.blackForestLabs.keyframes was used and the top-level frame images were ignored.",
			})
		}
	} else if firstFrame != nil {
		keyframes = []interface{}{toBlackForestLabsFile(firstFrame)}
		if lastFrame != nil {
			keyframes = append(keyframes, toBlackForestLabsFile(lastFrame))
		}
	}

	// Video continuation. FLUX 3 takes a single start_video and has no
	// reference-image concept, so image references cannot be honored.
	var startVideo *string
	var referenceVideos []provider.VideoModelV3File

	for i := range opts.InputReferences {
		file := opts.InputReferences[i]
		topLevel := ""
		if file.MediaType != "" {
			topLevel = videoTopLevelMediaType(file.MediaType)
		}
		switch topLevel {
		case "video":
			referenceVideos = append(referenceVideos, file)
		case "":
			warnings = append(warnings, types.Warning{
				Type: "compatibility", Feature: "inputReferences",
				Details: "FLUX 3 video only accepts a video reference, so the reference with no mediaType was treated as the video to continue from. Pass { url, mediaType: \"video/mp4\" } to be explicit.",
			})
			referenceVideos = append(referenceVideos, file)
		case "image":
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "inputReferences",
				Details: "FLUX 3 video has no reference-image input. Pass images as `image`, `frameImages`, or providerOptions.blackForestLabs.keyframes instead. The reference was ignored.",
			})
		default:
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "inputReferences",
				Details: fmt.Sprintf("FLUX 3 video only accepts a video reference; the %q reference was ignored.", file.MediaType),
			})
		}
	}

	if len(referenceVideos) > 0 {
		if keyframes != nil {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "inputReferences",
				Details: "FLUX 3 video cannot combine keyframes with a video to continue from. The video reference was ignored.",
			})
		} else {
			sv := toBlackForestLabsFile(&referenceVideos[0])
			startVideo = &sv
			if len(referenceVideos) > 1 {
				warnings = append(warnings, types.Warning{
					Type: "unsupported", Feature: "inputReferences",
					Details: "FLUX 3 video continues from a single video. Only the first video reference was used.",
				})
			}
		}
	}

	// Duration: whole seconds between 5 and 20, or omitted to let the API
	// pick one that matches the content.
	var duration *float64
	if opts.Duration != nil {
		origDuration := *opts.Duration
		d := origDuration
		if d != math.Trunc(d) {
			rounded := math.Round(d)
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "duration",
				Details: fmt.Sprintf("FLUX 3 video requires a whole number of seconds. The requested duration of %v was rounded to %v.", d, rounded),
			})
			d = rounded
		}
		if d > maxVideoDurationSeconds {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "duration",
				Details: fmt.Sprintf("FLUX 3 video supports at most %d seconds. The requested duration of %v was clamped to %d.", maxVideoDurationSeconds, origDuration, maxVideoDurationSeconds),
			})
			d = maxVideoDurationSeconds
		} else if d < minVideoDurationSeconds {
			warnings = append(warnings, types.Warning{
				Type: "unsupported", Feature: "duration",
				Details: fmt.Sprintf("FLUX 3 video requires at least %d seconds. The requested duration of %v was clamped to %d.", minVideoDurationSeconds, origDuration, minVideoDurationSeconds),
			})
			d = minVideoDurationSeconds
		}
		duration = &d
	}

	untimedKeyframeCount := 0
	for _, k := range keyframes {
		if _, ok := k.(string); ok {
			untimedKeyframeCount++
		}
	}
	if duration == nil && untimedKeyframeCount >= untimedKeyframesNeedingDuration {
		return nil, nil, providererrors.NewValidationError("duration",
			fmt.Sprintf("FLUX 3 video requires an explicit duration when %d or more keyframes are sent without a timestamp.", untimedKeyframesNeedingDuration), nil)
	}

	mode := "t2v"
	if keyframes != nil {
		mode = "i2v"
	} else if startVideo != nil {
		mode = "v2v"
	}

	body := map[string]interface{}{
		"mode":   mode,
		"prompt": opts.Prompt,
	}
	if aspectRatioSet {
		body["aspect_ratio"] = aspectRatio
	}
	if duration != nil {
		body["duration"] = *duration
	}
	if resolutionSet {
		body["resolution"] = resolution
	}
	if bflOpts.Version != nil {
		body["version"] = *bflOpts.Version
	}
	if opts.GenerateAudio != nil {
		body["generate_audio"] = *opts.GenerateAudio
	}
	if bflOpts.SafetyTolerance != nil {
		body["safety_tolerance"] = *bflOpts.SafetyTolerance
	}
	if bflOpts.Draft != nil {
		body["draft"] = *bflOpts.Draft
	}
	if mode == "i2v" {
		body["keyframes"] = keyframes
	}
	if mode == "v2v" {
		body["start_video"] = *startVideo
	}

	return body, warnings, nil
}

// DoStart submits a FLUX 3 video generation request and returns an opaque
// operation reference (TS BlackForestLabsVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	body, warnings, err := m.buildVideoArgs(&opts.VideoModelV3CallOptions)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, bflInternalRequest(http.MethodPost, "/"+m.modelID, body, opts.Headers))
	if err != nil {
		return nil, providererrors.NewProviderError("bfl", 0, "", err.Error(), err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("BFL API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var submit bflVideoSubmitResponse
	if err := json.Unmarshal(resp.Body, &submit); err != nil {
		return nil, fmt.Errorf("failed to decode submit response: %w", err)
	}
	if submit.ID == "" || submit.PollingURL == "" {
		return nil, fmt.Errorf("missing id or polling_url in BFL video submit response")
	}
	if parsed, err := url.Parse(submit.PollingURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid polling_url in BFL video submit response")
	}

	operation, _ := json.Marshal(bflVideoOperation{
		RequestID:  submit.ID,
		PollingURL: submit.PollingURL,
		Cost:       submit.Cost,
		InputMP:    submit.InputMP,
		OutputMP:   submit.OutputMP,
	})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHTTPHeader(resp.Headers),
		},
	}, nil
}

// ensurePollingURLHasID adds the request id as an `id` query parameter when
// the polling URL does not already carry one (TS doStatus's
// url.searchParams.set fallback).
func ensurePollingURLHasID(pollingURL, requestID string) (string, error) {
	u, err := url.Parse(pollingURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if q.Get("id") == "" {
		q.Set("id", requestID)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// fetchStatusJSON fetches and decodes the poll response for pollURL, which
// is provider-response data (BlackForestLabsVideoOperation.pollingUrl), so it
// is validated and DNS-pinned exactly like downloadImage's response-supplied
// sample URL. Credentials are only sent on the initial request when pollURL
// itself resolves to a trusted host (the developer-configured base URL or
// any *.bfl.ai host); the fileutil-validated redirect handling behind
// TrustRoutingTransport strips them again if any hop then leaves that origin
// (TS getFromApi({validateUrl:true, trustedOrigin}) plus the
// isTrustedUrl(...) ? headers : undefined guard on the initial request).
func (m *VideoModel) fetchStatusJSON(ctx context.Context, pollURL string, headers map[string]string, target interface{}) (map[string][]string, error) {
	baseURL := m.provider.baseURL()
	isTrusted := func(raw string) bool { return bflTrustedURL(raw, baseURL) }

	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	opts.URLValidator = fileutil.TrustedURLValidator(isTrusted)
	opts.Transport = fileutil.TrustRoutingTransport(isTrusted, nil, downloadTransport())
	if err := opts.URLValidator(pollURL); err != nil {
		return nil, err
	}
	if isTrusted(pollURL) {
		opts.Headers = headers
	}

	result, err := fileutil.PollJSON(ctx, pollURL, opts, target)
	if err != nil {
		return nil, err
	}
	return result.Headers, nil
}

// DoStatus checks the status of an asynchronous FLUX 3 video generation
// started with DoStart (TS BlackForestLabsVideoModel#doStatus).
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	var op bflVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("bfl: invalid operation reference: %w", err)
	}

	pollURL, err := ensurePollingURLHasID(op.PollingURL, op.RequestID)
	if err != nil {
		return nil, fmt.Errorf("bfl: invalid polling_url: %w", err)
	}

	headers := mergeStringMaps(m.provider.config.Headers, opts.Headers)

	var pollResp bflVideoPollResponse
	respHeaders, err := m.fetchStatusJSON(ctx, pollURL, headers, &pollResp)
	if err != nil {
		return nil, m.handleVideoPollError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   flattenHeaderSlice(respHeaders),
	}

	status := pollResp.Status
	if status == "" {
		status = pollResp.State
	}
	cost := pollResp.Cost
	if cost == nil {
		cost = op.Cost
	}

	if status == "Ready" {
		var result bflVideoResultData
		if len(pollResp.Result) == 0 {
			return nil, providererrors.NewVideoGenerationError("bfl", m.modelID,
				fmt.Sprintf("Black Forest Labs reported the video as Ready but returned no result.sample URL. Request id: %s", op.RequestID), nil)
		}
		if err := json.Unmarshal(pollResp.Result, &result); err != nil || result.Sample == "" {
			return nil, providererrors.NewVideoGenerationError("bfl", m.modelID,
				fmt.Sprintf("Black Forest Labs reported the video as Ready but returned no result.sample URL. Request id: %s", op.RequestID), nil)
		}

		video := map[string]interface{}{"id": op.RequestID, "videoUrl": result.Sample}
		if result.Seed != nil {
			video["seed"] = *result.Seed
		}
		if result.StartTime != nil {
			video["start_time"] = *result.StartTime
		}
		if result.EndTime != nil {
			video["end_time"] = *result.EndTime
		}
		if result.Duration != nil {
			video["duration"] = *result.Duration
		}
		if result.DraftCache != "" {
			video["draftCache"] = result.DraftCache
		}
		if cost != nil {
			video["cost"] = *cost
		}
		if op.InputMP != nil {
			video["inputMegapixels"] = *op.InputMP
		}
		if op.OutputMP != nil {
			video["outputMegapixels"] = *op.OutputMP
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status: provider.VideoOperationStatusCompleted,
			Videos: []provider.VideoModelV3VideoData{{Type: "url", URL: result.Sample, MediaType: "video/mp4"}},
			ProviderMetadata: map[string]interface{}{
				"blackForestLabs": map[string]interface{}{"videos": []map[string]interface{}{video}},
			},
			Response: responseInfo,
		}, nil
	}

	if videoTerminalFailureStatuses[status] {
		detail := describePollDetails(pollResp.Details)
		errMsg := fmt.Sprintf("Black Forest Labs video generation failed with status %q", status)
		if detail != "" {
			errMsg += ": " + detail
		}
		errMsg += fmt.Sprintf(". Request id: %s", op.RequestID)
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusError, Error: errMsg, Response: responseInfo}, nil
	}

	return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: responseInfo}, nil
}

// describePollDetails renders a failed poll's `details` payload, which is a
// plain string for moderation refusals and an object for everything else.
func describePollDetails(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// handleVideoPollError converts a fileutil poll error into a BFL API error
// where the response carries a structured {message, detail} envelope.
func (m *VideoModel) handleVideoPollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		var envelope struct {
			Message string      `json:"message"`
			Detail  interface{} `json:"detail"`
		}
		if jsonErr := json.Unmarshal(dlErr.Body, &envelope); jsonErr == nil {
			if s, ok := envelope.Detail.(string); ok && s != "" {
				return fmt.Errorf("Black Forest Labs API error: %s", s)
			}
			if envelope.Detail != nil {
				if b, mErr := json.Marshal(envelope.Detail); mErr == nil {
					return fmt.Errorf("Black Forest Labs API error: %s", string(b))
				}
			}
			if envelope.Message != "" {
				return fmt.Errorf("Black Forest Labs API error: %s", envelope.Message)
			}
		}
		return fmt.Errorf("Black Forest Labs API error: status check returned %d: %s", dlErr.StatusCode, string(dlErr.Body))
	}
	return fmt.Errorf("failed to check status: %w", err)
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus at a fixed interval until it completes or the configured
// timeout elapses (TS BlackForestLabsVideoModel#doGenerate, which is itself
// built directly on doStart/doStatus with no separate implementation).
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	var op bflVideoOperation
	_ = json.Unmarshal(startResult.Operation, &op)

	pollInterval := time.Duration(m.provider.config.PollIntervalMillis) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = defaultVideoPollIntervalMillis * time.Millisecond
	}
	pollTimeout := time.Duration(m.provider.config.PollTimeoutMillis) * time.Millisecond
	if pollTimeout <= 0 {
		pollTimeout = defaultVideoPollTimeoutMillis * time.Millisecond
	}
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		if time.Since(startTime) > pollTimeout {
			return nil, fmt.Errorf("Black Forest Labs video generation timed out after %dms. Request id: %s", pollTimeout.Milliseconds(), op.RequestID)
		}

		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			return nil, err
		}

		if status.Status == provider.VideoOperationStatusPending {
			continue
		}
		if status.Status == provider.VideoOperationStatusError {
			return nil, providererrors.NewVideoGenerationError("bfl", m.modelID, status.Error, nil)
		}

		return &provider.VideoModelV3Response{
			Videos:           status.Videos,
			Warnings:         append(append([]types.Warning{}, startResult.Warnings...), status.Warnings...),
			ProviderMetadata: status.ProviderMetadata,
			Response:         status.Response,
		}, nil
	}
}

func flattenHTTPHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func flattenHeaderSlice(h map[string][]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// bflVideoSubmitResponse is the JSON body returned by POST /{modelId}.
type bflVideoSubmitResponse struct {
	ID         string   `json:"id"`
	PollingURL string   `json:"polling_url"`
	Cost       *float64 `json:"cost"`
	InputMP    *float64 `json:"input_mp"`
	OutputMP   *float64 `json:"output_mp"`
}

// bflVideoPollResponse is the JSON body returned by the polling URL.
type bflVideoPollResponse struct {
	Status  string          `json:"status"`
	State   string          `json:"state"`
	Details json.RawMessage `json:"details"`
	Result  json.RawMessage `json:"result"`
	// Cost is `SettledCostResultResponse.cost` -- the price actually
	// charged, known only once generation finishes. Absent on the plain
	// ResultResponse variant returned while the task is still processing.
	Cost *float64 `json:"cost"`
}

// bflVideoResultData is the `result` payload once status is "Ready".
type bflVideoResultData struct {
	Sample     string   `json:"sample"`
	Seed       *float64 `json:"seed"`
	StartTime  *float64 `json:"start_time"`
	EndTime    *float64 `json:"end_time"`
	Duration   *float64 `json:"duration"`
	DraftCache string   `json:"draft_cache"`
}
