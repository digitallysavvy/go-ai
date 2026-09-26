package xai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// VideoModel implements the provider.VideoModelV3 interface for XAI
type VideoModel struct {
	provider *Provider
	modelID  string
}

type VideoExtendOptions struct {
	Video    string
	Prompt   string
	Duration *float64
	Headers  map[string]string
}

// NewVideoModel creates a new XAI video generation model
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{
		provider: prov,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "xai"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (XAI generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	maxVideos := 1
	return &maxVideos
}

// Extend extends an existing video using xAI extension mode.
func (m *VideoModel) Extend(ctx context.Context, opts VideoExtendOptions) (*provider.VideoModelV3Response, error) {
	providerOptions := map[string]interface{}{
		"xai": map[string]interface{}{
			"mode":     "extend-video",
			"videoUrl": opts.Video,
		},
	}
	return m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:          opts.Prompt,
		Duration:        opts.Duration,
		ProviderOptions: providerOptions,
		Headers:         opts.Headers,
	})
}

// XAIVideoProviderOptions contains provider-specific options for XAI video generation
type XAIVideoProviderOptions struct {
	// PollIntervalMs is the interval between status checks in milliseconds (default: 5000)
	PollIntervalMs *int `json:"pollIntervalMs,omitempty"`

	// PollTimeoutMs is the maximum time to wait for video generation in milliseconds (default: 600000)
	PollTimeoutMs *int `json:"pollTimeoutMs,omitempty"`

	// Resolution is the output resolution: "480p", "720p", or "1080p"
	Resolution *string `json:"resolution,omitempty"`

	// VideoURL is the source video URL for video editing/extension
	VideoURL *string `json:"videoUrl,omitempty"`

	// Mode selects the operation: edit-video, extend-video, reference-to-video
	Mode *string `json:"mode,omitempty"`

	// ReferenceImageURLs are reference image URLs (1-7) for reference-to-video mode
	ReferenceImageURLs []string `json:"referenceImageUrls,omitempty"`

	// ReferenceVoiceIDs are preset voice ids (up to 3) that give the R2V
	// subject a voice. Only applies to reference-to-video generation.
	ReferenceVoiceIDs []string `json:"referenceVoiceIds,omitempty"`

	// User is a unique identifier representing the end user, for abuse
	// monitoring. Not sent for video extension requests.
	User *string `json:"user,omitempty"`
}

