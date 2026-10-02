package topaz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Default polling configuration for VideoModel.DoGenerate's synchronous
// start+poll wrapper. Topaz's own TS provider has no doGenerate (the TS SDK
// core polls doStart/doStatus itself when both are implemented); Go's
// provider.VideoModelV3 interface requires DoGenerate directly, so this
// mirrors the equivalent BFL/Prodia Go wrapper with the same defaults used
// by the image model's own polling.
const (
	defaultVideoPollIntervalMillis = 2000
	defaultVideoPollTimeoutMillis  = 600_000
)

// mediaTypeContainers maps an input file's media type onto a Topaz source
// container.
var mediaTypeContainers = map[string]string{
	"video/mp4":        "mp4",
	"video/quicktime":  "mov",
	"video/mov":        "mov",
	"video/x-matroska": "mkv",
	"video/matroska":   "mkv",
	"video/webm":       "webm",
	"video/x-msvideo":  "avi",
	"video/avi":        "avi",
	"video/mpeg":       "mpeg",
	"video/mp2t":       "ts",
	"video/x-ms-wmv":   "wmv",
	"video/x-flv":      "flv",
	"video/3gpp":       "3gp",
	"video/x-m4v":      "m4v",
	"application/mxf":  "mxf",
}

// extensionContainers maps a URL's file extension onto a Topaz source
// container, for inputs whose container cannot be derived from a media
// type.
var extensionContainers = buildExtensionContainers()

func buildExtensionContainers() map[string]string {
	m := make(map[string]string, len(topazSourceContainers)+1)
	for container := range topazSourceContainers {
		m[container] = container
	}
	m["qt"] = "mov"
	return m
}

// containerMediaTypes maps a Topaz container onto the media type of the
// file it produces or expects.
var containerMediaTypes = map[string]string{
	"mp4":  "video/mp4",
	"m4v":  "video/mp4",
	"mov":  "video/quicktime",
	"mkv":  "video/x-matroska",
	"webm": "video/webm",
	"avi":  "video/x-msvideo",
	"mpeg": "video/mpeg",
	"mpg":  "video/mpeg",
	"ts":   "video/mp2t",
	"wmv":  "video/x-ms-wmv",
	"flv":  "video/x-flv",
	"3gp":  "video/3gpp",
	"mxf":  "application/mxf",
}

// VideoModel implements the provider.VideoModelV3 interface for Topaz Labs
// video enhancement models. DoStart creates an express request: Topaz
// fetches URL inputs itself, and file inputs are uploaded to the returned
// URL. Processing starts once Topaz has the video, and DoStatus polls.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Topaz video enhancement model.
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *VideoModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *VideoModel) Provider() string { return "topaz.video" }

