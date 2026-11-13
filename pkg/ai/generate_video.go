package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// GenerateVideo generates videos using a video model
// This function supports:
// - Text-to-video generation
// - Image-to-video generation
// - Batch generation (multiple videos)
// - Provider-specific options
func GenerateVideo(ctx context.Context, opts GenerateVideoOptions) (*GenerateVideoResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	// Set defaults
	if opts.N == 0 {
		opts.N = 1
	}

	if opts.ProviderOptions == nil {
		opts.ProviderOptions = map[string]interface{}{}
	}

	// Determine max videos per call
	maxPerCall := 1
	if opts.MaxVideosPerCall != nil {
		maxPerCall = *opts.MaxVideosPerCall
	} else if modelMax := opts.Model.MaxVideosPerCall(); modelMax != nil {
		maxPerCall = *modelMax
	}
	if maxPerCall <= 0 {
		return nil, fmt.Errorf("maxVideosPerCall must be > 0")
	}

	// Calculate number of API calls needed
	callCount := (opts.N + maxPerCall - 1) / maxPerCall

	// Execute parallel generation if multiple calls needed
	if callCount > 1 {
		return parallelGenerate(ctx, opts, maxPerCall, callCount)
	}

	// Single API call
	return singleGenerate(ctx, opts)
}

// singleGenerate handles a single video generation API call
func singleGenerate(ctx context.Context, opts GenerateVideoOptions) (*GenerateVideoResult, error) {
	normalized, err := normalizeVideoInputs(opts.Prompt, opts.FrameImages, opts.InputReferences)
	if err != nil {
		return nil, err
	}

	warnVideoPollWithoutStartStatus(opts)
	response, err := runVideoCall(ctx, opts, normalized, opts.N)
	if err != nil {
		return nil, err
	}
	response.Warnings = append(append([]types.Warning{}, normalized.Warnings...), response.Warnings...)

	return convertToGenerateVideoResult(ctx, response, opts.Model, opts.Download, opts.DownloadWithMetadata)
}

// parallelGenerate handles batch generation with multiple parallel API calls
func parallelGenerate(ctx context.Context, opts GenerateVideoOptions, maxPerCall, callCount int) (*GenerateVideoResult, error) {
	normalized, err := normalizeVideoInputs(opts.Prompt, opts.FrameImages, opts.InputReferences)
	if err != nil {
		return nil, err
	}

	warnVideoPollWithoutStartStatus(opts)

	type callResult struct {
		index    int
		response *provider.VideoModelV3Response
		err      error
	}

	resultChan := make(chan callResult, callCount)
	var wg sync.WaitGroup

	// Launch parallel API calls
	for i := 0; i < callCount; i++ {
		wg.Add(1)

		remaining := opts.N - (i * maxPerCall)
		n := min(remaining, maxPerCall)

		go func(index int, n int) {
			defer wg.Done()
			resp, err := runVideoCall(ctx, opts, normalized, n)
			resultChan <- callResult{index: index, response: resp, err: err}
		}(i, n)
	}

	// Wait for all calls to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	orderedResponses := make([]*provider.VideoModelV3Response, callCount)

	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		orderedResponses[result.index] = result.response
	}

	var videos []*types.GeneratedFile
	uploadedVideos := 0
	warnings := append([]types.Warning{}, normalized.Warnings...)
	var responses []VideoModelResponseMetadata
	var providerMetadata []map[string]interface{}

	for _, response := range orderedResponses {
		// Convert video data
		files, uploaded, err := convertVideoList(ctx, response.Videos, opts.Download, opts.DownloadWithMetadata)
		if err != nil {
			return nil, err
		}
		videos = append(videos, files...)
		uploadedVideos += uploaded

		// Collect warnings
		warnings = append(warnings, response.Warnings...)

		// Collect response metadata
		responses = append(responses, convertResponseInfo(response.Response, response.ProviderMetadata, opts.Model))

		// Collect provider metadata
		if response.ProviderMetadata != nil {
			providerMetadata = append(providerMetadata, response.ProviderMetadata)
		}
	}

	// Check if any videos were generated (TS 2e44144812: videos delivered to
	// the upload destination count too).
	if len(videos) == 0 && uploadedVideos == 0 {
		return nil, &NoVideoGeneratedError{Responses: responses}
	}

	logModelWarnings(warnings, opts.Model.Provider(), opts.Model.ModelID())

	return newGenerateVideoResult(videos, uploadedVideos, warnings, responses, mergeProviderMetadata(providerMetadata)), nil
}

