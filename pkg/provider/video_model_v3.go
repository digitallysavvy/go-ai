package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModelV3 is the v3 specification for video generation models
// This interface must be implemented by all video generation providers
type VideoModelV3 interface {
	// SpecificationVersion returns "v3"
	SpecificationVersion() string

	// Provider returns the provider name (e.g., "fal", "replicate", "google-vertex")
	Provider() string

	// ModelID returns the model identifier
	ModelID() string

	// MaxVideosPerCall returns the maximum videos per API call
	// Returns nil to use global default (1)
	// Some providers (e.g., Google Vertex AI) support batch generation
	MaxVideosPerCall() *int

	// DoGenerate generates videos
	DoGenerate(ctx context.Context, opts *VideoModelV3CallOptions) (*VideoModelV3Response, error)
}

// Video frame roles for VideoFrameImage.FrameType, mirroring the TypeScript
// SDK's Experimental_VideoModelV4FrameType.
const (
	VideoFrameTypeFirstFrame = "first_frame"
	VideoFrameTypeLastFrame  = "last_frame"
)

// VideoFrameImage is a role-tagged image input for image-to-video and
// first-last-frame generation (TS Experimental_VideoModelV4FrameImage).
type VideoFrameImage struct {
	// Image is the file used for this frame.
	Image VideoModelV3File

	// FrameType is which frame this image represents: VideoFrameTypeFirstFrame
	// or VideoFrameTypeLastFrame.
	FrameType string
}

// VideoModelStarter is an optional capability implemented by video models
// that support the asynchronous start/status flow (TS `doStart`). Its
// presence, together with VideoModelStatusChecker, lets core orchestrate
// polling or webhook-based completion instead of a provider-internal
// polling loop in DoGenerate.
type VideoModelStarter interface {
	DoStart(ctx context.Context, opts *VideoModelV3StartOptions) (*VideoModelV3OperationStartResult, error)
}

// VideoModelStatusChecker is an optional capability implemented by video
// models that support the asynchronous start/status flow (TS `doStatus`).
type VideoModelStatusChecker interface {
	DoStatus(ctx context.Context, opts *VideoModelV3StatusOptions) (*VideoModelV3OperationStatusResult, error)
}

// VideoOperationWebhook is the payload received from a webhook notification
// during asynchronous video generation (TS
// Experimental_VideoModelV4OperationWebhook).
type VideoOperationWebhook struct {
	Headers map[string]string
	Body    json.RawMessage
}

// VideoWebhookReceived blocks until the webhook notification for a started
// operation arrives, or ctx is done. It mirrors the TS `received` promise.
type VideoWebhookReceived func(ctx context.Context) (*VideoOperationWebhook, error)

// VideoWebhookFactory is supplied by the caller of GenerateVideo/StartVideo
// to obtain a callback URL and a way to wait for the webhook notification
// (TS GenerateVideoWebhookFactory).
type VideoWebhookFactory func(ctx context.Context) (url string, received VideoWebhookReceived, err error)

// VideoModelWebhookHandler is an optional capability signaling that the
// provider's API natively supports webhooks for the start/status flow (TS
// `handleWebhookOption`). When absent, core never invokes the caller's
// webhook factory and falls back to polling via DoStatus.
type VideoModelWebhookHandler interface {
	HandleWebhookOption(ctx context.Context, factory VideoWebhookFactory) (webhookURL string, received VideoWebhookReceived, err error)
}

// VideoModelV3StartOptions contains parameters for VideoModelStarter.DoStart.
// It embeds VideoModelV3CallOptions plus the optional webhook URL the
// provider should notify on completion.
type VideoModelV3StartOptions struct {
	VideoModelV3CallOptions

	// WebhookURL, when set, asks the provider to notify this URL when the
	// video generation completes.
	WebhookURL string
}

// VideoModelV3OperationStartResult is returned by DoStart when initiating an
// asynchronous video generation (TS
// Experimental_VideoModelV4OperationStartResult).
type VideoModelV3OperationStartResult struct {
	// Operation is an opaque, JSON-serializable reference passed to DoStatus
	// to check the status of the generation (e.g. a task ID or prediction URL).
	Operation json.RawMessage

	// Warnings for the call, e.g. unsupported features.
	Warnings []types.Warning

	// ProviderMetadata contains provider-specific metadata.
	ProviderMetadata map[string]interface{}

	// Response contains response metadata.
	Response VideoModelV3ResponseInfo
}

