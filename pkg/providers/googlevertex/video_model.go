package googlevertex

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
)

// VideoModel implements the provider.VideoModelV3 interface for Google
// Vertex AI (Veo). It ports @ai-sdk/google-vertex's GoogleVertexVideoModel
// (google-vertex-video-model.ts): the long-running-operation
// predictLongRunning/fetchPredictOperation endpoints, via DoStart/DoStatus.
// DoGenerate is the Go equivalent of the default start+poll behavior TS
// core's generateVideo would drive, built entirely on top of DoStart/
// DoStatus (TS GoogleVertexVideoModel implements only doStart/doStatus).
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Google Vertex AI video generation model.
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *VideoModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider name.
func (m *VideoModel) Provider() string { return "google-vertex" }

// ModelID returns the model ID.
func (m *VideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 4: Vertex supports multiple videos via
// sampleCount (TS `get maxVideosPerCall() { return 4; }`).
func (m *VideoModel) MaxVideosPerCall() *int {
	four := 4
	return &four
}

// GoogleVertexVideoModelOptions mirrors TS GoogleVertexVideoModelOptions
// (google-vertex-video-model-options.ts).
type GoogleVertexVideoModelOptions struct {
	PersonGeneration   *string                        `json:"personGeneration,omitempty"`
	NegativePrompt     *string                        `json:"negativePrompt,omitempty"`
	GenerateAudio      *bool                          `json:"generateAudio,omitempty"`
	GcsOutputDirectory *string                        `json:"gcsOutputDirectory,omitempty"`
	ReferenceImages    []vertexVideoReferenceImageOpt `json:"referenceImages,omitempty"`
}

type vertexVideoReferenceImageOpt struct {
	BytesBase64Encoded *string `json:"bytesBase64Encoded,omitempty"`
	GcsURI             *string `json:"gcsUri,omitempty"`
}

var vertexVideoHandledOptionKeys = map[string]bool{
	"pollIntervalMs": true, "pollTimeoutMs": true,
	"personGeneration": true, "negativePrompt": true, "generateAudio": true,
	"gcsOutputDirectory": true, "referenceImages": true,
}

// extractVideoProviderOptions extracts GoogleVertexVideoModelOptions,
// checking "googleVertex" first and falling back to the legacy "vertex" key
// (TS buildRequest's parseProviderOptions(...) ?? parseProviderOptions('vertex', ...)),
// plus any unrecognized keys (passthrough into `parameters`).
func extractVideoProviderOptions(opts map[string]interface{}) (*GoogleVertexVideoModelOptions, map[string]interface{}, error) {
	if opts == nil {
		return nil, nil, nil
	}
	raw, ok := opts["googleVertex"]
	if !ok {
		raw, ok = opts["vertex"]
	}
	if !ok {
		return nil, nil, nil
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal googleVertex video provider options: %w", err)
	}
	var provOpts GoogleVertexVideoModelOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal googleVertex video provider options: %w", err)
	}
	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &rawMap); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal googleVertex video provider options map: %w", err)
	}
	extra := make(map[string]interface{})
	for k, v := range rawMap {
		if !vertexVideoHandledOptionKeys[k] {
			extra[k] = v
		}
	}
	return &provOpts, extra, nil
}

func vertexVideoGetFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func vertexVideoGetLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func vertexVideoResolveStartImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	if img := vertexVideoGetFirstFrameImage(opts); img != nil {
		return img
	}
	return opts.Image
}

func vertexVideoGetInputReferences(opts *provider.VideoModelV3CallOptions) []provider.VideoModelV3File {
	if len(opts.FrameImages) > 0 {
		return nil
	}
	if len(opts.InputReferences) == 0 {
		return nil
	}
	return opts.InputReferences
}