// DoGenerate performs video generation with polling
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	warnings := []types.Warning{}

	// Extract provider options
	provOpts, extra, err := extractVideoProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	mode := resolveMode(provOpts)
	isEdit := mode == "edit-video"
	isExtension := mode == "extend-video"
	hasReferenceImages := mode == "reference-to-video"

	// Check for unsupported options and add warnings
	warnings = append(warnings, m.checkUnsupportedOptions(opts, provOpts, mode)...)

	// Build request body
	body, bodyWarnings := m.buildRequestBody(opts, provOpts, extra, isEdit, isExtension, hasReferenceImages)
	warnings = append(warnings, bodyWarnings...)

	// Determine endpoint
	endpoint := "/videos/generations"
	if isEdit {
		endpoint = "/videos/edits"
	} else if isExtension {
		endpoint = "/videos/extensions"
	}

	// Submit video generation/edit/extension request
	var createResp xaiVideoCreateResponse
	if err := m.provider.client.PostJSON(ctx, endpoint, body, &createResp); err != nil {
		return nil, m.handleError(err)
	}

	if createResp.RequestID == "" {
		return nil, providererrors.NewProviderError("xai", 0, "",
			fmt.Sprintf("No request_id returned from xAI API. Response: %+v", createResp), nil)
	}

	// Poll for completion
	pollInterval := 5 * time.Second
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs > 0 {
		pollInterval = time.Duration(*provOpts.PollIntervalMs) * time.Millisecond
	}

	pollTimeout := 600 * time.Second
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs > 0 {
		pollTimeout = time.Duration(*provOpts.PollTimeoutMs) * time.Millisecond
	}

	// Use polling utility
	pollOpts := polling.PollOptions{
		PollIntervalMs: int(pollInterval.Milliseconds()),
		PollTimeoutMs:  int(pollTimeout.Milliseconds()),
	}

	statusChecker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.checkVideoStatus(ctx, createResp.RequestID)
		if err != nil {
			return nil, m.handleError(err)
		}

		// Check if done
		if status.Status == "done" || (status.Status == "" && status.Video != nil && status.Video.URL != "") {
			// Terminal outcomes (moderation rejection, missing URL) are
			// reported as an upstream `failed` status via polling.JobResult
			// rather than thrown as a Go error, so they surface the same way
			// as any other job failure.
			if status.Video != nil && status.Video.RespectModeration != nil && !*status.Video.RespectModeration {
				return &polling.JobResult{
					Status: polling.JobStatusFailed,
					Error:  "Video generation was blocked due to a content policy violation.",
				}, nil
			}

			if status.Video == nil || status.Video.URL == "" {
				return &polling.JobResult{
					Status: polling.JobStatusFailed,
					Error:  "Video generation completed but no video URL was returned.",
				}, nil
			}
			return &polling.JobResult{
				Status:    polling.JobStatusCompleted,
				OutputURL: status.Video.URL,
				Metadata: map[string]interface{}{
					"video":    status.Video,
					"model":    status.Model,
					"usage":    status.Usage,
					"warnings": status.Warnings,
					"progress": status.Progress,
				},
			}, nil
		}

		// Check if expired
		if status.Status == "expired" {
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  "Video generation request expired.",
			}, nil
		}

		if status.Status == "failed" {
			errDetails := ""
			if status.Error != nil {
				if status.Error.Message != "" {
					errDetails = status.Error.Message
				} else {
					errDetails = status.Error.Code
				}
			}
			if errDetails != "" {
				return &polling.JobResult{
					Status: polling.JobStatusFailed,
					Error:  fmt.Sprintf("Video generation failed: %s", errDetails),
				}, nil
			}
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  "Video generation failed.",
			}, nil
		}

		// Still pending
		return &polling.JobResult{
			Status: polling.JobStatusProcessing,
		}, nil
	}

	jobResult, err := polling.PollForCompletion(ctx, statusChecker, pollOpts)

	if err != nil {
		return nil, err
	}

	// Extract video data from metadata
	videoData := jobResult.Metadata["video"].(*xaiVideoData)
	if warningItems, ok := jobResult.Metadata["warnings"].([]xaiWarning); ok {
		for _, w := range warningItems {
			msg := w.Message
			if msg == "" {
				msg = w.Code
			}
			if msg != "" {
				warnings = append(warnings, types.Warning{
					Type:    "provider-warning",
					Details: msg,
					Message: msg,
				})
			}
		}
	}

	// Build xai-scoped metadata.
	xaiMeta := map[string]interface{}{
		"requestId": createResp.RequestID,
		"videoUrl":  videoData.URL,
	}
	if videoData.Duration != nil {
		xaiMeta["duration"] = *videoData.Duration
	}
	// Cost is in the top-level usage object, not inside the video object.
	if usageData, ok := jobResult.Metadata["usage"].(*xaiVideoUsage); ok && usageData != nil {
		if usageData.CostInUsdTicks != nil {
			xaiMeta["costInUsdTicks"] = *usageData.CostInUsdTicks
		}
	}
	if progress, ok := jobResult.Metadata["progress"].(*int); ok && progress != nil {
		xaiMeta["progress"] = *progress
	}

	// Build response
	resp := &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "url",
				URL:       videoData.URL,
				MediaType: "video/mp4",
			},
		},
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"xai": xaiMeta,
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   map[string]string{},
		},
	}

	return resp, nil
}

// maxPendingStatusBodyBytes bounds how much of a 202 (still processing)
// status response body we attempt to parse. xAI answers 202 while a
// generation is still running, sometimes with an empty body; this is a
// generous bound for a `{status, progress}` payload of roughly 50 bytes.
const maxPendingStatusBodyBytes = 1024 * 1024