// newGenerateVideoResult builds the result; Video is the first video
// available to the SDK, nil for an upload-only result (TS throws from the
// video getter; known difference).
func newGenerateVideoResult(videos []*types.GeneratedFile, uploaded int, warnings []types.Warning, responses []VideoModelResponseMetadata, providerMetadata map[string]interface{}) *GenerateVideoResult {
	if videos == nil {
		videos = []*types.GeneratedFile{}
	}
	var first *types.GeneratedFile
	if len(videos) > 0 {
		first = videos[0]
	}
	return &GenerateVideoResult{
		Video:            first,
		Videos:           videos,
		UploadedVideos:   uploaded,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: providerMetadata,
	}
}

// convertVideoList converts the provider's videos, counting (not
// downloading) those the provider delivered to the caller's upload
// destination (type "uploaded").
func convertVideoList(ctx context.Context, data []provider.VideoModelV3VideoData, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) ([]*types.GeneratedFile, int, error) {
	files := make([]*types.GeneratedFile, 0, len(data))
	uploaded := 0
	for _, videoData := range data {
		if videoData.Type == provider.VideoDataTypeUploaded {
			uploaded++
			continue
		}
		file, err := convertVideoData(ctx, videoData, download, downloadWithMetadata)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, file)
	}
	return files, uploaded, nil
}

// runVideoCall executes a single provider call for n videos, either via
// DoGenerate (with retry) or, when the model supports it and Poll/Webhook was
// requested, via the asynchronous start/status flow.
func runVideoCall(ctx context.Context, opts GenerateVideoOptions, normalized *normalizedVideoInputs, n int) (*provider.VideoModelV3Response, error) {
	callOpts := &provider.VideoModelV3CallOptions{
		Prompt:          normalized.Prompt,
		PromptSet:       videoPromptTextProvided(opts.Prompt),
		N:               n,
		AspectRatio:     opts.AspectRatio,
		Resolution:      opts.Resolution,
		Duration:        opts.Duration,
		FPS:             opts.FPS,
		Seed:            opts.Seed,
		Image:           normalized.Image,
		FrameImages:     normalized.FrameImages,
		InputReferences: normalized.InputReferences,
		GenerateAudio:   opts.GenerateAudio,
		ProviderOptions: opts.ProviderOptions,
		AbortSignal:     ctx,
		Headers:         videoHeadersWithUserAgent(opts.Headers),
	}

	// Determine whether to use the start/status flow (TS useStartStatus:
	// hasStartStatus && (poll != null || webhook != null || model.doGenerate
	// == null)). VideoModelV3 always has DoGenerate, so a model whose TS twin
	// implements only doStart/doStatus marks itself with
	// videoStartStatusOnlyModel; its DoGenerate is a convenience shim that
	// GenerateVideo does not use.
	starter, hasStarter := opts.Model.(provider.VideoModelStarter)
	checker, hasChecker := opts.Model.(provider.VideoModelStatusChecker)
	hasStartStatus := hasStarter && hasChecker
	startStatusOnly, isStartStatusOnly := opts.Model.(videoStartStatusOnlyModel)
	useStartStatus := hasStartStatus && (opts.Poll != nil || opts.Webhook != nil || isStartStatusOnly)

	if useStartStatus {
		flow := startStatusFlowOptions{
			model:      opts.Model,
			starter:    starter,
			checker:    checker,
			callOpts:   callOpts,
			poll:       opts.Poll,
			webhook:    opts.Webhook,
			maxRetries: opts.MaxRetries,
		}
		if isStartStatusOnly {
			flow.defaultIntervalMs, flow.defaultTimeoutMs = startStatusOnly.VideoStartStatusPollDefaults(opts.ProviderOptions)
		}
		statusResult, err := executeStartStatusFlow(ctx, flow)
		if err != nil {
			return nil, err
		}
		return &provider.VideoModelV3Response{
			Videos:           statusResult.Videos,
			Warnings:         statusResult.Warnings,
			ProviderMetadata: statusResult.ProviderMetadata,
			Response:         statusResult.Response,
		}, nil
	}

	return doGenerateVideoWithRetry(ctx, opts.Model, callOpts, opts.MaxRetries)
}

