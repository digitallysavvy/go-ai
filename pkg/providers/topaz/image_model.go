package topaz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Default polling configuration for the image model, matching TS
// TopazImageModel's DEFAULT_POLL_INTERVAL_MILLIS / DEFAULT_POLL_TIMEOUT_MILLIS.
const (
	defaultImagePollIntervalMillis = 2000
	defaultImagePollTimeoutMillis  = 600_000
)

// ImageModel implements the provider.ImageModel interface for Topaz Labs.
// Topaz enhances an image that the caller supplies, so Files is required
// and exactly one image is processed per call.
type ImageModel struct {
	prov    *Provider
	modelID string
}

// NewImageModel creates a new Topaz image enhancement model.
func NewImageModel(prov *Provider, modelID string) *ImageModel {
	return &ImageModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *ImageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *ImageModel) Provider() string { return "topaz.image" }

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string { return m.modelID }

// MaxImagesPerCall returns 1: Topaz enhances a single image per call,
// matching TS TopazImageModel's `readonly maxImagesPerCall = 1`.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// DoGenerate submits an async enhance job, polls it, and downloads the
// resulting image (TS TopazImageModel#doGenerate).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	currentDate := time.Now()
	var warnings []types.Warning

	topazOpts := parseImageModelOptions(opts.ProviderOptions)
	addImageUnsupportedWarnings(opts, &warnings)

	formData, contentType, err := m.buildFormData(opts, topazOpts, &warnings)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := mergeAbortContext(ctx, opts.AbortSignal)
	defer cancel()

	resp, err := m.prov.client.Do(callCtx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/image/v1/enhance-gen/async",
		Body:   formData,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": contentType,
		}),
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	var submit topazImageSubmitResponse
	if err := json.Unmarshal(resp.Body, &submit); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode enhance response: %w", err)
	}
	if submit.ProcessID == "" {
		return nil, fmt.Errorf("Topaz did not return a process_id for the enhance request.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	pollInterval := defaultImagePollIntervalMillis * time.Millisecond
	pollTimeout := defaultImagePollTimeoutMillis * time.Millisecond
	if topazOpts != nil {
		if topazOpts.PollIntervalMillis != nil {
			pollInterval = time.Duration(*topazOpts.PollIntervalMillis) * time.Millisecond
		}
		if topazOpts.PollTimeoutMillis != nil {
			pollTimeout = time.Duration(*topazOpts.PollTimeoutMillis) * time.Millisecond
		}
	}

	status, err := m.waitForCompletion(callCtx, submit.ProcessID, opts.Headers, pollInterval, pollTimeout)
	if err != nil {
		// Topaz charges on completion, so a job nobody will download should
		// not be left running after the caller aborts. The pollTimeoutMillis
		// path above already cancels on its own internal deadline; this
		// additionally covers the caller's own ctx/AbortSignal firing mid-poll.
		if ctx.Err() != nil || (opts.AbortSignal != nil && opts.AbortSignal.Err() != nil) {
			m.cancelQuietly(context.Background(), submit.ProcessID, opts.Headers)
		}
		return nil, err
	}

	imageData, responseHeaders, err := m.download(callCtx, submit.ProcessID, opts.Headers)
	if err != nil {
		return nil, err
	}

	image := map[string]interface{}{"processId": submit.ProcessID}
	if status.Credits != nil {
		image["credits"] = *status.Credits
	}
	if status.OutputWidth != nil {
		image["width"] = *status.OutputWidth
	}
	if status.OutputHeight != nil {
		image["height"] = *status.OutputHeight
	}
	if status.OutputFormat != nil {
		image["format"] = *status.OutputFormat
	}

	return &types.ImageResult{
		Image:    imageData,
		Images:   [][]byte{imageData},
		MimeType: imageMimeType(status.OutputFormat, imageData),
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"topaz": map[string]interface{}{
				"images": []interface{}{image},
			},
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   responseHeaders,
		},
	}, nil
}

// addImageUnsupportedWarnings appends warnings for options Topaz image
// models do not support, mirroring TS TopazImageModel#addUnsupportedWarnings.
// Order matches TS exactly: prompt, aspectRatio, seed, mask, n.
func addImageUnsupportedWarnings(opts *provider.ImageGenerateOptions, warnings *[]types.Warning) {
	if opts.Prompt != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "prompt",
			Details: "Topaz image models enhance an existing image and do not take a text prompt. The prompt was ignored.",
		})
	}

	if opts.AspectRatio != "" {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "Topaz image models do not support aspectRatio. Use `size`, or the `outputWidth` / `outputHeight` provider options, to set output dimensions.",
		})
	}

	if opts.Seed != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
			Details: "Topaz image models do not support seed.",
		})
	}

	if opts.Mask != nil {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "mask",
			Details: "Topaz image models do not support masks. The mask was ignored.",
		})
	}

	if opts.N != nil && *opts.N > 1 {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "Topaz image models enhance one image per call. Only 1 image will be returned.",
		})
	}
}

