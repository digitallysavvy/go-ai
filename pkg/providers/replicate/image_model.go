package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ImageModel implements the provider.ImageModel interface for Replicate
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new Replicate image generation model
func NewImageModel(provider *Provider, modelID string) *ImageModel {
	return &ImageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *ImageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *ImageModel) Provider() string {
	return "replicate"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// ReplicateImagePollOptions holds the polling-related provider options
// forwarded via ImageGenerateOptions.ProviderOptions["replicate"]. It mirrors
// the TS SDK's replicateImageModelOptionsSchema pollIntervalMillis /
// maxPollAttempts fields.
type ReplicateImagePollOptions struct {
	// PollIntervalMillis is the delay between poll attempts when a prediction
	// exceeds the synchronous "Prefer: wait" duration. Defaults to 500ms.
	PollIntervalMillis *int
	// MaxPollAttempts is the maximum number of poll attempts before giving up.
	// Defaults to 240.
	MaxPollAttempts *int
}

const (
	defaultPollIntervalMillis = 500
	defaultMaxPollAttempts    = 240
)

// extractImagePollOptions reads pollIntervalMillis/maxPollAttempts from
// opts.ProviderOptions["replicate"], falling back to the TS SDK defaults.
func extractImagePollOptions(opts *provider.ImageGenerateOptions) ReplicateImagePollOptions {
	result := ReplicateImagePollOptions{}
	if opts == nil || opts.ProviderOptions == nil {
		return result
	}
	raw, ok := opts.ProviderOptions["replicate"].(map[string]interface{})
	if !ok {
		return result
	}
	if v, ok := numberOption(raw["pollIntervalMillis"]); ok {
		result.PollIntervalMillis = &v
	}
	if v, ok := numberOption(raw["maxPollAttempts"]); ok {
		result.MaxPollAttempts = &v
	}
	return result
}

func numberOption(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

const maxFlux2InputImages = 8

var flux2ModelPattern = regexp.MustCompile(`^black-forest-labs/flux-2-`)

func (m *ImageModel) isFlux2Model() bool {
	return flux2ModelPattern.MatchString(m.modelID)
}

// MaxImagesPerCall mirrors TS ReplicateImageModel#maxImagesPerCall: Flux-2
// models support up to 8 input images, every other model 1.
func (m *ImageModel) MaxImagesPerCall() int {
	if m.isFlux2Model() {
		return maxFlux2InputImages
	}
	return 1
}

// SupportsFileInputs mirrors TS ReplicateImageModel#supportsFileInputs.
func (m *ImageModel) SupportsFileInputs() *bool {
	switch m.modelID {
	case "black-forest-labs/flux-2-pro", "black-forest-labs/flux-2-dev",
		"black-forest-labs/flux-fill-pro", "black-forest-labs/flux-fill-dev":
		t := true
		return &t
	}
	return nil
}

// SupportsMaskInputs mirrors TS ReplicateImageModel#supportsMaskInputs.
func (m *ImageModel) SupportsMaskInputs() *bool {
	switch m.modelID {
	case "black-forest-labs/flux-fill-pro", "black-forest-labs/flux-fill-dev":
		t := true
		return &t
	}
	if v := m.SupportsFileInputs(); v != nil && *v {
		f := false
		return &f
	}
	return nil
}

// imageFileToDataURI mirrors TS convertImageModelFileToDataUri.
func imageFileToDataURI(f provider.ImageFile) string {
	if f.Type == "url" {
		return f.URL
	}
	return imageutil.ConvertToDataURI(f.Data, f.MediaType)
}

// DoGenerate performs image generation (TS ReplicateImageModel#doGenerate).
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	if err := validateImageOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	reqBody, warnings, prefer, err := m.buildRequest(opts)
	if err != nil {
		return nil, err
	}
	pollOpts := extractImagePollOptions(opts)
	currentDate := time.Now()

	modelID, _, hasVersion := strings.Cut(m.modelID, ":")
	path := "/models/" + modelID + "/predictions"
	if hasVersion {
		path = "/predictions"
	}

	// Prefer: wait asks Replicate to hold the request until the prediction
	// completes (or wait=N seconds with maxWaitTimeInSeconds).
	var initial replicateImagePrediction
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    path,
		Body:    reqBody,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Prefer": prefer}),
		// TS createJsonResponseHandler(replicateImageResponseSchema).
		ValidateResponse: validateImagePrediction,
	}, &initial)
	if err != nil {
		return nil, m.handleError(err)
	}

	prediction, err := m.pollImagePrediction(ctx, initial, opts.Headers, pollOpts)
	if err != nil {
		return nil, err
	}

	if prediction.Output == nil {
		return nil, providererrors.NewInvalidResponseDataError(prediction, "Replicate image generation completed without output.")
	}

	result, err := m.convertResponse(ctx, prediction)
	if err != nil {
		return nil, err
	}
	result.Warnings = warnings
	result.Response = &types.ResponseMetadata{
		Timestamp: currentDate,
		ModelID:   m.modelID,
		Headers:   providerutils.ExtractHeaders(httpResp.Headers),
	}
	return result, nil
}