// warnVideoPollWithoutStartStatus logs, once per GenerateVideo call (TS logs
// it before the calls are parallelized), that Poll / Webhook cannot be used
// because the model has no start/status flow.
func warnVideoPollWithoutStartStatus(opts GenerateVideoOptions) {
	if opts.Poll == nil && opts.Webhook == nil {
		return
	}
	_, hasStarter := opts.Model.(provider.VideoModelStarter)
	_, hasChecker := opts.Model.(provider.VideoModelStatusChecker)
	if hasStarter && hasChecker {
		return
	}
	logModelWarnings([]types.Warning{{
		Type:    "other",
		Message: "poll/webhook options were provided but the model does not support doStart/doStatus. Falling back to doGenerate.",
	}}, opts.Model.Provider(), opts.Model.ModelID())
}

func doGenerateVideoWithRetry(ctx context.Context, model provider.VideoModelV3, opts *provider.VideoModelV3CallOptions, maxRetries *int) (*provider.VideoModelV3Response, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries == 0 {
		return model.DoGenerate(ctx, opts)
	}

	var raw *provider.VideoModelV3Response
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var genErr error
		raw, genErr = model.DoGenerate(retryCtx, opts)
		return genErr
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// startStatusFlowOptions bundles the inputs to executeStartStatusFlow.
type startStatusFlowOptions struct {
	model      provider.VideoModelV3
	starter    provider.VideoModelStarter
	checker    provider.VideoModelStatusChecker
	callOpts   *provider.VideoModelV3CallOptions
	poll       *VideoPollOptions
	webhook    provider.VideoWebhookFactory
	maxRetries *int
	// defaultIntervalMs / defaultTimeoutMs replace the core defaults (5000 /
	// 600000) when positive and Poll leaves the value unset.
	defaultIntervalMs int
	defaultTimeoutMs  int
}

// videoStartStatusOnlyModel is implemented by a video model whose TS twin has
// no doGenerate (only doStart/doStatus), such as KlingAI, fal, Replicate and
// ByteDance. TS generateVideo always drives such a model through the core
// start/status flow: one idempotency key per start, doStatus retried on its
// own (never a second billable start), and the core poll defaults and error
// texts. VideoStartStatusPollDefaults returns the poll interval and timeout
// in milliseconds that the model's v0.5.2 provider options set (0 for the
// core default); GenerateVideoOptions.Poll values win over them.
type videoStartStatusOnlyModel interface {
	VideoStartStatusPollDefaults(providerOptions map[string]interface{}) (intervalMs, timeoutMs int)
}

// videoPollingTimeoutError is returned when a polling loop exceeds its
// configured timeout, mirroring the TS pollingTimeoutError.
type videoPollingTimeoutError struct {
	timeoutMs int
}

func (e *videoPollingTimeoutError) Error() string {
	return fmt.Sprintf("Video generation timed out after %dms.", e.timeoutMs)
}

// executeStartStatusFlow orchestrates the asynchronous start/status flow:
// start the generation, then either wait for a webhook notification or poll
// DoStatus until the operation reaches a terminal state. Mirrors TS
// generate-video.ts executeStartStatusFlow.
func executeStartStatusFlow(ctx context.Context, opts startStatusFlowOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	var earlyWarnings []types.Warning
	var webhookURL string
	var webhookReceived provider.VideoWebhookReceived

	if opts.webhook != nil {
		if handler, ok := opts.model.(provider.VideoModelWebhookHandler); ok {
			url, received, err := handler.HandleWebhookOption(ctx, opts.webhook)
			if err != nil {
				return nil, err
			}
			webhookURL = url
			webhookReceived = received
		} else {
			earlyWarnings = append(earlyWarnings, types.Warning{
				Type:    "unsupported",
				Feature: "webhook",
				Details: "This model does not support webhooks. Falling back to polling.",
			})
		}
	}

	// doStart is billable: mint one idempotency token per logical start,
	// outside the retry loop; a caller-supplied key wins.
	startHeaders := videoIdempotencyHeaders(opts.callOpts.Headers)
	startOpts := &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: *opts.callOpts,
		WebhookURL:              webhookURL,
	}
	startOpts.Headers = startHeaders

	startResult, err := doStartVideoWithRetry(ctx, opts.starter, startOpts, opts.maxRetries)
	if err != nil {
		return nil, err
	}

	allWarnings := append(append([]types.Warning{}, earlyWarnings...), startResult.Warnings...)
	var operationProviderMetadata map[string]interface{}
	if startResult.ProviderMetadata != nil {
		operationProviderMetadata = map[string]interface{}{}
		for k, v := range startResult.ProviderMetadata {
			operationProviderMetadata[k] = v
		}
	}

	intervalMs := 5000
	if opts.poll != nil && opts.poll.IntervalMs != nil {
		intervalMs = *opts.poll.IntervalMs
	} else if opts.defaultIntervalMs > 0 {
		intervalMs = opts.defaultIntervalMs
	}
	timeoutMs := 600000
	if opts.poll != nil && opts.poll.TimeoutMs != nil {
		timeoutMs = *opts.poll.TimeoutMs
	} else if opts.defaultTimeoutMs > 0 {
		timeoutMs = opts.defaultTimeoutMs
	}
	delayFn := defaultVideoPollDelay
	if opts.poll != nil && opts.poll.Delay != nil {
		delayFn = opts.poll.Delay
	}
	startTime := time.Now()
	timeoutErr := &videoPollingTimeoutError{timeoutMs: timeoutMs}

	if webhookReceived != nil {
		if err := waitForVideoWebhook(ctx, webhookReceived, timeoutMs, delayFn); err != nil {
			return nil, err
		}
	}

	for {
		if webhookReceived == nil {
			elapsedMs := int(time.Since(startTime).Milliseconds())
			if elapsedMs >= timeoutMs {
				return nil, timeoutErr
			}
			remaining := timeoutMs - elapsedMs
			wait := intervalMs
			if remaining < wait {
				wait = remaining
			}
			if err := delayFn(ctx, time.Duration(wait)*time.Millisecond); err != nil {
				return nil, err
			}
			if int(time.Since(startTime).Milliseconds()) >= timeoutMs {
				return nil, timeoutErr
			}
		}

		statusOpts := &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.callOpts.Headers,
		}

		var statusResult *provider.VideoModelV3OperationStatusResult
		if webhookReceived != nil {
			statusResult, err = doStatusVideoWithRetry(ctx, opts.checker, statusOpts, opts.maxRetries)
			if err != nil {
				return nil, err
			}
		} else {
			remaining := timeoutMs - int(time.Since(startTime).Milliseconds())
			if remaining < 0 {
				remaining = 0
			}
			statusCtx, cancel := context.WithTimeout(ctx, time.Duration(remaining)*time.Millisecond)

			// Race the status call against the timeout instead of just
			// passing statusCtx through: a provider whose DoStatus ignores
			// context cancellation would otherwise keep this call blocked
			// past the deadline, since context.WithTimeout only cancels
			// statusCtx, it does not forcibly interrupt an in-flight call.
			// TS enforces the same SDK-level deadline unconditionally via
			// Promise.race(statusRetry(...), statusTimeoutPromise).
			type statusOutcome struct {
				result *provider.VideoModelV3OperationStatusResult
				err    error
			}
			outcomeCh := make(chan statusOutcome, 1)
			go func() {
				r, e := doStatusVideoWithRetry(statusCtx, opts.checker, statusOpts, opts.maxRetries)
				outcomeCh <- statusOutcome{result: r, err: e}
			}()

			// Only the poll deadline is the polling timeout (TS
			// statusTimeoutController.signal.aborted); the caller's own
			// cancellation or deadline ends the call with its error.
			pollDeadline := func() bool { return ctx.Err() == nil && statusCtx.Err() == context.DeadlineExceeded }
			select {
			case outcome := <-outcomeCh:
				cancel()
				if outcome.err != nil {
					if pollDeadline() {
						return nil, timeoutErr
					}
					return nil, outcome.err
				}
				statusResult = outcome.result
			case <-statusCtx.Done():
				timedOut := pollDeadline()
				cancel()
				if timedOut {
					return nil, timeoutErr
				}
				// TS Promise.race settles with doStatus's rejection: the
				// abort reason.
				return nil, context.Cause(ctx)
			}
		}

		if statusResult.Status == provider.VideoOperationStatusError {
			return nil, errors.New(statusResult.Error)
		}

		if statusResult.Warnings != nil {
			allWarnings = append(allWarnings, statusResult.Warnings...)
		}
		if statusResult.ProviderMetadata != nil {
			if operationProviderMetadata == nil {
				operationProviderMetadata = map[string]interface{}{}
			}
			for k, v := range statusResult.ProviderMetadata {
				operationProviderMetadata[k] = mergeProviderMetadataValue(operationProviderMetadata[k], v)
			}
		}

		if statusResult.Status == provider.VideoOperationStatusCompleted {
			return &provider.VideoModelV3OperationStatusResult{
				Status:           provider.VideoOperationStatusCompleted,
				Videos:           statusResult.Videos,
				Warnings:         allWarnings,
				ProviderMetadata: operationProviderMetadata,
				Response:         statusResult.Response,
			}, nil
		}

		if webhookReceived != nil {
			return nil, errors.New("Video generation did not complete after webhook notification.") //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
}

func doStartVideoWithRetry(ctx context.Context, starter provider.VideoModelStarter, opts *provider.VideoModelV3StartOptions, maxRetries *int) (*provider.VideoModelV3OperationStartResult, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries == 0 {
		return starter.DoStart(ctx, opts)
	}
	var raw *provider.VideoModelV3OperationStartResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var startErr error
		raw, startErr = starter.DoStart(retryCtx, opts)
		return startErr
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func doStatusVideoWithRetry(ctx context.Context, checker provider.VideoModelStatusChecker, opts *provider.VideoModelV3StatusOptions, maxRetries *int) (*provider.VideoModelV3OperationStatusResult, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries == 0 {
		return checker.DoStatus(ctx, opts)
	}
	var raw *provider.VideoModelV3OperationStatusResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var statusErr error
		raw, statusErr = checker.DoStatus(retryCtx, opts)
		return statusErr
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// waitForVideoWebhook blocks until the webhook notification arrives, ctx is
// done, or timeoutMs elapses.
func waitForVideoWebhook(ctx context.Context, received provider.VideoWebhookReceived, timeoutMs int, delayFn func(context.Context, time.Duration) error) error {
	timeoutCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan error, 1)
	go func() {
		_, err := received(timeoutCtx)
		resultCh <- err
	}()

	timerErrCh := make(chan error, 1)
	go func() {
		if err := delayFn(timeoutCtx, time.Duration(timeoutMs)*time.Millisecond); err != nil {
			timerErrCh <- err
			return
		}
		timerErrCh <- fmt.Errorf("Video generation timed out after %dms.", timeoutMs) //nolint:staticcheck // matches TS SDK's exact error text
	}()

	select {
	case err := <-resultCh:
		return err
	case err := <-timerErrCh:
		return err
	}
}

func defaultVideoPollDelay(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// videoIdempotencyHeaders returns headers with an "idempotency-key" set to a
// freshly generated value, unless the caller already supplied one (any case).
func videoIdempotencyHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers)+1)
	hasKey := false
	for k, v := range headers {
		out[k] = v
		if strings.EqualFold(k, "idempotency-key") {
			hasKey = true
		}
	}
	if !hasKey {
		out["idempotency-key"] = newVideoIdempotencyKey()
	}
	return out
}

