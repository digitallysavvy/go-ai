package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// VideoModel implements the provider.VideoModelV3 interface for MiniMax
// video generation (TS MiniMaxVideoModel).
type VideoModel struct {
	prov    *Provider
	modelID string
}

// newVideoModel creates a new MiniMax video generation model.
func newVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *VideoModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name.
func (m *VideoModel) Provider() string {
	return "minimax.video"
}

// ModelID returns the model ID.
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns 1 (MiniMax generates one video per call).
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// minimaxOperation is the opaque operation reference returned by DoStart and
// passed back into DoStatus.
type minimaxOperation struct {
	TaskID         string                `json:"taskId"`
	ResolvedInputs minimaxResolvedInputs `json:"resolvedInputs"`
}

// minimaxResolvedInputs records which inputs actually made it into the
// request `content` after the caps and rejections a model imposes (TS
// resolvedInputs on MiniMaxVideoOperation): the warnings say an input was
// dropped, but not how many survived, which callers that meter usage need.
type minimaxResolvedInputs struct {
	ImageCount            int   `json:"imageCount"`
	ReferenceVideoIndices []int `json:"referenceVideoIndices,omitempty"`
}

// DoStart starts an asynchronous video generation via MiniMax's task
// creation endpoint and returns an opaque operation reference (TS
// MiniMaxVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	mmOpts, err := extractVideoProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	body, warnings, resolvedInputs, err := m.buildRequestBody(&opts.VideoModelV3CallOptions, mmOpts)
	if err != nil {
		return nil, err
	}

	// Progress notifications require a protocol-aware receiver, so we don't
	// implement VideoModelWebhookHandler; forward webhookUrl as callback_url
	// for a caller-owned receiver that echoes the challenge handshake.
	if opts.WebhookURL != "" {
		body["callback_url"] = opts.WebhookURL
	}

	submitResp, err := m.prov.videoClient.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/v2/video_generation",
		Headers: opts.Headers,
		Body:    body,
	})
	if err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to submit request: %v", err))
	}
	if submitResp.StatusCode >= 400 {
		return nil, m.parseAPIError(submitResp.Body, submitResp.StatusCode)
	}

	var createResp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(submitResp.Body, &createResp); err != nil {
		return nil, NewVideoGenerationError(fmt.Sprintf("failed to parse creation response: %v", err))
	}
	if createResp.TaskID == "" {
		return nil, NewVideoGenerationError(fmt.Sprintf("No task_id returned from the MiniMax API. Response: %s", string(submitResp.Body)))
	}

	operation, _ := json.Marshal(minimaxOperation{TaskID: createResp.TaskID, ResolvedInputs: resolvedInputs})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHeaders(submitResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via MiniMax's task query endpoint (TS
// MiniMaxVideoModel#doStatus / getStatus). The status URL is fetched
// through fileutil's validated-redirect poller, matching TS's
// `getFromApi({ validateUrl: true, trustedOrigin })`: any redirect away from
// the provider's own base URL is SSRF-validated and dialed through a
// DNS-pinning transport, and credentials are only forwarded on the trusted
// hop.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	result, statusErr, err := m.getStatus(ctx, opts)
	if err != nil {
		return nil, err
	}
	if statusErr != nil {
		result.Error = statusErr.Error()
	}
	return result, nil
}

