package heygen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	defaultPollIntervalMillis = 5000
	defaultPollTimeoutMillis  = 600_000
)

// frameSize maps a documented frame size to its HeyGen size class and aspect
// ratio. HeyGen's named size classes do not consistently equal the short edge.
// https://developers.heygen.com/docs/models/heygen-video#output
type frameSize struct {
	resolution  string
	aspectRatio string
}

var frameSizes = map[string]frameSize{
	"960x416":   {"480p", "21:9"},
	"832x480":   {"480p", "16:9"},
	"640x480":   {"480p", "4:3"},
	"480x480":   {"480p", "1:1"},
	"480x640":   {"480p", "3:4"},
	"480x832":   {"480p", "9:16"},
	"1536x672":  {"768p", "21:9"},
	"1344x768":  {"768p", "16:9"},
	"1024x768":  {"768p", "4:3"},
	"768x768":   {"768p", "1:1"},
	"768x1024":  {"768p", "3:4"},
	"768x1344":  {"768p", "9:16"},
	"1890x1080": {"1080p", "16:9"},
	"1080x1890": {"1080p", "9:16"},
	"2688x1536": {"2k", "16:9"},
	"1536x2688": {"2k", "9:16"},
}

// VideoModel implements provider.VideoModelV3 (plus the optional
// VideoModelStarter and VideoModelStatusChecker capabilities) for HeyGen.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new HeyGen video model.
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *VideoModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *VideoModel) Provider() string { return "heygen.video" }