func newVideoIdempotencyKey() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return "aisdk_vid_" + hex.EncodeToString(buf)
}

// convertPromptImage converts VideoPromptImage to VideoModelV3File.
func convertPromptImage(img *VideoPromptImage) (*provider.VideoModelV3File, error) {
	if img == nil {
		return nil, nil
	}

	if img.URL != "" {
		return &provider.VideoModelV3File{
			Type:      "url",
			URL:       img.URL,
			MediaType: img.MediaType,
		}, nil
	}

	// TS normalizeImageData: a DataContent string that starts with http://
	// or https:// is a URL file, not base64.
	if isVideoHTTPDataString(img.DataString) {
		return &provider.VideoModelV3File{Type: "url", URL: img.DataString, MediaType: img.MediaType}, nil
	}

	if img.DataString != "" {
		data, mediaType, err := decodeVideoPromptImageDataString(img.DataString)
		if err != nil {
			return nil, fmt.Errorf("invalid video prompt image data string: %w", err)
		}
		return &provider.VideoModelV3File{
			Type:      "file",
			Data:      data,
			MediaType: mediaType,
		}, nil
	}

	if len(img.Data) > 0 {
		mediaType := img.MediaType
		if mediaType == "" {
			mediaType = detectVideoPromptImageMediaType(img.Data)
		}

		return &provider.VideoModelV3File{
			Type:      "file",
			Data:      img.Data,
			MediaType: mediaType,
		}, nil
	}

	return nil, nil
}

