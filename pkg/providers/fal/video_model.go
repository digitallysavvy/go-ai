package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Fal.ai.
//
// It ports @ai-sdk/fal's FalVideoModel (fal-video-model.ts), which only
// implements doStart/doStatus (no synchronous doGenerate in TS). DoGenerate
// here is the Go equivalent of the default start+poll behavior TS core's
// generateVideo would drive, built entirely on top of DoStart/DoStatus.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Fal.ai video generation model
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{
		prov:    prov,
		modelID: modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "fal"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns nil (FAL generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// HandleWebhookOption implements provider.VideoModelWebhookHandler,
// forwarding the caller's webhook factory straight through (TS
// FalVideoModel#handleWebhookOption): fal.ai's queue webhook fires exactly
// once at completion, so no protocol-aware filtering is needed.
func (m *VideoModel) HandleWebhookOption(ctx context.Context, factory provider.VideoWebhookFactory) (string, provider.VideoWebhookReceived, error) {
	return factory(ctx)
}

// normalizedModelID strips the "fal-ai/" and "fal/" prefixes (in that
// order), matching TS FalVideoModel#normalizedModelId. The queue submission
// always targets "fal-ai/{normalizedModelId}" regardless of the caller's
// original prefix.
func (m *VideoModel) normalizedModelID() string {
	id := strings.TrimPrefix(m.modelID, "fal-ai/")
	id = strings.TrimPrefix(id, "fal/")
	return id
}

// falVideoOperation is the opaque operation reference returned by DoStart
// and passed back into DoStatus (TS operation: { responseUrl, submitUrl }).
type falVideoOperation struct {
	ResponseURL string `json:"responseUrl"`
	SubmitURL   string `json:"submitUrl"`
}

// falVideoHandledOptionKeys are the FalVideoModelOptions fields mapped to a
// specific wire key below; every other key is passed through as-is (TS
// buildRequestBody's Object.entries loop).
var falVideoHandledOptionKeys = map[string]bool{
	"loop": true, "motionStrength": true, "resolution": true,
	"negativePrompt": true, "promptOptimizer": true,
}

// buildStartRequestBody builds the queue submission body (TS
// FalVideoModel#buildRequestBody).
func (m *VideoModel) buildStartRequestBody(opts *provider.VideoModelV3CallOptions) (map[string]interface{}, error) {
	body := map[string]interface{}{}

	if opts.PromptSet || opts.Prompt != "" {
		body["prompt"] = opts.Prompt
	}

	if opts.Image != nil {
		if opts.Image.Type == "url" {
			body["image_url"] = opts.Image.URL
		} else {
			body["image_url"] = imageutil.ConvertToDataURI(opts.Image.Data, opts.Image.MediaType)
		}
	}

	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}

	// TS: `if (options.duration)` -- falsy (including 0) is skipped.
	if opts.Duration != nil && *opts.Duration != 0 {
		body["duration"] = strconv.FormatFloat(*opts.Duration, 'f', -1, 64) + "s"
	}

	// TS: `if (options.seed)` -- falsy (including 0) is skipped.
	if opts.Seed != nil && *opts.Seed != 0 {
		body["seed"] = *opts.Seed
	}

	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["fal"]; ok {
			falOpts, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("fal: invalid providerOptions.fal: expected object")
			}
			if v, ok := falOpts["loop"]; ok && v != nil {
				body["loop"] = v
			}
			if v, ok := falOpts["motionStrength"]; ok && v != nil {
				body["motion_strength"] = v
			}
			if v, ok := falOpts["resolution"]; ok && v != nil {
				body["resolution"] = v
			}
			if v, ok := falOpts["negativePrompt"]; ok && v != nil {
				body["negative_prompt"] = v
			}
			if v, ok := falOpts["promptOptimizer"]; ok && v != nil {
				body["prompt_optimizer"] = v
			}
			for k, v := range falOpts {
				if !falVideoHandledOptionKeys[k] {
					body[k] = v
				}
			}
		}
	}

	return body, nil
}

