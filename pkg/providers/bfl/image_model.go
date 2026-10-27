package bfl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// ImageModel implements the provider.ImageModel interface for Black Forest Labs
type ImageModel struct {
	provider *Provider
	modelID  string
}

// NewImageModel creates a new BFL image generation model
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
	return "black-forest-labs.image"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// SupportsFileInputs reports whether the model accepts file inputs for image
// editing. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsFileInputs() *bool {
	if bflModelAPIFor(m.modelID) != bflAPILegacy {
		return boolPtr(true)
	}
	switch m.modelID {
	case "flux-kontext-pro", "flux-kontext-max", "flux-pro-1.0-fill":
		return boolPtr(true)
	case "flux-pro-1.1-ultra", "flux-pro-1.1":
		return boolPtr(false)
	}
	return nil
}

// SupportsMaskInputs reports whether the model accepts a mask input for
// inpainting. Returns nil when support is unknown for the model ID.
func (m *ImageModel) SupportsMaskInputs() *bool {
	if m.modelID == "flux-pro-1.0-fill" {
		return boolPtr(true)
	}
	if m.SupportsFileInputs() == nil {
		return nil
	}
	return boolPtr(false)
}

func boolPtr(b bool) *bool {
	return &b
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	// TS: the response timestamp is taken before the request is sent.
	startedAt := time.Now()
	api := bflModelAPIFor(m.modelID)
	if api == bflAPILatest {
		merged, err := bflLatestProviderOptions(opts)
		if err != nil {
			return nil, err
		}
		o := *opts
		o.ProviderOptions = merged
		opts = &o
	}
	if err := validateBFLImageOptions(m.modelID, opts.ProviderOptions); err != nil {
		return nil, err
	}
	// TS getArgs parses the provider options before it checks the input image
	// count, so an invalid option wins over the image limit.
	if len(opts.Files) > 10 {
		return nil, fmt.Errorf("Black Forest Labs supports up to 10 input images.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	var reqBody map[string]interface{}
	var warnings []types.Warning
	if api != bflAPILegacy {
		var err error
		reqBody, warnings, err = m.buildFlux3Request(opts, api)
		if err != nil {
			return nil, err
		}
	} else {
		reqBody = m.buildRequestBody(opts)
		warnings = bflWarnings(opts)
	}

	// Create request
	endpoint := m.getEndpoint()
	resp, err := m.provider.client.Do(ctx, bflInternalRequest(http.MethodPost, endpoint, reqBody, opts.Headers))
	if err != nil {
		return nil, err // TS postJsonToApi lets a transport failure propagate unchanged
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, bflStatusError(resp.StatusCode, resp.Headers, resp.Body)
	}

	// TS createJsonResponseHandler(bflSubmitSchema).
	if err := validateBFLSubmitBody(resp.Body); err != nil {
		return nil, providerutils.InvalidJSONResponseError("bfl", resp.StatusCode, resp.Headers, resp.Body, err)
	}
	var createResp bflCreateResponse
	if err := json.Unmarshal(resp.Body, &createResp); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}

	// Poll for completion
	// TS combinedHeaders: provider headers (carrying the SDK user-agent suffix)
	// combined with the call headers, sent on the poll and custom-proxy download.
	requestHeaders := version.WithUserAgentSuffix(mergeStringMaps(m.provider.config.Headers, opts.Headers), version.ProviderUserAgent("black-forest-labs"))
	result, err := m.pollResult(ctx, createResp, bflPollOptions(opts.ProviderOptions), requestHeaders)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result, createResp, warnings, requestHeaders, startedAt)
}

func bflInternalRequest(method, path string, body interface{}, headers map[string]string) internalhttp.Request {
	return internalhttp.Request{Method: method, Path: path, Body: body, Headers: headers}
}

func (m *ImageModel) getEndpoint() string {
	return "/" + m.modelID
}

