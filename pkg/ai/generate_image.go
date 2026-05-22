package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
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

	// MaxImagesPerCall overrides the model's per-call image generation limit.
	// When N exceeds this value, GenerateImage makes multiple calls and
	// aggregates the results.
	MaxImagesPerCall int

	// MaxRetries is the maximum number of retries per image model call. Nil
	// defaults to 2; set to 0 to disable retries.
	MaxRetries *int

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
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	callImageCounts := imageCallCounts(opts.N, resolveMaxImagesPerCall(opts))

	images := make([]types.GeneratedFile, 0, totalImageCount(callImageCounts))
	var warnings []types.Warning
	var responses []*types.ResponseMetadata
	providerMetadata := map[string]interface{}{}
	usage := types.ImageUsage{}

	for _, callImageCount := range callImageCounts {
		callN := callImageCount
		raw, err := doGenerateImageWithRetry(ctx, opts.Model, &provider.ImageGenerateOptions{
			Prompt:          opts.Prompt,
			N:               &callN,
			Size:            opts.Size,
			AspectRatio:     opts.AspectRatio,
			Seed:            opts.Seed,
			Quality:         opts.Quality,
			Style:           opts.Style,
			Files:           opts.Files,
			Mask:            opts.Mask,
			ProviderOptions: opts.ProviderOptions,
			Headers:         opts.Headers,
		}, opts.MaxRetries)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, fmt.Errorf("no image generated")
		}

		callImages, err := generatedFilesFromImageResult(raw)
		if err != nil {
			return nil, err
		}
		images = append(images, callImages...)
		warnings = append(warnings, raw.Warnings...)
		usage.ImageCount += raw.Usage.ImageCount
		mergeImageProviderMetadata(providerMetadata, raw.ProviderMetadata)

		response := raw.Response
		if response == nil {
			response = &types.ResponseMetadata{
				ID:        newCallID(),
				Timestamp: time.Now(),
				ModelID:   opts.Model.ModelID(),
			}
		}
		responses = append(responses, response)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("no image generated")
	}

	return &GenerateImageResult{
		Images:           images,
		Image:            images[0],
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: providerMetadata,
		Usage:            usage,
	}, nil
}

func generatedFilesFromImageResult(raw *types.ImageResult) ([]types.GeneratedFile, error) {
	images := make([]types.GeneratedFile, 0, max(1, len(raw.Images)))
	appendImage := func(data []byte, mediaType, url string) {
		if len(data) == 0 && url == "" {
			return
		}
		images = append(images, types.GeneratedFile{
			Data:      data,
			URL:       url,
			MediaType: resolveGeneratedImageMediaType(data, mediaType),
		})
	}
	if len(raw.Base64Images) > 0 {
		for _, encoded := range raw.Base64Images {
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf("failed to decode generated image: %w", err)
			}
			appendImage(data, raw.MimeType, raw.URL)
		}
	} else {
		for _, img := range raw.Images {
			appendImage(img, raw.MimeType, raw.URL)
		}
	}
	if len(images) == 0 {
		if raw.Base64Image != "" {
			data, err := base64.StdEncoding.DecodeString(raw.Base64Image)
			if err != nil {
				return nil, fmt.Errorf("failed to decode generated image: %w", err)
			}
			appendImage(data, raw.MimeType, raw.URL)
		} else {
			appendImage(raw.Image, raw.MimeType, raw.URL)
		}
	}
	if len(images) == 0 && len(raw.Base64Image) == 0 {
		return nil, fmt.Errorf("no image generated")
	}

	return images, nil
}

func doGenerateImageWithRetry(ctx context.Context, model provider.ImageModel, opts *provider.ImageGenerateOptions, maxRetries *int) (*types.ImageResult, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries <= 0 {
		return model.DoGenerate(ctx, opts)
	}

	var result *types.ImageResult
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var err error
		result, err = model.DoGenerate(retryCtx, opts)
		return err
	})
	return result, err
}

func resolveMaxImagesPerCall(opts GenerateImageOptions) int {
	if opts.MaxImagesPerCall > 0 {
		return opts.MaxImagesPerCall
	}

	type maxImagesPerCallModel interface {
		MaxImagesPerCall() int
	}
	if model, ok := opts.Model.(maxImagesPerCallModel); ok {
		if limit := model.MaxImagesPerCall(); limit > 0 {
			return limit
		}
	}

	return 1
}

func resolveGeneratedImageMediaType(data []byte, mediaType string) string {
	if mediaType != "" {
		return mediaType
	}
	if len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 &&
		data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A &&
		data[6] == 0x1A && data[7] == 0x0A {
		return "image/png"
	}
	if len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x38 {
		return "image/gif"
	}
	if len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
		return "image/webp"
	}
	return "image/png"
}

func imageCallCounts(n *int, maxImagesPerCall int) []int {
	requested := 1
	if n != nil && *n > 0 {
		requested = *n
	}
	if maxImagesPerCall <= 0 {
		maxImagesPerCall = 1
	}

	callCount := (requested + maxImagesPerCall - 1) / maxImagesPerCall
	counts := make([]int, 0, callCount)
	for remaining := requested; remaining > 0; remaining -= maxImagesPerCall {
		count := maxImagesPerCall
		if remaining < maxImagesPerCall {
			count = remaining
		}
		counts = append(counts, count)
	}
	return counts
}

func totalImageCount(counts []int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func mergeImageProviderMetadata(dst map[string]interface{}, src map[string]interface{}) {
	for providerName, rawMetadata := range src {
		metadata, ok := rawMetadata.(map[string]interface{})
		if !ok {
			dst[providerName] = rawMetadata
			continue
		}
		if providerName == "gateway" {
			dst[providerName] = mergeGatewayImageMetadata(dst[providerName], metadata)
			continue
		}
		current, _ := dst[providerName].(map[string]interface{})
		if current == nil {
			current = map[string]interface{}{"images": []interface{}{}}
			dst[providerName] = current
		}
		for key, value := range metadata {
			if key != "images" {
				current[key] = value
			}
		}
		current["images"] = appendImageMetadata(current["images"], metadata["images"])
	}
}

func mergeGatewayImageMetadata(currentRaw interface{}, next map[string]interface{}) map[string]interface{} {
	current, _ := currentRaw.(map[string]interface{})
	if current == nil {
		current = map[string]interface{}{}
	}
	for key, value := range next {
		current[key] = value
	}
	if images, ok := current["images"].([]interface{}); ok && len(images) == 0 {
		delete(current, "images")
	}
	return current
}

func appendImageMetadata(currentRaw interface{}, nextRaw interface{}) []interface{} {
	current := toInterfaceSlice(currentRaw)
	return append(current, toInterfaceSlice(nextRaw)...)
}

func toInterfaceSlice(value interface{}) []interface{} {
	switch typed := value.(type) {
	case nil:
		return nil
	case []interface{}:
		return typed
	case []map[string]interface{}:
		items := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	default:
		return []interface{}{typed}
	}
}