// getStatus is the internal counterpart to DoStatus that keeps a task
// failure/cancellation/expiry as a typed *Error (TS's private getStatus,
// which returns an AISDKError on the status result rather than a string).
// DoStatus stringifies it into the public VideoModelV3OperationStatusResult
// (TS's public doStatus: `error: result.error.message`); DoGenerate calls
// getStatus directly so it can throw the typed error, matching TS's
// `if (result.status === 'error') throw result.error;`.
func (m *VideoModel) getStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, *Error, error) {
	var op minimaxOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, nil, fmt.Errorf("minimax: invalid operation reference: %w", err)
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(m.prov.videoBaseURL, nil)
	downloadOpts.Headers = internalhttp.MergeHeaders(m.prov.videoReqHeaders, opts.Headers)

	statusURL := strings.TrimRight(m.prov.videoBaseURL, "/") + "/v2/query/video_generation/" + providerutils.EncodePathSegment(op.TaskID)

	var statusResp minimaxStatusResponse
	result, err := fileutil.PollJSON(ctx, statusURL, downloadOpts, &statusResp)
	if err != nil {
		return nil, nil, m.handlePollError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   flattenHeaders(result.Headers),
	}

	task := statusResp.Task
	switch task.Status {
	case "succeeded":
		if task.Content == nil || task.Content.URL == "" {
			return nil, nil, NewVideoGenerationError(fmt.Sprintf("MiniMax video generation completed but no video URL was returned. Task ID: %s", op.TaskID))
		}

		meta := map[string]interface{}{
			"taskId":   op.TaskID,
			"videoUrl": task.Content.URL,
			"resolvedInputs": map[string]interface{}{
				"imageCount":            op.ResolvedInputs.ImageCount,
				"referenceVideoIndices": op.ResolvedInputs.ReferenceVideoIndices,
			},
		}
		if task.Duration != nil {
			meta["duration"] = *task.Duration
		}
		if task.Ratio != nil {
			meta["ratio"] = *task.Ratio
		}
		if task.Resolution != nil {
			meta["resolution"] = *task.Resolution
		}
		if task.Usage != nil {
			meta["usage"] = map[string]interface{}{
				"totalSeconds":  task.Usage.TotalSeconds,
				"inputSeconds":  task.Usage.InputSeconds,
				"outputSeconds": task.Usage.OutputSeconds,
			}
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status: provider.VideoOperationStatusCompleted,
			Videos: []provider.VideoModelV3VideoData{
				{Type: "url", URL: task.Content.URL, MediaType: "video/mp4"},
			},
			ProviderMetadata: map[string]interface{}{"minimax": meta},
			Response:         responseInfo,
		}, nil, nil

	case "failed":
		msg := "MiniMax video generation failed"
		if task.Error != nil && task.Error.Message != "" {
			msg += ": " + task.Error.Message
		}
		if task.Error != nil && task.Error.Code != nil {
			msg += fmt.Sprintf(" (%v)", task.Error.Code)
		}
		msg += fmt.Sprintf(". Task ID: %s", op.TaskID)
		return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Response: responseInfo,
			},
			NewVideoGenerationFailedError(msg), nil

	case "cancelled":
		return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Response: responseInfo,
			},
			NewVideoGenerationCancelledError(fmt.Sprintf("MiniMax video generation was cancelled. Task ID: %s", op.TaskID)), nil

	case "expired":
		return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Response: responseInfo,
			},
			NewVideoGenerationExpiredError(fmt.Sprintf("MiniMax video generation request expired. Task ID: %s", op.TaskID)), nil

	// 'queued' | 'preparing' | 'processing' | unknown → keep polling.
	default:
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusPending,
			Response: responseInfo,
		}, nil, nil
	}
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes (TS MiniMaxVideoModel#doGenerate).
// After completion, providerMetadata.minimax.resolvedInputs is re-derived
// from the reference-video indices recorded at DoStart time, resolving each
// index back to its original input URL (TS doGenerate's
// resolvedInputs.referenceVideoUrls derivation) — DoStatus alone (as called
// by the SDK's own async polling flow) reports referenceVideoIndices
// instead, since it has no access to the original call options.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	var op minimaxOperation
	_ = json.Unmarshal(startResult.Operation, &op)

	mmOpts, _ := extractVideoProviderOptions(opts.ProviderOptions)
	pollIntervalMs := minimaxDefaultPollInterval
	pollTimeoutMs := minimaxDefaultPollTimeout
	if mmOpts.PollIntervalMs != nil {
		pollIntervalMs = *mmOpts.PollIntervalMs
	}
	if mmOpts.PollTimeoutMs != nil {
		pollTimeoutMs = *mmOpts.PollTimeoutMs
	}

	var finalStatus *provider.VideoModelV3OperationStatusResult
	var jobFailureErr error

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, statusErr, err := m.getStatus(ctx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusCompleted:
			finalStatus = status
			return &polling.JobResult{Status: polling.JobStatusCompleted}, nil
		case provider.VideoOperationStatusError:
			// TS doGenerate throws the typed AISDKError from getStatus
			// directly (`throw result.error`), preserving its .name.
			jobFailureErr = statusErr
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: statusErr.Error()}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	pollOpts := polling.PollOptions{PollIntervalMs: pollIntervalMs, PollTimeoutMs: pollTimeoutMs}
	_, pollErr := polling.PollForCompletion(ctx, checker, pollOpts)
	if pollErr != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("video generation aborted: %w", ctx.Err())
		}
		if jobFailureErr != nil {
			return nil, jobFailureErr
		}
		if strings.Contains(pollErr.Error(), "polling timeout") || strings.Contains(pollErr.Error(), "max polling attempts") {
			return nil, NewTimeoutError(fmt.Sprintf("MiniMax video generation timed out after %dms. Task ID: %s", pollTimeoutMs, op.TaskID))
		}
		return nil, pollErr
	}

	providerMetadata := finalStatus.ProviderMetadata
	if mmMeta, ok := providerMetadata["minimax"].(map[string]interface{}); ok {
		var referenceVideoURLs []string
		for _, idx := range op.ResolvedInputs.ReferenceVideoIndices {
			if idx >= 0 && idx < len(opts.InputReferences) {
				file := opts.InputReferences[idx]
				if file.Type == "url" {
					referenceVideoURLs = append(referenceVideoURLs, file.URL)
				}
			}
		}
		resolved := map[string]interface{}{"imageCount": op.ResolvedInputs.ImageCount}
		if referenceVideoURLs != nil {
			resolved["referenceVideoUrls"] = referenceVideoURLs
		}
		enriched := map[string]interface{}{}
		for k, v := range mmMeta {
			enriched[k] = v
		}
		enriched["resolvedInputs"] = resolved
		providerMetadata = map[string]interface{}{"minimax": enriched}
	}

	return &provider.VideoModelV3Response{
		Videos:           finalStatus.Videos,
		Warnings:         append(append([]types.Warning{}, startResult.Warnings...), finalStatus.Warnings...),
		ProviderMetadata: providerMetadata,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: startResult.Response.Timestamp,
			ModelID:   finalStatus.Response.ModelID,
			Headers:   finalStatus.Response.Headers,
		},
	}, nil
}

