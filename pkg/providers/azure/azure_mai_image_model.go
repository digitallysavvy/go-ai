package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	stdhttp "net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// MAI rejects either side below 768px; 1024x1024 is its default output.
const (
	azureMaiImageMinSide       = 768
	azureMaiImageDefaultPixels = 1024 * 1024
)

// azureMaiImageModels are the lowercase model IDs served by the MAI image
// API, mirroring TS maiImageModels.
var azureMaiImageModels = map[string]bool{
	"mai-image-2.5":       true,
	"mai-image-2.5-flash": true,
	"mai-image-2.5-pro":   true,
	"mai-image-2.6":       true,
	"mai-image-2.6-flash": true,
}

// isMAIImageModel reports whether modelID is served by the MAI image API
// (case-insensitive), mirroring TS isMAIImageModel.
func isMAIImageModel(modelID string) bool {
	return azureMaiImageModels[strings.ToLower(modelID)]
}

// AzureImageModelOptions is the parsed providerOptions.azure for azure.Image,
// mirroring TS AzureImageModelOptions. Unknown keys are ignored.
type AzureImageModelOptions struct {
	// API selects the backend: "openai" or "mai". Defaults to "mai" for
	// MAI-Image models and "openai" otherwise.
	API string
	// AutoAspectRatio lets the model pick the output aspect ratio from the
	// prompt and any reference images. MAI-Image-2.6 models only.
	AutoAspectRatio *bool
	// WebGrounding grounds generation in Bing web search results.
	// MAI-Image-2.6 models only.
	WebGrounding *bool
}

// parseAzureImageProviderOptions parses providerOptions["azure"] for
// azure.Image, mirroring TS azureImageModelOptions (a non-strict object).
func parseAzureImageProviderOptions(providerOptions map[string]interface{}) (AzureImageModelOptions, error) {
	var result AzureImageModelOptions
	raw, ok := providerOptions["azure"]
	if !ok || raw == nil {
		return result, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return result, invalidAzureOption("providerOptions.azure", "must be an object")
	}
	if v, ok := m["api"]; ok && v != nil {
		s, ok := v.(string)
		if !ok || (s != "openai" && s != "mai") {
			return result, invalidAzureOption("providerOptions.azure.api", `must be "openai" or "mai"`)
		}
		result.API = s
	}
	for _, key := range []string{"autoAspectRatio", "webGrounding"} {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		b, ok := v.(bool)
		if !ok {
			return result, invalidAzureOption("providerOptions.azure."+key, "must be a boolean")
		}
		if key == "autoAspectRatio" {
			result.AutoAspectRatio = &b
		} else {
			result.WebGrounding = &b
		}
	}
	return result, nil
}

// azureMaiImageOnlyWarnings returns an "unsupported" warning for each
// MAI-only option that was set, used when a call is routed to the OpenAI
// backend (mirrors TS unsupportedOptionWarnings for the image model).
func azureMaiImageOnlyWarnings(opts AzureImageModelOptions) []types.Warning {
	var warnings []types.Warning
	for _, o := range []struct {
		key string
		set bool
	}{
		{"autoAspectRatio", opts.AutoAspectRatio != nil},
		{"webGrounding", opts.WebGrounding != nil},
	} {
		if o.set {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.azure." + o.key,
				Details: "This option requires the MAI image API.",
			})
		}
	}
	return warnings
}

// azureMaiImageModel calls the MAI image generations and edits endpoints
// (`/mai/v1/images/*`) on Azure AI Foundry, mirroring TS AzureMaiImageModel.
// The API returns one PNG per request and takes width/height, not size.
type azureMaiImageModel struct {
	modelID    string
	url        func(path string) (string, error)
	headers    func(ctx context.Context) (map[string]string, error)
	httpClient *stdhttp.Client
	now        func() time.Time
}

type azureMaiImageResponse struct {
	Created *float64 `json:"created"`
	Size    *string  `json:"size"`
	Usage   *struct {
		NumInputTextTokens  *int `json:"num_input_text_tokens"`
		NumInputImageTokens *int `json:"num_input_image_tokens"`
		NumOutputTokens     *int `json:"num_output_tokens"`
	} `json:"usage"`
	Data []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}

type azureMaiImageCallResult struct {
	response azureMaiImageResponse
	headers  map[string]string
}