// ModelID returns the model ID.
func (m *VideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 1: HeyGen generates one video per call.
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// heygenOperation is the opaque operation reference returned by DoStart and
// passed back into DoStatus. Resolution is retained across status calls.
type heygenOperation struct {
	VideoID    string `json:"videoId"`
	Mode       string `json:"mode"`
	Resolution string `json:"resolution"`
}

func (o heygenOperation) metadata() map[string]interface{} {
	return map[string]interface{}{"videoId": o.VideoID, "mode": o.Mode, "resolution": o.Resolution}
}

func invalidArg(field, message string) error {
	return &providererrors.InvalidArgumentError{Field: field, Message: message}
}

// DoStart submits a HeyGen video generation and returns an opaque operation
// reference (TS HeyGenVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	if opts == nil {
		opts = &provider.VideoModelV3StartOptions{}
	}
	currentDate := time.Now()
	warnings := []types.Warning{}

	heygenOpts, err := parseVideoModelOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	if heygenOpts == nil {
		heygenOpts = &VideoModelOptions{}
	}

	if promptLen := len(utf16.Encode([]rune(opts.Prompt))); promptLen == 0 || promptLen > 32000 {
		return nil, invalidArg("prompt", "HeyGen requires a prompt between 1 and 32,000 characters.")
	}
	if opts.Duration != nil {
		d := *opts.Duration
		if d != float64(int64(d)) || d < 5 || d > 15 {
			return nil, invalidArg("duration", "HeyGen duration must be an integer between 5 and 15 seconds.")
		}
	}
	if opts.Seed != nil && (*opts.Seed < 0 || int64(*opts.Seed) > 0xffffffff) {
		return nil, invalidArg("seed", "HeyGen seed must be an unsigned 32-bit integer.")
	}
	if opts.N > 1 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "n", Details: "HeyGen generates one video per call."})
	}
	if opts.FPS != nil && *opts.FPS != 24 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "fps", Details: "HeyGen generates video at 24 fps."})
	}
	if opts.GenerateAudio != nil && !*opts.GenerateAudio {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "generateAudio", Details: "HeyGen always generates audio alongside the video."})
	}

	frames := opts.FrameImages
	for _, frame := range frames {
		if frame.FrameType != provider.VideoFrameTypeFirstFrame {
			return nil, invalidArg("frameImages", "HeyGen supports a single first frame, not last-frame conditioning.")
		}
	}
	if len(frames) > 1 {
		return nil, invalidArg("frameImages", "HeyGen supports a single first frame, not last-frame conditioning.")
	}
	if len(frames) > 0 && opts.Image != nil {
		return nil, invalidArg("image", "Supply either image or a first frame, not both.")
	}
	var firstFrame *provider.VideoModelV3File
	if len(frames) > 0 {
		f := frames[0].Image
		firstFrame = &f
	} else {
		firstFrame = opts.Image
	}
	if firstFrame != nil && heygenOpts.Image != nil {
		return nil, invalidArg("image", "Supply either a standard image or providerOptions.heygen.image, not both.")
	}
	var image *VideoAsset
	if firstFrame != nil {
		a, err := toAsset(*firstFrame, "image")
		if err != nil {
			return nil, err
		}
		image = &a
	} else {
		image = heygenOpts.Image
	}

	var referenceImages, referenceVideos, referenceAudio []VideoAsset
	for _, reference := range opts.InputReferences {
		var kind string
		switch {
		case strings.HasPrefix(reference.MediaType, "video/"):
			kind = "video"
		case strings.HasPrefix(reference.MediaType, "audio/"):
			kind = "audio"
		case strings.HasPrefix(reference.MediaType, "image/"):
			kind = "image"
		default:
			return nil, invalidArg("inputReferences", "HeyGen references require an image, video, or audio mediaType.")
		}
		a, err := toAsset(reference, kind)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "video":
			referenceVideos = append(referenceVideos, a)
		case "audio":
			referenceAudio = append(referenceAudio, a)
		default:
			referenceImages = append(referenceImages, a)
		}
	}
	referenceImages = append(referenceImages, heygenOpts.ReferenceImages...)
	referenceVideos = append(referenceVideos, heygenOpts.ReferenceVideos...)
	referenceAudio = append(referenceAudio, heygenOpts.ReferenceAudio...)
	referenceCount := len(referenceImages) + len(referenceVideos) + len(referenceAudio)
	if len(referenceImages) > 9 || len(referenceVideos) > 3 || len(referenceAudio) > 3 || referenceCount > 12 {
		return nil, invalidArg("inputReferences", "HeyGen accepts at most 9 images, 3 videos, 3 audio references, and 12 references in total.")
	}

	mode := heygenOpts.Mode
	if mode == "" {
		switch {
		case image != nil:
			mode = "image_to_video"
		case referenceCount > 0:
			mode = "reference_to_video"
		default:
			mode = "text_to_video"
		}
	}
	if (mode == "image_to_video" && image == nil) || (mode != "image_to_video" && image != nil) {
		return nil, invalidArg("image", "A first-frame image is required for image_to_video and cannot be used in other modes.")
	}
	if (mode != "reference_to_video" && referenceCount > 0) ||
		(mode == "reference_to_video" && len(referenceImages)+len(referenceVideos) == 0) {
		return nil, invalidArg("inputReferences", "References are only supported in reference_to_video, which requires at least one image or video reference.")
	}

	var size *frameSize
	if opts.Resolution != "" && heygenOpts.Resolution == "" {
		fs, ok := frameSizes[opts.Resolution]
		if !ok {
			return nil, invalidArg("resolution", "Unsupported HeyGen frame size. Use a documented frame size or providerOptions.heygen.resolution to select 480p, 768p, 1080p, or 2k.")
		}
		size = &fs
	}
	resolution := heygenOpts.Resolution
	if resolution == "" {
		if size != nil {
			resolution = size.resolution
		} else {
			resolution = "768p"
		}
	}
	aspectRatio := opts.AspectRatio
	if aspectRatio == "" {
		switch {
		case size != nil:
			aspectRatio = size.aspectRatio
		case mode == "text_to_video":
			aspectRatio = "16:9"
		default:
			aspectRatio = "adaptive"
		}
	}
	if mode == "image_to_video" {
		if opts.AspectRatio != "" || size != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "aspectRatio",
				Details: "HeyGen image-to-video follows the first frame aspect ratio; only the resolution size class is applied.",
			})
		}
	} else {
		supported := map[string]bool{"21:9": true, "16:9": true, "4:3": true, "1:1": true, "3:4": true, "9:16": true}
		if mode == "reference_to_video" {
			supported["adaptive"] = true
		}
		if !supported[aspectRatio] {
			return nil, invalidArg("aspectRatio", fmt.Sprintf("Unsupported HeyGen aspect ratio for %s: %s.", mode, aspectRatio))
		}
		if (resolution == "1080p" || resolution == "2k") && aspectRatio != "16:9" && aspectRatio != "9:16" {
			return nil, invalidArg("aspectRatio", "HeyGen 1080p and 2k require an explicit 16:9 or 9:16 aspect ratio for text-to-video and reference-to-video.")
		}
	}

	duration := 5.0
	if opts.Duration != nil {
		duration = *opts.Duration
	}
	body := map[string]interface{}{
		"model":      m.modelID,
		"mode":       mode,
		"prompt":     opts.Prompt,
		"duration":   duration,
		"resolution": resolution,
	}
	if mode != "image_to_video" {
		body["aspect_ratio"] = aspectRatio
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	if heygenOpts.PromptEnhancement != "" {
		body["prompt_enhancement"] = heygenOpts.PromptEnhancement
	}
	if image != nil {
		b, err := assetBody(*image, "image")
		if err != nil {
			return nil, err
		}
		body["image"] = b
	}
	for _, group := range []struct {
		key    string
		kind   string
		assets []VideoAsset
	}{
		{"reference_images", "image", referenceImages},
		{"reference_videos", "video", referenceVideos},
		{"reference_audio", "audio", referenceAudio},
	} {
		if len(group.assets) == 0 {
			continue
		}
		items := make([]map[string]interface{}, 0, len(group.assets))
		for _, a := range group.assets {
			b, err := assetBody(a, group.kind)
			if err != nil {
				return nil, err
			}
			items = append(items, b)
		}
		body[group.key] = items
	}

	headers, err := m.prov.resolveHeaders(opts.Headers)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := mergeAbortContext(ctx, opts.AbortSignal)
	defer cancel()
	resp, err := m.prov.client.Do(callCtx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v3/models/videos",
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newHeyGenAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}
	var created struct {
		Data *struct {
			VideoID string `json:"video_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &created); err != nil || created.Data == nil || created.Data.VideoID == "" {
		return nil, providererrors.NewProviderError("heygen", resp.StatusCode, "", "Invalid JSON response", err)
	}

	op := heygenOperation{VideoID: created.Data.VideoID, Mode: mode, Resolution: resolution}
	operation, _ := json.Marshal(op)
	return &provider.VideoModelV3OperationStartResult{
		Operation:        operation,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"heygen": op.metadata()},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   flattenHeader(resp.Headers),
		},
	}, nil
}

type heygenStatusData struct {
	Status      string   `json:"status"`
	VideoURL    *string  `json:"video_url"`
	Duration    *float64 `json:"duration"`
	Width       *float64 `json:"width"`
	Height      *float64 `json:"height"`
	AspectRatio *string  `json:"aspect_ratio"`
	Seed        *float64 `json:"seed"`
	Timings     *struct {
		Inference *float64 `json:"inference"`
	} `json:"timings"`
	FailureCode    *string `json:"failure_code"`
	FailureMessage *string `json:"failure_message"`
}

// DoStatus checks the status of a generation started with DoStart (TS
// HeyGenVideoModel#doStatus). The returned video URL is signed; calling
// DoStatus again for a completed operation refreshes it.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	if opts == nil {
		opts = &provider.VideoModelV3StatusOptions{}
	}
	var op heygenOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("heygen: invalid operation reference: %w", err)
	}
	if op.VideoID == "" {
		return nil, fmt.Errorf("heygen: invalid operation reference: videoId must not be empty")
	}
	switch op.Mode {
	case "text_to_video", "image_to_video", "reference_to_video":
	default:
		return nil, fmt.Errorf("heygen: invalid operation reference: unknown mode %q", op.Mode)
	}
	switch op.Resolution {
	case "480p", "768p", "1080p", "2k":
	default:
		return nil, fmt.Errorf("heygen: invalid operation reference: unknown resolution %q", op.Resolution)
	}

	currentDate := time.Now()
	headers, err := m.prov.resolveHeaders(opts.Headers)
	if err != nil {
		return nil, err
	}
	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/v3/models/videos/" + providerutils.EncodePathSegment(op.VideoID),
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newHeyGenAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}
	var envelope struct {
		Data *heygenStatusData `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil || envelope.Data == nil || envelope.Data.Status == "" {
		return nil, providererrors.NewProviderError("heygen", resp.StatusCode, "", "Invalid JSON response", err)
	}
	data := envelope.Data

	response := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   flattenHeader(resp.Headers),
	}
	meta := op.metadata()
	if data.Duration != nil {
		meta["duration"] = *data.Duration
	}
	if data.Width != nil {
		meta["width"] = *data.Width
	}
	if data.Height != nil {
		meta["height"] = *data.Height
	}
	if data.AspectRatio != nil {
		meta["aspectRatio"] = *data.AspectRatio
	}
	if data.Seed != nil {
		meta["seed"] = *data.Seed
	}
	if data.Timings != nil && data.Timings.Inference != nil {
		meta["timings"] = map[string]interface{}{"inference": *data.Timings.Inference}
	}
	if data.FailureCode != nil {
		meta["failureCode"] = *data.FailureCode
	}

	switch data.Status {
	case "pending", "processing":
		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusPending,
			Response:         response,
			ProviderMetadata: map[string]interface{}{"heygen": meta},
		}, nil
	case "failed", "cancelled":
		message := "HeyGen video generation " + data.Status
		if data.FailureMessage != nil {
			message = *data.FailureMessage
		}
		if data.FailureCode != nil {
			message += fmt.Sprintf(" (%s)", *data.FailureCode)
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusError,
			Error:            message,
			ProviderMetadata: map[string]interface{}{"heygen": meta},
			Response:         response,
		}, nil
	}
	if data.Status != "completed" || data.VideoURL == nil || *data.VideoURL == "" {
		return nil, providererrors.NewInvalidResponseDataError(data, "HeyGen returned an unexpected video status or a completed video without a URL.")
	}
	return &provider.VideoModelV3OperationStatusResult{
		Status:   provider.VideoOperationStatusCompleted,
		Videos:   []provider.VideoModelV3VideoData{{Type: "url", URL: *data.VideoURL, MediaType: "video/mp4"}},
		Warnings: []types.Warning{},
		Response: response,
		// Per-video metadata is concatenated when generating multiple clips.
		ProviderMetadata: map[string]interface{}{"heygen": map[string]interface{}{"videos": []map[string]interface{}{meta}}},
	}, nil
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes or the configured timeout elapses.
// HeyGen's TS provider has no doGenerate of its own (the TS SDK core polls
// doStart/doStatus directly); this exists because Go's provider.VideoModelV3
// interface requires it.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	if opts == nil {
		opts = &provider.VideoModelV3CallOptions{}
	}
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	callCtx, cancel := mergeAbortContext(ctx, opts.AbortSignal)
	defer cancel()

	interval := defaultPollIntervalMillis
	if m.prov.config.PollIntervalMillis > 0 {
		interval = m.prov.config.PollIntervalMillis
	}
	timeout := defaultPollTimeoutMillis
	if m.prov.config.PollTimeoutMillis > 0 {
		timeout = m.prov.config.PollTimeoutMillis
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)

	for {
		select {
		case <-callCtx.Done():
			return nil, callCtx.Err()
		case <-time.After(time.Duration(interval) * time.Millisecond):
		}
		if time.Now().After(deadline) {
			var op heygenOperation
			_ = json.Unmarshal(startResult.Operation, &op)
			return nil, fmt.Errorf("heygen video generation timed out after %dms (video %s)", timeout, op.VideoID)
		}
		status, err := m.DoStatus(callCtx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusPending:
			continue
		case provider.VideoOperationStatusError:
			return nil, providererrors.NewVideoGenerationError("heygen", m.modelID, status.Error, nil)
		default:
			return &provider.VideoModelV3Response{
				Videos:           status.Videos,
				Warnings:         append(append([]types.Warning{}, startResult.Warnings...), status.Warnings...),
				ProviderMetadata: status.ProviderMetadata,
				Response:         status.Response,
			}, nil
		}
	}
}

