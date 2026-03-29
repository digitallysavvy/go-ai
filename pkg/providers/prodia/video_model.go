package prodia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// ProdiaVideoModel implements the provider.VideoModelV3 interface for the
// Prodia Wan 2.2 Lightning text-to-video and image-to-video models.
//
// MaxVideosPerCall is 1 — the Prodia API generates one video per request.
// For text-to-video (txt2vid) the request body is JSON.
// For image-to-video (img2vid) the request body is multipart/form-data.
type ProdiaVideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new ProdiaVideoModel for the given model ID.
func NewVideoModel(prov *Provider, modelID string) *ProdiaVideoModel {
	return &ProdiaVideoModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns "v3" to match the Go VideoModelV3 interface.
func (m *ProdiaVideoModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider identifier for this model type.
// Matches the TypeScript SDK's config.provider value: "prodia.video".
func (m *ProdiaVideoModel) Provider() string { return "prodia.video" }

// ModelID returns the model identifier.
func (m *ProdiaVideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 1 — Prodia generates at most one video per call.
func (m *ProdiaVideoModel) MaxVideosPerCall() *int {
	n := 1
	return &n
}

// ProdiaVideoProviderOptions contains Prodia-specific options for video
// generation.
type ProdiaVideoProviderOptions struct {
	// Resolution specifies the output video resolution (e.g. "480p", "720p").
	Resolution string `json:"resolution,omitempty"`
}

// extractVideoProviderOptions reads Prodia-specific options from the provider
// options map supplied in VideoModelV3CallOptions.
func extractVideoProviderOptions(opts *provider.VideoModelV3CallOptions) *ProdiaVideoProviderOptions {
	if opts.ProviderOptions == nil {
		return nil
	}
	raw, ok := opts.ProviderOptions["prodia"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var o ProdiaVideoProviderOptions
	if err := json.Unmarshal(b, &o); err != nil {
		return nil
	}
	return &o
}

// DoGenerate generates a video from the given options.
//
// When opts.Image is nil, a text-to-video request is sent as JSON.
// When opts.Image is non-nil, an image-to-video request is sent as
// multipart/form-data.
func (m *ProdiaVideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	// Validate aspect ratio if provided.
	if opts.AspectRatio != "" && !validAspectRatios[opts.AspectRatio] {
		return nil, fmt.Errorf("prodia: unsupported aspectRatio %q; valid values: 1:1, 2:3, 3:2, 4:5, 5:4, 4:7, 7:4, 9:16, 16:9, 9:21, 21:9", opts.AspectRatio)
	}

	provOpts := extractVideoProviderOptions(opts)

	jobConfig := map[string]interface{}{}
	if opts.Prompt != "" {
		jobConfig["prompt"] = opts.Prompt
	}
	if opts.Seed != nil {
		jobConfig["seed"] = *opts.Seed
	}
	if opts.AspectRatio != "" {
		jobConfig["aspect_ratio"] = opts.AspectRatio
	}
	// Resolution: prefer the standard field; fall back to provider options.
	resolution := opts.Resolution
	if resolution == "" && provOpts != nil && provOpts.Resolution != "" {
		resolution = provOpts.Resolution
	}
	if resolution != "" {
		jobConfig["resolution"] = resolution
	}

	body := map[string]interface{}{
		"type":   m.modelID,
		"config": jobConfig,
	}

	reqURL := fmt.Sprintf("%s/job?price=true", m.prov.effectiveBaseURL())
	apiKey := m.prov.effectiveAPIKey()

	var (
		respBodyBytes []byte
		respHeaders   http.Header
		reqErr        error
	)

	if opts.Image != nil {
		// img2vid: multipart/form-data request.
		imageData, imageMIME, err := resolveVideoImage(ctx, opts.Image)
		if err != nil {
			return nil, fmt.Errorf("prodia: failed to resolve input image: %w", err)
		}

		buf, contentType, err := buildMultipartJobRequest(body, imageData, imageMIME)
		if err != nil {
			return nil, fmt.Errorf("prodia: failed to build multipart request: %w", err)
		}

		respBodyBytes, respHeaders, reqErr = postMultipartToProdia(ctx, reqURL, apiKey, buf, contentType, "multipart/form-data; video/mp4")
	} else {
		// txt2vid: JSON request.
		resp, err := m.prov.client.Do(ctx, internalhttp.Request{
			Method:  "POST",
			Path:    "/job",
			Body:    body,
			Query:   map[string]string{"price": "true"},
			Headers: map[string]string{"Accept": "multipart/form-data; video/mp4"},
		})
		if err != nil {
			reqErr = fmt.Errorf("prodia API request failed: %w", err)
		} else if resp.StatusCode != 200 {
			reqErr = fmt.Errorf("prodia API returned status %d: %s", resp.StatusCode, string(resp.Body))
		} else {
			respBodyBytes = resp.Body
			respHeaders = resp.Headers
		}
	}

	if reqErr != nil {
		return nil, reqErr
	}

	// Derive the response Content-Type (needed for multipart boundary parsing).
	respCT := ""
	if respHeaders != nil {
		respCT = respHeaders.Get("Content-Type")
	}
	if respCT == "" {
		var err error
		respCT, err = detectMultipartContentType(respBodyBytes)
		if err != nil {
			return nil, fmt.Errorf("prodia: cannot detect multipart boundary: %w", err)
		}
	}

	jobResp, videoData, videoMIME, err := parseMultipartResponse(respCT, respBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("prodia: %w", err)
	}

	if len(videoData) == 0 {
		return nil, fmt.Errorf("prodia: multipart response missing output video")
	}
	if videoMIME == "" {
		videoMIME = "video/mp4"
	}

	// Convert response headers to the flat map expected by VideoModelV3ResponseInfo.
	headerMap := make(map[string]string)
	for k, vals := range respHeaders {
		if len(vals) > 0 {
			headerMap[k] = vals[0]
		}
	}

	return &provider.VideoModelV3Response{
		Videos: []provider.VideoModelV3VideoData{
			{
				Type:      "binary",
				Binary:    videoData,
				MediaType: videoMIME,
			},
		},
		ProviderMetadata: map[string]interface{}{
			"prodia": map[string]interface{}{
				"videos": []interface{}{buildProdiaProviderMetadata(jobResp)},
			},
		},
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   headerMap,
		},
	}, nil
}

// resolveVideoImage resolves a VideoModelV3File to raw bytes and a MIME type.
// For "file" type the data is used directly.
// For "url" type the URL is fetched using the shared DefaultHTTPClient.
func resolveVideoImage(ctx context.Context, file *provider.VideoModelV3File) ([]byte, string, error) {
	switch file.Type {
	case "file":
		mimeType := file.MediaType
		if mimeType == "" {
			mimeType = "image/png"
		}
		return file.Data, mimeType, nil
	case "url":
		req, err := http.NewRequestWithContext(ctx, "GET", file.URL, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create URL fetch request: %w", err)
		}
		resp, err := internalhttp.DefaultHTTPClient.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch image URL: %w", err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read image URL response: %w", err)
		}
		mimeType := resp.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return data, mimeType, nil
	default:
		return nil, "", fmt.Errorf("unsupported image file type %q", file.Type)
	}
}