func (m *azureMaiImageModel) doGenerate(ctx context.Context, opts *provider.ImageGenerateOptions, maiOpts AzureImageModelOptions) (*types.ImageResult, error) {
	currentDate := time.Now()
	if m.now != nil {
		currentDate = m.now()
	}
	warnings := []types.Warning{}

	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	if opts.Mask != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "mask",
			Details: "MAI image edits do not support masks.",
		})
	}

	var width, height *int
	if w, h, ok := parseAzureMaiImageSize(opts.Size); ok {
		width, height = &w, &h
	}
	if opts.Size != "" && opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "aspectRatio is ignored when size is set.",
		})
	} else if opts.AspectRatio != "" {
		if w, h, ok := azureMaiDimensionsForAspectRatio(opts.AspectRatio); ok {
			width, height = &w, &h
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "aspectRatio",
				Details: fmt.Sprintf("Invalid aspect ratio: %s.", opts.AspectRatio),
			})
		}
	}

	fields := []azureMaiField{
		{"model", m.modelID},
		{"prompt", opts.Prompt},
	}
	// TS sends { model, prompt, width, height, auto_aspect_ratio, web_grounding }.
	jsonBody := providerutils.OrderedMap{
		Keys: []string{"model", "prompt"},
		Vals: map[string]interface{}{"model": m.modelID, "prompt": opts.Prompt},
	}
	addField := func(jsonKey string, value interface{}) {
		fields = append(fields, azureMaiField{jsonKey, fmt.Sprint(value)})
		jsonBody.Keys = append(jsonBody.Keys, jsonKey)
		jsonBody.Vals[jsonKey] = value
	}
	if width != nil {
		addField("width", *width)
	}
	if height != nil {
		addField("height", *height)
	}
	if maiOpts.AutoAspectRatio != nil {
		addField("auto_aspect_ratio", *maiOpts.AutoAspectRatio)
	}
	if maiOpts.WebGrounding != nil {
		addField("web_grounding", *maiOpts.WebGrounding)
	}

	var images []azureMaiImageFile
	for _, file := range opts.Files {
		img, err := m.fileToImage(ctx, file, len(images)+1)
		if err != nil {
			return nil, err
		}
		images = append(images, img)
	}

	n := 1
	if opts.N != nil && *opts.N > 1 {
		n = *opts.N
	}
	// The API ignores `n`, so a call reached with n > 1 sends one request per
	// image.
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]azureMaiImageCallResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = m.call(callCtx, opts.Headers, jsonBody, fields, images)
			if errs[i] != nil {
				cancel()
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	var (
		usage                            types.ImageUsage
		hasUsage                         bool
		inTotal, outTotal, totalTotal    int
		inputSeen, outputSeen, totalSeen bool
		allImages                        [][]byte
		allBase64                        []string
		metadata                         = []map[string]interface{}{}
	)
	for _, r := range results {
		resp := r.response
		var textTokens, imageTokens *int
		if resp.Usage != nil {
			textTokens, imageTokens = resp.Usage.NumInputTextTokens, resp.Usage.NumInputImageTokens
			input, inputOK := addAzureMaiTokens(textTokens, imageTokens)
			output := resp.Usage.NumOutputTokens
			hasUsage = true
			if inputOK {
				inTotal += input
				inputSeen = true
			}
			if output != nil {
				outTotal += *output
				outputSeen = true
			}
			if inputOK || output != nil {
				totalTotal += input + derefInt(output)
				totalSeen = true
			}
		}
		for _, d := range resp.Data {
			meta := map[string]interface{}{}
			if resp.Created != nil {
				meta["created"] = *resp.Created
			}
			if resp.Size != nil {
				meta["size"] = *resp.Size
			}
			if textTokens != nil {
				meta["textTokens"] = *textTokens
			}
			if imageTokens != nil {
				meta["imageTokens"] = *imageTokens
			}
			metadata = append(metadata, meta)
			decoded, err := base64.StdEncoding.DecodeString(d.B64JSON)
			if err != nil {
				return nil, providererrors.NewProviderError("azure.image", 0, "", "failed to decode image: "+err.Error(), err)
			}
			allImages = append(allImages, decoded)
			allBase64 = append(allBase64, d.B64JSON)
		}
	}
	// An empty `data` array is returned empty like TS (core retries and then
	// throws NoImageGeneratedError); allImages[0] below is only read when set.
	usage.ImageCount = len(allImages)
	if hasUsage {
		if inputSeen {
			usage.InputTokens = inTotal
		}
		if outputSeen {
			usage.OutputTokens = outTotal
		}
		if totalSeen {
			usage.TotalTokens = totalTotal
		}
	}

	var firstImage []byte
	var firstBase64 string
	if len(allImages) > 0 {
		firstImage, firstBase64 = allImages[0], allBase64[0]
	}
	return &types.ImageResult{
		Image:        firstImage,
		Images:       allImages,
		Base64Image:  firstBase64,
		Base64Images: allBase64,
		MimeType:     "image/png",
		Usage:        usage,
		Warnings:     warnings,
		ProviderMetadata: map[string]interface{}{
			"azure": map[string]interface{}{"images": metadata},
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   results[0].headers,
		},
	}, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// addAzureMaiTokens mirrors TS addTokens: undefined only when both are.
func addAzureMaiTokens(a, b *int) (int, bool) {
	if a == nil && b == nil {
		return 0, false
	}
	return derefInt(a) + derefInt(b), true
}

type azureMaiField struct{ key, value string }

type azureMaiImageFile struct {
	data      []byte
	mediaType string
	name      string
}

func (m *azureMaiImageModel) fileToImage(ctx context.Context, file provider.ImageFile, index int) (azureMaiImageFile, error) {
	var data []byte
	mediaType := file.MediaType
	if file.Type == "url" {
		result, err := fileutil.DownloadWithMetadata(ctx, file.URL, fileutil.DefaultDownloadOptions())
		if err != nil {
			return azureMaiImageFile{}, err
		}
		data = result.Data
		if mediaType == "" {
			mediaType = result.ContentType
		}
	} else {
		data = file.Data
	}
	ext := "png"
	if mediaType == "image/jpeg" {
		ext = "jpg"
	}
	return azureMaiImageFile{data: data, mediaType: mediaType, name: fmt.Sprintf("image-%d.%s", index, ext)}, nil
}

func (m *azureMaiImageModel) call(ctx context.Context, extra map[string]string, jsonBody providerutils.OrderedMap, fields []azureMaiField, images []azureMaiImageFile) (azureMaiImageCallResult, error) {
	path := "/images/generations"
	if len(images) > 0 {
		path = "/images/edits"
	}
	urlStr, err := m.url(path)
	if err != nil {
		return azureMaiImageCallResult{}, err
	}
	baseHeaders, err := m.headers(ctx)
	if err != nil {
		return azureMaiImageCallResult{}, err
	}

	var body io.Reader
	contentType := "application/json"
	if len(images) > 0 {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, f := range fields {
			if err := w.WriteField(f.key, f.value); err != nil {
				return azureMaiImageCallResult{}, err
			}
		}
		for _, img := range images {
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, img.name))
			mt := img.mediaType
			if mt == "" {
				mt = "application/octet-stream"
			}
			h.Set("Content-Type", mt)
			part, err := w.CreatePart(h)
			if err != nil {
				return azureMaiImageCallResult{}, err
			}
			if _, err := part.Write(img.data); err != nil {
				return azureMaiImageCallResult{}, err
			}
		}
		if err := w.Close(); err != nil {
			return azureMaiImageCallResult{}, err
		}
		body = &buf
		contentType = w.FormDataContentType()
	} else {
		raw, err := json.Marshal(jsonBody)
		if err != nil {
			return azureMaiImageCallResult{}, err
		}
		body = bytes.NewReader(raw)
	}

	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, urlStr, body)
	if err != nil {
		return azureMaiImageCallResult{}, err
	}
	for k, v := range internalhttp.MergeHeaders(baseHeaders, extra) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return azureMaiImageCallResult{}, providererrors.NewProviderError("azure.image", 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := fileutil.ReadResponseWithSizeLimit(resp, urlStr, fileutil.DefaultMaxDownloadSize)
	if err != nil {
		return azureMaiImageCallResult{}, err
	}
	headers := providerutils.ExtractHeaders(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return azureMaiImageCallResult{}, azureMaiImageAPIError(resp.StatusCode, headers, respBody)
	}
	if err := providerutils.MediaResponseValidator("azure.image", providerutils.SchemaAzureMAIImage)(resp.StatusCode, resp.Header, respBody); err != nil {
		return azureMaiImageCallResult{}, err
	}
	var parsed azureMaiImageResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return azureMaiImageCallResult{}, providererrors.NewProviderError("azure.image", resp.StatusCode, "", "Invalid JSON response", err)
	}
	return azureMaiImageCallResult{response: parsed, headers: headers}, nil
}