// convertFileToVertexImage converts a VideoModelV3File to the Vertex-style
// image payload (TS convertFileToVertexImage).
func convertFileToVertexImage(file *provider.VideoModelV3File, warnings *[]types.Warning) map[string]interface{} {
	if file.Type == "url" {
		if strings.HasPrefix(file.URL, "gs://") {
			return map[string]interface{}{"gcsUri": file.URL, "mimeType": "image/png"}
		}
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "URL-based image input",
			Details: "Vertex AI video models require base64-encoded images or GCS URIs. URL will be ignored.",
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

// convertInputReferenceImage converts an inputReferences entry into the
// request's referenceImages shape (TS convertInputReferenceImage).
func convertInputReferenceImage(file *provider.VideoModelV3File, warnings *[]types.Warning) map[string]interface{} {
	image := convertFileToVertexImage(file, warnings)
	if image == nil {
		return nil
	}
	return map[string]interface{}{"image": image, "referenceType": "asset"}
}

func convertProviderReferenceImageVertex(ref vertexVideoReferenceImageOpt) map[string]interface{} {
	out := map[string]interface{}{}
	if ref.BytesBase64Encoded != nil {
		out["bytesBase64Encoded"] = *ref.BytesBase64Encoded
	}
	if ref.GcsURI != nil {
		out["gcsUri"] = *ref.GcsURI
	}
	return out
}

// vertexVideoResolutionMap mirrors TS's resolutionMap in buildRequest.
var vertexVideoResolutionMap = map[string]string{
	"1280x720":  "720p",
	"1920x1080": "1080p",
	"3840x2160": "4k",
}

// buildRequest builds the predictLongRunning request body (TS
// GoogleVertexVideoModel#buildRequest).
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

	if startImage := vertexVideoResolveStartImage(opts); startImage != nil {
		if image := convertFileToVertexImage(startImage, &warnings); image != nil {
			instance["image"] = image
		}
	}

	if lastFrame := vertexVideoGetLastFrameImage(opts); lastFrame != nil {
		if image := convertFileToVertexImage(lastFrame, &warnings); image != nil {
			instance["lastFrame"] = image
		}
	}

	if inputRefs := vertexVideoGetInputReferences(opts); inputRefs != nil {
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
			refs = append(refs, convertProviderReferenceImageVertex(ref))
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
		if mapped, ok := vertexVideoResolutionMap[opts.Resolution]; ok {
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

	generateAudio := opts.GenerateAudio
	if generateAudio == nil && provOpts != nil {
		generateAudio = provOpts.GenerateAudio
	}
	if generateAudio != nil {
		parameters["generateAudio"] = *generateAudio
	}

	if provOpts != nil {
		if provOpts.PersonGeneration != nil {
			parameters["personGeneration"] = *provOpts.PersonGeneration
		}
		if provOpts.NegativePrompt != nil {
			parameters["negativePrompt"] = *provOpts.NegativePrompt
		}
		if provOpts.GcsOutputDirectory != nil {
			parameters["gcsOutputDirectory"] = *provOpts.GcsOutputDirectory
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

// vertexVideoOperation is the opaque operation reference returned by
// DoStart and passed back into DoStatus.
type vertexVideoOperation struct {
	OperationName string `json:"operationName"`
}

// DoStart starts an asynchronous video generation via Veo's
// predictLongRunning endpoint and returns an opaque operation reference (TS
// GoogleVertexVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := time.Now()
	callOpts := &opts.VideoModelV3CallOptions

	body, warnings, err := m.buildRequest(callOpts)
	if err != nil {
		return nil, err
	}

	var operation vertexVideoOperationWire
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
		return nil, providererrors.NewVideoGenerationError("google-vertex", m.modelID, "No operation name returned from API", nil)
	}

	op, _ := json.Marshal(vertexVideoOperation{OperationName: operation.Name})

	return &provider.VideoModelV3OperationStartResult{
		Operation: op,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   convertVertexVideoHeaders(httpResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via the model's fetchPredictOperation endpoint (TS
// GoogleVertexVideoModel#doStatus). Unlike the Generative AI API, Vertex
// polls with a POST carrying {operationName} in the body rather than a GET
// on the operation's own resource path.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := time.Now()

	var op vertexVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("google-vertex: invalid operation reference: %w", err)
	}

	var operation vertexVideoOperationWire
	httpResp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    fmt.Sprintf("/models/%s:fetchPredictOperation", m.modelID),
		Body:    map[string]interface{}{"operationName": op.OperationName},
		Headers: opts.Headers,
	}, &operation)
	if err != nil {
		return nil, m.handleError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   convertVertexVideoHeaders(httpResp.Headers),
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
// completed status result (TS GoogleVertexVideoModel#buildCompletedResult).
// Each video is either inline base64 data (bytesBase64Encoded) or a GCS URI,
// depending on whether providerOptions.googleVertex.gcsOutputDirectory was
// set.
func (m *VideoModel) buildCompletedResult(operation *vertexVideoOperationWire, responseInfo provider.VideoModelV3ResponseInfo) (*provider.VideoModelV3OperationStatusResult, error) {
	if operation.Response == nil || len(operation.Response.Videos) == 0 {
		raw, _ := json.Marshal(operation)
		return nil, providererrors.NewVideoGenerationError("google-vertex", m.modelID, fmt.Sprintf("No videos in response. Response: %s", string(raw)), nil)
	}

	videos := []provider.VideoModelV3VideoData{}
	videoMetadata := []map[string]interface{}{}

	for _, v := range operation.Response.Videos {
		mediaType := v.MimeType
		if mediaType == "" {
			mediaType = "video/mp4"
		}
		switch {
		case v.BytesBase64Encoded != "":
			videos = append(videos, provider.VideoModelV3VideoData{Type: "base64", Data: v.BytesBase64Encoded, MediaType: mediaType})
			meta := map[string]interface{}{}
			if v.MimeType != "" {
				meta["mimeType"] = v.MimeType
			}
			videoMetadata = append(videoMetadata, meta)
		case v.GcsURI != "":
			videos = append(videos, provider.VideoModelV3VideoData{Type: "url", URL: v.GcsURI, MediaType: mediaType})
			meta := map[string]interface{}{"gcsUri": v.GcsURI}
			if v.MimeType != "" {
				meta["mimeType"] = v.MimeType
			}
			videoMetadata = append(videoMetadata, meta)
		}
	}

	if len(videos) == 0 {
		return nil, providererrors.NewVideoGenerationError("google-vertex", m.modelID, "No valid videos in response", nil)
	}

	payload := map[string]interface{}{"videos": videoMetadata}

	return &provider.VideoModelV3OperationStatusResult{
		Status:   provider.VideoOperationStatusCompleted,
		Videos:   videos,
		Warnings: []types.Warning{},
		ProviderMetadata: map[string]interface{}{
			"googleVertex": payload,
			// Legacy keys preserved for backward compatibility.
			"google-vertex": payload,
			"vertex":        payload,
		},
		Response: responseInfo,
	}, nil
}

// DoGenerate generates videos synchronously by starting the operation and
// polling DoStatus until it completes. Go's VideoModelV3 always requires
// DoGenerate (unlike TS, where GoogleVertexVideoModel implements only
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
			jobFailureErr = providererrors.NewVideoGenerationError("google-vertex", m.modelID, status.Error, nil)
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
		return nil, providererrors.NewVideoGenerationError("google-vertex", m.modelID, "polling failed", pollErr)
	}

	return &provider.VideoModelV3Response{
		Videos:           finalStatus.Videos,
		Warnings:         append(append([]types.Warning{}, startResult.Warnings...), finalStatus.Warnings...),
		ProviderMetadata: finalStatus.ProviderMetadata,
		Response:         finalStatus.Response,
	}, nil
}

// getPollOptions extracts polling options from provider options, checking
// "googleVertex" first and falling back to the legacy "vertex" key.
func (m *VideoModel) getPollOptions(providerOpts map[string]interface{}) polling.PollOptions {
	// TS has no synchronous doGenerate for this model: core's generateVideo
	// always drives doStart/doStatus itself, defaulting to
	// intervalMs=5000/timeoutMs=600_000 (generate-video.ts). Match that
	// default here rather than polling.DefaultPollOptions()'s 2s/5min,
	// which is unrelated to video and would poll faster/timeout sooner
	// than TS by default.
	opts := polling.PollOptions{PollIntervalMs: 5000, PollTimeoutMs: 600000}

	if providerOpts != nil {
		raw, ok := providerOpts["googleVertex"]
		if !ok {
			raw, ok = providerOpts["vertex"]
		}
		if ok {
			if vertexOpts, ok := raw.(map[string]interface{}); ok {
				if interval, ok := vertexOpts["pollIntervalMs"].(int); ok {
					opts.PollIntervalMs = interval
				}
				if timeout, ok := vertexOpts["pollTimeoutMs"].(int); ok {
					opts.PollTimeoutMs = timeout
				}
			}
		}
	}

	return opts
}

func (m *VideoModel) handleError(err error) error {
	return NewLanguageModel(m.prov, "").HandleError(err)
}

// convertVertexVideoHeaders flattens net/http.Header into map[string]string,
// taking the first value for each header key.
func convertVertexVideoHeaders(h http.Header) map[string]string {
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

// vertexVideoOperationWire mirrors TS googleVertexOperationSchema.
type vertexVideoOperationWire struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Code    *int   `json:"code,omitempty"`
		Message string `json:"message"`
		Status  string `json:"status,omitempty"`
	} `json:"error,omitempty"`
	Response *struct {
		Videos []struct {
			BytesBase64Encoded string `json:"bytesBase64Encoded,omitempty"`
			GcsURI             string `json:"gcsUri,omitempty"`
			MimeType           string `json:"mimeType,omitempty"`
		} `json:"videos,omitempty"`
		RaiMediaFilteredCount *int `json:"raiMediaFilteredCount,omitempty"`
	} `json:"response,omitempty"`
}