// checkVideoStatus fetches the current status of a video generation/edit/
// extension job. It uses the lower-level client.Get (rather than GetJSON) so
// a 202 response with an empty or non-JSON body can be treated as "pending"
// instead of failing on JSON unmarshal.
func (m *VideoModel) checkVideoStatus(ctx context.Context, requestID string) (*xaiVideoStatusResponse, error) {
	statusPath := "/videos/" + providerutils.EncodePathSegment(requestID)

	resp, err := m.provider.client.Get(ctx, statusPath)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == 202 {
		// Bound how much of the body we attempt to parse; treat an empty,
		// invalid, or oversized payload as a pending status rather than an
		// error.
		limited, readErr := io.ReadAll(io.LimitReader(bytes.NewReader(resp.Body), maxPendingStatusBodyBytes+1))
		if readErr != nil || len(limited) > maxPendingStatusBodyBytes || len(limited) == 0 {
			return &xaiVideoStatusResponse{Status: "pending"}, nil
		}

		var status xaiVideoStatusResponse
		if err := json.Unmarshal(limited, &status); err != nil {
			return &xaiVideoStatusResponse{Status: "pending"}, nil
		}
		if status.Status == "" && (status.Video == nil || status.Video.URL == "") {
			status.Status = "pending"
		}
		return &status, nil
	}

	if resp.StatusCode >= 400 {
		return nil, providererrors.NewProviderError("xai", resp.StatusCode, "",
			parseXAIErrorMessage(resp.Body), nil)
	}

	var status xaiVideoStatusResponse
	if err := json.Unmarshal(resp.Body, &status); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return &status, nil
}

// buildRequestBody constructs the API request body
func (m *VideoModel) buildRequestBody(
	opts *provider.VideoModelV3CallOptions,
	provOpts *XAIVideoProviderOptions,
	extra map[string]interface{},
	isEdit bool,
	isExtension bool,
	hasReferenceImages bool,
) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}

	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Add duration (not for edits; extension allows duration)
	if !isEdit && opts.Duration != nil {
		body["duration"] = *opts.Duration
	}

	// Add aspect ratio (not for edits or extension)
	if !isEdit && !isExtension && opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	// Add resolution (not for edits or extension)
	if !isEdit && !isExtension && provOpts.Resolution != nil {
		body["resolution"] = *provOpts.Resolution
	} else if !isEdit && !isExtension && opts.Resolution != "" {
		// Map standard resolution to XAI format
		mapped := mapResolution(opts.Resolution)
		if mapped != "" {
			body["resolution"] = mapped
		}
	}

	// Video editing/extension: add source video URL
	if (isEdit || isExtension) && provOpts.VideoURL != nil {
		body["video"] = map[string]interface{}{
			"url": *provOpts.VideoURL,
		}
	}

	// Image-to-video: add source image
	if opts.Image != nil {
		body["image"] = m.convertImageToXAIFormat(opts.Image)
	}

	// Reference images for R2V (reference-to-video) generation.
	if hasReferenceImages {
		referenceImages := resolveReferenceImages(provOpts)

		if referenceImages != nil {
			body["reference_images"] = referenceImages
		} else {
			// Explicit R2V with no usable image references would silently
			// send a plain generations request; tell the caller rather than
			// ever sending an empty `reference_images: []`.
			warnings = append(warnings, unsupportedVideoWarning("referenceImageUrls",
				"xAI reference-to-video requires at least one image reference. The video will be generated without reference images."))
		}

		if len(provOpts.ReferenceVoiceIDs) > 0 {
			voiceIDs := provOpts.ReferenceVoiceIDs
			if len(voiceIDs) > 3 {
				warnings = append(warnings, unsupportedVideoWarning("referenceVoiceIds",
					"xAI reference-to-video accepts at most 3 referenceVoiceIds. Extra voice ids were ignored."))
				voiceIDs = voiceIDs[:3]
			}
			audios := make([]map[string]interface{}, 0, len(voiceIDs))
			for _, voiceID := range voiceIDs {
				audios = append(audios, map[string]interface{}{"voice_id": voiceID})
			}
			body["reference_audios"] = audios
		}

		// Reference-to-video is limited to 720p; downgrade a 1080p request.
		if res, ok := body["resolution"].(string); ok && res == "1080p" {
			warnings = append(warnings, unsupportedVideoWarning("resolution",
				"xAI reference-to-video is limited to 720p. The request was downgraded from 1080p to 720p."))
			body["resolution"] = "720p"
		}
	}

	// 1080p requires grok-imagine-video-1.5; the original grok-imagine-video
	// rejects it. Warn, but send the request as the caller asked.
	if res, ok := body["resolution"].(string); ok && res == "1080p" && m.modelID == ModelGrokImagineVideo {
		warnings = append(warnings, unsupportedVideoWarning("resolution",
			fmt.Sprintf("xAI model %q does not support 1080p. Use %q for 1080p, or a lower resolution. The request was sent with 1080p.",
				ModelGrokImagineVideo, ModelGrokImagineVideo15)))
	}

	// Preset reference voices only apply to reference-to-video generation.
	if !hasReferenceImages && len(provOpts.ReferenceVoiceIDs) > 0 {
		warnings = append(warnings, unsupportedVideoWarning("referenceVoiceIds",
			"xAI only supports reference voices for reference-to-video generation. The reference voices were ignored."))
	}

	if !isExtension && provOpts.User != nil {
		body["user"] = *provOpts.User
	}

	// Passthrough any extra provider options not handled above.
	for k, v := range extra {
		body[k] = v
	}

	return body, warnings
}

