package ai

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
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

	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	// Set defaults
	if opts.N == 0 {
		opts.N = 1
	}

	if opts.ProviderOptions == nil {
		opts.ProviderOptions = map[string]interface{}{}
	}

	// Determine max videos per call
	maxPerCall := 1
	if opts.MaxVideosPerCall != nil {
		maxPerCall = *opts.MaxVideosPerCall
	} else if modelMax := opts.Model.MaxVideosPerCall(); modelMax != nil {
		maxPerCall = *modelMax
	}
	if maxPerCall <= 0 {
		return nil, fmt.Errorf("maxVideosPerCall must be > 0")
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
		var err error
		imageFile, err = convertPromptImage(opts.Prompt.Image)
		if err != nil {
			return nil, err
		}
	}

	// Build call options
	callOpts := &provider.VideoModelV3CallOptions{
		Prompt:          opts.Prompt.Text,
		PromptSet:       videoPromptTextProvided(opts.Prompt),
		N:               opts.N,
		AspectRatio:     opts.AspectRatio,
		Resolution:      opts.Resolution,
		Duration:        opts.Duration,
		FPS:             opts.FPS,
		Seed:            opts.Seed,
		Image:           imageFile,
		ProviderOptions: opts.ProviderOptions,
		AbortSignal:     ctx,
		Headers:         videoHeadersWithUserAgent(opts.Headers),
	}

	// Call provider
	response, err := doGenerateVideoWithRetry(ctx, opts.Model, callOpts, opts.MaxRetries)
	if err != nil {
		return nil, err
	}

	// Convert response
	return convertToGenerateVideoResult(ctx, response, opts.Model, opts.Download, opts.DownloadWithMetadata)
}

// parallelGenerate handles batch generation with multiple parallel API calls
func parallelGenerate(ctx context.Context, opts GenerateVideoOptions, maxPerCall, callCount int) (*GenerateVideoResult, error) {
	type callResult struct {
		index    int
		response *provider.VideoModelV3Response
		err      error
	}

	resultChan := make(chan callResult, callCount)
	var wg sync.WaitGroup

	// Convert prompt image once if provided
	var imageFile *provider.VideoModelV3File
	if opts.Prompt.Image != nil {
		var err error
		imageFile, err = convertPromptImage(opts.Prompt.Image)
		if err != nil {
			return nil, err
		}
	}

	// Launch parallel API calls
	for i := 0; i < callCount; i++ {
		wg.Add(1)

		remaining := opts.N - (i * maxPerCall)
		n := min(remaining, maxPerCall)

		go func(index int, n int) {
			defer wg.Done()

			callOpts := &provider.VideoModelV3CallOptions{
				Prompt:          opts.Prompt.Text,
				PromptSet:       videoPromptTextProvided(opts.Prompt),
				N:               n,
				AspectRatio:     opts.AspectRatio,
				Resolution:      opts.Resolution,
				Duration:        opts.Duration,
				FPS:             opts.FPS,
				Seed:            opts.Seed,
				Image:           imageFile,
				ProviderOptions: opts.ProviderOptions,
				AbortSignal:     ctx,
				Headers:         videoHeadersWithUserAgent(opts.Headers),
			}

			resp, err := doGenerateVideoWithRetry(ctx, opts.Model, callOpts, opts.MaxRetries)
			resultChan <- callResult{index: index, response: resp, err: err}
		}(i, n)
	}

	// Wait for all calls to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	orderedResponses := make([]*provider.VideoModelV3Response, callCount)

	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		orderedResponses[result.index] = result.response
	}

	var videos []*types.GeneratedFile
	var warnings []types.Warning
	var responses []VideoModelResponseMetadata
	var providerMetadata []map[string]interface{}

	for _, response := range orderedResponses {
		// Convert video data
		for _, videoData := range response.Videos {
			file, err := convertVideoData(ctx, videoData, opts.Download, opts.DownloadWithMetadata)
			if err != nil {
				return nil, err
			}
			videos = append(videos, file)
		}

		// Collect warnings
		warnings = append(warnings, response.Warnings...)

		// Collect response metadata
		responses = append(responses, convertResponseInfo(response.Response, response.ProviderMetadata, opts.Model))

		// Collect provider metadata
		if response.ProviderMetadata != nil {
			providerMetadata = append(providerMetadata, response.ProviderMetadata)
		}
	}

	// Check if any videos were generated
	if len(videos) == 0 {
		return nil, &NoVideoGeneratedError{Responses: responses}
	}

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: mergeProviderMetadata(providerMetadata),
	}, nil
}