func (m *ImageModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) || providerutils.IsInvalidJSONResponse(err) {
		return err // TS throws these unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return replicateAPIError(statusErr.StatusCode, statusErr.Headers, statusErr.Body)
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// replicateErrorMessage mirrors TS replicateFailedResponseHandler: the
// message is `detail ?? error ?? 'Unknown Replicate error'` of a JSON object
// error body; a body that is not that shape falls back to the status text.
func replicateErrorMessage(status int, body []byte) string {
	var parsed struct {
		Detail *string `json:"detail"`
		Error  *string `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil {
		switch {
		case parsed.Detail != nil:
			return *parsed.Detail
		case parsed.Error != nil:
			return *parsed.Error
		}
		return "Unknown Replicate error"
	}
	return http.StatusText(status)
}

// replicateAPIError builds the provider error for a non-2xx Replicate
// response: status, headers and body are kept and the message follows
// replicateErrorMessage.
func replicateAPIError(status int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: status, Headers: headers, Body: body}
	return providererrors.NewProviderErrorFromResponse("replicate", 0, "", replicateErrorMessage(status, body), cause)
}

// buildRequest builds the prediction request body, warnings and Prefer header.
func (m *ImageModel) buildRequest(opts *provider.ImageGenerateOptions) (map[string]interface{}, []types.Warning, string, error) {
	warnings := []types.Warning{}
	input := map[string]interface{}{"prompt": opts.Prompt}
	if opts.AspectRatio != "" {
		input["aspect_ratio"] = opts.AspectRatio
	}
	if opts.Size != "" {
		input["size"] = opts.Size
	}
	if opts.Seed != nil {
		input["seed"] = *opts.Seed
	}
	if opts.N != nil {
		input["num_outputs"] = *opts.N
	}

	if len(opts.Files) > 0 {
		if m.isFlux2Model() {
			for i := 0; i < len(opts.Files) && i < maxFlux2InputImages; i++ {
				key := "input_image"
				if i > 0 {
					key = fmt.Sprintf("input_image_%d", i+1)
				}
				input[key] = imageFileToDataURI(opts.Files[i])
			}
			if len(opts.Files) > maxFlux2InputImages {
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: fmt.Sprintf("Flux-2 models support up to %d input images. Additional images are ignored.", maxFlux2InputImages),
				})
			}
		} else {
			input["image"] = imageFileToDataURI(opts.Files[0])
			if len(opts.Files) > 1 {
				warnings = append(warnings, types.Warning{
					Type:    "other",
					Message: "This Replicate model only supports a single input image. Additional images are ignored.",
				})
			}
		}
	}

	if opts.Mask != nil {
		if m.isFlux2Model() {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "Flux-2 models do not support mask input. The mask will be ignored.",
			})
		} else {
			input["mask"] = imageFileToDataURI(*opts.Mask)
		}
	}

	prefer := "wait"
	if raw, ok := opts.ProviderOptions["replicate"]; ok && raw != nil {
		repOpts, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, "", fmt.Errorf("replicate: invalid providerOptions.replicate: expected object")
		}
		for k, v := range repOpts {
			switch k {
			case "maxWaitTimeInSeconds":
				if v != nil {
					switch n := v.(type) {
					case float64:
						prefer = fmt.Sprintf("wait=%v", n)
					case int:
						prefer = fmt.Sprintf("wait=%d", n)
					case int64:
						prefer = fmt.Sprintf("wait=%d", n)
					}
				}
			case "pollIntervalMillis", "maxPollAttempts":
			default:
				input[k] = v
			}
		}
	}

	body := map[string]interface{}{"input": input}
	if _, version, ok := strings.Cut(m.modelID, ":"); ok {
		body["version"] = version
	}
	return body, warnings, prefer, nil
}

// pollImagePrediction polls the prediction's urls.get until it completes (TS
// ReplicateImageModel#pollPrediction). The poll URL comes from a response
// body, so it is SSRF-validated and credentials are only sent when it is
// same-origin with the configured base URL.
func (m *ImageModel) pollImagePrediction(ctx context.Context, prediction replicateImagePrediction, callHeaders map[string]string, pollOpts ReplicateImagePollOptions) (replicateImagePrediction, error) {
	maxAttempts := defaultMaxPollAttempts
	if pollOpts.MaxPollAttempts != nil {
		maxAttempts = *pollOpts.MaxPollAttempts
	}
	pollIntervalMillis := defaultPollIntervalMillis
	if pollOpts.PollIntervalMillis != nil {
		pollIntervalMillis = *pollOpts.PollIntervalMillis
	}
	pollInterval := time.Duration(pollIntervalMillis) * time.Millisecond

	current := prediction
	for i := 0; i < maxAttempts; i++ {
		if done, err := completedImagePrediction(current); err != nil {
			return replicateImagePrediction{}, err
		} else if done {
			return current, nil
		}

		pollURL := current.URLs.Get
		if pollURL == "" {
			if current.ID == "" {
				return replicateImagePrediction{}, providererrors.NewInvalidResponseDataError(current, "Replicate prediction response is missing urls.get.")
			}
			pollURL = strings.TrimRight(m.provider.baseURL(), "/") + "/predictions/" + current.ID
		}
		dl := fileutil.TrustedOriginDownloadOptions(m.provider.baseURL(), m.provider.client.HTTPClient().Transport)
		dl.Headers = m.provider.credentialedHeaders(pollURL, callHeaders)
		polled, err := fileutil.PollJSON(ctx, pollURL, dl, nil)
		if err != nil {
			return replicateImagePrediction{}, m.handlePollError(err)
		}
		// TS createJsonResponseHandler(replicateImageResponseSchema).
		if err := validateImagePrediction(http.StatusOK, headersOf(polled.Headers), polled.Data); err != nil {
			return replicateImagePrediction{}, err
		}
		var next replicateImagePrediction
		if err := json.Unmarshal(polled.Data, &next); err != nil {
			return replicateImagePrediction{}, fmt.Errorf("failed to decode status response: %w", err)
		}
		current = next

		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return replicateImagePrediction{}, ctx.Err()
			case <-time.After(pollInterval):
			}
		}
	}

	if done, err := completedImagePrediction(current); err != nil {
		return replicateImagePrediction{}, err
	} else if done {
		return current, nil
	}
	return replicateImagePrediction{}, fmt.Errorf("Replicate image generation did not complete after %d polling attempts.", maxAttempts) //nolint:staticcheck // mirrors TS message (proper noun)
}

func (m *ImageModel) handlePollError(err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return replicateAPIError(dlErr.StatusCode, http.Header(dlErr.Headers), dlErr.Body)
	}
	// TS getFromApi lets any other failure (an abort, "Cannot connect to
	// API", a URL rejected by validateUrl) propagate unchanged.
	return err
}

// completedImagePrediction mirrors TS getCompletedPrediction.
func completedImagePrediction(p replicateImagePrediction) (bool, error) {
	if p.Status == "failed" || p.Status == "canceled" {
		return false, providererrors.NewInvalidResponseDataError(
			p,
			fmt.Sprintf("Replicate image generation %s: %s", p.Status, replicateErrorOrUnknown(p.Error)),
		)
	}
	return p.Output != nil || p.Status == "succeeded", nil
}

// convertResponse downloads every output image (TS downloads all of them).
func (m *ImageModel) convertResponse(ctx context.Context, prediction replicateImagePrediction) (*types.ImageResult, error) {
	var urls []string
	switch v := prediction.Output.(type) {
	case []interface{}:
		for _, item := range v {
			if urlStr, ok := item.(string); ok && urlStr != "" {
				urls = append(urls, urlStr)
			}
		}
	case string:
		if v != "" {
			urls = append(urls, v)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no image URL in prediction output")
	}

	images := make([][]byte, 0, len(urls))
	mimeType := ""
	for _, u := range urls {
		data, ct, err := m.downloadImage(ctx, u)
		if err != nil {
			// TS downloads with replicateFailedResponseHandler, so an HTTP
			// failure is an API error whose message is `detail ?? error`.
			var dlErr *providererrors.DownloadError
			if errors.As(err, &dlErr) && dlErr.StatusCode >= 400 {
				return nil, replicateAPIError(dlErr.StatusCode, http.Header(dlErr.Headers), dlErr.Body)
			}
			return nil, fmt.Errorf("failed to download image: %w", err)
		}
		images = append(images, data)
		if mimeType == "" {
			mimeType = ct
		}
	}
	if mimeType == "" {
		mimeType = "image/png"
	}

	return &types.ImageResult{
		Image:    images[0],
		Images:   images,
		MimeType: mimeType,
		URL:      urls[0],
		Usage:    types.ImageUsage{ImageCount: len(images)},
	}, nil
}

// downloadImage fetches an output image. The URL is response data: it is
// SSRF-validated (same-origin with the base URL exempt) and no credentials
// are sent.
func (m *ImageModel) downloadImage(ctx context.Context, url string) ([]byte, string, error) {
	opts := fileutil.TrustedOriginDownloadOptions(m.provider.baseURL(), m.provider.client.HTTPClient().Transport)
	opts.Timeout = 30 * time.Second
	res, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, "", err
	}
	ct := res.ContentType
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if !strings.HasPrefix(ct, "image/") {
		ct = ""
	}
	return res.Data, ct, nil
}

type replicateImagePrediction struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Output interface{} `json:"output"`
	Error  string      `json:"error"`
	URLs   struct {
		Get string `json:"get"`
	} `json:"urls"`
}

// replicateErrorOrUnknown mirrors TS's `prediction.error ?? 'Unknown error'`.
func replicateErrorOrUnknown(errMsg string) string {
	if errMsg == "" {
		return "Unknown error"
	}
	return errMsg
}
