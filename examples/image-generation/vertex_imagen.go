//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

func main() {
	// Get configuration from environment
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	accessToken := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || accessToken == "" {
		fmt.Println("Please set the following environment variables:")
		fmt.Println("  GOOGLE_VERTEX_PROJECT")
		fmt.Println("  GOOGLE_VERTEX_LOCATION (e.g., us-central1)")
		fmt.Println("  GOOGLE_VERTEX_ACCESS_TOKEN")
		fmt.Println("\nTo get an access token, run:")
		fmt.Println("  gcloud auth print-access-token")
		os.Exit(1)
	}

	// Create Google Vertex AI provider
	prov, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    location,
		AccessToken: accessToken,
	})
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	// Create Imagen model
	model, err := prov.ImageModel("imagen-3.0-generate-001")
	if err != nil {
		fmt.Printf("Error creating model: %v\n", err)
		os.Exit(1)
	}

	// Generate image
	fmt.Println("Generating image with Vertex AI Imagen 3.0...")
	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "A futuristic cityscape at night with neon lights and flying cars",
		N:      &n,
		Size:   "1920x1080", // Converts to 16:9 aspect ratio
	})
	if err != nil {
		fmt.Printf("Error generating image: %v\n", err)
		os.Exit(1)
	}

	// Save image to file
	filename := "vertex_imagen_output.png"
	err = os.WriteFile(filename, result.Image, 0644)
	if err != nil {
		fmt.Printf("Error saving image: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Image generated successfully!\n")
	fmt.Printf("  File: %s\n", filename)
	fmt.Printf("  MIME Type: %s\n", result.MimeType)
	fmt.Printf("  Size: %d bytes\n", len(result.Image))
	fmt.Printf("  Images generated: %d\n", result.Usage.ImageCount)
}
