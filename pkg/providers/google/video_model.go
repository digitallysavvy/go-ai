package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// VideoModel implements the provider.VideoModelV3 interface for Google
// Generative AI (Veo). It ports @ai-sdk/google's GoogleVideoModel
// (google-video-model.ts): the long-running-operation predictLongRunning
// endpoint, via DoStart/DoStatus. DoGenerate is the Go equivalent of the
// default start+poll behavior TS core's generateVideo would drive, built
// entirely on top of DoStart/DoStatus (TS GoogleVideoModel implements only
// doStart/doStatus).
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Google Generative AI video generation model
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
	return m.prov.Name()
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns 4: Google supports multiple videos via
// sampleCount (TS `get maxVideosPerCall() { return 4; }`).
func (m *VideoModel) MaxVideosPerCall() *int {
	four := 4
	return &four
}

// GoogleVideoModelOptions mirrors TS GoogleVideoModelOptions
// (google-video-model-options.ts).
type GoogleVideoModelOptions struct {
	PersonGeneration *string                        `json:"personGeneration,omitempty"`
	NegativePrompt   *string                        `json:"negativePrompt,omitempty"`
	ReferenceImages  []googleVideoReferenceImageOpt `json:"referenceImages,omitempty"`
}

type googleVideoReferenceImageOpt struct {
	BytesBase64Encoded *string `json:"bytesBase64Encoded,omitempty"`
	GcsURI             *string `json:"gcsUri,omitempty"`
}

var googleVideoHandledOptionKeys = map[string]bool{
	"pollIntervalMs": true, "pollTimeoutMs": true,
	"personGeneration": true, "negativePrompt": true, "referenceImages": true,
}

// extractVideoProviderOptions extracts GoogleVideoModelOptions and any
// unrecognized keys (passthrough into `parameters`), mirroring TS
// buildRequest's Object.entries loop over googleOptions.
func extractVideoProviderOptions(opts map[string]interface{}) (*GoogleVideoModelOptions, map[string]interface{}, error) {
	if opts == nil {
		return nil, nil, nil
	}
	raw, ok := opts["google"]
	if !ok {
		return nil, nil, nil
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal google video provider options: %w", err)
	}
	var provOpts GoogleVideoModelOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal google video provider options: %w", err)
	}
	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal google video provider options map: %w", err)
	}
	extra := make(map[string]interface{})
	for k, v := range rawMap {
		if !googleVideoHandledOptionKeys[k] {
			extra[k] = v
		}
	}
	return &provOpts, extra, nil
}

// getFirstFrameImage returns the frameImages first_frame entry, if any (TS
// getFirstFrameImage).
func googleVideoGetFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// googleVideoGetLastFrameImage returns the frameImages last_frame entry, if
// any (TS getLastFrameImage).
func googleVideoGetLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

// googleVideoResolveStartImage prefers a frameImages first_frame over the
// legacy top-level Image field (TS resolveStartImage).
func googleVideoResolveStartImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	if img := googleVideoGetFirstFrameImage(opts); img != nil {
		return img
	}
	return opts.Image
}

// googleVideoGetInputReferences returns InputReferences unless FrameImages
// were also supplied, matching TS getInputReferences (frameImages and
// inputReferences cannot be combined at the provider level).
func googleVideoGetInputReferences(opts *provider.VideoModelV3CallOptions) []provider.VideoModelV3File {
	if len(opts.FrameImages) > 0 {
		return nil
	}
	if len(opts.InputReferences) == 0 {
		return nil
	}
	return opts.InputReferences
}

// convertFileToGoogleImage converts a VideoModelV3File to the Vertex-style
// image payload Veo's predictLongRunning endpoint expects, or nil (with a
// warning) for a URL that is not a gs:// URI (TS convertFileToGoogleImage).
func convertFileToGoogleImage(file *provider.VideoModelV3File, warnings *[]types.Warning) map[string]interface{} {
	if file.Type == "url" {
		if strings.HasPrefix(file.URL, "gs://") {
			return map[string]interface{}{"gcsUri": file.URL, "mimeType": "image/png"}
		}
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "URL-based image input",
			Details: "Google Generative AI video models require base64-encoded images or GCS URIs. URL will be ignored.",
		})
		return nil
	}

	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	return map[string]interface{}{
		"bytesBase64Encoded": base64.StdEncoding.EncodeToString(file.Data),
		"mimeType":           mediaType,
	}
}

