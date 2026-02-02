package ai

import (
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

	// Provider-specific options
	ProviderOptions map[string]interface{}

	// Maximum retries per call (default: 2)
	MaxRetries int

	// Additional HTTP headers
	Headers map[string]string
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

	// Or raw image data
	Data []byte

	// Media type (e.g., "image/png", "image/jpeg")
	MediaType string
}

// GenerateVideoResult contains generated videos and metadata
type GenerateVideoResult struct {
	// Video is the primary generated video (first video in Videos array)
	Video *types.GeneratedFile

	// Videos contains all generated videos
	Videos []*types.GeneratedFile

	// Warnings from the generation process
	Warnings []types.Warning

	// Responses contains metadata for each API call
	Responses []VideoModelResponseMetadata

	// ProviderMetadata contains provider-specific metadata
	ProviderMetadata map[string]interface{}
}

// VideoModelResponseMetadata contains metadata for a video generation API call
type VideoModelResponseMetadata struct {
	// Timestamp of the response
	Timestamp string

	// ModelID that generated the response
	ModelID string

	// Headers from the response
	Headers map[string]string
}
