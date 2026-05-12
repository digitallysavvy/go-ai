package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	// Resolution is the output resolution: "480p" or "720p"
	Resolution *string `json:"resolution,omitempty"`

	// VideoURL is the source video URL for video editing
	VideoURL *string `json:"videoUrl,omitempty"`

	// Mode selects the operation: edit-video, extend-video, reference-to-video
	Mode *string `json:"mode,omitempty"`

	// ReferenceImageURLs are reference image URLs for reference-to-video mode
	ReferenceImageURLs []string `json:"referenceImageUrls,omitempty"`
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
	body := m.buildRequestBody(opts, provOpts, extra, isEdit, isExtension, hasReferenceImages)

	// Determine endpoint
	endpoint := "/v1/videos/generations"
	if isEdit {
		endpoint = "/v1/videos/edits"
	} else if isExtension {
		endpoint = "/v1/videos/extensions"
	}

	// Submit video generation/edit request
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
		var status xaiVideoStatusResponse
		statusPath := fmt.Sprintf("/v1/videos/%s", createResp.RequestID)

		if err := m.provider.client.GetJSON(ctx, statusPath, &status); err != nil {
			return nil, m.handleError(err)
		}

		// Check if done
		if status.Status == "done" || (status.Status == "" && status.Video != nil && status.Video.URL != "") {
			// Check for moderation rejection: respect_moderation == false means blocked.
			if status.Video != nil && status.Video.RespectModeration != nil && !*status.Video.RespectModeration {
				return nil, &ModerationError{
					Code:    "",
					Message: "Video generation was blocked due to a content policy violation.",
				}
			}

			if status.Video == nil || status.Video.URL == "" {
				return nil, providererrors.NewProviderError("xai", 0, "",
					"Video generation completed but no video URL was returned", nil)
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
				Error:  "Video generation request expired",
			}, nil
		}

		if status.Status == "failed" {
			return &polling.JobResult{
				Status: polling.JobStatusFailed,
				Error:  "Video generation failed",
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

// buildRequestBody constructs the API request body
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, provOpts *XAIVideoProviderOptions, extra map[string]interface{}, isEdit bool, isExtension bool, hasReferenceImages bool) map[string]interface{} {
	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": opts.Prompt,
	}

	// Add duration (not for edits)
	if !isEdit && opts.Duration != nil {
		body["duration"] = *opts.Duration
	}

	// Add aspect ratio (not for edits)
	if !isEdit && !isExtension && opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	// Add resolution (not for edits)
	if !isEdit && !isExtension && provOpts.Resolution != nil {
		body["resolution"] = *provOpts.Resolution
	} else if !isEdit && !isExtension && opts.Resolution != "" {
		// Map standard resolution to XAI format
		mapped := mapResolution(opts.Resolution)
		if mapped != "" {
			body["resolution"] = mapped
		}
	}

	// Video editing: add source video URL
	if (isEdit || isExtension) && provOpts.VideoURL != nil {
		body["video"] = map[string]interface{}{
			"url": *provOpts.VideoURL,
		}
	}

	if hasReferenceImages {
		items := make([]map[string]interface{}, 0, len(provOpts.ReferenceImageURLs))
		for _, u := range provOpts.ReferenceImageURLs {
			items = append(items, map[string]interface{}{"url": u})
		}
		body["reference_images"] = items
	}

	// Image-to-video: add source image
	if opts.Image != nil {
		body["image"] = m.convertImageToXAIFormat(opts.Image)
	}

	// Passthrough any extra provider options not handled above.
	for k, v := range extra {
		body[k] = v
	}

	return body
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
			fmt.Sprintf("Unrecognized resolution %q. Use providerOptions.xai.resolution with \"480p\" or \"720p\" instead.", opts.Resolution),
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
		"1280x720": "720p",
		"854x480":  "480p",
		"640x480":  "480p",
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

	if _, ok := rawMap["referenceImageUrls"]; !ok {
		return nil
	}
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
	Status   string         `json:"status"`
	Video    *xaiVideoData  `json:"video,omitempty"`
	Model    string         `json:"model,omitempty"`
	Usage    *xaiVideoUsage `json:"usage,omitempty"`
	Progress *int           `json:"progress,omitempty"`
	Warnings []xaiWarning   `json:"warnings,omitempty"`
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