// ModelID returns the model ID.
func (m *VideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 1: Topaz enhances a single video per call.
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// topazVideoOperation is the opaque operation reference returned by
// DoStart and passed back into DoStatus.
type topazVideoOperation struct {
	RequestID       string `json:"requestId"`
	OutputContainer string `json:"outputContainer"`
}

// DoStart submits a Topaz video express request and returns an opaque
// operation reference (TS TopazVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	if opts == nil {
		opts = &provider.VideoModelV3StartOptions{}
	}
	currentDate := time.Now()
	var warnings []types.Warning

	topazOpts := parseVideoModelOptions(opts.ProviderOptions)
	addVideoUnsupportedWarnings(&opts.VideoModelV3CallOptions, &warnings)

	input, err := selectInputVideo(&opts.VideoModelV3CallOptions, &warnings)
	if err != nil {
		return nil, err
	}

	container, err := resolveContainer(input, topazOpts)
	if err != nil {
		return nil, err
	}

	source, err := resolveSource(topazOpts, container)
	if err != nil {
		return nil, err
	}

	output, outputContainer, err := buildOutput(&opts.VideoModelV3CallOptions, topazOpts, source)
	if err != nil {
		return nil, err
	}

	filter, err := buildFilter(m.modelID, topazOpts)
	if err != nil {
		return nil, err
	}
	filters := []interface{}{filter}
	for _, f := range topazOpts.additionalFilters() {
		filters = append(filters, f)
	}

	sourceBody := map[string]interface{}{"container": source.container}
	if source.metadata != nil {
		sourceBody["duration"] = source.metadata.duration
		sourceBody["frameCount"] = source.metadata.frameCount
		sourceBody["frameRate"] = source.metadata.frameRate
		sourceBody["resolution"] = map[string]interface{}{
			"width":  source.metadata.width,
			"height": source.metadata.height,
		}
	}

	var uploadBytes []byte
	if input.Type == "url" {
		// Topaz fetches URL inputs itself, so, like other providers, the
		// bytes never need to pass through the SDK.
		sourceBody["external"] = map[string]interface{}{"provider": "s3", "presignedUrl": input.URL}
	} else {
		uploadBytes = input.Data
		sourceBody["size"] = len(uploadBytes)
	}

	body := map[string]interface{}{
		"source":  sourceBody,
		"output":  output,
		"filters": filters,
	}

	contentType := containerMediaTypes[source.container]
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	callCtx, cancel := mergeAbortContext(ctx, opts.AbortSignal)
	defer cancel()

	resp, err := m.prov.client.Do(callCtx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/video/express",
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	var created topazVideoExpressResponse
	if err := json.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode video express response: %w", err)
	}

	requestID := created.RequestID
	if requestID == "" {
		return nil, fmt.Errorf("Topaz did not return a requestId for the video request.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	if uploadBytes != nil {
		if err := m.uploadVideo(callCtx, requestID, uploadBytes, created.UploadURLs, contentType); err != nil {
			// Canceling before processing starts refunds any reserved
			// credits.
			m.cancelQuietly(context.Background(), requestID, opts.Headers)
			return nil, err
		}
	}

	operation, marshalErr := json.Marshal(topazVideoOperation{RequestID: requestID, OutputContainer: outputContainer})
	if marshalErr != nil {
		return nil, fmt.Errorf("topaz: failed to encode operation reference: %w", marshalErr)
	}

	metadata := map[string]interface{}{"requestId": requestID}
	// Preliminary: Topaz estimates up front only when it gets source
	// metadata, and the completed status carries the billed value.
	if created.Estimates != nil && len(created.Estimates.Cost) > 0 {
		metadata["estimatedCredits"] = created.Estimates.Cost
	}

	return &provider.VideoModelV3OperationStartResult{
		Operation:        operation,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"topaz": metadata},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   flattenHeader(resp.Headers),
		},
	}, nil
}