// VideoModelV3StatusOptions contains parameters for
// VideoModelStatusChecker.DoStatus.
type VideoModelV3StatusOptions struct {
	// Operation is the opaque reference returned by DoStart.
	Operation json.RawMessage

	// Headers are additional HTTP headers.
	Headers map[string]string
}

// Video operation status values for VideoModelV3OperationStatusResult.Status.
const (
	VideoOperationStatusPending   = "pending"
	VideoOperationStatusCompleted = "completed"
	VideoOperationStatusError     = "error"
)

// VideoModelV3OperationStatusResult is returned by DoStatus when checking the
// status of an asynchronous video generation started with DoStart (TS
// Experimental_VideoModelV4OperationStatusResult).
type VideoModelV3OperationStatusResult struct {
	// Status is one of VideoOperationStatusPending, VideoOperationStatusCompleted,
	// or VideoOperationStatusError.
	Status string

	// Videos contains the generated videos. Only set when Status is
	// VideoOperationStatusCompleted.
	Videos []VideoModelV3VideoData

	// Error is a human-readable error message. Only set when Status is
	// VideoOperationStatusError.
	Error string

	// Warnings for the call.
	Warnings []types.Warning

	// ProviderMetadata contains provider-specific metadata.
	ProviderMetadata map[string]interface{}

	// Response contains response metadata.
	Response VideoModelV3ResponseInfo
}

// VideoModelV3CallOptions contains parameters for video generation
type VideoModelV3CallOptions struct {
	// Text prompt for video generation (required for text-to-video, optional for image-to-video)
	Prompt string

	// PromptSet reports whether Prompt was explicitly provided. It lets providers
	// preserve the TypeScript SDK distinction between prompt: "" and an omitted
	// prompt for image-only generation.
	PromptSet bool

	// Number of videos to generate (default: 1)
	N int

	// Aspect ratio in format "width:height" (e.g., "16:9", "9:16", "1:1")
	AspectRatio string

	// Resolution in format "widthxheight" (e.g., "1920x1080", "1280x720")
	Resolution string

	// Duration in seconds
	Duration *float64

	// Frames per second (24, 30, 60)
	FPS *int

	// Seed for reproducible generation
	Seed *int

	// Image for image-to-video generation (optional)
	Image *VideoModelV3File

	// FrameImages are role-tagged image inputs for first-last-frame
	// generation. Each entry declares whether it is the first_frame or
	// last_frame of the generated video.
	FrameImages []VideoFrameImage

	// InputReferences are reference inputs for reference-to-video
	// generation. Each entry is an image or video file.
	InputReferences []VideoModelV3File

	// GenerateAudio requests that the model generate audio alongside the
	// video, when supported.
	GenerateAudio *bool

	// Provider-specific options
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation
	AbortSignal context.Context

	// Additional HTTP headers
	Headers map[string]string
}

// VideoModelV3File represents input image or video file
type VideoModelV3File struct {
	// Type is "url" or "file"
	Type string

	// URL for type="url"
	URL string

	// Data for type="file" (raw binary data)
	Data []byte

	// MediaType for type="file" (e.g., "image/png", "image/jpeg", "video/mp4")
	MediaType string

	// ProviderOptions carries optional provider-specific metadata for the
	// file part.
	ProviderOptions map[string]interface{}
}

// VideoModelV3Response contains generated video data
type VideoModelV3Response struct {
	// Videos contains the generated video data
	Videos []VideoModelV3VideoData

	// Warnings from the generation process
	Warnings []types.Warning

	// ProviderMetadata contains provider-specific metadata
	ProviderMetadata map[string]interface{}

	// Response contains response metadata
	Response VideoModelV3ResponseInfo
}

// VideoModelV3VideoData represents a generated video
type VideoModelV3VideoData struct {
	// Type is "url", "base64", or "binary"
	Type string

	// URL for type="url"
	URL string

	// Data for type="base64" (base64-encoded video)
	Data string

	// Binary for type="binary" (raw video data)
	Binary []byte

	// MediaType (e.g., "video/mp4", "video/webm", "video/quicktime")
	MediaType string
}

// VideoModelV3ResponseInfo contains response metadata
type VideoModelV3ResponseInfo struct {
	// Timestamp of the response
	Timestamp time.Time

	// ModelID that generated the response
	ModelID string

	// Headers from the response
	Headers map[string]string
}
