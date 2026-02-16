//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
)

// Example: Image-to-Video with File Upload
// This example demonstrates animating a local image file using FAL's Kling Video model
//
// Usage:
//   export FAL_API_KEY=your-api-key
//   go run image-to-video-file.go path/to/your/image.png

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run image-to-video-file.go <path-to-image>")
		fmt.Println("Example: go run image-to-video-file.go cat.png")
		os.Exit(1)
	}

	imagePath := os.Args[1]

	// Read image file
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		log.Fatalf("Failed to read image file: %v", err)
	}

	// Detect mime type from file extension
	mimeType := detectMimeType(imagePath)
	if mimeType == "" {
		log.Fatal("Unsupported image format. Supported: .jpg, .jpeg, .png, .webp, .gif")
	}

	// Create FAL provider
	cfg := fal.Config{
		APIKey: os.Getenv("FAL_API_KEY"),
	}

	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	prov := fal.New(cfg)

	// Get video model
	model, err := prov.VideoModel("fal-ai/kling-video/v2.5-turbo/pro/image-to-video")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("Generating video from local image file...")
	fmt.Printf("Image: %s (%d bytes, %s)\n", imagePath, len(imageData), mimeType)
	fmt.Println("This may take 2-3 minutes...")
	fmt.Println()

	result, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "The subject slowly turns and looks at the camera with natural movement",
		AspectRatio: "16:9",
		Image: &provider.VideoModelV3File{
			Type:      "file",
			Data:      imageData,
			MediaType: mimeType,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print result
	if len(result.Videos) > 0 {
		video := result.Videos[0]
		fmt.Println("✓ Video generated successfully!")
		fmt.Printf("  URL: %s\n", video.URL)
		fmt.Printf("  Type: %s\n", video.MediaType)
		fmt.Printf("\nDownload your video from the URL above.\n")
	}
}

func detectMimeType(filename string) string {
	ext := ""
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			ext = filename[i:]
			break
		}
	}

	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}
