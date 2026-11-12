package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
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
	raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["replicate"].(map[string]interface{})
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
		// Every other Go integer and float kind (typed literals).
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return int(rv.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return int(rv.Uint()), true
		case reflect.Float32, reflect.Float64:
			return int(rv.Float()), true
		}
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

	modelID, _, hasVersion := splitReplicateModelID(m.modelID)
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
		Body:    orderedImageBody(reqBody),
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
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
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
	message, _ := replicateErrorMessageData(status, body)
	return message
}

// replicateErrorMessageData is replicateErrorMessage plus the TS
// APICallError.data: the zod output of replicateErrorSchema ({detail?,
// error?}, undeclared keys stripped) for a matching body, else nil.
func replicateErrorMessageData(status int, body []byte) (string, interface{}) {
	// z.string().optional(): absent is fine, null or a non-string is off-schema.
	var parsed struct {
		Detail json.RawMessage `json:"detail"`
		Error  json.RawMessage `json:"error"`
	}
	optional := func(raw json.RawMessage) (value string, present, ok bool) {
		if len(raw) == 0 {
			return "", false, true
		}
		if string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
			return "", false, false
		}
		return value, true, true
	}
	if len(body) > 0 && securejson.Unmarshal(body, &parsed) == nil {
		detail, hasDetail, detailOK := optional(parsed.Detail)
		errorField, hasError, errorOK := optional(parsed.Error)
		if detailOK && errorOK {
			data := map[string]interface{}{}
			if hasDetail {
				data["detail"] = detail
			}
			if hasError {
				data["error"] = errorField
			}
			switch {
			case hasDetail:
				return detail, data
			case hasError:
				return errorField, data
			}
			return "Unknown Replicate error", data
		}
	}
	return http.StatusText(status), nil
}

// replicateAPIError builds the provider error for a non-2xx Replicate
// response: status, headers and body are kept and the message follows
// replicateErrorMessage.
func replicateAPIError(status int, headers http.Header, body []byte) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: status, Headers: headers, Body: body}
	message, data := replicateErrorMessageData(status, body)
	perr := providererrors.NewProviderErrorFromResponse("replicate", 0, "", message, cause)
	perr.Data = data
	return perr
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
	if raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["replicate"]; ok && raw != nil {
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
						prefer = "wait=" + providerutils.JSNumberString(n)
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
	if _, version, ok := splitReplicateModelID(m.modelID); ok {
		body["version"] = version
	}
	return body, warnings, prefer, nil
}

