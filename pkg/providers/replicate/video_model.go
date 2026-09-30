package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// VideoModel implements the provider.VideoModelV3 interface for Replicate.
// It ports @ai-sdk/replicate's ReplicateVideoModel (replicate-video-model.ts),
// which only implements doStart/doStatus (no synchronous doGenerate in TS).
// DoGenerate here is the Go equivalent of the default start+poll behavior TS
// core's generateVideo would drive, built entirely on top of DoStart/DoStatus.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Replicate video generation model
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
	return "replicate"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns 1 (Replicate video models support 1 video at a
// time).
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// HandleWebhookOption implements provider.VideoModelWebhookHandler,
// forwarding the caller's webhook factory straight through (TS
// ReplicateVideoModel#handleWebhookOption): Replicate's own webhook fires
// exactly once at completion, so no protocol-aware filtering is needed.
func (m *VideoModel) HandleWebhookOption(ctx context.Context, factory provider.VideoWebhookFactory) (string, provider.VideoWebhookReceived, error) {
	return factory(ctx)
}

// ReplicateVideoModelOptions mirrors TS ReplicateVideoModelOptions
// (replicate-video-model-options.ts).
var replicateVideoHandledOptionKeys = map[string]bool{
	"pollIntervalMs": true, "pollTimeoutMs": true, "maxWaitTimeInSeconds": true,
	"guidance_scale": true, "num_inference_steps": true, "motion_bucket_id": true,
	"cond_aug": true, "decoding_t": true, "video_length": true,
	"sizing_strategy": true, "frames_per_second": true, "prompt_optimizer": true,
}

// buildInput builds the Replicate prediction `input` object (TS
// ReplicateVideoModel#buildInput).
func (m *VideoModel) buildInput(opts *provider.VideoModelV3CallOptions) (map[string]interface{}, error) {
	input := map[string]interface{}{}

	if opts.PromptSet || opts.Prompt != "" {
		input["prompt"] = opts.Prompt
	}

	if opts.Image != nil {
		if opts.Image.Type == "url" {
			input["image"] = opts.Image.URL
		} else {
			input["image"] = imageutil.ConvertToDataURI(opts.Image.Data, opts.Image.MediaType)
		}
	}

	if opts.AspectRatio != "" {
		input["aspect_ratio"] = opts.AspectRatio
	}

	if opts.Resolution != "" {
		input["size"] = opts.Resolution
	}

	if opts.Duration != nil && *opts.Duration != 0 {
		input["duration"] = *opts.Duration
	}

	if opts.FPS != nil && *opts.FPS != 0 {
		input["fps"] = *opts.FPS
	}

	if opts.Seed != nil && *opts.Seed != 0 {
		input["seed"] = *opts.Seed
	}

	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["replicate"]; ok {
			repOpts, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("replicate: invalid providerOptions.replicate: expected object")
			}
			for _, key := range []string{
				"guidance_scale", "num_inference_steps", "motion_bucket_id",
				"cond_aug", "decoding_t", "video_length", "sizing_strategy",
				"frames_per_second", "prompt_optimizer",
			} {
				if v, ok := repOpts[key]; ok && v != nil {
					input[key] = v
				}
			}
			for k, v := range repOpts {
				if !replicateVideoHandledOptionKeys[k] {
					input[k] = v
				}
			}
		}
	}

	return input, nil
}

// replicateVideoOperation is the opaque operation reference returned by
// DoStart and passed back into DoStatus.
type replicateVideoOperation struct {
	GetURL string `json:"getUrl"`
}