// DoStatus checks the status of a Topaz video request started with
// DoStart (TS TopazVideoModel#doStatus).
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	if opts == nil {
		opts = &provider.VideoModelV3StatusOptions{}
	}
	currentDate := time.Now()

	var op topazVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("topaz: invalid operation reference: %w", err)
	}
	if op.RequestID == "" {
		return nil, fmt.Errorf("topaz: operation reference is missing requestId")
	}

	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/video/" + op.RequestID + "/status",
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	var status topazVideoStatusResponse
	if err := json.Unmarshal(resp.Body, &status); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode video status response: %w", err)
	}

	response := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   flattenHeader(resp.Headers),
	}

	switch status.Status {
	case "complete":
		// Topaz confirmed it invoices the lower bound of estimates.cost,
		// which it recomputes once the source upload has been received.
		credits := lowerBoundCredits(status.Estimates)

		if status.Download == nil || status.Download.URL == "" {
			return nil, fmt.Errorf("Topaz reported request %s complete but returned no download URL.", op.RequestID) //nolint:staticcheck // matches TS SDK's exact error text
		}

		mediaType := containerMediaTypes[op.OutputContainer]
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		metadata := map[string]interface{}{"requestId": op.RequestID}
		if credits != nil {
			metadata["credits"] = *credits
		}
		if status.Estimates != nil && len(status.Estimates.Cost) > 0 {
			metadata["estimatedCredits"] = status.Estimates.Cost
		}
		if status.OutputSize != nil {
			metadata["outputSize"] = status.OutputSize
		}
		if status.Download.ExpiresAt != nil {
			metadata["expiresAt"] = status.Download.ExpiresAt
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status: provider.VideoOperationStatusCompleted,
			Videos: []provider.VideoModelV3VideoData{{
				Type:      "url",
				URL:       status.Download.URL,
				MediaType: mediaType,
			}},
			ProviderMetadata: map[string]interface{}{"topaz": metadata},
			Response:         response,
		}, nil

	case "failed", "canceled":
		errMsg := fmt.Sprintf("Topaz video request %s %s", op.RequestID, status.Status)
		if status.ErrorCode != nil {
			errMsg += fmt.Sprintf(" (%s)", *status.ErrorCode)
		}
		if status.Message != nil {
			errMsg += fmt.Sprintf(": %s", *status.Message)
		} else {
			errMsg += "."
		}

		metadata := map[string]interface{}{"requestId": op.RequestID}
		if status.ErrorCode != nil {
			metadata["errorCode"] = *status.ErrorCode
		}

		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusError,
			Error:            errMsg,
			ProviderMetadata: map[string]interface{}{"topaz": metadata},
			Response:         response,
		}, nil

	default:
		// Documented in-progress values are requested, accepted,
		// initializing, preprocessing, processing, postprocessing and
		// canceling. Anything else keeps polling too, so a newly added
		// state cannot break running jobs.
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: response}, nil
	}
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus at a fixed interval until it completes or the configured
// timeout elapses. Topaz's TS provider has no doGenerate of its own (the TS
// SDK core polls doStart/doStatus directly); this exists only because Go's
// provider.VideoModelV3 interface requires it.
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

	pollIntervalMillis := defaultVideoPollIntervalMillis
	if m.prov.config.VideoPollIntervalMillis > 0 {
		pollIntervalMillis = m.prov.config.VideoPollIntervalMillis
	}
	pollTimeoutMillis := defaultVideoPollTimeoutMillis
	if m.prov.config.VideoPollTimeoutMillis > 0 {
		pollTimeoutMillis = m.prov.config.VideoPollTimeoutMillis
	}
	deadline := time.Now().Add(time.Duration(pollTimeoutMillis) * time.Millisecond)

	for {
		select {
		case <-callCtx.Done():
			return nil, callCtx.Err()
		case <-time.After(time.Duration(pollIntervalMillis) * time.Millisecond):
		}

		if time.Now().After(deadline) {
			var op topazVideoOperation
			_ = json.Unmarshal(startResult.Operation, &op)
			return nil, fmt.Errorf("topaz video generation timed out after %dms (request %s)", pollTimeoutMillis, op.RequestID)
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
			return nil, providererrors.NewVideoGenerationError("topaz", m.modelID, status.Error, nil)
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

// addVideoUnsupportedWarnings appends warnings for options Topaz video
// models do not support, mirroring TS TopazVideoModel#addUnsupportedWarnings.
func addVideoUnsupportedWarnings(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) {
	// generateVideo requires a prompt, so an empty one is the expected way
	// to call an enhancement model and does not warrant a warning.
	if opts.PromptSet && strings.TrimSpace(opts.Prompt) != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "prompt",
			Details: "Topaz video models enhance an existing video and do not take a text prompt. The prompt was ignored.",
		})
	}

	if opts.AspectRatio != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "Topaz video models do not support aspectRatio. Use `resolution`, or the `output` provider option, to set the output dimensions.",
		})
	}

	if opts.Seed != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: "Topaz video models do not support seed.",
		})
	}

	if opts.Duration != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "duration",
			Details: "Topaz video models enhance the whole input video, so duration was ignored. Pass the input duration via the `source.duration` provider option instead.",
		})
	}

	if opts.GenerateAudio != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "generateAudio",
			Details: "Topaz video models do not generate audio. Use the `output.audioTransfer` provider option to control how the input audio track is carried over.",
		})
	}

	if len(opts.FrameImages) > 0 {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frameImages",
			Details: "Topaz video models do not support first/last frame generation. The frame images were ignored.",
		})
	}

	if opts.N > 1 {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "Topaz video models enhance one video per call. Only 1 video will be produced.",
		})
	}
}

// selectInputVideo picks the single input video from InputReferences
// (TS TopazVideoModel#selectInputVideo).
func selectInputVideo(opts *provider.VideoModelV3CallOptions, warnings *[]types.Warning) (*provider.VideoModelV3File, error) {
	references := opts.InputReferences
	var videos []provider.VideoModelV3File
	for i := range references {
		if isVideoReference(&references[i]) {
			videos = append(videos, references[i])
		}
	}

	if len(videos) == 0 {
		if opts.Image != nil {
			return nil, providererrors.NewValidationError("image",
				"Topaz video models enhance an existing video, not a still image. Pass the input video via `inputReferences`.", nil)
		}
		return nil, providererrors.NewValidationError("inputReferences",
			"Topaz video models require an input video. Pass it via `inputReferences`, "+
				"e.g. `inputReferences: [{ type: \"file\", mediaType: \"video/mp4\", data: bytes }]`. "+
				"For URL references, include `mediaType` so the reference is recognized as a video.", nil)
	}

	if len(references) > 1 {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "inputReferences",
			Details: "Topaz video models enhance a single video. Only the first video reference was used.",
		})
	}

	return &videos[0], nil
}