// isVideoHTTPDataString is the TS normalizeImageData URL check on a
// DataContent string (case-sensitive prefixes).
func isVideoHTTPDataString(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// splitVideoDataURL mirrors TS splitDataUrl: the header is everything before
// the first comma, the media type is header.split(';')[0].split(':')[1] (so an
// empty type stays "" and is not replaced by detection), and the content is
// the second comma-separated part only.
func splitVideoDataURL(value string) (mediaType, content string) {
	header, rest, _ := strings.Cut(value, ",")
	if parts := strings.Split(strings.SplitN(header, ";", 2)[0], ":"); len(parts) > 1 {
		mediaType = parts[1]
	}
	content, _, _ = strings.Cut(rest, ",")
	return mediaType, content
}

func decodeVideoPromptImageDataString(value string) ([]byte, string, error) {
	mediaType := ""
	content := value
	isDataURL := false
	if strings.HasPrefix(value, "data:") {
		isDataURL = true
		mediaType, content = splitVideoDataURL(value)
	}

	data, err := fileutil.ConvertBase64ToBytes(content)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "" && !isDataURL {
		mediaType = detectVideoPromptImageMediaType(data)
	}
	return data, mediaType, nil
}

// detectVideoPromptImageMediaType detects an image media type from the bytes
// of a prompt or frame image, falling back to image/png (TS
// detectFileMediaType with restrictToImages).
func detectVideoPromptImageMediaType(data []byte) string {
	if mt, ok := fileutil.DetectMediaTypeSignature(data, "image"); ok {
		return mt
	}
	return "image/png"
}

// normalizedVideoInputs holds the shared, provider-ready video call inputs
// resolved from a GenerateVideo/StartVideo caller's prompt, frameImages and
// inputReferences options, mirroring TS normalizeVideoCallInputs.
type normalizedVideoInputs struct {
	Prompt          string
	Image           *provider.VideoModelV3File
	FrameImages     []provider.VideoFrameImage
	InputReferences []provider.VideoModelV3File
	Warnings        []types.Warning
}

// normalizeVideoInputs applies the shared frameImages/inputReferences
// precedence rules used by GenerateVideo and StartVideo:
//   - inputReferences are ignored (with a warning) when frameImages are also
//     provided; the two cannot be combined.
//   - prompt.image is ignored (with a warning) when a first_frame frameImage
//     is provided; the frameImage takes precedence as the start image.
func normalizeVideoInputs(prompt VideoPrompt, frameImages []VideoFrameImageInput, inputReferences []VideoReferenceInput) (*normalizedVideoInputs, error) {
	image, err := convertPromptImage(prompt.Image)
	if err != nil {
		return nil, err
	}

	var normalizedFrameImages []provider.VideoFrameImage
	for _, frame := range frameImages {
		converted, err := convertPromptImage(&frame.Image)
		if err != nil {
			return nil, err
		}
		if converted == nil {
			continue
		}
		normalizedFrameImages = append(normalizedFrameImages, provider.VideoFrameImage{
			Image:     *converted,
			FrameType: frame.FrameType,
		})
	}

	var normalizedInputReferences []provider.VideoModelV3File
	for _, ref := range inputReferences {
		converted, err := convertReferenceImage(&ref.Data)
		if err != nil {
			return nil, err
		}
		if converted == nil {
			continue
		}
		normalizedInputReferences = append(normalizedInputReferences, *converted)
	}

	effectiveInputReferences := normalizedInputReferences
	var warnings []types.Warning

	if len(normalizedFrameImages) > 0 && len(normalizedInputReferences) > 0 {
		effectiveInputReferences = nil
		warnings = append(warnings, types.Warning{
			Type: "other",
			Message: "inputReferences were ignored because frameImages were provided; " +
				"frameImages and inputReferences cannot be combined.",
		})
	}

	var firstFrameImage *provider.VideoModelV3File
	for i := range normalizedFrameImages {
		if normalizedFrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			firstFrameImage = &normalizedFrameImages[i].Image
			break
		}
	}

	if image != nil && firstFrameImage != nil {
		warnings = append(warnings, types.Warning{
			Type: "other",
			Message: "prompt.image was ignored because a first_frame frameImage was provided; " +
				"the first_frame frameImage takes precedence as the start image.",
		})
	}

	resolvedImage := image
	if firstFrameImage != nil {
		resolvedImage = firstFrameImage
	}

	return &normalizedVideoInputs{
		Prompt:          prompt.Text,
		Image:           resolvedImage,
		FrameImages:     normalizedFrameImages,
		InputReferences: effectiveInputReferences,
		Warnings:        warnings,
	}, nil
}