// buildRequestBody builds the /v2/video_generation request body from call
// options and typed provider options (TS MiniMaxVideoModel#getArgs).
func (m *VideoModel) buildRequestBody(opts *provider.VideoModelV3CallOptions, mmOpts *VideoModelOptions) (map[string]interface{}, []types.Warning, minimaxResolvedInputs, error) {
	warnings := []types.Warning{}

	if opts.FPS != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "fps", Details: fmt.Sprintf("%s does not support a custom frame rate.", m.modelID)})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed", Details: fmt.Sprintf("%s does not support a seed.", m.modelID)})
	}
	if opts.N > 1 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "n", Details: fmt.Sprintf("%s generates a single video per call. Only 1 video will be generated.", m.modelID)})
	}
	if opts.GenerateAudio != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "generateAudio", Details: fmt.Sprintf("The %s API does not expose an audio parameter. The generateAudio option was ignored.", m.modelID)})
	}

	resSetting, hasResSetting := minimaxModelResolutionSettings[m.modelID]
	supportedResolutions := []string{"480P", "768P", "2K"}
	if hasResSetting {
		supportedResolutions = resSetting.supported
	}
	supportedResSet := map[string]bool{}
	quoted := make([]string, len(supportedResolutions))
	for i, r := range supportedResolutions {
		supportedResSet[r] = true
		quoted[i] = fmt.Sprintf("%q", r)
	}
	supportedResNames := strings.Join(quoted, " or ")

	var resolution string
	if mmOpts.Resolution != nil {
		resolution = *mmOpts.Resolution
	}
	if resolution != "" && !supportedResSet[resolution] {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "resolution",
			Details: fmt.Sprintf("%s supports %s. The provider resolution %q was ignored.", m.modelID, supportedResNames, resolution),
		})
		resolution = ""
	}

	if opts.Resolution != "" {
		mapped, mappedOK := resolveTopLevelResolution(opts.Resolution)
		switch {
		case resolution != "":
			if !mappedOK {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "resolution",
					Details: fmt.Sprintf("Unrecognized resolution %q. %s supports %s, so providerOptions.minimax.resolution (%q) was used instead.", opts.Resolution, m.modelID, supportedResNames, resolution),
				})
			} else if mapped != resolution {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "resolution",
					Details: fmt.Sprintf("The resolution %q selects %s, but providerOptions.minimax.resolution (%q) was used instead.", opts.Resolution, mapped, resolution),
				})
			}
		case mappedOK && supportedResSet[mapped]:
			resolution = mapped
		default:
			details := fmt.Sprintf("Unrecognized resolution %q. %s supports %s.", opts.Resolution, m.modelID, supportedResNames)
			if mappedOK {
				details = fmt.Sprintf("%s does not support the resolution %q. It supports %s.", m.modelID, mapped, supportedResNames)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "resolution", Details: details})
		}
	}
	if resolution == "" {
		if hasResSetting {
			resolution = resSetting.def
		} else {
			resolution = minimaxDefaultResolution
		}
	}

	content := []map[string]interface{}{
		{"type": "text", "text": opts.Prompt},
	}

	sentImageCount := 0
	var sentReferenceVideoIndices []int

	firstFrameImage := minimaxGetFirstFrame(opts)
	firstFrame := firstFrameImage
	if firstFrame == nil {
		firstFrame = opts.Image
	}
	lastFrame := minimaxGetLastFrame(opts)

	if firstFrame != nil {
		if mt := minimaxNonImageFrameMediaType(firstFrame); mt != "" {
			feature := "image"
			if firstFrameImage != nil {
				feature = "frameImages"
			}
			details := fmt.Sprintf("%s only accepts an image as a frame image; the %q file was ignored.", m.modelID, firstFrame.MediaType)
			if mt == "video" {
				details = fmt.Sprintf("%s does not accept a video as a frame image. The video was ignored.", m.modelID)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature, Details: details})
			firstFrame = nil
		}
	}

	if lastFrame != nil {
		if firstFrame == nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "frameImages",
				Details: fmt.Sprintf("%s requires a first_frame when a last_frame is provided. The last_frame was ignored.", m.modelID),
			})
			lastFrame = nil
		} else if mt := minimaxNonImageFrameMediaType(lastFrame); mt != "" {
			details := fmt.Sprintf("%s only accepts an image as a frame image; the %q last_frame was ignored.", m.modelID, lastFrame.MediaType)
			if mt == "video" {
				details = fmt.Sprintf("%s does not accept a video as a frame image. The last_frame video was ignored.", m.modelID)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frameImages", Details: details})
			lastFrame = nil
		}
	}

	usesFrameImages := firstFrame != nil || lastFrame != nil

	referenceFiles := opts.InputReferences
	var referenceAudioURLs []string
	if mmOpts.ReferenceAudioUrls != nil {
		referenceAudioURLs = mmOpts.ReferenceAudioUrls
	}
	supportsReferences := m.modelID != ModelH3Max

	if !supportsReferences && len(referenceFiles) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "inputReferences",
			Details: "MiniMax-H3-Max does not support reference-to-video inputs. The references were ignored.",
		})
	}
	if !supportsReferences && len(referenceAudioURLs) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "referenceAudioUrls",
			Details: "MiniMax-H3-Max does not support reference audio. The audio was ignored.",
		})
	}

	usesReferences := supportsReferences && (len(referenceFiles) > 0 || len(referenceAudioURLs) > 0)

	if usesFrameImages {
		if firstFrame != nil {
			encoded, err := minimaxEncodeFile(firstFrame)
			if err != nil {
				return nil, nil, minimaxResolvedInputs{}, err
			}
			content = append(content, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": encoded},
				"role":      "first_frame",
			})
			sentImageCount++
		}
		if lastFrame != nil {
			encoded, err := minimaxEncodeFile(lastFrame)
			if err != nil {
				return nil, nil, minimaxResolvedInputs{}, err
			}
			content = append(content, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": encoded},
				"role":      "last_frame",
			})
			sentImageCount++
		}
		if usesReferences {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "inputReferences",
				Details: "MiniMax-H3 cannot combine frame images with reference inputs. The references were ignored.",
			})
		}
	} else if usesReferences {
		var referenceImages []provider.VideoModelV3File
		type refVideo struct {
			file  provider.VideoModelV3File
			index int
		}
		var referenceVideos []refVideo

		for idx, file := range referenceFiles {
			topLevel := ""
			if file.MediaType != "" {
				topLevel = minimaxTopLevelMediaType(file.MediaType)
			}
			switch topLevel {
			case "video":
				referenceVideos = append(referenceVideos, refVideo{file: file, index: idx})
			case "image":
				referenceImages = append(referenceImages, file)
			case "":
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "inputReferences",
					Details: "MiniMax-H3 requires an explicit mediaType to route URL references as video or image. Pass { data: url, mediaType: \"video/mp4\" } for video references. The reference was treated as an image.",
				})
				referenceImages = append(referenceImages, file)
			default:
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "inputReferences",
					Details: fmt.Sprintf("MiniMax-H3 only accepts image and video references; the %q reference was ignored. Pass reference audio via providerOptions.minimax.referenceAudioUrls.", file.MediaType),
				})
			}
		}

		imgLimit := len(referenceImages)
		if imgLimit > minimaxMaxReferenceImages {
			imgLimit = minimaxMaxReferenceImages
		}
		for i := 0; i < imgLimit; i++ {
			encoded, err := minimaxEncodeFile(&referenceImages[i])
			if err != nil {
				return nil, nil, minimaxResolvedInputs{}, err
			}
			content = append(content, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": encoded},
				"role":      "reference_image",
			})
			sentImageCount++
		}
		if len(referenceImages) > minimaxMaxReferenceImages {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "inputReferences",
				Details: fmt.Sprintf("MiniMax-H3 accepts at most %d reference images. Extra images were ignored.", minimaxMaxReferenceImages),
			})
		}

		vidLimit := len(referenceVideos)
		if vidLimit > minimaxMaxReferenceVideos {
			vidLimit = minimaxMaxReferenceVideos
		}
		for i := 0; i < vidLimit; i++ {
			rv := referenceVideos[i]
			encoded, err := minimaxEncodeFile(&rv.file)
			if err != nil {
				return nil, nil, minimaxResolvedInputs{}, err
			}
			content = append(content, map[string]interface{}{
				"type":      "video_url",
				"video_url": map[string]interface{}{"url": encoded},
				"role":      "reference_video",
			})
			// Retain input positions, not request URLs or inline data, in
			// the operation.
			if rv.file.Type == "url" {
				sentReferenceVideoIndices = append(sentReferenceVideoIndices, rv.index)
			}
		}
		if len(referenceVideos) > minimaxMaxReferenceVideos {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "inputReferences",
				Details: fmt.Sprintf("MiniMax-H3 accepts at most %d reference videos. Extra videos were ignored.", minimaxMaxReferenceVideos),
			})
		}

		if len(referenceAudioURLs) > 0 {
			if len(referenceImages) == 0 && len(referenceVideos) == 0 {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "referenceAudioUrls",
					Details: "MiniMax-H3 reference audio must be paired with at least one reference image or video. The audio was ignored.",
				})
			} else {
				audioLimit := len(referenceAudioURLs)
				if audioLimit > minimaxMaxReferenceAudios {
					audioLimit = minimaxMaxReferenceAudios
				}
				for i := 0; i < audioLimit; i++ {
					content = append(content, map[string]interface{}{
						"type":      "audio_url",
						"audio_url": map[string]interface{}{"url": referenceAudioURLs[i]},
						"role":      "reference_audio",
					})
				}
				if len(referenceAudioURLs) > minimaxMaxReferenceAudios {
					warnings = append(warnings, types.Warning{
						Type:    "unsupported",
						Feature: "referenceAudioUrls",
						Details: fmt.Sprintf("%s accepts at most %d reference audios. Extra audios were ignored.", m.modelID, minimaxMaxReferenceAudios),
					})
				}
			}
		}
	}

	isTextToVideo := len(content) == 1
	var ratio string
	if mmOpts.Ratio != nil {
		ratio = *mmOpts.Ratio
	}
	if ratio == "" && opts.AspectRatio != "" {
		if minimaxVideoRatios[opts.AspectRatio] {
			ratio = opts.AspectRatio
		} else {
			details := fmt.Sprintf("%s does not support the aspect ratio %q. Using the provider default (adaptive).", m.modelID, opts.AspectRatio)
			if isTextToVideo {
				details = fmt.Sprintf("%s does not support the aspect ratio %q. Using the default (%s).", m.modelID, opts.AspectRatio, minimaxDefaultAspectRatio)
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "aspectRatio", Details: details})
			if isTextToVideo {
				ratio = minimaxDefaultAspectRatio
			}
		}
	}
	if ratio == "adaptive" && isTextToVideo {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: fmt.Sprintf("%s text-to-video does not support the adaptive aspect ratio. Using the default (%s).", m.modelID, minimaxDefaultAspectRatio),
		})
		ratio = minimaxDefaultAspectRatio
	}
	if usesFrameImages && ratio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: fmt.Sprintf("%s derives the aspect ratio from the frame image; the requested ratio was ignored.", m.modelID),
		})
		ratio = ""
	}
	if ratio == "" && isTextToVideo {
		ratio = minimaxDefaultAspectRatio
	}

	minDurationSeconds := 5
	if m.modelID == ModelH3 {
		minDurationSeconds = 4
	}
	duration := minimaxDefaultDuration
	if opts.Duration != nil {
		raw := *opts.Duration
		rounded := math.Round(raw)
		if rounded != raw {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "duration",
				Details: fmt.Sprintf("%s requires a whole number of seconds. The requested duration of %v was rounded to %v.", m.modelID, raw, rounded),
			})
		}
		d := int(rounded)
		if d > minimaxMaxDuration {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "duration",
				Details: fmt.Sprintf("%s supports at most %d seconds. The requested duration of %v was clamped to %d.", m.modelID, minimaxMaxDuration, raw, minimaxMaxDuration),
			})
			d = minimaxMaxDuration
		} else if d < minDurationSeconds {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "duration",
				Details: fmt.Sprintf("%s requires at least %d seconds. The requested duration of %v was clamped to %d.", m.modelID, minDurationSeconds, raw, minDurationSeconds),
			})
			d = minDurationSeconds
		}
		duration = d
	}

	body := map[string]interface{}{
		"model":      m.modelID,
		"content":    content,
		"resolution": resolution,
		"duration":   duration,
	}
	if ratio != "" {
		body["ratio"] = ratio
	}
	if mmOpts.AigcWatermark != nil {
		body["aigc_watermark"] = *mmOpts.AigcWatermark
	}

	return body, warnings, minimaxResolvedInputs{ImageCount: sentImageCount, ReferenceVideoIndices: sentReferenceVideoIndices}, nil
}