func (m *ImageModel) buildRequestBody(opts *provider.ImageGenerateOptions) map[string]interface{} {
	reqBody := map[string]interface{}{
		"prompt": opts.Prompt,
	}

	finalAspectRatio := opts.AspectRatio
	// Parse size if provided
	if opts.Size != "" {
		if finalAspectRatio == "" {
			finalAspectRatio = bflSizeAspectRatio(opts.Size)
		}
		// TS: width: Number(widthStr), height: Number(heightStr) from
		// size.split('x'); a side Number() cannot read is NaN, which
		// JSON.stringify writes as null (a missing side is Number(undefined)).
		parts := strings.Split(opts.Size, "x")
		width, height := jsNumber(parts[0]), math.NaN()
		if len(parts) > 1 {
			height = jsNumber(parts[1])
		}
		reqBody["width"] = bflJSONNumber(width)
		reqBody["height"] = bflJSONNumber(height)
	}
	if finalAspectRatio != "" {
		reqBody["aspect_ratio"] = finalAspectRatio
	}
	inputImageField := "input_image"
	if m.modelID == "flux-pro-1.0-fill" {
		inputImageField = "image"
	}
	for i, file := range opts.Files {
		key := inputImageField
		if i > 0 {
			key = fmt.Sprintf("%s_%d", inputImageField, i+1)
		}
		reqBody[key] = bflImageValue(file)
	}
	if opts.Mask != nil {
		reqBody["mask"] = bflImageValue(*opts.Mask)
	}
	if opts.Seed != nil {
		reqBody["seed"] = *opts.Seed
	}
	for key, value := range extractBFLProviderOptions(opts.ProviderOptions) {
		if value != nil {
			reqBody[key] = value
		}
	}

	return reqBody
}

// flux3ImageModelID is the FLUX 3 image model, which uses a stricter request
// schema than the legacy FLUX endpoints.
const flux3ImageModelID = "flux-3-image"

type bflModelAPI int

const (
	bflAPILegacy bflModelAPI = iota
	bflAPIFlux3
	bflAPILatest
)

var bflLegacyModelRe = regexp.MustCompile(`^flux-[12](?:[.-]|$)`)

// bflModelAPIFor mirrors the TS modelApi getter: the known FLUX 1/2 families
// keep the legacy request format, flux-3-image has its own API controls, and
// every other (unknown) model ID inherits the latest supported image API
// (currently FLUX 3 Image Klein).
func bflModelAPIFor(modelID string) bflModelAPI {
	if modelID == "flux-pro" || modelID == "flux-dev" ||
		strings.HasPrefix(modelID, "flux-pro-") ||
		strings.HasPrefix(modelID, "flux-kontext-") ||
		bflLegacyModelRe.MatchString(modelID) {
		return bflAPILegacy
	}
	if modelID == flux3ImageModelID {
		return bflAPIFlux3
	}
	return bflAPILatest
}

// bflLatestProviderOptions fills width/height of the latest API from size
// (explicit provider options win), returning a copy of the provider options.
func bflLatestProviderOptions(opts *provider.ImageGenerateOptions) (map[string]interface{}, error) {
	if opts.Size == "" {
		return opts.ProviderOptions, nil
	}
	// TS: width: Number(widthStr), height: Number(heightStr), then the explicit
	// provider options spread over them. A size Number() cannot read becomes
	// NaN, which only fails validation when no explicit option replaces it.
	parts := strings.Split(opts.Size, "x")
	width, height := jsNumber(parts[0]), math.NaN()
	if len(parts) > 1 {
		height = jsNumber(parts[1])
	}
	merged := make(map[string]interface{}, len(opts.ProviderOptions)+1)
	for k, v := range opts.ProviderOptions {
		merged[k] = v
	}
	bfl := map[string]interface{}{"width": sizeDimension(width), "height": sizeDimension(height)}
	if raw, ok := merged["blackForestLabs"].(map[string]interface{}); ok {
		for k, v := range raw {
			bfl[k] = v
		}
	} else if merged["blackForestLabs"] != nil {
		return merged, nil // let validation report the non-object
	}
	merged["blackForestLabs"] = bfl
	return merged, nil
}

// jsNumber mirrors JavaScript Number(string): blank is 0, 0x/0o/0b prefixes are
// integers, and anything unreadable (including Go-only syntax such as "inf")
// is NaN.
func jsNumber(s string) float64 {
	f, ok := providerutils.JSNumber(s)
	if !ok {
		return math.NaN()
	}
	return f
}