// resolveReferenceImages resolves the reference images for R2V generation
// from the ReferenceImageURLs provider option. Empty entries are skipped so
// an empty `reference_images: []` is never sent.
func resolveReferenceImages(provOpts *XAIVideoProviderOptions) []map[string]interface{} {
	if len(provOpts.ReferenceImageURLs) == 0 {
		return nil
	}

	refs := make([]map[string]interface{}, 0, len(provOpts.ReferenceImageURLs))
	for _, url := range provOpts.ReferenceImageURLs {
		if url == "" {
			continue
		}
		refs = append(refs, map[string]interface{}{"url": url})
	}

	if len(refs) == 0 {
		return nil
	}
	return refs
}

// convertImageToXAIFormat converts VideoModelV3File to XAI image format
func (m *VideoModel) convertImageToXAIFormat(img *provider.VideoModelV3File) map[string]interface{} {
	if img.Type == "url" {
		return map[string]interface{}{
			"url": img.URL,
		}
	}

	// Convert binary data to base64 data URL
	base64Data := base64.StdEncoding.EncodeToString(img.Data)
	mediaType := img.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, base64Data)

	return map[string]interface{}{
		"url": dataURL,
	}
}

// checkUnsupportedOptions checks for unsupported options and generates warnings
func (m *VideoModel) checkUnsupportedOptions(opts *provider.VideoModelV3CallOptions, provOpts *XAIVideoProviderOptions, mode string) []types.Warning {
	warnings := []types.Warning{}
	isEdit := mode == "edit-video"
	isExtension := mode == "extend-video"

	if opts.FPS != nil {
		warnings = append(warnings, unsupportedVideoWarning("fps", "xAI video models do not support custom FPS."))
	}

	if opts.Seed != nil {
		warnings = append(warnings, unsupportedVideoWarning("seed", "xAI video models do not support seed."))
	}

	if opts.N > 1 {
		warnings = append(warnings, unsupportedVideoWarning("n", "xAI video models do not support generating multiple videos per call. Only 1 video will be generated."))
	}

	if isEdit && opts.Duration != nil {
		warnings = append(warnings, unsupportedVideoWarning("duration", "xAI video editing does not support custom duration."))
	}

	if isEdit && opts.AspectRatio != "" {
		warnings = append(warnings, unsupportedVideoWarning("aspectRatio", "xAI video editing does not support custom aspect ratio."))
	}

	if isEdit && (provOpts.Resolution != nil || opts.Resolution != "") {
		warnings = append(warnings, unsupportedVideoWarning("resolution", "xAI video editing does not support custom resolution."))
	}

	if isExtension && opts.AspectRatio != "" {
		warnings = append(warnings, unsupportedVideoWarning("aspectRatio", "xAI video extension does not support custom aspect ratio."))
	}

	if isExtension && (provOpts.Resolution != nil || opts.Resolution != "") {
		warnings = append(warnings, unsupportedVideoWarning("resolution", "xAI video extension does not support custom resolution."))
	}

	if !isEdit && !isExtension && provOpts.Resolution == nil && opts.Resolution != "" && mapResolution(opts.Resolution) == "" {
		warnings = append(warnings, unsupportedVideoWarning(
			"resolution",
			fmt.Sprintf("Unrecognized resolution %q. Use providerOptions.xai.resolution with \"480p\", \"720p\", or \"1080p\" instead.", opts.Resolution),
		))
	}

	return warnings
}

func unsupportedVideoWarning(feature, details string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: feature,
		Details: details,
		Message: details,
	}
}

func resolveMode(provOpts *XAIVideoProviderOptions) string {
	if provOpts.Mode != nil && *provOpts.Mode != "" {
		return *provOpts.Mode
	}
	if provOpts.VideoURL != nil && *provOpts.VideoURL != "" {
		return "edit-video"
	}
	if len(provOpts.ReferenceImageURLs) > 0 {
		return "reference-to-video"
	}
	return ""
}