// convertReferenceImage converts a VideoPromptImage input reference to a
// VideoModelV3File. Unlike convertPromptImage (used for prompt.image and
// frameImages, which are always images), a reference may be an image or a
// video: when no explicit MediaType is given, detection considers both image
// and video signatures instead of defaulting to an image type.
func convertReferenceImage(img *VideoPromptImage) (*provider.VideoModelV3File, error) {
	if img == nil {
		return nil, nil
	}

	if img.URL != "" {
		return &provider.VideoModelV3File{Type: "url", URL: img.URL, MediaType: img.MediaType}, nil
	}

	if isVideoHTTPDataString(img.DataString) {
		return &provider.VideoModelV3File{Type: "url", URL: img.DataString, MediaType: img.MediaType}, nil
	}

	if img.DataString != "" {
		data, mediaType, err := decodeVideoReferenceDataString(img.DataString, img.MediaType)
		if err != nil {
			return nil, fmt.Errorf("invalid video reference data string: %w", err)
		}
		return &provider.VideoModelV3File{Type: "file", Data: data, MediaType: mediaType}, nil
	}

	if len(img.Data) > 0 {
		mediaType := img.MediaType
		if mediaType == "" {
			mediaType = detectAnyFileMediaType(img.Data)
		}
		return &provider.VideoModelV3File{Type: "file", Data: img.Data, MediaType: mediaType}, nil
	}

	return nil, nil
}