// buildFormData builds the multipart/form-data body for the enhance
// request (TS TopazImageModel#buildFormData).
func (m *ImageModel) buildFormData(opts *provider.ImageGenerateOptions, topazOpts *ImageModelOptions, warnings *[]types.Warning) (io.Reader, string, error) {
	if len(opts.Files) == 0 {
		return nil, "", providererrors.NewValidationError("files",
			"Topaz image models enhance an existing image. Pass the input image via `prompt.images`.", nil)
	}

	if len(opts.Files) > 1 {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "files",
			Details: "Topaz image models enhance one image per call. Only the first file was used.",
		})
	}
	file := opts.Files[0]

	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	if err := writer.WriteField("model", resolveTopazImageAPIModelID(m.modelID)); err != nil {
		return nil, "", err
	}

	if file.Type == "url" && file.URL != "" {
		// Topaz fetches the source itself when given a URL, so the bytes
		// never need to pass through the SDK.
		if err := writer.WriteField("source_url", file.URL); err != nil {
			return nil, "", err
		}
	} else {
		part, err := writer.CreateFormFile("image", "image"+topazImageExtension(file.MediaType))
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(file.Data); err != nil {
			return nil, "", err
		}
	}

	width, height := parseSize(opts.Size)
	if topazOpts != nil && topazOpts.OutputWidth != nil {
		width = topazOpts.OutputWidth
	}
	if topazOpts != nil && topazOpts.OutputHeight != nil {
		height = topazOpts.OutputHeight
	}

	if err := writeIntField(writer, "output_width", width); err != nil {
		return nil, "", err
	}
	if err := writeIntField(writer, "output_height", height); err != nil {
		return nil, "", err
	}

	if topazOpts != nil {
		if err := writeStringField(writer, "output_format", topazOpts.OutputFormat); err != nil {
			return nil, "", err
		}
		if err := writeBoolField(writer, "crop_to_fill", topazOpts.CropToFill); err != nil {
			return nil, "", err
		}
		if err := writeStringField(writer, "webhook_url", topazOpts.WebhookURL); err != nil {
			return nil, "", err
		}

		// Model-specific settings use camelCase, unlike the request schema
		// fields above.
		if err := writeStringField(writer, "enhancementStrength", topazOpts.EnhancementStrength); err != nil {
			return nil, "", err
		}
		if err := writeBoolField(writer, "grain", topazOpts.Grain); err != nil {
			return nil, "", err
		}
		if err := writeFloatField(writer, "grainDensity", topazOpts.GrainDensity); err != nil {
			return nil, "", err
		}
		if err := writeStringField(writer, "grainModel", topazOpts.GrainModel); err != nil {
			return nil, "", err
		}
		if err := writeFloatField(writer, "grainSize", topazOpts.GrainSize); err != nil {
			return nil, "", err
		}
		if err := writeFloatField(writer, "grainStrength", topazOpts.GrainStrength); err != nil {
			return nil, "", err
		}
		if err := writeIntField(writer, "inputWidth", topazOpts.InputWidth); err != nil {
			return nil, "", err
		}
		if err := writeIntField(writer, "inputHeight", topazOpts.InputHeight); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return buf, writer.FormDataContentType(), nil
}

func writeStringField(w *multipart.Writer, key string, value *string) error {
	if value == nil {
		return nil
	}
	return w.WriteField(key, *value)
}

func writeBoolField(w *multipart.Writer, key string, value *bool) error {
	if value == nil {
		return nil
	}
	return w.WriteField(key, strconv.FormatBool(*value))
}

func writeIntField(w *multipart.Writer, key string, value *int) error {
	if value == nil {
		return nil
	}
	return w.WriteField(key, strconv.Itoa(*value))
}

func writeFloatField(w *multipart.Writer, key string, value *float64) error {
	if value == nil {
		return nil
	}
	return w.WriteField(key, strconv.FormatFloat(*value, 'g', -1, 64))
}

// parseSize parses a "{width}x{height}" call option.
func parseSize(size string) (width, height *int) {
	if size == "" {
		return nil, nil
	}
	var w, h int
	if _, err := fmt.Sscanf(size, "%dx%d", &w, &h); err != nil {
		return nil, nil
	}
	return &w, &h
}

// topazImageExtension picks a file extension for the uploaded image part
// from its media type. Topaz only processes JPEG, PNG and TIFF.
func topazImageExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/tiff", "image/tif":
		return ".tiff"
	default:
		return ".png"
	}
}

// imageMimeType derives the result's MIME type from the status response's
// output_format, falling back to sniffing the downloaded bytes.
func imageMimeType(outputFormat *string, data []byte) string {
	if outputFormat != nil {
		switch strings.ToLower(*outputFormat) {
		case "jpeg", "jpg":
			return "image/jpeg"
		case "png":
			return "image/png"
		case "tiff", "tif":
			return "image/tiff"
		}
	}
	if detected := fileutil.DetectMediaType(data); detected.MimeType != "" {
		return detected.MimeType
	}
	return "image/png"
}