// isVideoReference reports whether file is recognized as a video
// reference, mirroring TS isVideoReference.
func isVideoReference(file *provider.VideoModelV3File) bool {
	if file.Type == "file" {
		return strings.HasPrefix(strings.ToLower(file.MediaType), "video/")
	}
	if file.MediaType != "" {
		return strings.HasPrefix(strings.ToLower(file.MediaType), "video/")
	}
	return containerFromURL(file.URL) != ""
}

// containerFromURL derives a Topaz source container from a URL's file
// extension (TS containerFromUrl).
func containerFromURL(rawURL string) string {
	withoutQuery := rawURL
	if idx := strings.IndexAny(rawURL, "?#"); idx >= 0 {
		withoutQuery = rawURL[:idx]
	}
	idx := strings.LastIndex(withoutQuery, ".")
	if idx < 0 {
		return ""
	}
	extension := strings.ToLower(withoutQuery[idx+1:])
	return extensionContainers[extension]
}

// resolveContainer determines the Topaz source container for the input
// video, mirroring TS resolveContainer.
func resolveContainer(input *provider.VideoModelV3File, topazOpts *VideoModelOptions) (string, error) {
	if topazOpts != nil && topazOpts.Source != nil && topazOpts.Source.Container != nil {
		declared := *topazOpts.Source.Container
		if !topazSourceContainers[declared] {
			return "", providererrors.NewValidationError("providerOptions.topaz.source.container",
				fmt.Sprintf("%q is not a container Topaz accepts for the input video.", declared), nil)
		}
		return declared, nil
	}

	if input.Type == "file" {
		container := mediaTypeContainers[strings.ToLower(input.MediaType)]
		if container == "" {
			return "", providererrors.NewValidationError("inputReferences",
				fmt.Sprintf("Could not map the media type %q onto a Topaz container. Set the `source.container` provider option explicitly.", input.MediaType), nil)
		}
		return container, nil
	}

	container := ""
	if input.MediaType != "" {
		container = mediaTypeContainers[strings.ToLower(input.MediaType)]
	}
	if container == "" {
		container = containerFromURL(input.URL)
	}
	if container == "" {
		return "", providererrors.NewValidationError("inputReferences",
			fmt.Sprintf("Could not determine the container of the input video at %q. Set the `source.container` provider option, or pass `mediaType` on the reference.", input.URL), nil)
	}
	return container, nil
}

// sourceMetadata holds the resolved width/height/duration/frameRate/
// frameCount for the input video.
type sourceMetadata struct {
	duration   float64
	frameRate  float64
	frameCount int
	width      int
	height     int
}

// resolvedSource is the container plus optional metadata resolved for the
// request body.
type resolvedSource struct {
	container string
	metadata  *sourceMetadata
}

// resolveSource reads the optional source metadata, which is all or
// nothing. Starlight models require it, and it lets Topaz estimate the
// cost up front. Nothing is read out of the video bytes: no provider
// package inspects media files (TS resolveSource).
func resolveSource(topazOpts *VideoModelOptions, container string) (resolvedSource, error) {
	var source *VideoSource
	if topazOpts != nil {
		source = topazOpts.Source
	}
	if source == nil {
		return resolvedSource{container: container}, nil
	}

	if source.Width == nil && source.Height == nil && source.Duration == nil &&
		source.FrameRate == nil && source.FrameCount == nil {
		return resolvedSource{container: container}, nil
	}

	var frameCount *int
	if source.FrameCount != nil {
		frameCount = source.FrameCount
	} else if source.Duration != nil && source.FrameRate != nil {
		// Matches TS Math.round(duration * frameRate); int() alone would
		// truncate toward zero instead of rounding.
		n := int(math.Round(*source.Duration * *source.FrameRate))
		frameCount = &n
	}

	var missing []string
	if source.Width == nil {
		missing = append(missing, "source.width")
	}
	if source.Height == nil {
		missing = append(missing, "source.height")
	}
	if source.Duration == nil {
		missing = append(missing, "source.duration")
	}
	if source.FrameRate == nil {
		missing = append(missing, "source.frameRate")
	}

	if source.Width == nil || source.Height == nil || source.Duration == nil ||
		source.FrameRate == nil || frameCount == nil {
		return resolvedSource{}, providererrors.NewValidationError("providerOptions.topaz.source",
			fmt.Sprintf("Source metadata must be complete. Missing: %s.", strings.Join(missing, ", ")), nil)
	}

	return resolvedSource{
		container: container,
		metadata: &sourceMetadata{
			duration:   *source.Duration,
			frameRate:  *source.FrameRate,
			frameCount: *frameCount,
			width:      *source.Width,
			height:     *source.Height,
		},
	}, nil
}

