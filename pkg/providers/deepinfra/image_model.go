package deepinfra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModel implements the provider.ImageModel interface for DeepInfra.
//
// DeepInfra's image generation API is its own protocol (POST
// {baseURL}/{modelId} with num_images/aspect_ratio/width/height), not
// OpenAI-compatible; image editing uses a separate OpenAI-compatible
// /images/edits multipart endpoint. Mirrors TS DeepInfraImageModel.
type ImageModel struct {
	provider    *Provider
	modelID     string
	baseURL     string
	editBaseURL string
}

// NewImageModel creates a new DeepInfra image generation model.
func NewImageModel(p *Provider, modelID, baseURL, editBaseURL string) *ImageModel {
	return &ImageModel{provider: p, modelID: modelID, baseURL: baseURL, editBaseURL: editBaseURL}
}

// SpecificationVersion returns the specification version.
func (m *ImageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *ImageModel) Provider() string { return "deepinfra" }

// ModelID returns the model ID.
func (m *ImageModel) ModelID() string { return m.modelID }

// MaxImagesPerCall returns the maximum number of images per call.
func (m *ImageModel) MaxImagesPerCall() int { return 1 }

// DoGenerate performs image generation or, when Files are supplied, image
// editing via DeepInfra's OpenAI-compatible /images/edits endpoint.
func (m *ImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if len(opts.Files) > 0 {
		return m.doEdit(ctx, opts)
	}
	return m.doGenerate(ctx, opts)
}

func (m *ImageModel) doGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body := map[string]interface{}{
		"prompt": opts.Prompt,
	}
	if opts.N != nil {
		body["num_images"] = *opts.N
	}
	if opts.AspectRatio != "" {
		body["aspect_ratio"] = opts.AspectRatio
	}
	// Some DeepInfra models support size while others support aspect ratio;
	// allow passing either and leave validation to the server.
	if width, height, ok := splitImageSize(opts.Size); ok {
		body["width"] = width
		body["height"] = height
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	for key, value := range deepInfraImageOptions(opts.ProviderOptions) {
		body[key] = value
	}

	client := m.provider.imageClient(m.baseURL)
	resp, err := client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/" + m.modelID,
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, providererrors.NewProviderError("deepinfra", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, m.parseGenerateError(resp)
	}

	var decoded struct {
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("deepinfra: failed to decode image response: %w", err)
	}

	result := &types.ImageResult{Usage: types.ImageUsage{ImageCount: len(decoded.Images)}}
	for _, image := range decoded.Images {
		b64 := stripDataURLPrefix(image)
		result.Base64Images = append(result.Base64Images, b64)
		if decodedBytes, err := base64.StdEncoding.DecodeString(b64); err == nil {
			result.Images = append(result.Images, decodedBytes)
			if result.Image == nil {
				result.Image = decodedBytes
				result.Base64Image = b64
			}
		}
	}
	result.MimeType = "image/png"
	return result, nil
}

var dataURLPrefix = regexp.MustCompile(`^data:image/\w+;base64,`)

func stripDataURLPrefix(image string) string {
	return dataURLPrefix.ReplaceAllString(image, "")
}