// waitForCompletion polls the Topaz status endpoint until the job
// completes, fails, is cancelled, or pollTimeout elapses (TS
// TopazImageModel#waitForCompletion). ctx is checked between poll
// attempts so the caller's cancellation (or the mergeAbortContext-derived
// AbortSignal) interrupts the wait promptly instead of only once a request
// is in flight.
func (m *ImageModel) waitForCompletion(ctx context.Context, processID string, headers map[string]string, pollInterval, pollTimeout time.Duration) (*topazImageStatusResponse, error) {
	deadline := time.Now().Add(pollTimeout)
	lastStatus := "unknown"

	for {
		status, err := m.getStatus(ctx, processID, headers)
		if err != nil {
			return nil, err
		}
		if status.Status != "" {
			lastStatus = status.Status
		}

		if status.Status == "Completed" {
			return status, nil
		}
		if status.Status == "Failed" || status.Status == "Cancelled" {
			return nil, fmt.Errorf("Topaz image enhancement %s for process %s.", strings.ToLower(status.Status), processID) //nolint:staticcheck // matches TS SDK's exact error text
		}

		if time.Now().Add(pollInterval).After(deadline) {
			m.cancelQuietly(context.Background(), processID, headers)
			return nil, fmt.Errorf( //nolint:staticcheck // matches TS SDK's exact error text
				"Topaz image enhancement did not finish within %dms (process %s, last status %s). Increase the `pollTimeoutMillis` provider option if the job needs longer.",
				pollTimeout.Milliseconds(), processID, lastStatus)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (m *ImageModel) getStatus(ctx context.Context, processID string, headers map[string]string) (*topazImageStatusResponse, error) {
	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/image/v1/status/" + processID,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}
	var status topazImageStatusResponse
	if err := json.Unmarshal(resp.Body, &status); err != nil {
		return nil, fmt.Errorf("topaz: failed to decode status response: %w", err)
	}
	return &status, nil
}

// cancelQuietly cancels a Topaz image job, best-effort (TS
// TopazImageModel#cancelQuietly). Deliberately not tied to the caller's
// context, which may be the reason polling stopped.
func (m *ImageModel) cancelQuietly(ctx context.Context, processID string, headers map[string]string) {
	_, _ = m.prov.client.Do(ctx, internalhttp.Request{ //nolint:errcheck
		Method:  http.MethodDelete,
		Path:    "/image/v1/cancel/" + processID,
		Headers: headers,
	})
}

// download fetches the Topaz-hosted download URL and then downloads the
// image bytes (TS TopazImageModel#download). The download URL comes from
// the Topaz response body, so it is validated and DNS-pinned exactly like
// every other response-supplied download URL in this SDK, and the API key
// is only forwarded when the URL stays on the configured base URL.
func (m *ImageModel) download(ctx context.Context, processID string, headers map[string]string) ([]byte, map[string]string, error) {
	resp, err := m.prov.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/image/v1/download/" + processID,
		Headers: headers,
	})
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, nil, newTopazAPIError(resp.StatusCode, resp.Body, resp.Headers)
	}

	var downloadResp topazImageDownloadResponse
	if err := json.Unmarshal(resp.Body, &downloadResp); err != nil {
		return nil, nil, fmt.Errorf("topaz: failed to decode download response: %w", err)
	}
	if downloadResp.DownloadURL == "" {
		return nil, nil, fmt.Errorf("Topaz did not return a download URL for process %s.", processID) //nolint:staticcheck // matches TS SDK's exact error text
	}

	downloadOpts := fileutil.TrustedOriginDownloadOptions(m.prov.baseURL(), nil)
	if fileutil.IsSameOrigin(downloadResp.DownloadURL, m.prov.baseURL()) {
		// The download URL stays on the configured base URL, so it is
		// trusted and gets the full header set (provider defaults,
		// including the API key, plus any per-call headers) -- not just
		// the per-call headers already passed through client.Do for the
		// earlier calls in this method, which merges in the client's own
		// defaults automatically.
		downloadOpts.Headers = internalhttp.MergeHeaders(m.prov.client.Headers(), headers)
	}

	result, err := fileutil.DownloadWithMetadata(ctx, downloadResp.DownloadURL, downloadOpts)
	if err != nil {
		return nil, nil, err
	}

	responseHeaders := make(map[string]string, len(result.Headers))
	for k, vs := range result.Headers {
		if len(vs) > 0 {
			responseHeaders[k] = vs[0]
		}
	}
	return result.Data, responseHeaders, nil
}

type topazImageSubmitResponse struct {
	ProcessID string `json:"process_id"`
}

type topazImageStatusResponse struct {
	// Documented values are Pending, Processing, Completed, Cancelled and
	// Failed. Unknown values keep polling rather than failing the parse.
	Status       string   `json:"status"`
	Credits      *float64 `json:"credits"`
	OutputWidth  *float64 `json:"output_width"`
	OutputHeight *float64 `json:"output_height"`
	OutputFormat *string  `json:"output_format"`
}

type topazImageDownloadResponse struct {
	DownloadURL string `json:"download_url"`
}