// convertProviderReferenceImage converts a providerOptions.google.
// referenceImages entry to the request's referenceImages shape (TS
// convertProviderReferenceImage).
func convertProviderReferenceImage(ref googleVideoReferenceImageOpt) map[string]interface{} {
	if ref.BytesBase64Encoded != nil {
		return map[string]interface{}{
			"image":         map[string]interface{}{"bytesBase64Encoded": *ref.BytesBase64Encoded, "mimeType": "image/png"},
			"referenceType": "asset",
		}
	}
	if ref.GcsURI != nil {
		return map[string]interface{}{
			"image":         map[string]interface{}{"gcsUri": *ref.GcsURI, "mimeType": "image/png"},
			"referenceType": "asset",
		}
	}
	return map[string]interface{}{}
}

// convertInputReferenceImage converts an inputReferences entry into the
// request's referenceImages shape (TS convertInputReferenceImage).
func convertInputReferenceImage(file *provider.VideoModelV3File, warnings *[]types.Warning) map[string]interface{} {
	image := convertFileToGoogleImage(file, warnings)
	if image == nil {
		return nil
	}
	return map[string]interface{}{"image": image, "referenceType": "asset"}
}

// googleVideoResolutionMap mirrors TS's resolutionMap in buildRequest.
var googleVideoResolutionMap = map[string]string{
	"1280x720":  "720p",
	"1920x1080": "1080p",
	"3840x2160": "4k",
}

// buildRequest builds the predictLongRunning request body (TS
// GoogleVideoModel#buildRequest).
func (m *VideoModel) buildRequest(opts *provider.VideoModelV3CallOptions) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}

	provOpts, extra, err := extractVideoProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, nil, err
	}

	instance := map[string]interface{}{}

	if opts.PromptSet || opts.Prompt != "" {
		instance["prompt"] = opts.Prompt
	}

	if startImage := googleVideoResolveStartImage(opts); startImage != nil {
		if image := convertFileToGoogleImage(startImage, &warnings); image != nil {
			instance["image"] = image
		}
	}

	if lastFrame := googleVideoGetLastFrameImage(opts); lastFrame != nil {
		if image := convertFileToGoogleImage(lastFrame, &warnings); image != nil {
			instance["lastFrame"] = image
		}
	}

	if inputRefs := googleVideoGetInputReferences(opts); inputRefs != nil {
		refs := make([]map[string]interface{}, 0, len(inputRefs))
		for i := range inputRefs {
			if converted := convertInputReferenceImage(&inputRefs[i], &warnings); converted != nil {
				refs = append(refs, converted)
			}
		}
		instance["referenceImages"] = refs
	} else if provOpts != nil && provOpts.ReferenceImages != nil {
		refs := make([]map[string]interface{}, 0, len(provOpts.ReferenceImages))
		for _, ref := range provOpts.ReferenceImages {
			refs = append(refs, convertProviderReferenceImage(ref))
		}
		instance["referenceImages"] = refs
	}

	parameters := map[string]interface{}{
		"sampleCount": opts.N,
	}

	if opts.AspectRatio != "" {
		parameters["aspectRatio"] = opts.AspectRatio
	}

	if opts.Resolution != "" {
		if mapped, ok := googleVideoResolutionMap[opts.Resolution]; ok {
			parameters["resolution"] = mapped
		} else {
			parameters["resolution"] = opts.Resolution
		}
	}

	// TS: `if (options.duration)` -- falsy (including 0) is skipped.
	if opts.Duration != nil && *opts.Duration != 0 {
		parameters["durationSeconds"] = *opts.Duration
	}

	// TS: `if (options.seed)` -- falsy (including 0) is skipped.
	if opts.Seed != nil && *opts.Seed != 0 {
		parameters["seed"] = *opts.Seed
	}

	if provOpts != nil {
		if provOpts.PersonGeneration != nil {
			parameters["personGeneration"] = *provOpts.PersonGeneration
		}
		if provOpts.NegativePrompt != nil {
			parameters["negativePrompt"] = *provOpts.NegativePrompt
		}
	}
	for k, v := range extra {
		parameters[k] = v
	}

	return map[string]interface{}{
		"instances":  []map[string]interface{}{instance},
		"parameters": parameters,
	}, warnings, nil
}

// googleVideoOperation is the opaque operation reference returned by
// DoStart and passed back into DoStatus.
type googleVideoOperation struct {
	OperationName string `json:"operationName"`
}