// mapResolution maps standard resolution strings to XAI format
func mapResolution(resolution string) string {
	resolutionMap := map[string]string{
		"1920x1080": "1080p",
		"1280x720":  "720p",
		"854x480":   "480p",
		"640x480":   "480p",
	}

	if mapped, ok := resolutionMap[resolution]; ok {
		return mapped
	}

	return ""
}

// extractVideoProviderOptions extracts XAI-specific provider options and any
// unrecognized keys (which are passed through to the API request body).
func extractVideoProviderOptions(opts map[string]interface{}) (*XAIVideoProviderOptions, map[string]interface{}, error) {
	if opts == nil {
		return &XAIVideoProviderOptions{}, nil, nil
	}

	xaiRaw, ok := opts["xai"]
	if !ok {
		return &XAIVideoProviderOptions{}, nil, nil
	}

	// Convert to JSON and back to struct
	jsonData, err := json.Marshal(xaiRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal provider options: %w", err)
	}

	var provOpts XAIVideoProviderOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal provider options: %w", err)
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal provider options map: %w", err)
	}
	if err := validateVideoProviderOptions(rawMap, &provOpts); err != nil {
		return nil, nil, err
	}

	// Collect any unrecognized keys for passthrough to the API.
	known := map[string]bool{
		"pollIntervalMs": true, "pollTimeoutMs": true,
		"resolution": true, "videoUrl": true,
		"mode": true, "referenceImageUrls": true,
		"referenceVoiceIds": true, "user": true,
	}
	extra := make(map[string]interface{})
	for k, v := range rawMap {
		if !known[k] {
			extra[k] = v
		}
	}

	return &provOpts, extra, nil
}

func validateVideoProviderOptions(rawMap map[string]interface{}, provOpts *XAIVideoProviderOptions) error {
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs <= 0 {
		return fmt.Errorf("xai provider option pollIntervalMs must be positive")
	}
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs <= 0 {
		return fmt.Errorf("xai provider option pollTimeoutMs must be positive")
	}

	if _, ok := rawMap["referenceImageUrls"]; ok {
		if len(provOpts.ReferenceImageURLs) == 0 {
			return fmt.Errorf("xai provider option referenceImageUrls must contain at least 1 image")
		}
		if len(provOpts.ReferenceImageURLs) > 7 {
			return fmt.Errorf("xai provider option referenceImageUrls must contain at most 7 images")
		}
		for _, url := range provOpts.ReferenceImageURLs {
			if url == "" {
				return fmt.Errorf("xai provider option referenceImageUrls must not contain empty URLs")
			}
		}
	}

	if len(provOpts.ReferenceVoiceIDs) > 0 {
		for _, voiceID := range provOpts.ReferenceVoiceIDs {
			if voiceID == "" {
				return fmt.Errorf("xai provider option referenceVoiceIds must not contain empty ids")
			}
		}
	}

	return nil
}

// handleError converts provider errors
func (m *VideoModel) handleError(err error) error {
	if provErr, ok := err.(*providererrors.ProviderError); ok {
		return provErr
	}
	return providererrors.NewProviderError("xai", 0, "", err.Error(), err)
}

// xaiVideoCreateResponse represents the video creation API response
type xaiVideoCreateResponse struct {
	RequestID string `json:"request_id"`
}

// xaiVideoStatusResponse represents the video status API response
type xaiVideoStatusResponse struct {
	Status   string             `json:"status"`
	Video    *xaiVideoData      `json:"video,omitempty"`
	Model    string             `json:"model,omitempty"`
	Usage    *xaiVideoUsage     `json:"usage,omitempty"`
	Progress *int               `json:"progress,omitempty"`
	Warnings []xaiWarning       `json:"warnings,omitempty"`
	Error    *xaiVideoStatusErr `json:"error,omitempty"`
}

// xaiVideoStatusErr holds the error details from a failed video status response.
type xaiVideoStatusErr struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// xaiVideoUsage holds top-level usage data from the video status response.
type xaiVideoUsage struct {
	CostInUsdTicks *int64 `json:"cost_in_usd_ticks,omitempty"`
}

// xaiVideoData represents video data in the status response
type xaiVideoData struct {
	URL               string   `json:"url"`
	Duration          *float64 `json:"duration,omitempty"`
	RespectModeration *bool    `json:"respect_moderation,omitempty"`
}

type xaiWarning struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
