package provider

import (
	"context"
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

// VideoModelV3CallOptions contains parameters for video generation
type VideoModelV3CallOptions struct {
	// Text prompt for video generation (required for text-to-video, optional for image-to-video)
	Prompt string

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

	// Provider-specific options
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation
	AbortSignal context.Context

	// Additional HTTP headers
	Headers map[string]string
}

// VideoModelV3File represents input image file
type VideoModelV3File struct {
	// Type is "url" or "file"
	Type string

	// URL for type="url"
	URL string

	// Data for type="file" (raw binary data)
	Data []byte

	// MediaType for type="file" (e.g., "image/png", "image/jpeg")
	MediaType string
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
