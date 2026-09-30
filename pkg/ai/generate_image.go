package ai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	retryutil "github.com/digitallysavvy/go-ai/pkg/internal/retry"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
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
	Images   []types.GeneratedFile `json:"images"`
	Image    types.GeneratedFile   `json:"image"`
	Warnings []types.Warning       `json:"warnings,omitempty"`

	// Responses and ProviderMetadata are the aggregated (deprecated) view
	// across every call. Deprecated: use Calls for per-call diagnostics.
	Responses        []*types.ResponseMetadata `json:"responses,omitempty"`
	ProviderMetadata map[string]interface{}    `json:"providerMetadata,omitempty"`
	Usage            types.ImageUsage          `json:"usage"`

	// Calls exposes each individual model call's images, warnings, response
	// metadata and usage, including calls that returned zero images (e.g.
	// content-filtered or exhausted-retry attempts). Mirrors TS
	// GenerateImageResult.calls (audit row cc29073 / WG10).
	Calls []GenerateImageCall `json:"calls,omitempty"`
}

// GenerateImageCall is the per-call diagnostic record described on
// GenerateImageResult.Calls, mirroring TS GenerateImageResult['calls'][number].
type GenerateImageCall struct {
	Images           []types.GeneratedFile   `json:"images"`
	ProviderMetadata map[string]interface{}  `json:"providerMetadata,omitempty"`
	Response         *types.ResponseMetadata `json:"response,omitempty"`
	Warnings         []types.Warning         `json:"warnings,omitempty"`
	Usage            types.ImageUsage        `json:"usage"`
}

// NoImageGeneratedError is returned when every model call produced zero
// images. Mirrors TS error/no-image-generated-error.ts (audit row fc8e8ac /
// WG10): Calls carries per-call diagnostics (including empty/failed
// attempts) so callers can inspect why no image was produced.
type NoImageGeneratedError struct {
	Responses []*types.ResponseMetadata
	Calls     []GenerateImageCall
}

func (e *NoImageGeneratedError) Error() string {
	return "No image generated."
}

// IsNoImageGeneratedError reports whether err is a NoImageGeneratedError.
func IsNoImageGeneratedError(err error) bool {
	var target *NoImageGeneratedError
	return errors.As(err, &target)
}

// retryableNoImageResultError signals that a model call returned zero images
// without being classified as non-retryable, so the caller may retry it
// (bounded by MaxRetries) instead of treating it as a hard failure. Mirrors
// TS generate-image.ts's RetryableNoImageResultError (audit row 45099daf24 /
// WG10).
type retryableNoImageResultError struct{}

func (e *retryableNoImageResultError) Error() string {
	return "No image generated."
}

func isRetryableNoImageResultError(err error) bool {
	var target *retryableNoImageResultError
	return errors.As(err, &target)
}