// azureMaiImageAPIError builds the error for a non-2xx MAI image response:
// the JSON error message when present, else the HTTP status text with the
// raw body preserved.
func azureMaiImageAPIError(status int, headers map[string]string, body []byte) error {
	message := stdhttp.StatusText(status)
	if message == "" {
		message = "Azure MAI image request failed with status " + strconv.Itoa(status) + "."
	}
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		message = parsed.Error.Message
	}
	return &providererrors.ProviderError{
		Provider:        "azure.image",
		StatusCode:      status,
		Message:         message,
		ResponseHeaders: headers,
		ResponseBody:    string(body),
	}
}

// parseAzureMaiImageSize parses a "WxH" size string.
func parseAzureMaiImageSize(size string) (int, int, bool) {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.Atoi(parts[0])
	h, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return w, h, true
}

// azureMaiDimensionsForAspectRatio keeps the default 1024x1024 pixel budget,
// raising the short side to 768, mirroring TS dimensionsForAspectRatio.
func azureMaiDimensionsForAspectRatio(aspectRatio string) (int, int, bool) {
	parts := strings.Split(aspectRatio, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, err1 := strconv.ParseFloat(parts[0], 64)
	h, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || !(w > 0 && h > 0) {
		return 0, 0, false
	}

	ratio := w / h
	width := math.Sqrt(azureMaiImageDefaultPixels * ratio)
	height := width / ratio
	if math.Min(width, height) < azureMaiImageMinSide {
		if width < height {
			width, height = azureMaiImageMinSide, azureMaiImageMinSide/ratio
		} else {
			width, height = azureMaiImageMinSide*ratio, azureMaiImageMinSide
		}
	}
	floor16 := func(v float64) int { return int(math.Floor(v/16+1e-9)) * 16 }
	return max(azureMaiImageMinSide, floor16(width)), max(azureMaiImageMinSide, floor16(height)), true
}