// bflJSONNumber mirrors JSON.stringify of a JS number: NaN and Infinity
// become null.
func bflJSONNumber(f float64) interface{} {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return sizeDimension(f)
}

// sizeDimension keeps whole numbers as ints (so they validate as integers) and
// anything else (NaN, fractions) as a float the integer schema rejects.
func sizeDimension(f float64) interface{} {
	if f == math.Trunc(f) && !math.IsInf(f, 0) && math.Abs(f) < 1e15 {
		return int(f)
	}
	return f
}

// flux3AspectRatios are the aspect ratios the FLUX 3 endpoint accepts.
var flux3AspectRatios = map[string]bool{
	"21:9": true, "2:1": true, "16:9": true, "3:2": true, "7:5": true,
	"4:3": true, "5:4": true, "1:1": true, "4:5": true, "3:4": true,
	"5:7": true, "2:3": true, "9:16": true, "1:2": true, "9:21": true,
}

// flux3SupportedOptions are the blackForestLabs provider options FLUX 3
// accepts; every other option produces an unsupported warning.
var flux3SupportedOptions = map[string]bool{
	"grounding": true, "pollIntervalMillis": true, "pollTimeoutMillis": true,
	"resolution": true, "safetyTolerance": true, "version": true,
}

// flux3LatestSupportedOptions are the options the latest image API accepts.
var flux3LatestSupportedOptions = map[string]bool{
	"height": true, "outputFormat": true, "pollIntervalMillis": true,
	"pollTimeoutMillis": true, "resolution": true, "safetyTolerance": true,
	"steps": true, "width": true,
}

// bflImageOptionOrder is the key order of TS blackForestLabsImageProviderOptions.
var bflImageOptionOrder = []string{
	"resolution", "grounding", "version", "imagePrompt", "imagePromptStrength",
	"inputImage", "inputImage2", "inputImage3", "inputImage4", "inputImage5",
	"inputImage6", "inputImage7", "inputImage8", "inputImage9", "inputImage10",
	"steps", "guidance", "width", "height", "outputFormat", "promptUpsampling",
	"raw", "safetyTolerance", "webhookSecret", "webhookUrl", "pollIntervalMillis", "pollTimeoutMillis",
}

// buildFlux3Request builds the FLUX 3 request body and its warnings, mirroring
// the flux-3-image branch of TS BlackForestLabsImageModel.getArgs.
func (m *ImageModel) buildFlux3Request(opts *provider.ImageGenerateOptions, api bflModelAPI) (map[string]interface{}, []types.Warning, error) {
	warnings := []types.Warning{}
	latest := api == bflAPILatest
	if opts.Size != "" && !latest {
		if opts.AspectRatio == "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "size",
				Details: "Deriving aspect_ratio from size. FLUX 3 uses the resolution provider option to select output resolution.",
			})
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "size",
				Details: "FLUX 3 ignores size when aspectRatio is provided. Use the resolution provider option to select output resolution.",
			})
		}
	}

	var raw map[string]interface{}
	if opts.ProviderOptions != nil {
		raw, _ = opts.ProviderOptions["blackForestLabs"].(map[string]interface{})
	}

	finalAspectRatio := opts.AspectRatio
	if opts.Size != "" && finalAspectRatio == "" {
		finalAspectRatio = bflSizeAspectRatio(opts.Size)
	}

	// Exact dimensions (latest API) take precedence over aspect ratio and
	// resolution.
	hasDimensions := latest && raw["width"] != nil
	// The endpoint uses 21:9 and 9:21 instead of their reduced forms.
	aspectRatio := finalAspectRatio
	if hasDimensions {
		aspectRatio = ""
	}
	switch aspectRatio {
	case "7:3":
		aspectRatio = "21:9"
	case "3:7":
		aspectRatio = "9:21"
	}
	if aspectRatio != "" && !flux3AspectRatios[aspectRatio] {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: fmt.Sprintf("FLUX 3 does not support aspect ratio %s. Using the endpoint's default auto aspect ratio.", finalAspectRatio),
		})
		aspectRatio = ""
	}
	if opts.Mask != nil {
		return nil, nil, &providererrors.UnsupportedFunctionalityError{Functionality: "FLUX 3 image masks"}
	}
	if opts.Seed != nil && !latest {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	supported := flux3SupportedOptions
	if latest {
		supported = flux3LatestSupportedOptions
	}
	// TS iterates the zod-parsed options, so warnings follow the schema's key
	// order and unknown keys (stripped by zod) never warn.
	for _, key := range bflImageOptionOrder {
		if value, ok := raw[key]; ok && value != nil && !supported[key] {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "blackForestLabs." + key})
		}
	}

	body := map[string]interface{}{"prompt": opts.Prompt}
	if aspectRatio != "" {
		body["aspect_ratio"] = aspectRatio
	}
	if len(opts.Files) > 0 {
		images := make([]string, 0, len(opts.Files))
		for _, file := range opts.Files {
			images = append(images, bflImageValue(file))
		}
		body["images"] = images
	}
	mapping := map[string]string{
		"resolution": "resolution", "grounding": "grounding",
		"safetyTolerance": "safety_tolerance", "version": "version",
	}
	if latest {
		mapping = map[string]string{
			"resolution": "resolution", "safetyTolerance": "safety_tolerance",
			"width": "width", "height": "height", "steps": "steps",
			"outputFormat": "output_format",
		}
		if opts.Seed != nil {
			body["seed"] = *opts.Seed
		}
	}
	for from, to := range mapping {
		if hasDimensions && from == "resolution" {
			continue
		}
		if v := raw[from]; v != nil {
			body[to] = v
		}
	}
	return body, warnings, nil
}