// minimaxGetFirstFrame returns the frameImages first_frame entry, if any.
func minimaxGetFirstFrame(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// minimaxGetLastFrame returns the frameImages last_frame entry, if any.
func minimaxGetLastFrame(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// minimaxTopLevelMediaType returns the part of a MIME type before the "/",
// or the whole string when there is none.
func minimaxTopLevelMediaType(mediaType string) string {
	if idx := strings.Index(mediaType, "/"); idx >= 0 {
		return mediaType[:idx]
	}
	return mediaType
}

// minimaxNonImageFrameMediaType returns the offending top-level media type
// when a frame is something other than an image, or "" when it is an image
// or carries no media type (a URL input the core emits without one).
func minimaxNonImageFrameMediaType(f *provider.VideoModelV3File) string {
	if f.MediaType == "" {
		return ""
	}
	top := minimaxTopLevelMediaType(f.MediaType)
	if top == "image" {
		return ""
	}
	return top
}

// resolveTopLevelResolution normalizes a top-level Resolution call option
// (a WxH pixel size, or a named tier itself) to a MiniMax resolution tier,
// returning ("", false) when it cannot be resolved (TS
// resolveTopLevelResolution).
func resolveTopLevelResolution(resolution string) (string, bool) {
	named := strings.ToUpper(resolution)
	if minimaxVideoResolutions[named] {
		return named, true
	}
	if mapped, ok := minimaxResolutionMap[resolution]; ok {
		return mapped, true
	}
	return "", false
}

// minimaxEncodeFile encodes a VideoModelV3File to the string form MiniMax
// expects: a URL is passed through unchanged, and inline data is encoded as
// a data URI (TS convertImageModelFileToDataUri).
func minimaxEncodeFile(f *provider.VideoModelV3File) (string, error) {
	if f.Type == "url" {
		return f.URL, nil
	}
	if f.Type == "file" {
		return imageutil.ConvertToDataURI(f.Data, f.MediaType), nil
	}
	return "", fmt.Errorf("minimax: unsupported file type: %s", f.Type)
}

// parseAPIError parses a MiniMax API error response (TS
// minimaxVideoFailedResponseHandler / minimaxVideoErrorSchema).
func (m *VideoModel) parseAPIError(body []byte, statusCode int) error {
	var errResp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != nil && errResp.Error.Message != "" {
		return NewAPIError(statusCode, errResp.Error.Message)
	}
	return NewAPIError(statusCode, string(body))
}

// handlePollError converts a fileutil poll error (a providererrors.DownloadError
// for a non-2xx HTTP response, or a validation/network error) into a
// MiniMax API error where possible.
func (m *VideoModel) handlePollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return m.parseAPIError(dlErr.Body, dlErr.StatusCode)
	}
	return NewVideoGenerationError(fmt.Sprintf("failed to check status: %v", err))
}

// flattenHeaders flattens a map[string][]string (net/http.Header or a
// fileutil.DownloadResult's Headers) into the map[string]string expected by
// VideoModelV3ResponseInfo.Headers, taking the first value for each key.
func flattenHeaders(headers map[string][]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// minimaxStatusResponse is the response from the task status endpoint (TS
// minimaxVideoStatusResponseSchema).
type minimaxStatusResponse struct {
	Task struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Content *struct {
			URL string `json:"url"`
		} `json:"content"`
		Resolution *string  `json:"resolution"`
		Duration   *float64 `json:"duration"`
		Ratio      *string  `json:"ratio"`
		Usage      *struct {
			TotalSeconds  *float64 `json:"total_seconds"`
			InputSeconds  *float64 `json:"input_seconds"`
			OutputSeconds *float64 `json:"output_seconds"`
		} `json:"usage"`
		Error *struct {
			Code    interface{} `json:"code"`
			Message string      `json:"message"`
		} `json:"error"`
	} `json:"task"`
}
