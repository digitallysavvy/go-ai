package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateImageOptions contains options for image generation.
type GenerateImageOptions struct {
	Model provider.ImageModel

	Prompt      string
	N           *int
	Size        string
	AspectRatio string
	Seed        *int
	Quality     string
	Style       string
	Files       []provider.ImageFile
	Mask        *provider.ImageFile
	Headers     map[string]string

	ProviderOptions map[string]interface{}
}

// GenerateImageResult contains the result of an image generation operation.
type GenerateImageResult struct {
	Images           []types.GeneratedFile     `json:"images"`
	Image            types.GeneratedFile       `json:"image"`
	Warnings         []types.Warning           `json:"warnings,omitempty"`
	Responses        []*types.ResponseMetadata `json:"responses,omitempty"`
	ProviderMetadata map[string]interface{}    `json:"providerMetadata,omitempty"`
	Usage            types.ImageUsage          `json:"usage"`
}

// GenerateImage generates one or more images using the provided image model.
func GenerateImage(ctx context.Context, opts GenerateImageOptions) (*GenerateImageResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	raw, err := opts.Model.DoGenerate(ctx, &provider.ImageGenerateOptions{
		Prompt:          opts.Prompt,
		N:               opts.N,
		Size:            opts.Size,
		AspectRatio:     opts.AspectRatio,
		Seed:            opts.Seed,
		Quality:         opts.Quality,
		Style:           opts.Style,
		Files:           opts.Files,
		Mask:            opts.Mask,
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("no image generated")
	}

	images := make([]types.GeneratedFile, 0, max(1, len(raw.Images)))
	appendImage := func(data []byte, mediaType, url string) {
		if len(data) == 0 && url == "" {
			return
		}
		images = append(images, types.GeneratedFile{
			Data:      data,
			URL:       url,
			MediaType: mediaType,
		})
	}
	for _, img := range raw.Images {
		appendImage(img, raw.MimeType, raw.URL)
	}
	if len(images) == 0 {
		appendImage(raw.Image, raw.MimeType, raw.URL)
	}
	if len(images) == 0 && len(raw.Base64Image) == 0 {
		return nil, fmt.Errorf("no image generated")
	}

	first := types.GeneratedFile{}
	if len(images) > 0 {
		first = images[0]
	}

	response := raw.Response
	if response == nil {
		response = &types.ResponseMetadata{
			ID:        newCallID(),
			Timestamp: time.Now(),
			ModelID:   opts.Model.ModelID(),
		}
	}

	return &GenerateImageResult{
		Images:           images,
		Image:            first,
		Warnings:         raw.Warnings,
		Responses:        []*types.ResponseMetadata{response},
		ProviderMetadata: raw.ProviderMetadata,
		Usage:            raw.Usage,
	}, nil
}