// DoStart starts an asynchronous video generation via the fal.ai queue and
// returns an opaque operation reference (TS FalVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := time.Now()
	callOpts := &opts.VideoModelV3CallOptions

	body, err := m.buildStartRequestBody(callOpts)
	if err != nil {
		return nil, err
	}

	queuePath := fmt.Sprintf("%s/fal-ai/%s", m.prov.queueHost, m.normalizedModelID())
	submitURL := queuePath
	if opts.WebhookURL != "" {
		submitURL = queuePath + "?fal_webhook=" + url.QueryEscape(opts.WebhookURL)
	}

	var queueResp falVideoQueueResponse
	httpResp, err := m.prov.absClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    submitURL,
		Body:    body,
		Headers: callOpts.Headers,
	}, &queueResp)
	if err != nil {
		return nil, m.handleError(err)
	}

	if queueResp.ResponseURL == "" {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "No response URL returned from queue endpoint", nil)
	}

	operation, _ := json.Marshal(falVideoOperation{ResponseURL: queueResp.ResponseURL, SubmitURL: queuePath})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  []types.Warning{},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   convertHTTPHeaders(httpResp.Headers),
		},
	}, nil
}

// falStillInProgressDetail is the special "not an error" body the fal.ai
// queue API returns while a job is still processing (TS doStatus's
// `body.detail === 'Request is still in progress'` check).
const falStillInProgressDetail = "Request is still in progress"

// DoStatus checks the status of an asynchronous video generation started
// with DoStart, fetching the queue's response_url (TS
// FalVideoModel#doStatus). The status URL is fetched through fileutil's
// validated-redirect poller: hops same-origin with the submit URL skip the
// generic SSRF blocklist (matching TS's `credentialedOrigin`/
// `trustedOrigin: submitUrl`), while every other origin is fully validated.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := time.Now()

	var op falVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("fal: invalid operation reference: %w", err)
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(op.SubmitURL, nil)
	downloadOpts.Headers = opts.Headers

	var statusResp falVideoStatusResponse
	result, err := fileutil.PollJSON(ctx, op.ResponseURL, downloadOpts, &statusResp)
	if err != nil {
		var dlErr *providererrors.DownloadError
		if errors.As(err, &dlErr) && dlErr.Body != nil {
			var detail struct {
				Detail string `json:"detail"`
			}
			if jsonErr := json.Unmarshal(dlErr.Body, &detail); jsonErr == nil && detail.Detail == falStillInProgressDetail {
				return &provider.VideoModelV3OperationStatusResult{
					Status: provider.VideoOperationStatusPending,
					Response: provider.VideoModelV3ResponseInfo{
						Timestamp: currentDate,
						ModelID:   m.modelID,
						Headers:   map[string]string{},
					},
				}, nil
			}
			return &provider.VideoModelV3OperationStatusResult{
				Status: provider.VideoOperationStatusError,
				Error:  parseFalErrorMessage(dlErr.Body, dlErr.Message),
				Response: provider.VideoModelV3ResponseInfo{
					Timestamp: currentDate,
					ModelID:   m.modelID,
					Headers:   flattenFalHeaders(dlErr.Headers),
				},
			}, nil
		}
		return nil, err
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   flattenFalHeaders(result.Headers),
	}

	if statusResp.Video == nil || statusResp.Video.URL == "" {
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "No video URL in response", nil)
	}

	mediaType := statusResp.Video.ContentType
	if mediaType == "" {
		mediaType = "video/mp4"
	}

	falMeta := map[string]interface{}{
		"videos": []map[string]interface{}{
			{
				"url":         statusResp.Video.URL,
				"width":       statusResp.Video.Width,
				"height":      statusResp.Video.Height,
				"duration":    statusResp.Video.Duration,
				"fps":         statusResp.Video.FPS,
				"contentType": statusResp.Video.ContentType,
			},
		},
	}
	if statusResp.Seed != nil {
		falMeta["seed"] = *statusResp.Seed
	}
	if statusResp.Timings != nil {
		falMeta["timings"] = statusResp.Timings
	}
	if statusResp.HasNSFWConcepts != nil {
		falMeta["has_nsfw_concepts"] = statusResp.HasNSFWConcepts
	}
	if statusResp.Prompt != "" {
		falMeta["prompt"] = statusResp.Prompt
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status: provider.VideoOperationStatusCompleted,
		Videos: []provider.VideoModelV3VideoData{
			{Type: "url", URL: statusResp.Video.URL, MediaType: mediaType},
		},
		Warnings:         []types.Warning{},
		ProviderMetadata: map[string]interface{}{"fal": falMeta},
		Response:         responseInfo,
	}, nil
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where FalVideoModel implements only doStart/
// doStatus and the core generate-video flow polls it directly), so this
// method is the Go equivalent of that default polling behavior, built
// entirely on top of DoStart/DoStatus.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	pollOpts := m.getPollOptions(opts.ProviderOptions)

	var finalStatus *provider.VideoModelV3OperationStatusResult
	var jobFailureErr error

	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{
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
			jobFailureErr = providererrors.NewVideoGenerationError("fal", m.modelID, status.Error, nil)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: status.Error}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	_, pollErr := polling.PollForCompletion(ctx, checker, pollOpts)
	if pollErr != nil {
		if jobFailureErr != nil {
			return nil, jobFailureErr
		}
		return nil, providererrors.NewVideoGenerationError("fal", m.modelID, "polling failed", pollErr)
	}

	return &provider.VideoModelV3Response{
		Videos:           finalStatus.Videos,
		Warnings:         append(append([]types.Warning{}, startResult.Warnings...), finalStatus.Warnings...),
		ProviderMetadata: finalStatus.ProviderMetadata,
		Response:         finalStatus.Response,
	}, nil
}