func bflWarnings(opts *provider.ImageGenerateOptions) []types.Warning {
	if opts == nil || opts.Size == "" {
		return nil
	}
	if opts.AspectRatio != "" {
		return []types.Warning{{
			Type:    "unsupported",
			Feature: "size",
			Details: "Black Forest Labs ignores size when aspectRatio is provided. Use the width and height provider options to specify dimensions for models that support them",
		}}
	}
	return []types.Warning{{
		Type:    "unsupported",
		Feature: "size",
		Details: "Deriving aspect_ratio from size. Use the width and height provider options to specify dimensions for models that support them.",
	}}
}

// bflSizeAspectRatio mirrors TS convertSizeToAspectRatio: Number() of each
// side of size.split('x'), undefined unless both are finite and positive,
// then the gcd-reduced ratio ("" for undefined).
func bflSizeAspectRatio(size string) string {
	parts := strings.Split(size, "x")
	w, h := jsNumber(parts[0]), math.NaN()
	if len(parts) > 1 {
		h = jsNumber(parts[1])
	}
	if math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) || w <= 0 || h <= 0 {
		return ""
	}
	x, y := math.Abs(w), math.Abs(h)
	for y != 0 {
		x, y = y, math.Mod(x, y)
	}
	return jsRound(w/x) + ":" + jsRound(h/x)
}

// jsRound is Math.round formatted as a JS number string.
func jsRound(f float64) string {
	return strconv.FormatFloat(math.Floor(f+0.5), 'f', -1, 64)
}

func bflImageValue(file provider.ImageFile) string {
	if file.Type == "url" || file.URL != "" {
		return file.URL
	}
	return base64.StdEncoding.EncodeToString(file.Data)
}

func extractBFLProviderOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["blackForestLabs"].(map[string]interface{})
	if !ok {
		return nil
	}
	out := map[string]interface{}{}
	mappings := map[string]string{
		"imagePromptStrength": "image_prompt_strength",
		"imagePrompt":         "image_prompt",
		"outputFormat":        "output_format",
		"promptUpsampling":    "prompt_upsampling",
		"safetyTolerance":     "safety_tolerance",
		"webhookSecret":       "webhook_secret",
		"webhookUrl":          "webhook_url",
	}
	for key, value := range raw {
		if mapped, ok := mappings[key]; ok {
			out[mapped] = value
			continue
		}
		switch key {
		case "width", "height", "steps", "guidance", "raw":
			out[key] = value
		}
	}
	return out
}

