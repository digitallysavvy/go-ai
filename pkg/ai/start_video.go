package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// StartVideoOptions configures an asynchronous video generation started with
// ExperimentalStartVideo. It mirrors GenerateVideoOptions minus the
// download/poll/webhook fields that only apply to the synchronous /
// orchestrated flows.
type StartVideoOptions struct {
	// Model to use. Must implement provider.VideoModelStarter.
	Model provider.VideoModelV3

	// Prompt can be text-only or image+text for image-to-video.
	Prompt VideoPrompt

	// Number of videos to generate (default: 1). Must not exceed the
	// model's MaxVideosPerCall — fan out with multiple StartVideo calls.
	N int

	// Maximum videos per API call (provider-specific). If not set, uses the
	// model's MaxVideosPerCall() value for the limit check.
	MaxVideosPerCall *int

	// Aspect ratio in format "width:height", or "adaptive".
	AspectRatio string

	// Resolution in format "widthxheight".
	Resolution string

	// Duration in seconds.
	Duration *float64

	// Frames per second.
	FPS *int

	// Seed for reproducible generation.
	Seed *int

	// FrameImages are role-tagged image inputs for image-to-video and
	// first-last-frame generation.
	FrameImages []VideoFrameImageInput

	// InputReferences are reference image or video inputs for
	// reference-to-video generation.
	InputReferences []VideoReferenceInput

	// GenerateAudio requests that the model generate audio alongside the
	// video, when supported.
	GenerateAudio *bool

	// Provider-specific options.
	ProviderOptions map[string]interface{}

	// Maximum retries for the start call (default: 2). Set to 0 to disable.
	MaxRetries *int

	// Additional HTTP headers.
	Headers map[string]string

	// WebhookURL, when set, asks the provider to notify this URL when the
	// generation reaches a terminal state.
	WebhookURL string
}

// StartVideoResult is the result of an ExperimentalStartVideo call.
type StartVideoResult struct {
	// Operation is a JSON-serializable opaque reference to the started
	// generation. Persist it and pass it to ExperimentalGetVideoStatus to
	// retrieve the status and result later, from any process.
	Operation json.RawMessage `json:"operation"`

	// Warnings for the call, e.g. unsupported settings.
	Warnings []types.Warning `json:"warnings"`

	// ProviderMetadata is passed through from the provider. Carries the
	// provider's own job identifiers (e.g. the AI Gateway's
	// providerMetadata.gateway.asyncJob.jobId).
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// Response is response metadata from the provider.
	Response VideoModelResponseMetadata `json:"response"`
}

// ExperimentalStartVideo starts an asynchronous video generation and returns
// immediately with an opaque operation reference, without waiting for the
// video to finish.
//
// This is the fire-and-forget counterpart to GenerateVideo: use it to fan out
// many jobs, to submit from a process that will not stay alive, or together
// with WebhookURL so the provider notifies your endpoint at the terminal
// state. Check the outcome with ExperimentalGetVideoStatus, or let your
// webhook receiver fetch the result.
func ExperimentalStartVideo(ctx context.Context, opts StartVideoOptions) (*StartVideoResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	starter, ok := opts.Model.(provider.VideoModelStarter)
	if !ok {
		return nil, fmt.Errorf(
			"Video model %s does not implement doStart. Use GenerateVideo for models without an asynchronous start/status flow.", //nolint:staticcheck // matches TS SDK's exact error text
			opts.Model.ModelID(),
		)
	}

	n := opts.N
	if n == 0 {
		n = 1
	}
	if n < 1 {
		return nil, fmt.Errorf("Invalid n: expected a positive integer, received %d.", n) //nolint:staticcheck // matches TS SDK's exact error text
	}

	knownMax := opts.MaxVideosPerCall
	if knownMax == nil {
		knownMax = opts.Model.MaxVideosPerCall()
	}
	if knownMax != nil && n > *knownMax {
		return nil, fmt.Errorf(
			"Video model %s supports at most %d video(s) per call, but %d were requested. Split the batch across multiple StartVideo calls.", //nolint:staticcheck // matches TS SDK's exact error text
			opts.Model.ModelID(), *knownMax, n,
		)
	}

	normalized, err := normalizeVideoInputs(opts.Prompt, opts.FrameImages, opts.InputReferences)
	if err != nil {
		return nil, err
	}

	providerOptions := opts.ProviderOptions
	if providerOptions == nil {
		providerOptions = map[string]interface{}{}
	}

	callOpts := provider.VideoModelV3CallOptions{
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
		ProviderOptions: providerOptions,
		AbortSignal:     ctx,
		Headers:         videoIdempotencyHeaders(videoHeadersWithUserAgent(opts.Headers)),
	}

	startOpts := &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: callOpts,
		WebhookURL:              opts.WebhookURL,
	}

	startResult, err := doStartVideoWithRetry(ctx, starter, startOpts, opts.MaxRetries)
	if err != nil {
		return nil, err
	}

	warnings := append(append([]types.Warning{}, normalized.Warnings...), startResult.Warnings...)

	return &StartVideoResult{
		Operation:        startResult.Operation,
		Warnings:         warnings,
		ProviderMetadata: startResult.ProviderMetadata,
		Response:         convertResponseInfo(startResult.Response, startResult.ProviderMetadata, opts.Model),
	}, nil
}