// getPollOptions extracts polling options from provider options
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	opts := polling.DefaultPollOptions()

	if providerOpts != nil {
		if falOpts, ok := providerOpts["fal"].(map[string]interface{}); ok {
			if interval, ok := falOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := falOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

func (m *VideoModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message := parseFalErrorMessage(statusErr.Body, string(statusErr.Body))
		providerErr := providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, "", message, err)
		providerErr.ResponseHeaders = flattenFalHTTPHeaders(statusErr.Headers)
		return providerErr
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// convertHTTPHeaders flattens net/http.Header (map[string][]string) into the
// map[string]string expected by VideoModelV3ResponseInfo.Headers, taking the
// first value for each header key.
func convertHTTPHeaders(h http.Header) map[string]string {
	return flattenFalHTTPHeaders(h)
}

func flattenFalHTTPHeaders(h map[string][]string) map[string]string {
	if len(h) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

// flattenFalHeaders flattens a fileutil.DownloadResult's headers
// (map[string][]string) the same way flattenFalHTTPHeaders does for
// net/http.Header.
func flattenFalHeaders(h map[string][]string) map[string]string {
	return flattenFalHTTPHeaders(h)
}

// falVideoQueueResponse is the response from the queue submission endpoint
// (TS falJobResponseSchema).
type falVideoQueueResponse struct {
	RequestID   string `json:"request_id,omitempty"`
	ResponseURL string `json:"response_url,omitempty"`
}

// falVideoStatusResponse mirrors TS falVideoResponseSchema.
type falVideoStatusResponse struct {
	Video *struct {
		URL         string   `json:"url"`
		Width       *int     `json:"width,omitempty"`
		Height      *int     `json:"height,omitempty"`
		Duration    *float64 `json:"duration,omitempty"`
		FPS         *float64 `json:"fps,omitempty"`
		ContentType string   `json:"content_type,omitempty"`
	} `json:"video,omitempty"`
	Seed    *int `json:"seed,omitempty"`
	Timings *struct {
		Inference *float64 `json:"inference,omitempty"`
	} `json:"timings,omitempty"`
	HasNSFWConcepts []bool `json:"has_nsfw_concepts,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
}