// toAsset converts a standard video file input into a HeyGen asset.
func toAsset(file provider.VideoModelV3File, kind string) (VideoAsset, error) {
	if file.MediaType != "" && !strings.HasPrefix(file.MediaType, kind+"/") {
		return VideoAsset{}, invalidArg(kind, fmt.Sprintf("Expected a %s mediaType.", kind))
	}
	if file.Type == "url" {
		return VideoAsset{Type: AssetTypeURL, URL: file.URL}, nil
	}
	return VideoAsset{
		Type:      AssetTypeBase64,
		MediaType: file.MediaType,
		Data:      base64.StdEncoding.EncodeToString(file.Data),
	}, nil
}

// assetBody builds the wire representation of an asset, validating it.
func assetBody(asset VideoAsset, kind string) (map[string]interface{}, error) {
	switch asset.Type {
	case AssetTypeURL:
		if !isHTTPSURL(asset.URL) {
			return nil, invalidArg(kind, "HeyGen input URLs must be absolute HTTPS URLs.")
		}
		return map[string]interface{}{"type": "url", "url": asset.URL}, nil
	case AssetTypeAssetID:
		return map[string]interface{}{"type": "asset_id", "asset_id": asset.AssetID}, nil
	case AssetTypeBase64:
		if !strings.HasPrefix(asset.MediaType, kind+"/") {
			return nil, invalidArg(kind, fmt.Sprintf("Expected a %s mediaType.", kind))
		}
		return map[string]interface{}{"type": "base64", "media_type": asset.MediaType, "data": asset.Data}, nil
	}
	return nil, invalidArg(kind, "Unsupported HeyGen asset type.")
}

func flattenHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}