// orderedImageBody sends the prediction body in the TS literal order:
// input {prompt, aspect_ratio, size, seed, num_outputs, the image inputs,
// mask, then the input options (sorted: a Go map has no caller order; a
// repeated key keeps its first position)}, then version.
func orderedImageBody(body map[string]interface{}) *jsonorder.Object {
	out := jsonorder.New()
	if input, ok := body["input"].(map[string]interface{}); ok {
		ordered := jsonorder.New()
		lead := []string{"prompt", "aspect_ratio", "size", "seed", "num_outputs", "input_image"}
		for i := 2; i <= maxFlux2InputImages; i++ {
			lead = append(lead, fmt.Sprintf("input_image_%d", i))
		}
		lead = append(lead, "image", "mask")
		for _, k := range lead {
			if v, has := input[k]; has {
				ordered.Set(k, v)
			}
		}
		rest := make([]string, 0)
		for k := range input {
			if _, done := ordered.Get(k); !done {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		for _, k := range rest {
			ordered.Set(k, input[k])
		}
		out.Set("input", ordered)
	}
	if v, ok := body["version"]; ok {
		out.Set("version", v)
	}
	return out
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

		next, err := m.pollImagePredictionOnce(ctx, current, callHeaders, i < maxAttempts-1, pollInterval)
		if err != nil {
			// TS 0d47a6cf8e: the caller aborted (during the poll or the
			// delay): cancel the prediction so it does not keep running
			// (and billing) on Replicate after we return.
			if ctx.Err() != nil {
				m.cancelPrediction(ctx, current.ID)
			}
			return replicateImagePrediction{}, err
		}
		current = next
	}

	if done, err := completedImagePrediction(current); err != nil {
		return replicateImagePrediction{}, err
	} else if done {
		return current, nil
	}
	// Polling gave up: cancel the prediction instead of leaving it running.
	m.cancelPrediction(ctx, current.ID)
	return replicateImagePrediction{}, fmt.Errorf("Replicate image generation did not complete after %d polling attempts.", maxAttempts) //nolint:staticcheck // mirrors TS message (proper noun)
}

// pollImagePredictionOnce is one TS poll iteration: GET urls.get, then wait
// the poll interval unless it is the last attempt.
func (m *ImageModel) pollImagePredictionOnce(ctx context.Context, current replicateImagePrediction, callHeaders map[string]string, wait bool, pollInterval time.Duration) (replicateImagePrediction, error) {
	pollURL := current.URLs.Get
	if pollURL == "" {
		// Unreachable after replicateImageResponseSchema (urls.get is
		// required); kept as a config-derived URL with the ID encoded.
		if current.ID == "" {
			return replicateImagePrediction{}, providererrors.NewInvalidResponseDataError(current, "Replicate prediction response is missing urls.get.")
		}
		pollURL = m.provider.baseURL() + "/predictions/" + providerutils.EncodePathSegment(current.ID)
	}
	dl := fileutil.TrustedOriginDownloadOptions(m.provider.baseURL(), m.provider.client.HTTPClient().Transport)
	dl.Headers = m.provider.credentialedHeaders(pollURL, callHeaders)
	polled, err := fileutil.PollJSON(ctx, pollURL, dl, nil)
	if err != nil {
		return replicateImagePrediction{}, m.handlePollError(ctx, err)
	}
	// TS createJsonResponseHandler(replicateImageResponseSchema).
	if err := validateImagePrediction(http.StatusOK, headersOf(polled.Headers), polled.Data); err != nil {
		return replicateImagePrediction{}, err
	}
	var next replicateImagePrediction
	if err := json.Unmarshal(polled.Data, &next); err != nil {
		return replicateImagePrediction{}, fmt.Errorf("failed to decode status response: %w", err)
	}
	if wait {
		select {
		case <-ctx.Done():
			return replicateImagePrediction{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return next, nil
}

// replicateCancelTimeout bounds the best-effort cancel request. TS sends it
// with no signal and no deadline; Go bounds it so the goroutine cannot leak
// (coordinator ruling, round 88, S7).
var replicateCancelTimeout = 30 * time.Second

// cancelPrediction is TS cancelReplicatePrediction, fire-and-forget: POST
// <baseURL>/predictions/<encoded id>/cancel with an empty body and the
// provider headers only (never the call headers or the prediction's
// urls.cancel). It never receives the caller's cancellation and every error
// is ignored, so the abort or timeout error that triggered it wins.
func (m *ImageModel) cancelPrediction(ctx context.Context, predictionID string) {
	if predictionID == "" {
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), replicateCancelTimeout)
	go func() {
		defer cancel()
		_, _ = m.provider.client.Do(cancelCtx, internalhttp.Request{
			Method: http.MethodPost,
			Path:   "/predictions/" + providerutils.EncodePathSegment(predictionID) + "/cancel",
			Body:   []byte{},
		})
	}()
}

func (m *ImageModel) handlePollError(ctx context.Context, err error) error {
	var dlErr *providererrors.DownloadError
	if errors.As(err, &dlErr) && dlErr.Body != nil {
		return replicateAPIError(dlErr.StatusCode, http.Header(dlErr.Headers), dlErr.Body)
	}
	// TS getFromApi handleFetchError: a network failure is the retryable
	// "Cannot connect to API" error and an abort stays as is; a URL or
	// redirect rejected by validateUrl keeps its DownloadError.
	if cause, ok := fileutil.TransportFailure(err); ok {
		return internalhttp.TransportErrorCtx(ctx, cause)
	}
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
			if errors.As(err, &dlErr) && dlErr.StatusCode >= 100 && (dlErr.StatusCode < 200 || dlErr.StatusCode >= 300) {
				return nil, replicateAPIError(dlErr.StatusCode, http.Header(dlErr.Headers), dlErr.Body)
			}
			// TS getFromApi lets any other failure (a URL rejected by
			// validateUrl, an abort) propagate unchanged.
			return nil, err
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
	opts.Timeout = fileutil.NoTimeout // TS getFromApi sets no timeout
	res, err := fileutil.DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		// TS getFromApi with replicateFailedResponseHandler: a non-2xx
		// download is the Replicate API error, a network failure the
		// retryable connect error.
		return nil, "", m.handlePollError(ctx, err)
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
	Error  *string     `json:"error"` // pointer: TS `error ?? 'Unknown error'` keeps ""
	URLs   struct {
		Get string `json:"get"`
	} `json:"urls"`
}

// replicateErrorOrUnknown mirrors TS's `prediction.error ?? 'Unknown error'`.
func replicateErrorOrUnknown(errMsg *string) string {
	if errMsg == nil {
		return "Unknown error"
	}
	return *errMsg
}
