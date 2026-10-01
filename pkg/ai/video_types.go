package ai

import (
	"context"
	"errors"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateVideoOptions configures video generation
type GenerateVideoOptions struct {
	// Model to use for video generation
	Model provider.VideoModelV3

	// Prompt can be text-only or image+text for image-to-video
	Prompt VideoPrompt

	// Number of videos to generate (default: 1)
	N int

	// Maximum videos per API call (provider-specific)
	// If not set, uses the model's MaxVideosPerCall() value
	MaxVideosPerCall *int

	// Aspect ratio in format "width:height" (e.g., "16:9", "9:16", "1:1"), or
	// "adaptive" to inherit the ratio from the input media.
	AspectRatio string

	// Resolution in format "widthxheight" (e.g., "1920x1080", "1280x720")
	Resolution string

	// Duration in seconds
	Duration *float64

	// Frames per second (24, 30, 60)
	FPS *int

	// Seed for reproducible generation
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

	// Provider-specific options
	ProviderOptions map[string]interface{}

	// Maximum retries per call (default: 2). Set to 0 to disable retries.
	MaxRetries *int

	// Additional HTTP headers
	Headers map[string]string

	// Download is a custom download function for fetching video outputs from URLs.
	// Use CreateURLDownload() to create a download function with custom size limits.
	// Default: 2 GiB limit
	Download URLDownloadFunction

	// DownloadWithMetadata is a custom download function for fetching video
	// outputs from URLs while preserving a downloader-provided media type.
	// When set, it takes precedence over Download.
	DownloadWithMetadata URLDownloadWithMetadataFunction

	// Poll configures the asynchronous start/status flow when the model
	// implements VideoModelStarter/VideoModelStatusChecker. When set (or
	// Webhook is set), the SDK orchestrates polling or webhook-based
	// completion instead of calling DoGenerate directly.
	Poll *VideoPollOptions

	// Webhook, when set together with a model that supports the start/status
	// flow, asks the SDK to use webhook-based completion instead of polling.
	Webhook provider.VideoWebhookFactory
}

// VideoFrameImageInput is a role-tagged image input for image-to-video and
// first-last-frame generation.
type VideoFrameImageInput struct {
	// Image is the file used for this frame.
	Image VideoPromptImage

	// FrameType is provider.VideoFrameTypeFirstFrame or
	// provider.VideoFrameTypeLastFrame.
	FrameType string
}

// VideoReferenceInput is a reference image or video input for
// reference-to-video generation.
type VideoReferenceInput struct {
	// Data is the reference image or video.
	Data VideoPromptImage
}

// VideoPollOptions configures polling for models that support the
// asynchronous start/status flow.
type VideoPollOptions struct {
	// IntervalMs is the interval between status checks in milliseconds.
	// Default: 5000.
	IntervalMs *int

	// TimeoutMs is the maximum time to wait for completion in milliseconds.
	// When used with Webhook, it also limits how long the SDK waits for the
	// webhook notification. Default: 600000 (10 minutes).
	TimeoutMs *int

	// Delay is a custom delay implementation for polling intervals and
	// webhook timeouts. Default: a built-in timer-based delay honoring ctx.
	Delay func(ctx context.Context, delay time.Duration) error
}

// VideoPrompt represents text or image+text prompt
type VideoPrompt struct {
	// Text prompt (required for text-to-video, optional for image-to-video)
	Text string

	// Image for image-to-video generation (optional)
	Image *VideoPromptImage
}

// VideoPromptImage represents input image
type VideoPromptImage struct {
	// URL to image
	URL string

	// DataString is a base64, base64url, or data URL image string.
	DataString string

	// Or raw image data
	Data []byte

	// Media type (e.g., "image/png", "image/jpeg")
	MediaType string
}

// GenerateVideoResult contains generated videos and metadata
type GenerateVideoResult struct {
	// Video is the primary generated video (first video in Videos array)
	Video *types.GeneratedFile `json:"video"`

	// Videos contains all generated videos
	Videos []*types.GeneratedFile `json:"videos"`

	// Warnings from the generation process
	Warnings []types.Warning `json:"warnings"`

	// Responses contains metadata for each API call
	Responses []VideoModelResponseMetadata `json:"responses"`

	// ProviderMetadata contains provider-specific metadata
	ProviderMetadata map[string]interface{} `json:"providerMetadata"`
}

// VideoModelResponseMetadata contains metadata for a video generation API call
type VideoModelResponseMetadata struct {
	// Timestamp of the response
	Timestamp time.Time `json:"timestamp"`

	// ModelID that generated the response
	ModelID string `json:"modelId"`

	// Headers from the response
	Headers map[string]string `json:"headers,omitempty"`

	// ProviderMetadata contains provider-specific metadata for this response.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`
}

// NoVideoGeneratedError is returned when a video model returns no videos.
type NoVideoGeneratedError struct {
	Responses []VideoModelResponseMetadata
}

func (e *NoVideoGeneratedError) Error() string {
	return "No video generated."
}

// IsNoVideoGeneratedError reports whether err is a NoVideoGeneratedError.
func IsNoVideoGeneratedError(err error) bool {
	var target *NoVideoGeneratedError
	return errors.As(err, &target)
}