func decodeVideoReferenceDataString(value string, explicitMediaType string) ([]byte, string, error) {
	mediaType := explicitMediaType
	content := value
	isDataURL := false
	if strings.HasPrefix(value, "data:") {
		isDataURL = true
		var urlType string
		urlType, content = splitVideoDataURL(value)
		if mediaType == "" {
			mediaType = urlType
		}
	}

	data, err := fileutil.ConvertBase64ToBytes(content)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "" && !isDataURL {
		mediaType = detectAnyFileMediaType(data)
	}
	return data, mediaType, nil
}

// detectAnyFileMediaType detects the media type of a reference's bytes across
// every known signature (image, video, audio, document), defaulting to
// "image/png" when none matches (TS detectFileMediaType without
// restrictToImages).
func detectAnyFileMediaType(data []byte) string {
	if mt, ok := fileutil.DetectMediaTypeSignature(data, ""); ok {
		return mt
	}
	return "image/png"
}

// convertVideoData converts VideoModelV3VideoData to GeneratedFile.
func convertVideoData(ctx context.Context, data provider.VideoModelV3VideoData, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) (*types.GeneratedFile, error) {
	switch data.Type {
	case "url":
		videoBytes, downloadedMediaType, err := downloadVideoURL(ctx, data.URL, download, downloadWithMetadata)
		if err != nil {
			return nil, err
		}

		mediaType := data.MediaType
		if !isUsableVideoMediaType(mediaType) {
			mediaType = downloadedMediaType
		}
		if !isUsableVideoMediaType(mediaType) {
			mediaType = detectVideoBytesMediaType(videoBytes)
		}
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		return &types.GeneratedFile{
			Data:      videoBytes,
			MediaType: mediaType,
		}, nil

	case "base64":
		decoded, err := fileutil.ConvertBase64ToBytes(data.Data)
		if err != nil {
			return nil, err
		}
		mediaType := data.MediaType
		if mediaType == "" {
			mediaType = "video/mp4"
		}
		return &types.GeneratedFile{
			Data:      decoded,
			MediaType: mediaType,
		}, nil

	case "binary":
		mediaType := data.MediaType
		if mediaType == "" && len(data.Binary) > 0 {
			mediaType = detectVideoBytesMediaType(data.Binary)
		}
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		return &types.GeneratedFile{
			Data:      data.Binary,
			MediaType: mediaType,
		}, nil

	default:
		return nil, fmt.Errorf("unknown video data type: %s", data.Type)
	}
}

