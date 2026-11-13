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

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
// topazExpressBodyOrder is the key order of the TS /video/express body
// ({source: {container, duration, frameCount, frameRate, resolution,
// external | size}, output: buildOutput(...), filters: [{model, ...}]}).
var topazExpressBodyOrder = map[string][]string{
	"":                  {"source", "output", "filters"},
	"source":            {"container", "duration", "frameCount", "frameRate", "resolution", "external", "size"},
	"source.resolution": {"width", "height"},
	"source.external":   {"provider", "presignedUrl"},
	"output":            {"resolution", "frameRate", "audioTransfer", "audioCodec", "audioBitrate", "videoEncoder", "videoProfile", "videoBitrate", "dynamicCompressionLevel", "cropToFit", "container"},
	"output.resolution": {"width", "height"},
	"filters":           {"model"},
}

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
	if err := validateVideoOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	currentDate := time.Now()
	var warnings []types.Warning

	topazOpts := parseVideoModelOptions(opts.ProviderOptions)
	// TS resolves the provider headers (loadApiKey) right after the options,
	// before the input checks, and reuses them for the create and the cancel.
	authHeaders, err := m.prov.authHeaders(ctx)
	if err != nil {
		return nil, err
	}
	headers := internalhttp.MergeHeaders(authHeaders, opts.Headers)
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
	// TS `bytes != null` for every file input: an empty file is uploaded
	// too (a zero-length Uint8Array is not nullish).
	upload := input.Type != "url"
	if !upload {
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

	resp, err := m.prov.presetClient.Do(callCtx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/video/express",
		// TS JSON.stringify of the express body literal: the key order of
		// source, output (buildOutput) and the filter, not sorted keys.
		Body:    bytes.NewBufferString(providerutils.StringifyOrdered(body, topazExpressBodyOrder)),
		Headers: headers,
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS returns a rejected headers()/fetch error as is
	}
	// TS response.text() / json(): a BOM is dropped and invalid UTF-8
	// replaced (WHATWG TextDecoder) before the body is parsed.
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	if err := providerutils.MediaResponseValidator("topaz", providerutils.SchemaTopazVideoCreate)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var created topazVideoExpressResponse
	if err := jsonexact.Unmarshal(resp.Body, &created); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode video express response: %w", err)
	}

	requestID := created.RequestID
	// TS requireRequestId: only a missing or null requestId fails, with an
	// InvalidResponseDataError whose data is that value (tools-11).
	if _, present := topazStringField(resp.Body, "requestId"); !present {
		return nil, providererrors.NewInvalidResponseDataError(nil, "Topaz did not return a requestId for the video request.")
	}

	if upload {
		if err := m.uploadVideo(callCtx, requestID, uploadBytes, created.UploadURLs, contentType); err != nil {
			// Canceling before processing starts refunds any reserved
			// credits.
			m.cancelQuietly(context.WithoutCancel(callCtx), requestID, headers)
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
	if created.Estimates != nil && created.Estimates.Cost != nil {
		metadata["estimatedCredits"] = created.Estimates.Cost
	}

	return &provider.VideoModelV3OperationStartResult{
		Operation:        operation,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"topaz": metadata},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
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
		return nil, internalhttp.UnwrapFetchRejection(err) // TS returns a rejected headers()/fetch error as is
	}
	// TS response.text() / json(): a BOM is dropped and invalid UTF-8
	// replaced (WHATWG TextDecoder) before the body is parsed.
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	if err := providerutils.MediaResponseValidator("topaz", providerutils.SchemaTopazVideoStatus)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var status topazVideoStatusResponse
	if err := jsonexact.Unmarshal(resp.Body, &status); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode video status response: %w", err)
	}

	response := provider.VideoModelV3ResponseInfo{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   providerutils.ExtractHeaders(resp.Headers),
	}

	switch status.Status {
	case "complete":
		// Topaz confirmed it invoices the lower bound of estimates.cost,
		// which it recomputes once the source upload has been received.
		credits := lowerBoundCredits(status.Estimates)

		// TS `status.download?.url == null`: an InvalidResponseDataError
		// with the parsed status as data; an empty URL is a URL (tools-11).
		parsedStatus, _ := topazStringField(resp.Body, "status")
		downloadRaw, _ := parsedStatus.(map[string]interface{})["download"].(map[string]interface{})
		if downloadRaw["url"] == nil {
			return nil, providererrors.NewInvalidResponseDataError(topazParsedData(parsedStatus, topazVideoStatusKeys), fmt.Sprintf("Topaz reported request %s complete but returned no download URL.", op.RequestID))
		}

		mediaType := containerMediaTypes[op.OutputContainer]
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		metadata := map[string]interface{}{"requestId": op.RequestID}
		if credits != nil {
			metadata["credits"] = *credits
		}
		if status.Estimates != nil && status.Estimates.Cost != nil {
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

// VideoStartStatusPollDefaults marks the model as start/status only (TS
// TopazVideoModel has no doGenerate), so ai.GenerateVideo drives it through
// the core start/status flow (one idempotency key per start, status retried
// without a second start). It returns the Go-only Config
// VideoPollIntervalMillis / VideoPollTimeoutMillis when positive, else 0
// (the core 5000 / 600000 defaults); GenerateVideoOptions.Poll wins.
// Topaz has no poll provider options. provc CR r89.
func (m *VideoModel) VideoStartStatusPollDefaults(map[string]interface{}) (intervalMs, timeoutMs int) {
	if v := m.prov.config.VideoPollIntervalMillis; v > 0 {
		intervalMs = v
	}
	if v := m.prov.config.VideoPollTimeoutMillis; v > 0 {
		timeoutMs = v
	}
	return intervalMs, timeoutMs
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
	// to call an enhancement model and does not warrant a warning. TS
	// `prompt != null && prompt.trim() !== ''`: a non-blank prompt warns
	// whether or not PromptSet marks it (a direct DoStart caller may leave
	// it unset), and "" is blank either way.
	if providerutils.JSTrim(opts.Prompt) != "" {
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
			return nil, invalidArgument("image",
				"Topaz video models enhance an existing video, not a still image. Pass the input video via `inputReferences`.", nil)
		}
		return nil, invalidArgument("inputReferences",
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
		return strings.HasPrefix(providerutils.JSToLower(file.MediaType), "video/")
	}
	if file.MediaType != "" {
		return strings.HasPrefix(providerutils.JSToLower(file.MediaType), "video/")
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
	// TS withoutQuery.split('.').pop(): with no dot the whole string is the
	// "extension".
	extension := providerutils.JSToLower(withoutQuery[strings.LastIndex(withoutQuery, ".")+1:])
	return extensionContainers[extension]
}

// resolveContainer determines the Topaz source container for the input
// video, mirroring TS resolveContainer.
func resolveContainer(input *provider.VideoModelV3File, topazOpts *VideoModelOptions) (string, error) {
	if topazOpts != nil && topazOpts.Source != nil && topazOpts.Source.Container != nil {
		declared := *topazOpts.Source.Container
		if !topazSourceContainers[declared] {
			return "", invalidArgument("providerOptions.topaz.source.container",
				fmt.Sprintf("%q is not a container Topaz accepts for the input video.", declared), nil)
		}
		return declared, nil
	}

	if input.Type == "file" {
		container := mediaTypeContainers[providerutils.JSToLower(input.MediaType)]
		if container == "" {
			return "", invalidArgument("inputReferences",
				fmt.Sprintf("Could not map the media type \"%s\" onto a Topaz container. Set the `source.container` provider option explicitly.", input.MediaType), nil)
		}
		return container, nil
	}

	container := ""
	if input.MediaType != "" {
		container = mediaTypeContainers[providerutils.JSToLower(input.MediaType)]
	}
	if container == "" {
		container = containerFromURL(input.URL)
	}
	if container == "" {
		return "", invalidArgument("inputReferences",
			fmt.Sprintf("Could not determine the container of the input video at \"%s\". Set the `source.container` provider option, or pass `mediaType` on the reference.", input.URL), nil)
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
		return resolvedSource{}, invalidArgument("providerOptions.topaz.source",
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

// parseResolution is TS `resolution.split('x').map(Number)` destructured to
// [width, height]: each part is JS Number(part) (NaN for non-numeric text, a
// fraction kept), and a missing part is undefined (nil). Nil for an unset
// resolution.
func parseResolution(resolution string) (width, height *float64) {
	if resolution == "" {
		return nil, nil
	}
	parts := strings.Split(resolution, "x")
	num := func(s string) *float64 {
		f, ok := providerutils.JSNumber(s)
		if !ok {
			f = math.NaN()
		}
		return &f
	}
	width = num(parts[0])
	if len(parts) > 1 {
		height = num(parts[1])
	}
	return width, height
}

// resolutionDimension is TS `output?.width ?? resolution.width ??
// source.metadata?.width`: the first value that is not undefined (NaN is
// not nullish), as its JSON value (JSON.stringify writes NaN and Infinity
// as null). ok is false when every value is undefined.
func resolutionDimension(output *int, fromResolution *float64, source *int) (value interface{}, ok bool) {
	switch {
	case output != nil:
		return *output, true
	case fromResolution != nil:
		f := *fromResolution
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, true
		}
		return f, true
	case source != nil:
		return *source, true
	}
	return nil, false
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
		return nil, "", invalidArgument("providerOptions.topaz.output.container",
			fmt.Sprintf("%q is not a container Topaz can produce for the enhanced video.", *output.Container), nil)
	}

	resWidth, resHeight := parseResolution(opts.Resolution)

	width, hasWidth := resolutionDimension(outputWidth(output), resWidth, sourceWidth(source))
	height, hasHeight := resolutionDimension(outputHeight(output), resHeight, sourceHeight(source))

	if !hasWidth || !hasHeight {
		return nil, "", invalidArgument("resolution",
			"Topaz needs the output resolution. Set the `resolution` call option or the `output.width` / `output.height` provider options.", nil)
	}

	frameRate := firstFloatPtr(outputFrameRate(output), intToFloatPtr(opts.FPS), sourceFrameRate(source))

	audioTransfer := "Copy"
	if output != nil && output.AudioTransfer != nil {
		audioTransfer = *output.AudioTransfer
	}

	result := map[string]interface{}{
		"resolution": map[string]interface{}{"width": width, "height": height},
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

	// TS fetchWithValidatedEndpoint (c66402d340) uploads with
	// redirect: 'error': net/http would replay the PUT body (a bytes.Reader
	// sets GetBody) on a 307/308, sending the whole input video to the
	// redirect target, so any redirect fails the upload.
	client := &http.Client{Transport: opts.Transport, Timeout: opts.Timeout}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		return fmt.Errorf("topaz: the input video upload was redirected to %s; redirects are rejected", req.URL.Redacted())
	}
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
		// TS APICallError: responseBody is response.text() (WHATWG UTF-8
		// decode: BOM dropped, invalid bytes replaced) and responseHeaders
		// the lower-cased response headers.
		apiErr.ResponseBody = textdecode.UTF8(body)
		apiErr.ResponseHeaders = make(map[string]string, len(resp.Header))
		for k := range resp.Header {
			apiErr.ResponseHeaders[strings.ToLower(k)] = strings.Join(resp.Header.Values(k), ", ")
		}
		return apiErr
	}
	return nil
}

// cancelQuietly cancels a Topaz video request, best-effort (TS
// TopazVideoModel#cancelQuietly). Deliberately not tied to the caller's
// context, which may be the reason the start failed.
func (m *VideoModel) cancelQuietly(ctx context.Context, requestID string, headers map[string]string) {
	_, _ = m.prov.presetClient.Do(ctx, internalhttp.Request{ //nolint:errcheck
		Method:  http.MethodDelete,
		Path:    "/video/" + requestID,
		Headers: headers,
		// TS calls fetchImpl directly with the resolved headers only (no
		// runtime user-agent tag).
		RawFetch: true,
	})
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