// parseResolution splits a "{width}x{height}" string.
func parseResolution(resolution string) (width, height *int) {
	if resolution == "" {
		return nil, nil
	}
	var w, h int
	if _, err := fmt.Sscanf(resolution, "%dx%d", &w, &h); err != nil {
		return nil, nil
	}
	return &w, &h
}

// buildOutput builds the output request field and resolves the output
// container Topaz will actually produce (TS buildOutput /
// resolveOutputContainer).
func buildOutput(opts *provider.VideoModelV3CallOptions, topazOpts *VideoModelOptions, source resolvedSource) (map[string]interface{}, string, error) {
	var output *VideoOutput
	if topazOpts != nil {
		output = topazOpts.Output
	}
	if output != nil && output.Container != nil && !topazOutputContainers[*output.Container] {
		return nil, "", providererrors.NewValidationError("providerOptions.topaz.output.container",
			fmt.Sprintf("%q is not a container Topaz can produce for the enhanced video.", *output.Container), nil)
	}

	resWidth, resHeight := parseResolution(opts.Resolution)

	width := firstIntPtr(outputWidth(output), resWidth, sourceWidth(source))
	height := firstIntPtr(outputHeight(output), resHeight, sourceHeight(source))

	if width == nil || height == nil {
		return nil, "", providererrors.NewValidationError("resolution",
			"Topaz needs the output resolution. Set the `resolution` call option or the `output.width` / `output.height` provider options.", nil)
	}

	frameRate := firstFloatPtr(outputFrameRate(output), intToFloatPtr(opts.FPS), sourceFrameRate(source))

	audioTransfer := "Copy"
	if output != nil && output.AudioTransfer != nil {
		audioTransfer = *output.AudioTransfer
	}

	result := map[string]interface{}{
		"resolution": map[string]interface{}{"width": *width, "height": *height},
	}
	if frameRate != nil {
		result["frameRate"] = *frameRate
	}
	result["audioTransfer"] = audioTransfer

	if audioTransfer != "None" {
		audioCodec := "AAC"
		if output != nil && output.AudioCodec != nil {
			audioCodec = *output.AudioCodec
		}
		result["audioCodec"] = audioCodec
	} else if output != nil && output.AudioCodec != nil {
		result["audioCodec"] = *output.AudioCodec
	}

	if output != nil {
		if output.AudioBitrate != nil {
			result["audioBitrate"] = *output.AudioBitrate
		}
		if output.VideoEncoder != nil {
			result["videoEncoder"] = *output.VideoEncoder
		}
		if output.VideoProfile != nil {
			result["videoProfile"] = *output.VideoProfile
		}
		if output.VideoBitrate != nil {
			result["videoBitrate"] = *output.VideoBitrate
		}
		if output.DynamicCompressionLevel != nil {
			result["dynamicCompressionLevel"] = *output.DynamicCompressionLevel
		}
		if output.CropToFit != nil {
			result["cropToFit"] = *output.CropToFit
		}
	}

	outputContainer := resolveOutputContainer(output, source.container)
	result["container"] = outputContainer

	return result, outputContainer, nil
}

// resolveOutputContainer mirrors Topaz's container rules so DoStatus can
// report the media type of the file Topaz will actually produce (TS
// resolveOutputContainer).
func resolveOutputContainer(output *VideoOutput, sourceContainer string) string {
	if output != nil && output.VideoEncoder != nil {
		switch *output.VideoEncoder {
		case "ProRes":
			return "mov"
		case "AV1", "VP9":
			return "mp4"
		}
	}

	if output != nil && output.Container != nil {
		return *output.Container
	}

	// The default H265 encoder only writes mp4, mov and mkv.
	if sourceContainer == "mov" || sourceContainer == "mkv" {
		return sourceContainer
	}
	return "mp4"
}