// DoStart starts an asynchronous video generation via Veo's
// predictLongRunning endpoint and returns an opaque operation reference (TS
// GoogleVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := time.Now()
	callOpts := &opts.VideoModelV3CallOptions

	body, warnings, err := m.buildRequest(callOpts)
	if err != nil {
		return nil, err
	}

	var operation googleVideoOperationWire
	httpResp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    fmt.Sprintf("/models/%s:predictLongRunning", m.modelID),
		Body:    body,
		Headers: callOpts.Headers,
	}, &operation)
	if err != nil {
		return nil, m.handleError(err)
	}

	if operation.Name == "" {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "No operation name returned from API", nil)
	}

	op, _ := json.Marshal(googleVideoOperation{OperationName: operation.Name})

	return &provider.VideoModelV3OperationStartResult{
		Operation: op,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   convertGoogleHeaders(httpResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via the operation's status endpoint (TS
// GoogleVideoModel#doStatus).
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := time.Now()

	var op googleVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("google: invalid operation reference: %w", err)
	}

	var operation googleVideoOperationWire
	httpResp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/" + op.OperationName,
		Headers: opts.Headers,
	}, &operation)
	if err != nil {
		return nil, m.handleError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   convertGoogleHeaders(httpResp.Headers),
	}

	if !operation.Done {
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: responseInfo}, nil
	}

	if operation.Error != nil {
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    fmt.Sprintf("Video generation failed: %s", operation.Error.Message),
			Response: responseInfo,
		}, nil
	}

	return m.buildCompletedResult(&operation, responseInfo)
}

// buildCompletedResult converts a done operation's response into a
// completed status result (TS GoogleVideoModel#buildCompletedResult),
// appending the API key to same-origin download URLs so the returned URL is
// directly fetchable.
func (m *VideoModel) buildCompletedResult(operation *googleVideoOperationWire, responseInfo provider.VideoModelV3ResponseInfo) (*provider.VideoModelV3OperationStatusResult, error) {
	if operation.Response == nil || operation.Response.GenerateVideoResponse == nil ||
		len(operation.Response.GenerateVideoResponse.GeneratedSamples) == 0 {
		raw, _ := json.Marshal(operation)
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, fmt.Sprintf("No videos in response. Response: %s", string(raw)), nil)
	}

	apiKey := m.prov.client.Headers()["x-goog-api-key"]

	videos := []provider.VideoModelV3VideoData{}
	videoMetadata := []map[string]interface{}{}

	for _, sample := range operation.Response.GenerateVideoResponse.GeneratedSamples {
		if sample.Video == nil || sample.Video.URI == "" {
			continue
		}
		videoURL := sample.Video.URI
		if apiKey != "" && providerutils.IsSameOrigin(sample.Video.URI, m.prov.config.BaseURL) {
			sep := "?"
			if strings.Contains(videoURL, "?") {
				sep = "&"
			}
			videoURL = videoURL + sep + "key=" + apiKey
		}
		videos = append(videos, provider.VideoModelV3VideoData{Type: "url", URL: videoURL, MediaType: "video/mp4"})
		videoMetadata = append(videoMetadata, map[string]interface{}{"uri": sample.Video.URI})
	}

	if len(videos) == 0 {
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "No valid videos in response", nil)
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status:           provider.VideoOperationStatusCompleted,
		Videos:           videos,
		Warnings:         []types.Warning{},
		ProviderMetadata: map[string]interface{}{"google": map[string]interface{}{"videos": videoMetadata}},
		Response:         responseInfo,
	}, nil
}

// DoGenerate generates videos synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where GoogleVideoModel implements only doStart/
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
			jobFailureErr = providererrors.NewVideoGenerationError("google", m.modelID, status.Error, nil)
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
		return nil, providererrors.NewVideoGenerationError("google", m.modelID, "polling failed", pollErr)
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
		if googleOpts, ok := providerOpts["google"].(map[string]interface{}); ok {
			if interval, ok := googleOpts["pollIntervalMs"].(int); ok {
				opts.PollIntervalMs = interval
			}
			if timeout, ok := googleOpts["pollTimeoutMs"].(int); ok {
				opts.PollTimeoutMs = timeout
			}
		}
	}

	return opts
}

func (m *VideoModel) handleError(err error) error {
	return NewLanguageModel(m.prov, "").HandleError(err)
}

// convertGoogleHeaders flattens net/http.Header into map[string]string,
// taking the first value for each header key.
func convertGoogleHeaders(h http.Header) map[string]string {
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

// googleVideoOperationWire mirrors TS googleOperationSchema.
type googleVideoOperationWire struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Code    *int   `json:"code,omitempty"`
		Message string `json:"message"`
		Status  string `json:"status,omitempty"`
	} `json:"error,omitempty"`
	Response *struct {
		GenerateVideoResponse *struct {
			GeneratedSamples []struct {
				Video *struct {
					URI string `json:"uri,omitempty"`
				} `json:"video,omitempty"`
			} `json:"generatedSamples,omitempty"`
		} `json:"generateVideoResponse,omitempty"`
	} `json:"response,omitempty"`
}