// convertToGenerateVideoResult converts provider response to GenerateVideoResult
func convertToGenerateVideoResult(ctx context.Context, response *provider.VideoModelV3Response, model provider.VideoModelV3, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) (*GenerateVideoResult, error) {
	responses := []VideoModelResponseMetadata{convertResponseInfo(response.Response, response.ProviderMetadata, model)}
	if len(response.Videos) == 0 {
		return nil, &NoVideoGeneratedError{Responses: responses}
	}

	videos, uploaded, err := convertVideoList(ctx, response.Videos, download, downloadWithMetadata)
	if err != nil {
		return nil, err
	}
	warnings := response.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	logModelWarnings(warnings, model.Provider(), model.ModelID())

	return newGenerateVideoResult(videos, uploaded, warnings, responses, mergeProviderMetadata([]map[string]interface{}{response.ProviderMetadata})), nil
}

func downloadVideoURL(ctx context.Context, url string, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) ([]byte, string, error) {
	// TS downloads new URL(videoData.url): the WHATWG serialization (a URL
	// that does not parse is used as given, like downloadGeneratedFileURL).
	if href, err := urlnorm.Href(url); err == nil {
		url = href
	}
	if downloadWithMetadata != nil {
		result, err := downloadWithMetadata(ctx, url)
		if err != nil {
			return nil, "", err
		}
		if result == nil {
			return nil, "", fmt.Errorf("download returned nil result for %s", url)
		}
		return result.Data, result.MediaType, nil
	}
	if download != nil {
		data, err := download(ctx, url)
		return data, "", err
	}

	opts := fileutil.DefaultDownloadOptions()
	opts.URLValidator = downloadURLValidator
	opts.Transport = downloadTransport()
	// TS createDownload() sets no timeout: a large video on a slow link must
	// not be cut off after 60 s; ctx still cancels it.
	opts.Timeout = fileutil.NoTimeout
	result, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
}

// detectVideoBytesMediaType detects a video media type from bytes (TS
// detectMediaType with topLevelType video); empty when no signature matches.
func detectVideoBytesMediaType(data []byte) string {
	mt, _ := fileutil.DetectMediaTypeSignature(data, "video")
	return mt
}

func isUsableVideoMediaType(mediaType string) bool {
	return mediaType != "" && mediaType != "application/octet-stream"
}

func videoHeadersWithUserAgent(headers map[string]string) map[string]string {
	return version.WithUserAgentSuffix(headers, version.UserAgent())
}

func videoPromptTextProvided(prompt VideoPrompt) bool {
	return prompt.Image == nil || prompt.Text != ""
}

// convertResponseInfo converts VideoModelV3ResponseInfo to VideoModelResponseMetadata.
func convertResponseInfo(info provider.VideoModelV3ResponseInfo, providerMetadata map[string]interface{}, model provider.VideoModelV3) VideoModelResponseMetadata {
	if info.Timestamp.IsZero() {
		info.Timestamp = time.Now()
	}
	if info.ModelID == "" && model != nil {
		info.ModelID = model.ModelID()
	}
	return VideoModelResponseMetadata{
		Timestamp:        info.Timestamp,
		ModelID:          info.ModelID,
		Headers:          info.Headers,
		ProviderMetadata: providerMetadata,
	}
}

// mergeProviderMetadata merges multiple provider metadata maps
func mergeProviderMetadata(metadataList []map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for _, metadata := range metadataList {
		for providerName, value := range metadata {
			existing, ok := merged[providerName]
			if !ok {
				merged[providerName] = value
				continue
			}
			merged[providerName] = mergeProviderMetadataValue(existing, value)
		}
	}

	return merged
}

func mergeProviderMetadataValue(existing, next interface{}) interface{} {
	existingMap, existingOK := existing.(map[string]interface{})
	nextMap, nextOK := next.(map[string]interface{})
	if !existingOK || !nextOK {
		return next
	}

	merged := make(map[string]interface{}, len(existingMap)+len(nextMap))
	for k, v := range existingMap {
		merged[k] = v
	}
	for k, v := range nextMap {
		if k == "videos" {
			if existingVideos, ok := sliceToInterfaces(merged[k]); ok {
				if nextVideos, ok := sliceToInterfaces(v); ok {
					merged[k] = append(existingVideos, nextVideos...)
					continue
				}
			}
		}
		merged[k] = v
	}
	return merged
}

func sliceToInterfaces(value interface{}) ([]interface{}, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