// GenerateImage generates one or more images using the provided image model.
func GenerateImage(ctx context.Context, opts GenerateImageOptions) (*GenerateImageResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	// TS generate-image.ts tags every call's headers with `ai/${VERSION}`
	// (`headersWithUserAgent = withUserAgentSuffix(headers ?? {}, ai/${VERSION})`).
	opts.Headers = version.WithUserAgentSuffix(opts.Headers, version.UserAgent())

	callImageCounts := imageCallCounts(opts.N, resolveMaxImagesPerCall(opts))

	images := make([]types.GeneratedFile, 0, totalImageCount(callImageCounts))
	var warnings []types.Warning
	var responses []*types.ResponseMetadata
	var calls []GenerateImageCall
	providerMetadata := map[string]interface{}{}
	usage := types.ImageUsage{}

	for _, callImageCount := range callImageCounts {
		callN := callImageCount
		_, attempts, err := doGenerateImageWithRetry(ctx, opts.Model, &provider.ImageGenerateOptions{
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

		// Every attempt for this call (including empty/exhausted-retry ones)
		// contributes its own diagnostics entry and its images/warnings/
		// usage/response to the aggregate, matching TS generate-image.ts's
		// flattened resultGroups (audit rows 45099daf24/fc8e8ac/cc29073 /
		// WG10).
		for _, raw := range attempts {
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
			calls = append(calls, GenerateImageCall{
				Images:           callImages,
				ProviderMetadata: raw.ProviderMetadata,
				Response:         response,
				Warnings:         raw.Warnings,
				Usage:            raw.Usage,
			})
		}
	}

	logModelWarnings(warnings, opts.Model.Provider(), opts.Model.ModelID())

	if len(images) == 0 {
		return nil, &NoImageGeneratedError{Responses: responses, Calls: calls}
	}

	return &GenerateImageResult{
		Images:           images,
		Image:            images[0],
		Warnings:         warnings,
		Responses:        responses,
		ProviderMetadata: providerMetadata,
		Usage:            usage,
		Calls:            calls,
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
	// A zero-image result is not an error here: it may be a legitimate
	// (possibly retryable) empty attempt. doGenerateImageWithRetry/
	// GenerateImage decide whether that is a hard failure.
	return images, nil
}

// doGenerateImageWithRetry calls model.DoGenerate, retrying (up to
// maxRetries) both ordinary retryable provider errors and an unclassified
// empty-image result (result.IsRetryable != false). Returns the last
// attempt's result, every attempt made for this call (for diagnostics, even
// when the last attempt has zero images and retries are exhausted), and a
// non-nil error only for a genuine failure (a provider error, or retries
// exhausted on something other than an empty-image result). Mirrors TS
// generate-image.ts's per-call retry + RetryableNoImageResultError handling
// (audit row 45099daf24 / WG10).
func doGenerateImageWithRetry(ctx context.Context, model provider.ImageModel, opts *provider.ImageGenerateOptions, maxRetries *int) (*types.ImageResult, []*types.ImageResult, error) {
	retries := preparedMaxRetries(maxRetries)

	var attempts []*types.ImageResult
	var result *types.ImageResult
	runOnce := func(retryCtx context.Context) error {
		genResult, genErr := model.DoGenerate(retryCtx, opts)
		if genErr != nil {
			return genErr
		}
		result = genResult
		attempts = append(attempts, genResult)
		if !imageResultHasImages(genResult) && imageResultIsRetryable(genResult) {
			return &retryableNoImageResultError{}
		}
		return nil
	}

	var err error
	if retries <= 0 {
		err = runOnce(ctx)
	} else {
		err = retryutil.Do(ctx, retryutil.Config{
			MaxRetries:   retries,
			InitialDelay: 2 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   2,
			Jitter:       false,
			ShouldRetry: func(err error) bool {
				return isGatewayCallRetryable(err) || isRetryableNoImageResultError(err)
			},
		}, runOnce)
	}

	if err != nil {
		if isRetryableNoImageResultError(err) {
			// Retries exhausted on an unclassified empty result: keep every
			// attempt for diagnostics and let the caller decide whether the
			// overall call (across every model call) still failed.
			return result, attempts, nil
		}
		return nil, attempts, err
	}
	return result, attempts, nil
}

// imageResultHasImages reports whether result carries at least one image in
// any of the shapes a provider may return them in.
func imageResultHasImages(result *types.ImageResult) bool {
	if result == nil {
		return false
	}
	return len(result.Images) > 0 || len(result.Base64Images) > 0 ||
		result.Base64Image != "" || len(result.Image) > 0
}

// imageResultIsRetryable reports whether an empty result may be retried.
// nil (unclassified) defaults to retryable; only an explicit false disables
// retrying, matching TS's `result.isRetryable !== false`.
func imageResultIsRetryable(result *types.ImageResult) bool {
	return result == nil || result.IsRetryable == nil || *result.IsRetryable
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

// gatewayCostMetadataKeys are the numeric cost fields the AI Gateway attaches
// to image-generation provider metadata that must be summed (not
// overwritten) across split multi-call requests. Mirrors TS
// generate-image.ts's gatewayCostMetadataKeys (audit row dd32de2 / WG10).
var gatewayCostMetadataKeys = []string{
	"cost", "gatewayCost", "inferenceCost", "inputInferenceCost",
	"marketCost", "outputInferenceCost", "surchargeCost",
}

func mergeGatewayImageMetadata(currentRaw interface{}, next map[string]interface{}) map[string]interface{} {
	current, hasCurrent := currentRaw.(map[string]interface{})

	merged := map[string]interface{}{}
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range next {
		merged[key] = value
	}

	if hasCurrent {
		for _, key := range gatewayCostMetadataKeys {
			if sum, ok := addDecimalStrings(current[key], next[key]); ok {
				merged[key] = sum
			}
		}
	}

	if images, ok := merged["images"].([]interface{}); ok && len(images) == 0 {
		delete(merged, "images")
	}
	return merged
}

var decimalStringRe = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

// addDecimalStrings adds two decimal-string numbers with exact (BigInt-based)
// arithmetic, avoiding float imprecision for currency values. Returns
// ("", false) when either value is not a valid non-negative decimal string,
// matching TS addDecimalStrings returning undefined in that case (the caller
// then leaves the field's "last write wins" value untouched).
func addDecimalStrings(v1, v2 interface{}) (string, bool) {
	s1, ok1 := v1.(string)
	s2, ok2 := v2.(string)
	if !ok1 || !ok2 || !decimalStringRe.MatchString(s1) || !decimalStringRe.MatchString(s2) {
		return "", false
	}

	int1, frac1, _ := strings.Cut(s1, ".")
	int2, frac2, _ := strings.Cut(s2, ".")
	precision := len(frac1)
	if len(frac2) > precision {
		precision = len(frac2)
	}

	n1, ok := new(big.Int).SetString(int1+frac1+strings.Repeat("0", precision-len(frac1)), 10)
	if !ok {
		return "", false
	}
	n2, ok := new(big.Int).SetString(int2+frac2+strings.Repeat("0", precision-len(frac2)), 10)
	if !ok {
		return "", false
	}

	sumStr := new(big.Int).Add(n1, n2).String()
	for len(sumStr) < precision+1 {
		sumStr = "0" + sumStr
	}

	if precision == 0 {
		return sumStr, true
	}

	result := sumStr[:len(sumStr)-precision] + "." + sumStr[len(sumStr)-precision:]
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")
	return result, true
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
