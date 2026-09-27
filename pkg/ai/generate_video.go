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
	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
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
	warnings := append([]types.Warning{}, normalized.Warnings...)
	var responses []VideoModelResponseMetadata
	var providerMetadata []map[string]interface{}

	for _, response := range orderedResponses {
		// Convert video data
		for _, videoData := range response.Videos {
			file, err := convertVideoData(ctx, videoData, opts.Download, opts.DownloadWithMetadata)
			if err != nil {
				return nil, err
			}
			videos = append(videos, file)
		}

		// Collect warnings
		warnings = append(warnings, response.Warnings...)

		// Collect response metadata
		responses = append(responses, convertResponseInfo(response.Response, response.ProviderMetadata, opts.Model))

		// Collect provider metadata
		if response.ProviderMetadata != nil {
			providerMetadata = append(providerMetadata, response.ProviderMetadata)
		}
	}

	// Check if any videos were generated
	if len(videos) == 0 {
		return nil, &NoVideoGeneratedError{Responses: responses}
	}

	logModelWarnings(warnings, opts.Model.Provider(), opts.Model.ModelID())

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: mergeProviderMetadata(providerMetadata),
	}, nil
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

	// Determine whether to use the start/status flow: hasStartStatus &&
	// (Poll != nil || Webhook != nil). Unlike TS, DoGenerate is always
	// present on VideoModelV3, so there is no "model has no doGenerate" case.
	starter, hasStarter := opts.Model.(provider.VideoModelStarter)
	checker, hasChecker := opts.Model.(provider.VideoModelStatusChecker)
	hasStartStatus := hasStarter && hasChecker
	useStartStatus := hasStartStatus && (opts.Poll != nil || opts.Webhook != nil)

	if (opts.Poll != nil || opts.Webhook != nil) && !hasStartStatus {
		logModelWarnings([]types.Warning{{
			Type:    "other",
			Message: "poll/webhook options were provided but the model does not support doStart/doStatus. Falling back to doGenerate.",
		}}, opts.Model.Provider(), opts.Model.ModelID())
	}

	if useStartStatus {
		statusResult, err := executeStartStatusFlow(ctx, startStatusFlowOptions{
			model:      opts.Model,
			starter:    starter,
			checker:    checker,
			callOpts:   callOpts,
			poll:       opts.Poll,
			webhook:    opts.Webhook,
			maxRetries: opts.MaxRetries,
		})
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

func doGenerateVideoWithRetry(ctx context.Context, model provider.VideoModelV3, opts *provider.VideoModelV3CallOptions, maxRetries *int) (*provider.VideoModelV3Response, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries == 0 {
		return model.DoGenerate(ctx, opts)
	}

	var raw *provider.VideoModelV3Response
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
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
	}
	timeoutMs := 600000
	if opts.poll != nil && opts.poll.TimeoutMs != nil {
		timeoutMs = *opts.poll.TimeoutMs
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

			select {
			case outcome := <-outcomeCh:
				cancel()
				if outcome.err != nil {
					if statusCtx.Err() == context.DeadlineExceeded {
						return nil, timeoutErr
					}
					return nil, outcome.err
				}
				statusResult = outcome.result
			case <-statusCtx.Done():
				cancel()
				if statusCtx.Err() == context.DeadlineExceeded {
					return nil, timeoutErr
				}
				return nil, statusCtx.Err()
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
			return nil, errors.New("Video generation did not complete after webhook notification.")
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
		MaxDelay:     60 * time.Second,
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
		MaxDelay:     60 * time.Second,
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
		timerErrCh <- fmt.Errorf("Video generation timed out after %dms.", timeoutMs)
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

func decodeVideoPromptImageDataString(value string) ([]byte, string, error) {
	mediaType := ""
	content := value
	isDataURL := false
	if strings.HasPrefix(value, "data:") {
		isDataURL = true
		header, body, ok := strings.Cut(value, ",")
		if ok {
			content = body
			if strings.HasPrefix(header, "data:") {
				mediaType = strings.TrimPrefix(strings.SplitN(header, ";", 2)[0], "data:")
			}
		}
	}

	data, err := types.DecodeFileDataString(content)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "" {
		if isDataURL {
			mediaType = "image/png"
		} else {
			mediaType = detectVideoPromptImageMediaType(data)
		}
	}
	return data, mediaType, nil
}

func detectVideoPromptImageMediaType(data []byte) string {
	switch {
	case len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 &&
		data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A &&
		data[6] == 0x1A && data[7] == 0x0A:
		return "image/png"
	case len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x38:
		return "image/gif"
	case len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 &&
		data[10] == 0x42 && data[11] == 0x50:
		return "image/webp"
	default:
		return "image/png"
	}
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
		header, body, ok := strings.Cut(value, ",")
		if ok {
			content = body
			if mediaType == "" && strings.HasPrefix(header, "data:") {
				mediaType = strings.TrimPrefix(strings.SplitN(header, ";", 2)[0], "data:")
			}
		}
	}

	data, err := types.DecodeFileDataString(content)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "" {
		if isDataURL {
			mediaType = "image/png"
		} else {
			mediaType = detectAnyFileMediaType(data)
		}
	}
	return data, mediaType, nil
}

// detectAnyFileMediaType detects an image or video media type from binary
// data, trying image signatures first (they are shorter and more specific)
// and falling back to video signatures, defaulting to "image/png" when
// neither matches (mirrors TS detectFileMediaType with restrictToImages=false).
func detectAnyFileMediaType(data []byte) string {
	if mt := detectImageMediaTypeOrEmpty(data); mt != "" {
		return mt
	}
	if mt := detectVideoMediaTypeOrEmpty(data); mt != "" {
		return mt
	}
	return "image/png"
}

func detectImageMediaTypeOrEmpty(data []byte) string {
	switch {
	case len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A:
		return "image/png"
	case len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38:
		return "image/gif"
	case len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50:
		return "image/webp"
	default:
		return ""
	}
}

func detectVideoMediaTypeOrEmpty(data []byte) string {
	switch {
	case len(data) >= 4 &&
		data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3:
		return "video/webm"
	case len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x41 && data[9] == 0x56 && data[10] == 0x49 && data[11] == 0x20:
		return "video/x-msvideo"
	case len(data) >= 12 &&
		data[4] == 0x66 && data[5] == 0x74 && data[6] == 0x79 && data[7] == 0x70 &&
		data[8] == 0x71 && data[9] == 0x74:
		return "video/quicktime"
	case len(data) >= 8 &&
		data[4] == 0x66 && data[5] == 0x74 && data[6] == 0x79 && data[7] == 0x70:
		return "video/mp4"
	default:
		return ""
	}
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
			mediaType = media.DetectVideoMediaType(videoBytes)
		}
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		return &types.GeneratedFile{
			Data:      videoBytes,
			MediaType: mediaType,
		}, nil

	case "base64":
		decoded, err := types.DecodeFileDataString(data.Data)
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
			mediaType = media.DetectVideoMediaType(data.Binary)
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

	videos := make([]*types.GeneratedFile, 0, len(response.Videos))
	for _, videoData := range response.Videos {
		file, err := convertVideoData(ctx, videoData, download, downloadWithMetadata)
		if err != nil {
			return nil, err
		}
		videos = append(videos, file)
	}
	warnings := response.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	logModelWarnings(warnings, model.Provider(), model.ModelID())

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: mergeProviderMetadata([]map[string]interface{}{response.ProviderMetadata}),
	}, nil
}

func downloadVideoURL(ctx context.Context, url string, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) ([]byte, string, error) {
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
	result, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
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
