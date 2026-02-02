package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	if opts.Prompt.Text == "" && opts.Prompt.Image == nil {
		return nil, fmt.Errorf("prompt text or image is required")
	}

	// Set defaults
	if opts.N == 0 {
		opts.N = 1
	}

	if opts.MaxRetries == 0 {
		opts.MaxRetries = 2
	}

	// Determine max videos per call
	maxPerCall := 1
	if opts.MaxVideosPerCall != nil {
		maxPerCall = *opts.MaxVideosPerCall
	} else if modelMax := opts.Model.MaxVideosPerCall(); modelMax != nil {
		maxPerCall = *modelMax
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
	// Convert prompt image if provided
	var imageFile *provider.VideoModelV3File
	if opts.Prompt.Image != nil {
		imageFile = convertPromptImage(opts.Prompt.Image)
	}

	// Build call options
	callOpts := &provider.VideoModelV3CallOptions{
		Prompt:          opts.Prompt.Text,
		N:               opts.N,
		AspectRatio:     opts.AspectRatio,
		Resolution:      opts.Resolution,
		Duration:        opts.Duration,
		FPS:             opts.FPS,
		Seed:            opts.Seed,
		Image:           imageFile,
		ProviderOptions: opts.ProviderOptions,
		AbortSignal:     ctx,
		Headers:         opts.Headers,
	}

	// Call provider
	response, err := opts.Model.DoGenerate(ctx, callOpts)
	if err != nil {
		return nil, err
	}

	// Convert response
	return convertToGenerateVideoResult(ctx, response, opts.Model)
}

// parallelGenerate handles batch generation with multiple parallel API calls
func parallelGenerate(ctx context.Context, opts GenerateVideoOptions, maxPerCall, callCount int) (*GenerateVideoResult, error) {
	type callResult struct {
		response *provider.VideoModelV3Response
		err      error
	}

	resultChan := make(chan callResult, callCount)
	var wg sync.WaitGroup

	// Convert prompt image once if provided
	var imageFile *provider.VideoModelV3File
	if opts.Prompt.Image != nil {
		imageFile = convertPromptImage(opts.Prompt.Image)
	}

	// Launch parallel API calls
	for i := 0; i < callCount; i++ {
		wg.Add(1)

		remaining := opts.N - (i * maxPerCall)
		n := min(remaining, maxPerCall)

		go func(n int) {
			defer wg.Done()

			callOpts := &provider.VideoModelV3CallOptions{
				Prompt:          opts.Prompt.Text,
				N:               n,
				AspectRatio:     opts.AspectRatio,
				Resolution:      opts.Resolution,
				Duration:        opts.Duration,
				FPS:             opts.FPS,
				Seed:            opts.Seed,
				Image:           imageFile,
				ProviderOptions: opts.ProviderOptions,
				AbortSignal:     ctx,
				Headers:         opts.Headers,
			}

			resp, err := opts.Model.DoGenerate(ctx, callOpts)
			resultChan <- callResult{response: resp, err: err}
		}(n)
	}

	// Wait for all calls to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var videos []*types.GeneratedFile
	var warnings []types.Warning
	var responses []VideoModelResponseMetadata
	var providerMetadata []map[string]interface{}

	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}

		// Convert video data
		for _, videoData := range result.response.Videos {
			file, err := convertVideoData(ctx, videoData)
			if err != nil {
				return nil, err
			}
			videos = append(videos, file)
		}

		// Collect warnings
		warnings = append(warnings, result.response.Warnings...)

		// Collect response metadata
		responses = append(responses, convertResponseInfo(result.response.Response))

		// Collect provider metadata
		if result.response.ProviderMetadata != nil {
			providerMetadata = append(providerMetadata, result.response.ProviderMetadata)
		}
	}

	// Check if any videos were generated
	if len(videos) == 0 {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: mergeProviderMetadata(providerMetadata),
	}, nil
}

// convertPromptImage converts VideoPromptImage to VideoModelV3File
func convertPromptImage(img *VideoPromptImage) *provider.VideoModelV3File {
	if img == nil {
		return nil
	}

	if img.URL != "" {
		return &provider.VideoModelV3File{
			Type: "url",
			URL:  img.URL,
		}
	}

	if len(img.Data) > 0 {
		mediaType := img.MediaType
		if mediaType == "" {
			mediaType = media.DetectImageMediaType(img.Data)
		}

		return &provider.VideoModelV3File{
			Type:      "file",
			Data:      img.Data,
			MediaType: mediaType,
		}
	}

	return nil
}

// convertVideoData converts VideoModelV3VideoData to GeneratedFile
func convertVideoData(ctx context.Context, data provider.VideoModelV3VideoData) (*types.GeneratedFile, error) {
	switch data.Type {
	case "url":
		return &types.GeneratedFile{
			URL:       data.URL,
			MediaType: data.MediaType,
		}, nil

	case "base64":
		// Base64 data is already in data.Data as string
		// We keep it as is for now (could decode if needed)
		return &types.GeneratedFile{
			Data:      []byte(data.Data),
			MediaType: data.MediaType,
		}, nil

	case "binary":
		mediaType := data.MediaType
		if mediaType == "" && len(data.Binary) > 0 {
			mediaType = media.DetectVideoMediaType(data.Binary)
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
func convertToGenerateVideoResult(ctx context.Context, response *provider.VideoModelV3Response, model provider.VideoModelV3) (*GenerateVideoResult, error) {
	if len(response.Videos) == 0 {
		return nil, providererrors.NewNoVideoGeneratedError()
	}

	videos := make([]*types.GeneratedFile, 0, len(response.Videos))
	for _, videoData := range response.Videos {
		file, err := convertVideoData(ctx, videoData)
		if err != nil {
			return nil, err
		}
		videos = append(videos, file)
	}

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         response.Warnings,
		Responses:        []VideoModelResponseMetadata{convertResponseInfo(response.Response)},
		ProviderMetadata: response.ProviderMetadata,
	}, nil
}

// convertResponseInfo converts VideoModelV3ResponseInfo to VideoModelResponseMetadata
func convertResponseInfo(info provider.VideoModelV3ResponseInfo) VideoModelResponseMetadata {
	return VideoModelResponseMetadata{
		Timestamp: info.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		ModelID:   info.ModelID,
		Headers:   info.Headers,
	}
}

// mergeProviderMetadata merges multiple provider metadata maps
func mergeProviderMetadata(metadataList []map[string]interface{}) map[string]interface{} {
	if len(metadataList) == 0 {
		return nil
	}

	merged := make(map[string]interface{})
	for _, metadata := range metadataList {
		for k, v := range metadata {
			merged[k] = v
		}
	}

	return merged
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