// DoStart starts an asynchronous video generation via a Replicate prediction
// and returns an opaque operation reference (TS
// ReplicateVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := time.Now()
	callOpts := &opts.VideoModelV3CallOptions

	input, err := m.buildInput(callOpts)
	if err != nil {
		return nil, err
	}

	modelID, version, hasVersion := strings.Cut(m.modelID, ":")

	predictionPath := fmt.Sprintf("/models/%s/predictions", modelID)
	if hasVersion {
		predictionPath = "/predictions"
	}

	body := map[string]interface{}{"input": input}
	if hasVersion {
		body["version"] = version
	}
	if opts.WebhookURL != "" {
		body["webhook"] = opts.WebhookURL
		body["webhook_events_filter"] = []string{"completed"}
	}

	var prediction replicatePredictionWire
	httpResp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    predictionPath,
		Body:    body,
		Headers: callOpts.Headers,
	}, &prediction)
	if err != nil {
		return nil, m.handleError(err)
	}

	op, _ := json.Marshal(replicateVideoOperation{GetURL: prediction.URLs.Get})

	return &provider.VideoModelV3OperationStartResult{
		Operation: op,
		Warnings:  []types.Warning{},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   convertReplicateHeaders(httpResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via the prediction's `urls.get` endpoint (TS
// ReplicateVideoModel#doStatus). The status URL is fetched through
// fileutil's validated-redirect poller: hops same-origin with the
// provider's configured base URL skip the generic SSRF blocklist, matching
// TS's `credentialedOrigin`/`trustedOrigin: this.config.baseURL`.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := time.Now()

	var op replicateVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("replicate: invalid operation reference: %w", err)
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(m.prov.config.BaseURL, m.prov.client.HTTPClient().Transport)
	downloadOpts.Headers = m.mergeHeaders(opts.Headers)

	var prediction replicatePredictionWire
	result, err := fileutil.PollJSON(ctx, op.GetURL, downloadOpts, &prediction)
	if err != nil {
		return nil, m.handlePollError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   flattenReplicateHeaders(result.Headers),
	}

	switch prediction.Status {
	case "failed":
		errMsg := "Unknown error"
		if prediction.Error != nil {
			errMsg = *prediction.Error
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    fmt.Sprintf("Video generation failed: %s", errMsg),
			Response: responseInfo,
		}, nil

	case "canceled":
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    "Video generation was canceled",
			Response: responseInfo,
		}, nil

	case "succeeded":
		if prediction.Output == nil {
			return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "No video URL in response", nil)
		}
		outputURL, ok := prediction.Output.(string)
		if !ok {
			return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "No video URL in response", nil)
		}

		meta := map[string]interface{}{
			"videos":       []map[string]interface{}{{"url": outputURL}},
			"predictionId": prediction.ID,
		}
		if prediction.Metrics != nil {
			meta["metrics"] = prediction.Metrics
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusCompleted,
			Videos:           []provider.VideoModelV3VideoData{{Type: "url", URL: outputURL, MediaType: "video/mp4"}},
			Warnings:         []types.Warning{},
			ProviderMetadata: map[string]interface{}{"replicate": meta},
			Response:         responseInfo,
		}, nil

	default:
		// "starting" or "processing"
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: responseInfo}, nil
	}
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where ReplicateVideoModel implements only
// doStart/doStatus and the core generate-video flow polls it directly), so
// this method is the Go equivalent of that default polling behavior, built
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
			jobFailureErr = providererrors.NewVideoGenerationError("replicate", m.modelID, status.Error, nil)
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
		return nil, providererrors.NewVideoGenerationError("replicate", m.modelID, "polling failed", pollErr)
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
	// TS has no synchronous doGenerate for this model: core's generateVideo
	// always drives doStart/doStatus itself, defaulting to
	// intervalMs=5000/timeoutMs=600_000 (generate-video.ts). Match that
	// default here rather than polling.DefaultPollOptions()'s 2s/5min,
	// which is unrelated to video and would poll faster/timeout sooner
	// than TS by default.
	opts := polling.PollOptions{PollIntervalMs: 5000, PollTimeoutMs: 600000}

	if providerOpts != nil {
		if repOpts, ok := providerOpts["replicate"].(map[string]interface{}); ok {
			if interval, ok := repOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := repOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

// mergeHeaders combines the provider's default headers (Authorization,
// Content-Type) with request-specific headers, matching TS's
// combineHeaders(this.config.headers, headers) forwarded to the status GET.
func (m *VideoModel) mergeHeaders(headers map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m.prov.client.Headers() {
		out[k] = v
	}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func (m *VideoModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, "", string(statusErr.Body), err)
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// handlePollError converts a fileutil poll error (a providererrors.DownloadError
// for a non-2xx HTTP response, or a validation/network error) into a
// Replicate API error where possible.
func (m *VideoModel) handlePollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return providererrors.NewProviderError(m.Provider(), dlErr.StatusCode, "", string(dlErr.Body), err)
	}
	return fmt.Errorf("failed to check status: %w", err)
}

// convertReplicateHeaders flattens net/http.Header into map[string]string,
// taking the first value for each header key.
func convertReplicateHeaders(h http.Header) map[string]string {
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

// flattenReplicateHeaders flattens a fileutil.DownloadResult's headers
// (map[string][]string) the same way convertReplicateHeaders does for
// net/http.Header.
func flattenReplicateHeaders(h map[string][]string) map[string]string {
	if len(h) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// replicatePredictionWire mirrors TS replicatePredictionSchema.
type replicatePredictionWire struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output,omitempty"`
	Error  *string     `json:"error,omitempty"`
	URLs   struct {
		Get string `json:"get"`
	} `json:"urls"`
	Metrics *struct {
		PredictTime *float64 `json:"predict_time,omitempty"`
	} `json:"metrics,omitempty"`
}