func doGenerateVideoWithRetry(ctx context.Context, model provider.VideoModelV3, opts *provider.VideoModelV3CallOptions, maxRetries *int) (*provider.VideoModelV3Response, error) {
	retries := preparedMaxRetries(maxRetries)
	if retries == 0 {
		return model.DoGenerate(ctx, opts)
	}

	var raw *provider.VideoModelV3Response
	err := retryutil.Do(ctx, retryutil.Config{
		MaxRetries:   retries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2,
		Jitter:       false,
		ShouldRetry:  isGatewayCallRetryable,
	}, func(retryCtx context.Context) error {
		var genErr error
		raw, genErr = model.DoGenerate(retryCtx, opts)
		return genErr
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// convertPromptImage converts VideoPromptImage to VideoModelV3File.
func convertPromptImage(img *VideoPromptImage) (*provider.VideoModelV3File, error) {
	if img == nil {
		return nil, nil
	}

	if img.URL != "" {
		return &provider.VideoModelV3File{
			Type: "url",
			URL:  img.URL,
		}, nil
	}

	if img.DataString != "" {
		data, mediaType, err := decodeVideoPromptImageDataString(img.DataString)
		if err != nil {
			return nil, fmt.Errorf("invalid video prompt image data string: %w", err)
		}
		return &provider.VideoModelV3File{
			Type:      "file",
			Data:      data,
			MediaType: mediaType,
		}, nil
	}

	if len(img.Data) > 0 {
		mediaType := img.MediaType
		if mediaType == "" {
			mediaType = detectVideoPromptImageMediaType(img.Data)
		}

		return &provider.VideoModelV3File{
			Type:      "file",
			Data:      img.Data,
			MediaType: mediaType,
		}, nil
	}

	return nil, nil
}

func decodeVideoPromptImageDataString(value string) ([]byte, string, error) {
	mediaType := ""
	content := value
	isDataURL := false
	if strings.HasPrefix(value, "data:") {
		isDataURL = true
		header, body, ok := strings.Cut(value, ",")
		if ok {
			content = body
			if strings.HasPrefix(header, "data:") {
				mediaType = strings.TrimPrefix(strings.SplitN(header, ";", 2)[0], "data:")
			}
		}
	}

	data, err := types.DecodeFileDataString(content)
	if err != nil {
		return nil, "", err
	}
	if mediaType == "" {
		if isDataURL {
			mediaType = "image/png"
		} else {
			mediaType = detectVideoPromptImageMediaType(data)
		}
	}
	return data, mediaType, nil
}

func detectVideoPromptImageMediaType(data []byte) string {
	switch {
	case len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 &&
		data[2] == 0x4E && data[3] == 0x47 &&
		data[4] == 0x0D && data[5] == 0x0A &&
		data[6] == 0x1A && data[7] == 0x0A:
		return "image/png"
	case len(data) >= 4 &&
		data[0] == 0x47 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x38:
		return "image/gif"
	case len(data) >= 3 &&
		data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 12 &&
		data[0] == 0x52 && data[1] == 0x49 &&
		data[2] == 0x46 && data[3] == 0x46 &&
		data[8] == 0x57 && data[9] == 0x45 &&
		data[10] == 0x42 && data[11] == 0x50:
		return "image/webp"
	default:
		return "image/png"
	}
}

// convertVideoData converts VideoModelV3VideoData to GeneratedFile.
func convertVideoData(ctx context.Context, data provider.VideoModelV3VideoData, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) (*types.GeneratedFile, error) {
	switch data.Type {
	case "url":
		videoBytes, downloadedMediaType, err := downloadVideoURL(ctx, data.URL, download, downloadWithMetadata)
		if err != nil {
			return nil, err
		}

		mediaType := data.MediaType
		if !isUsableVideoMediaType(mediaType) {
			mediaType = downloadedMediaType
		}
		if !isUsableVideoMediaType(mediaType) {
			mediaType = media.DetectVideoMediaType(videoBytes)
		}
		if mediaType == "" {
			mediaType = "video/mp4"
		}

		return &types.GeneratedFile{
			Data:      videoBytes,
			MediaType: mediaType,
		}, nil

	case "base64":
		decoded, err := types.DecodeFileDataString(data.Data)
		if err != nil {
			return nil, err
		}
		mediaType := data.MediaType
		if mediaType == "" {
			mediaType = "video/mp4"
		}
		return &types.GeneratedFile{
			Data:      decoded,
			MediaType: mediaType,
		}, nil

	case "binary":
		mediaType := data.MediaType
		if mediaType == "" && len(data.Binary) > 0 {
			mediaType = media.DetectVideoMediaType(data.Binary)
		}
		if mediaType == "" {
			mediaType = "video/mp4"
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
func convertToGenerateVideoResult(ctx context.Context, response *provider.VideoModelV3Response, model provider.VideoModelV3, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) (*GenerateVideoResult, error) {
	responses := []VideoModelResponseMetadata{convertResponseInfo(response.Response, response.ProviderMetadata, model)}
	if len(response.Videos) == 0 {
		return nil, &NoVideoGeneratedError{Responses: responses}
	}

	videos := make([]*types.GeneratedFile, 0, len(response.Videos))
	for _, videoData := range response.Videos {
		file, err := convertVideoData(ctx, videoData, download, downloadWithMetadata)
		if err != nil {
			return nil, err
		}
		videos = append(videos, file)
	}
	warnings := response.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}

	return &GenerateVideoResult{
		Video:            videos[0],
		Videos:           videos,
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: mergeProviderMetadata([]map[string]interface{}{response.ProviderMetadata}),
	}, nil
}

func downloadVideoURL(ctx context.Context, url string, download URLDownloadFunction, downloadWithMetadata URLDownloadWithMetadataFunction) ([]byte, string, error) {
	if downloadWithMetadata != nil {
		result, err := downloadWithMetadata(ctx, url)
		if err != nil {
			return nil, "", err
		}
		if result == nil {
			return nil, "", fmt.Errorf("download returned nil result for %s", url)
		}
		return result.Data, result.MediaType, nil
	}
	if download != nil {
		data, err := download(ctx, url)
		return data, "", err
	}

	opts := fileutil.DefaultDownloadOptions()
	opts.URLValidator = downloadURLValidator
	result, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
}

func isUsableVideoMediaType(mediaType string) bool {
	return mediaType != "" && mediaType != "application/octet-stream"
}

func videoHeadersWithUserAgent(headers map[string]string) map[string]string {
	return version.WithUserAgentSuffix(headers, version.UserAgent())
}

func videoPromptTextProvided(prompt VideoPrompt) bool {
	return prompt.Image == nil || prompt.Text != ""
}

// convertResponseInfo converts VideoModelV3ResponseInfo to VideoModelResponseMetadata.
func convertResponseInfo(info provider.VideoModelV3ResponseInfo, providerMetadata map[string]interface{}, model provider.VideoModelV3) VideoModelResponseMetadata {
	if info.Timestamp.IsZero() {
		info.Timestamp = time.Now()
	}
	if info.ModelID == "" && model != nil {
		info.ModelID = model.ModelID()
	}
	return VideoModelResponseMetadata{
		Timestamp:        info.Timestamp,
		ModelID:          info.ModelID,
		Headers:          info.Headers,
		ProviderMetadata: providerMetadata,
	}
}

// mergeProviderMetadata merges multiple provider metadata maps
func mergeProviderMetadata(metadataList []map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for _, metadata := range metadataList {
		for providerName, value := range metadata {
			existing, ok := merged[providerName]
			if !ok {
				merged[providerName] = value
				continue
			}
			merged[providerName] = mergeProviderMetadataValue(existing, value)
		}
	}

	return merged
}

func mergeProviderMetadataValue(existing, next interface{}) interface{} {
	existingMap, existingOK := existing.(map[string]interface{})
	nextMap, nextOK := next.(map[string]interface{})
	if !existingOK || !nextOK {
		return next
	}

	merged := make(map[string]interface{}, len(existingMap)+len(nextMap))
	for k, v := range existingMap {
		merged[k] = v
	}
	for k, v := range nextMap {
		if k == "videos" {
			if existingVideos, ok := sliceToInterfaces(merged[k]); ok {
				if nextVideos, ok := sliceToInterfaces(v); ok {
					merged[k] = append(existingVideos, nextVideos...)
					continue
				}
			}
		}
		merged[k] = v
	}
	return merged
}

func sliceToInterfaces(value interface{}) ([]interface{}, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