func outputWidth(o *VideoOutput) *int {
	if o == nil {
		return nil
	}
	return o.Width
}

func outputHeight(o *VideoOutput) *int {
	if o == nil {
		return nil
	}
	return o.Height
}

func outputFrameRate(o *VideoOutput) *float64 {
	if o == nil {
		return nil
	}
	return o.FrameRate
}

func sourceWidth(s resolvedSource) *int {
	if s.metadata == nil {
		return nil
	}
	w := s.metadata.width
	return &w
}

func sourceHeight(s resolvedSource) *int {
	if s.metadata == nil {
		return nil
	}
	h := s.metadata.height
	return &h
}

func sourceFrameRate(s resolvedSource) *float64 {
	if s.metadata == nil {
		return nil
	}
	fr := s.metadata.frameRate
	return &fr
}

func firstIntPtr(values ...*int) *int {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstFloatPtr(values ...*float64) *float64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func intToFloatPtr(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

// additionalFilters returns the AdditionalFilters option as a nil-safe
// slice.
func (o *VideoModelOptions) additionalFilters() []map[string]interface{} {
	if o == nil {
		return nil
	}
	return o.AdditionalFilters
}

// uploadVideo uploads the input video's bytes to the presigned upload URL
// Topaz returned, mirroring TS TopazVideoModel#uploadVideo. The upload URL
// comes from the Topaz response body; it is validated and DNS-pinned
// exactly like every other response-supplied URL in this SDK, and -
// because a presigned URL already carries its own credentials - the
// provider's own API key is never attached to this request, trusted origin
// or not.
func (m *VideoModel) uploadVideo(ctx context.Context, requestID string, data []byte, uploadURLs []string, contentType string) error {
	if len(uploadURLs) == 0 {
		return providererrors.NewInvalidResponseDataError(uploadURLs,
			fmt.Sprintf("Topaz returned no upload URL for request %s.", requestID))
	}
	uploadURL := uploadURLs[0]

	opts := fileutil.TrustedOriginDownloadOptions(m.prov.baseURL(), nil)
	if err := opts.URLValidator(uploadURL); err != nil {
		return err
	}

	client := fileutil.NewDownloadClient(uploadURL, opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", version.ProviderUserAgent("topaz"))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := fileutil.ReadResponseWithSizeLimit(resp, uploadURL, fileutil.DefaultMaxDownloadSize)
		apiErr := providererrors.NewProviderError("topaz", resp.StatusCode, "",
			fmt.Sprintf("Uploading the input video failed with status %d.", resp.StatusCode), nil)
		apiErr.ResponseBody = string(body)
		return apiErr
	}
	return nil
}

// cancelQuietly cancels a Topaz video request, best-effort (TS
// TopazVideoModel#cancelQuietly). Deliberately not tied to the caller's
// context, which may be the reason the start failed.
func (m *VideoModel) cancelQuietly(ctx context.Context, requestID string, headers map[string]string) {
	_, _ = m.prov.client.Do(ctx, internalhttp.Request{ //nolint:errcheck
		Method:  http.MethodDelete,
		Path:    "/video/" + requestID,
		Headers: headers,
	})
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

func lowerBoundCredits(estimates *topazVideoEstimates) *float64 {
	if estimates == nil || len(estimates.Cost) == 0 {
		return nil
	}
	min := estimates.Cost[0]
	for _, v := range estimates.Cost[1:] {
		if v < min {
			min = v
		}
	}
	return &min
}

// topazVideoEstimates carries cost (credits) and time (seconds) estimate
// pairs, each [lowerBound, upperBound].
type topazVideoEstimates struct {
	Cost []float64 `json:"cost"`
	Time []float64 `json:"time"`
}

type topazVideoExpressResponse struct {
	RequestID  string               `json:"requestId"`
	UploadURLs []string             `json:"uploadUrls"`
	Estimates  *topazVideoEstimates `json:"estimates"`
}

type topazVideoStatusResponse struct {
	Status     string               `json:"status"`
	Message    *string              `json:"message"`
	ErrorCode  *string              `json:"errorCode"`
	OutputSize interface{}          `json:"outputSize"`
	Estimates  *topazVideoEstimates `json:"estimates"`
	Download   *struct {
		URL       string      `json:"url"`
		ExpiresAt interface{} `json:"expiresAt"`
	} `json:"download"`
}