type bflPollConfig struct {
	Interval time.Duration
	Timeout  time.Duration
}

func bflPollOptions(providerOptions map[string]interface{}) bflPollConfig {
	cfg := bflPollConfig{}
	raw, _ := providerOptions["blackForestLabs"].(map[string]interface{})
	if v, ok := intFromInterface(raw["pollIntervalMillis"]); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Millisecond
	}
	if v, ok := intFromInterface(raw["pollTimeoutMillis"]); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Millisecond
	}
	return cfg
}

func (m *ImageModel) pollResult(ctx context.Context, createResp bflCreateResponse, pollCfg bflPollConfig, headers map[string]string) (bflResult, error) {
	pollInterval := pollCfg.Interval
	if pollInterval <= 0 && m.provider.config.PollIntervalMillis > 0 {
		pollInterval = time.Duration(m.provider.config.PollIntervalMillis) * time.Millisecond
	}
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	pollTimeout := pollCfg.Timeout
	if pollTimeout <= 0 && m.provider.config.PollTimeoutMillis > 0 {
		pollTimeout = time.Duration(m.provider.config.PollTimeoutMillis) * time.Millisecond
	}
	if pollTimeout <= 0 {
		pollTimeout = 60 * time.Second
	}
	pollURL := createResp.PollingURL
	if pollURL == "" {
		pollURL = fmt.Sprintf("/get_result?id=%s", createResp.ID)
	}
	if strings.HasPrefix(pollURL, "http://") || strings.HasPrefix(pollURL, "https://") {
		if withID, err := ensurePollingURLHasID(pollURL, createResp.ID); err == nil {
			pollURL = withID
		}
	}

	// A wall-clock deadline (TS pollForImageUrl's AbortController + setTimeout),
	// not a fixed attempt count: a slow poll request (or a slow provider) could
	// otherwise let the loop run well past pollTimeout before it notices, since
	// counting attempts only bounds wall time when every request completes
	// instantly. Deriving pollCtx from the caller's ctx and canceling it both
	// on timeout and on return also aborts any poll request in flight.
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(pollTimeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()

	for {
		body, err := m.getPollBody(pollCtx, pollURL, headers)
		if err != nil {
			if timedOut.Load() {
				return bflResult{}, fmt.Errorf("Black Forest Labs generation timed out.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			return bflResult{}, err
		}

		// TS createJsonResponseHandler(bflPollSchema): an unknown status, a
		// missing status/state or a malformed result is "Invalid JSON response".
		if err := validateBFLPollBody(body); err != nil {
			return bflResult{}, providerutils.InvalidJSONResponseError("bfl", http.StatusOK, nil, body, err)
		}
		var result bflResult
		if err := json.Unmarshal(body, &result); err != nil {
			return bflResult{}, fmt.Errorf("failed to decode result: %w", err)
		}
		if result.Status == "" {
			result.Status = result.State
		}

		if result.Status == "Ready" {
			if result.Result.Sample == "" {
				return bflResult{}, fmt.Errorf("Black Forest Labs poll response is Ready but missing result.sample") //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
			}
			return result, nil
		}

		if result.Status == "Error" || result.Status == "Failed" {
			return bflResult{}, fmt.Errorf("Black Forest Labs generation failed.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if result.Status == "Request Moderated" || result.Status == "Content Moderated" || result.Status == "Task not found" {
			return bflResult{}, fmt.Errorf("Black Forest Labs generation failed: %s.", result.Status) //nolint:staticcheck // matches TS SDK's exact error text
		}

		select {
		case <-pollCtx.Done():
			if timedOut.Load() {
				return bflResult{}, fmt.Errorf("Black Forest Labs generation timed out.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			return bflResult{}, pollCtx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// getPollBody fetches the poll status body. pollURL is provider-response
// data (bflCreateResponse.PollingURL, or a path resolved against the
// developer-configured base URL as a fallback), so it must be validated and
// DNS-pinned exactly like downloadImage's response-supplied sample URL --
// mirrors TS pollForImageUrl's getFromApi({validateUrl: true, trustedOrigin}),
// which validates the poll URL and only forwards credentials when it stays on
// a trusted host. Before this fix the request went straight through the
// provider's plain HTTP client with no SSRF check, no DNS pinning and no
// redirect protection, so a malicious or compromised poll URL could reach an
// internal address (e.g. the cloud metadata endpoint).
func (m *ImageModel) getPollBody(ctx context.Context, pollURL string, headers map[string]string) ([]byte, error) {
	resolvedURL := pollURL
	if !strings.HasPrefix(pollURL, "http://") && !strings.HasPrefix(pollURL, "https://") {
		resolvedURL = strings.TrimRight(m.provider.baseURL(), "/") + "/" + strings.TrimLeft(pollURL, "/")
	}
	baseURL := m.provider.baseURL()
	isTrusted := func(raw string) bool { return bflTrustedURL(raw, baseURL) }
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	opts.URLValidator = fileutil.TrustedURLValidator(isTrusted)
	opts.Transport = fileutil.TrustRoutingTransport(isTrusted, nil, downloadTransport())
	if err := opts.URLValidator(resolvedURL); err != nil {
		return nil, err
	}
	trusted := isTrusted(resolvedURL)
	// Validates each redirect hop and drops credentials on cross-origin hops.
	client := fileutil.NewDownloadClient(resolvedURL, opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return nil, err
	}
	if trusted {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("X-Key") == "" {
			req.Header.Set("X-Key", providerutils.APIKeyOrEnv(m.provider.config.APIKey, "BFL_API_KEY"))
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	limit := opts.MaxSize
	if limit == 0 {
		limit = fileutil.DefaultMaxDownloadSize
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("BFL poll response exceeded maximum size of %d bytes", limit)
	}
	if resp.StatusCode >= 400 {
		return nil, bflStatusError(resp.StatusCode, resp.Header, body)
	}
	return body, nil
}

// bflHostIsBFL reports whether rawURL is on bfl.ai or one of its subdomains.
func bflHostIsBFL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "bfl.ai" || strings.HasSuffix(host, ".bfl.ai")
}

func bflTerminalTaskStatus(status string) bool {
	switch status {
	case "Content Moderated", "Error", "Failed", "Request Moderated", "Task not found":
		return true
	}
	return false
}

func bflTrustedURL(urlText, baseURL string) bool {
	if providerutils.IsSameOrigin(urlText, baseURL) {
		return true
	}
	u, err := url.Parse(urlText)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "bfl.ai" || strings.HasSuffix(host, ".bfl.ai")
}

func (m *ImageModel) convertResponse(ctx context.Context, result bflResult, createResp bflCreateResponse, warnings []types.Warning, headers map[string]string, startedAt time.Time) (*types.ImageResult, error) {
	if result.Result.Sample == "" {
		return nil, fmt.Errorf("no image URL in result")
	}

	// Download image from URL
	imageData, responseHeaders, err := m.downloadImage(ctx, result.Result.Sample, headers)
	if err != nil {
		return nil, err // TS getFromApi errors propagate unchanged
	}

	return &types.ImageResult{
		Image:            imageData,
		Images:           [][]byte{imageData},
		MimeType:         "image/png",
		URL:              result.Result.Sample,
		Usage:            types.ImageUsage{},
		Warnings:         warnings,
		ProviderMetadata: bflProviderMetadata(createResp, result),
		Response: &types.ResponseMetadata{
			Timestamp: startedAt,
			ModelID:   m.modelID,
			Headers:   responseHeaders,
		},
	}, nil
}

func bflProviderMetadata(createResp bflCreateResponse, result bflResult) map[string]interface{} {
	image := map[string]interface{}{}
	if result.Result.Seed != nil {
		image["seed"] = *result.Result.Seed
	}
	if result.Result.StartTime != nil {
		image["start_time"] = *result.Result.StartTime
	}
	if result.Result.EndTime != nil {
		image["end_time"] = *result.Result.EndTime
	}
	if result.Result.Duration != nil {
		image["duration"] = *result.Result.Duration
	}
	if createResp.Cost != nil {
		image["cost"] = *createResp.Cost
	}
	if createResp.InputMP != nil {
		image["inputMegapixels"] = *createResp.InputMP
	}
	if createResp.OutputMP != nil {
		image["outputMegapixels"] = *createResp.OutputMP
	}
	// TS always reports one (possibly empty) entry per image.
	return map[string]interface{}{
		"blackForestLabs": map[string]interface{}{
			"images": []map[string]interface{}{image},
		},
	}
}

// downloadTransport is the DNS-pinning transport used for response-supplied
// foreign download URLs. Tests replace it to intercept those downloads.
var downloadTransport = fileutil.SafeTransport

func (m *ImageModel) downloadImage(ctx context.Context, url string, headers map[string]string) ([]byte, map[string]string, error) {
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := fileutil.Download(ctx, url, fileutil.DefaultDownloadOptions())
		return data, nil, err
	}
	baseURL := m.provider.baseURL()
	isTrusted := func(raw string) bool { return bflTrustedURL(raw, baseURL) }
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	// The developer-configured origin (which may be self-hosted, e.g.
	// http://localhost) and any *.bfl.ai host are trusted and use the plain
	// transport with no SSRF check; every other hop -- including a redirect
	// away from a trusted origin -- is validated and DNS-pinned. Choosing the
	// transport per hop (rather than once for the whole request, as before)
	// matches TS fetchWithValidatedRedirects, and validating trusted hops
	// through isTrusted rather than unconditionally also fixes the previous
	// rejection of a self-hosted trusted base URL.
	opts.URLValidator = fileutil.TrustedURLValidator(isTrusted)
	opts.Transport = fileutil.TrustRoutingTransport(isTrusted, nil, downloadTransport())
	if err := opts.URLValidator(url); err != nil {
		return nil, nil, err
	}
	// Signed BFL downloads need no credentials; only a custom proxy (a
	// same-origin, non-bfl.ai base URL) receives provider and request headers.
	trusted := providerutils.IsSameOrigin(url, baseURL) && !bflHostIsBFL(baseURL)
	// Validates each redirect hop and drops credentials on cross-origin hops.
	client := fileutil.NewDownloadClient(url, opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	if trusted {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("X-Key") == "" {
			req.Header.Set("X-Key", providerutils.APIKeyOrEnv(m.provider.config.APIKey, "BFL_API_KEY"))
		}
	} else {
		// Like TS ({'user-agent': combinedHeaders['user-agent']}), only the
		// user agent travels to delivery hosts: the combined provider/call
		// value (the caller's own agent plus the SDK suffix).
		userAgent := headers["user-agent"]
		if userAgent == "" {
			userAgent = version.WithUserAgentSuffix(nil, version.ProviderUserAgent("black-forest-labs"))["user-agent"]
		}
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// TS createStatusCodeErrorResponseHandler: an APICallError whose
		// message is the HTTP status text.
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		cause := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Header, Body: errBody}
		return nil, nil, providererrors.NewProviderErrorFromResponse("bfl", 0, "", http.StatusText(resp.StatusCode), cause)
	}
	limit := opts.MaxSize
	if limit == 0 {
		limit = fileutil.DefaultMaxDownloadSize
	}
	if resp.ContentLength > limit {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, nil, providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).", url, limit, resp.ContentLength), nil)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, limit), nil)
	}
	return data, providerutils.ExtractHeaders(resp.Header), nil
}

func mergeStringMaps(headers ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, h := range headers {
		for k, v := range h {
			merged[k] = v
		}
	}
	return merged
}

type bflCreateResponse struct {
	ID         string   `json:"id"`
	PollingURL string   `json:"polling_url"`
	Cost       *float64 `json:"cost"`
	InputMP    *float64 `json:"input_mp"`
	OutputMP   *float64 `json:"output_mp"`
}

type bflResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // "Pending", "Reasoning", "Generating", "Ready", "Error", "Failed", "Request Moderated", "Content Moderated", "Task not found"
	State  string `json:"state"`
	Result struct {
		Sample    string   `json:"sample"` // URL to the generated image
		Seed      *float64 `json:"seed"`
		StartTime *float64 `json:"start_time"`
		EndTime   *float64 `json:"end_time"`
		Duration  *float64 `json:"duration"`
	} `json:"result"`
}

func intFromInterface(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