func splitImageSize(size string) (width, height string, ok bool) {
	if size == "" {
		return "", "", false
	}
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type deepInfraGenerateError struct {
	Detail struct {
		Error string `json:"error"`
	} `json:"detail"`
}

func (m *ImageModel) parseGenerateError(resp *internalhttp.Response) error {
	var errResp deepInfraGenerateError
	message := string(resp.Body)
	if err := json.Unmarshal(resp.Body, &errResp); err == nil && errResp.Detail.Error != "" {
		message = errResp.Detail.Error
	}
	return providererrors.NewProviderError("deepinfra", resp.StatusCode, "", message, nil)
}

// doEdit performs image editing via DeepInfra's OpenAI-compatible
// /images/edits multipart endpoint (baseURL with /inference replaced by
// /openai, mirroring TS getEditUrl).
func (m *ImageModel) doEdit(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	body, contentType, err := m.buildEditMultipartBody(ctx, opts)
	if err != nil {
		return nil, err
	}

	client := m.provider.imageClient(m.editBaseURL)
	resp, err := client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/images/edits",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("deepinfra", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, m.parseEditError(resp)
	}

	var decoded struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		return nil, fmt.Errorf("deepinfra: failed to decode image edit response: %w", err)
	}

	result := &types.ImageResult{Usage: types.ImageUsage{ImageCount: len(decoded.Data)}}
	for _, item := range decoded.Data {
		result.Base64Images = append(result.Base64Images, item.B64JSON)
		if decodedBytes, err := base64.StdEncoding.DecodeString(item.B64JSON); err == nil {
			result.Images = append(result.Images, decodedBytes)
			if result.Image == nil {
				result.Image = decodedBytes
				result.Base64Image = item.B64JSON
			}
		}
	}
	result.MimeType = "image/png"
	return result, nil
}

type deepInfraEditError struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (m *ImageModel) parseEditError(resp *internalhttp.Response) error {
	var errResp deepInfraEditError
	message := "Unknown error"
	if err := json.Unmarshal(resp.Body, &errResp); err == nil && errResp.Error != nil && errResp.Error.Message != "" {
		message = errResp.Error.Message
	}
	return providererrors.NewProviderError("deepinfra", resp.StatusCode, "", message, nil)
}

func (m *ImageModel) buildEditMultipartBody(ctx context.Context, opts *provider.ImageGenerateOptions) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		err := func() error {
			if err := writer.WriteField("model", m.modelID); err != nil {
				return err
			}
			if opts.Prompt != "" {
				if err := writer.WriteField("prompt", opts.Prompt); err != nil {
					return err
				}
			}
			if opts.N != nil {
				if err := writer.WriteField("n", strconv.Itoa(*opts.N)); err != nil {
					return err
				}
			}
			if opts.Size != "" {
				if err := writer.WriteField("size", opts.Size); err != nil {
					return err
				}
			}
			for key, value := range deepInfraImageOptions(opts.ProviderOptions) {
				if err := writer.WriteField(key, fmt.Sprintf("%v", value)); err != nil {
					return err
				}
			}

			imageField := "image"
			if len(opts.Files) > 1 {
				imageField = "image[]"
			}
			for i, file := range opts.Files {
				if err := writeImageFilePart(ctx, writer, imageField, fmt.Sprintf("image-%d.png", i), file); err != nil {
					return err
				}
			}
			if opts.Mask != nil {
				if err := writeImageFilePart(ctx, writer, "mask", "mask.png", *opts.Mask); err != nil {
					return err
				}
			}
			return writer.Close()
		}()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	return pr, writer.FormDataContentType(), nil
}

func writeImageFilePart(ctx context.Context, writer *multipart.Writer, fieldName, filename string, file provider.ImageFile) error {
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		return err
	}
	if file.Type == "url" && file.URL != "" {
		data, err := fileutil.Download(ctx, file.URL, fileutil.DefaultDownloadOptions())
		if err != nil {
			return err
		}
		_, err = part.Write(data)
		return err
	}
	_, err = part.Write(file.Data)
	return err
}

// deepInfraImageOptions returns providerOptions.deepinfra as-is (a loose,
// already-snake_case map merged directly into the request body/form fields),
// mirroring TS's z.looseObject deepInfraImageModelOptionsSchema: fields like
// negative_prompt, num_inference_steps, guidance_scale, guidance,
// response_format, quality, style, and user pass through untransformed, and
// any other key the caller sets is forwarded too.
func deepInfraImageOptions(providerOptions map[string]interface{}) map[string]interface{} {
	opts, _ := providerOptions["deepinfra"].(map[string]interface{})
	return opts
}
