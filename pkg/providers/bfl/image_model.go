package bfl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	return "bfl"
}

// ModelID returns the model ID
func (m *ImageModel) ModelID() string {
	return m.modelID
}

// DoGenerate performs image generation
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if opts == nil {
		opts = &provider.ImageGenerateOptions{}
	}
	if len(opts.Files) > 10 {
		return nil, fmt.Errorf("black forest labs supports up to 10 input images")
	}
	reqBody := m.buildRequestBody(opts)

	// Create request
	endpoint := m.getEndpoint()
	resp, err := m.provider.client.Do(ctx, bflInternalRequest(http.MethodPost, endpoint, reqBody, opts.Headers))
	if err != nil {
		return nil, providererrors.NewProviderError("bfl", 0, "", err.Error(), err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LBFL API returned status %d: %s", resp.StatusCode, string(resp.Body))
	}

	var createResp bflCreateResponse
	if err := json.Unmarshal(resp.Body, &createResp); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}
	if err := validateBFLCreateResponse(createResp); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}

	// Poll for completion
	requestHeaders := mergeStringMaps(m.provider.config.Headers, opts.Headers)
	result, err := m.pollResult(ctx, createResp, bflPollOptions(opts.ProviderOptions), requestHeaders)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(ctx, result, createResp, bflWarnings(opts), requestHeaders)
}

func validateBFLCreateResponse(resp bflCreateResponse) error {
	if resp.ID == "" {
		return fmt.Errorf("missing id")
	}
	if resp.PollingURL == "" {
		return fmt.Errorf("missing polling_url")
	}
	parsed, err := url.Parse(resp.PollingURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid polling_url")
	}
	return nil
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
		var width, height int
		_, _ = fmt.Sscanf(opts.Size, "%dx%d", &width, &height)
		if width > 0 && height > 0 {
			if finalAspectRatio == "" {
				finalAspectRatio = bflAspectRatio(width, height)
			}
			reqBody["width"] = width
			reqBody["height"] = height
		}
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

func bflAspectRatio(width, height int) string {
	divisor := gcd(width, height)
	return strconv.Itoa(width/divisor) + ":" + strconv.Itoa(height/divisor)
}

func gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
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
	maxAttempts := int((pollTimeout + pollInterval - 1) / pollInterval)
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	pollURL := createResp.PollingURL
	if pollURL == "" {
		pollURL = fmt.Sprintf("/get_result?id=%s", createResp.ID)
	}
	if strings.HasPrefix(pollURL, "http://") || strings.HasPrefix(pollURL, "https://") {
		separator := "?"
		if strings.Contains(pollURL, "?") {
			separator = "&"
		}
		if !strings.Contains(pollURL, "?id=") && !strings.Contains(pollURL, "&id=") {
			pollURL += separator + "id=" + createResp.ID
		}
	}

	for i := 0; i < maxAttempts; i++ {
		select {
		case <-ctx.Done():
			return bflResult{}, ctx.Err()
		default:
		}

		body, err := m.getPollBody(ctx, pollURL, headers)
		if err != nil {
			return bflResult{}, err
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
				return bflResult{}, fmt.Errorf("Black Forest Labs poll response is Ready but missing result.sample")
			}
			return result, nil
		}

		if result.Status == "Error" || result.Status == "Failed" {
			return bflResult{}, fmt.Errorf("Black Forest Labs generation failed.")
		}

		select {
		case <-ctx.Done():
			return bflResult{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return bflResult{}, fmt.Errorf("Black Forest Labs generation timed out.")
}

func (m *ImageModel) getPollBody(ctx context.Context, pollURL string, headers map[string]string) ([]byte, error) {
	resolvedURL := pollURL
	if !strings.HasPrefix(pollURL, "http://") && !strings.HasPrefix(pollURL, "https://") {
		resolvedURL = strings.TrimRight(m.provider.baseURL(), "/") + "/" + strings.TrimLeft(pollURL, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return nil, err
	}
	if bflTrustedURL(resolvedURL, m.provider.baseURL()) {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("X-Key") == "" {
			req.Header.Set("X-Key", m.provider.config.APIKey)
		}
	}
	resp, err := m.provider.client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("BFL API returned status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
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

func (m *ImageModel) convertResponse(ctx context.Context, result bflResult, createResp bflCreateResponse, warnings []types.Warning, headers map[string]string) (*types.ImageResult, error) {
	if result.Result.Sample == "" {
		return nil, fmt.Errorf("no image URL in result")
	}

	// Download image from URL
	imageData, responseHeaders, err := m.downloadImage(ctx, result.Result.Sample, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to download image: %w", err)
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
			Timestamp: time.Now(),
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
	if len(image) == 0 {
		return nil
	}
	return map[string]interface{}{
		"blackForestLabs": map[string]interface{}{
			"images": []map[string]interface{}{image},
		},
	}
}

func (m *ImageModel) downloadImage(ctx context.Context, url string, headers map[string]string) ([]byte, map[string]string, error) {
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := fileutil.Download(ctx, url, fileutil.DefaultDownloadOptions())
		return data, nil, err
	}
	opts := fileutil.DefaultDownloadOptions()
	opts.Timeout = 30 * time.Second
	if opts.URLValidator != nil {
		if err := opts.URLValidator(url); err != nil {
			return nil, nil, err
		}
	}
	client := &http.Client{Timeout: opts.Timeout}
	if opts.URLValidator != nil {
		validator := opts.URLValidator
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return providererrors.NewDownloadError(url, 0, "", "Too many redirects (max 10)", nil)
			}
			return validator(req.URL.String())
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	if bflTrustedURL(url, m.provider.baseURL()) {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("X-Key") == "" {
			req.Header.Set("X-Key", m.provider.config.APIKey)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, nil, providererrors.NewDownloadError(url, resp.StatusCode, resp.Status, "", nil)
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
	Status string `json:"status"` // "Pending", "Ready", "Error", "Failed", "Request Moderated"
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
