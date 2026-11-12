package openai

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// compatImageArgs mirrors TS OpenAICompatibleImageModel#getArgs: the provider
// options under the raw provider name, overridden by the camelCase name, are
// merged into the request body verbatim.
func (m *ImageModel) compatImageArgs(providerOptions map[string]interface{}) (map[string]interface{}, []types.Warning) {
	key, _, _ := strings.Cut(m.provider.Name(), ".")
	key = providerutils.JSTrim(key) // TS provider.split('.')[0].trim()
	camel := providerutils.ToOpenAICompatibleCamelCase(key)
	args := map[string]interface{}{}
	var warnings []types.Warning
	if camel != key {
		if v, ok := providerOptions[key]; ok && v != nil {
			msg := "Use '" + camel + "' instead."
			warnings = append(warnings, types.Warning{Type: "deprecated", Setting: "providerOptions key '" + key + "'", Message: msg, Details: msg})
		}
	}
	for _, k := range []string{key, camel} {
		if raw, ok := providerutils.NormalizeProviderOptionsObject(providerOptions[k]); ok {
			for name, v := range raw {
				args[name] = v
			}
		}
	}
	return args, warnings
}

// doCompat is OpenAICompatibleImageModel#doGenerate: JSON /images/generations
// or multipart /images/edits, provider options passed through, no
// response_format.
func (m *ImageModel) doCompat(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	var warnings []types.Warning
	if opts.AspectRatio != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "aspectRatio", Details: "This model does not support aspect ratio. Use `size` instead."})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	args, argWarnings := m.compatImageArgs(opts.ProviderOptions)
	warnings = append(warnings, argWarnings...)

	var response openaiCompatImageResponse
	var httpResp *internalhttp.Response
	var err error
	if len(opts.Files) > 0 {
		var body io.Reader
		var contentType string
		if opts, err = prefetchOpenAIImageInputs(ctx, opts); err != nil {
			return nil, err
		}
		body, contentType, err = m.buildCompatEditBody(ctx, opts, args)
		if err != nil {
			return nil, err
		}
		httpResp, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
			ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaCompatImage),
			Method:           http.MethodPost,
			Path:             "/images/edits",
			Query:            m.provider.config.ImageQuery,
			Headers:          internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
			Body:             body,
		}, &response)
	} else {
		body := map[string]interface{}{"model": m.modelID}
		if opts.Prompt != "" {
			body["prompt"] = opts.Prompt
		}
		if opts.N != nil {
			body["n"] = *opts.N
		}
		if opts.Size != "" {
			body["size"] = opts.Size
		}
		for k, v := range args {
			body[k] = v
		}
		httpResp, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
			ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaCompatImage),
			Method:           http.MethodPost,
			Path:             "/images/generations",
			Query:            m.provider.config.ImageQuery,
			Body:             body,
			Headers:          opts.Headers,
		}, &response)
	}
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		if noResp, ok := internalhttp.NoResponseError(err); ok {
			return nil, noResp // TS handleFetchError: an abort or timeout is returned unchanged
		}
		if providererrors.IsLoadAPIKeyError(err) {
			return nil, err // TS throws LoadAPIKeyError unwrapped
		}
		if pe, ok := m.provider.apiError(m.Provider(), err); ok {
			return nil, pe // the wrapper's TS errorStructure
		}
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	images := make([][]byte, 0, len(response.Data))
	b64 := make([]string, 0, len(response.Data))
	// TS getImages: base64 stays untouched when every item has it; items
	// with only a URL are downloaded (in order). The provider headers go only
	// to the configured API origin (TS credentialedOrigin); other hosts get
	// the non-credential allowlist, and cross-origin redirects drop them all.
	for _, d := range response.Data {
		if d.B64JSON != nil { // TS b64_json != null: "" is data
			raw, err := fileutil.ConvertBase64ToBytes(*d.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 image: %w", err)
			}
			images = append(images, raw)
			b64 = append(b64, *d.B64JSON)
			continue
		}
		if d.URL == nil {
			return nil, providererrors.NewProviderError(m.Provider(), 0, "", "Expected url in the image response", nil)
		}
		dlOpts := fileutil.TrustedOriginDownloadOptions(m.provider.config.BaseURL, m.provider.client.HTTPClient().Transport)
		// TS resolves config.headers() inside the call: an APIKeyFunc gets
		// the caller's ctx, once per download, and its error fails the call.
		providerHeaders, err := m.provider.client.RequestHeadersCtx(ctx)
		if err != nil {
			return nil, err
		}
		dlOpts.Headers = m.compatDownloadHeaders(providerHeaders, *d.URL, opts.Headers)
		raw, err := fileutil.Download(ctx, *d.URL, dlOpts)
		if err != nil {
			// TS getFromApi with createStatusCodeErrorResponseHandler: an
			// HTTP failure is an APICallError with the status text; any
			// other failure (URL validation, abort) propagates unchanged.
			return nil, providerutils.StatusCodeDownloadError(m.Provider(), err)
		}
		images = append(images, raw)
		b64 = append(b64, base64.StdEncoding.EncodeToString(raw))
	}
	result := &types.ImageResult{
		Images:       images,
		Base64Images: b64,
		MimeType:     "image/png",
		Usage:        types.ImageUsage{ImageCount: len(images)},
		Warnings:     warnings,
		Response:     &types.ResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID, Headers: providerutils.ExtractHeaders(httpResp.Headers)},
	}
	if len(images) > 0 {
		result.Image, result.Base64Image = images[0], b64[0]
	}
	if u := response.Usage; u != nil {
		result.Usage.InputTokens, result.Usage.OutputTokens, result.Usage.TotalTokens = u.InputTokens, u.OutputTokens, u.TotalTokens
	}
	return result, nil
}

// openaiCompatImageResponse is TS openaiCompatibleImageResponseSchema: each
// item has b64_json or url (nullish strings, so a present "" is a value).
type openaiCompatImageResponse struct {
	Data []struct {
		B64JSON *string `json:"b64_json"`
		URL     *string `json:"url"`
	} `json:"data"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// safeUntrustedFirstHopHeaders mirrors TS SAFE_UNTRUSTED_FIRST_HOP_HEADERS:
// non-credential request metadata that may reach a host other than the
// configured API origin.
var safeUntrustedFirstHopHeaders = map[string]bool{
	"accept": true, "accept-language": true, "baggage": true, "cache-control": true,
	"idempotency-key": true, "if-match": true, "if-modified-since": true,
	"if-none-match": true, "if-range": true, "if-unmodified-since": true,
	"pragma": true, "range": true, "traceparent": true, "tracestate": true,
	"user-agent": true, "x-correlation-id": true, "x-request-id": true,
}

// compatDownloadHeaders returns the headers for downloading a URL-only image
// (TS fetchUntrustedUrl with credentialedOrigin = the request URL): the
// provider and call headers when the URL is same-origin with the configured
// base URL, otherwise only the non-credential allowlist.
func (m *ImageModel) compatDownloadHeaders(providerHeaders map[string]string, imageURL string, callHeaders map[string]string) map[string]string {
	all := internalhttp.MergeHeaders(providerHeaders, callHeaders)
	same := fileutil.IsSameOrigin(imageURL, m.provider.config.BaseURL)
	out := make(map[string]string, len(all))
	for k, v := range all {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "content-length" {
			continue // request-body metadata of the POST, not of the download
		}
		if same || safeUntrustedFirstHopHeaders[lk] {
			out[k] = v
		}
	}
	return out
}

// buildCompatEditBody builds the multipart form of TS convertToFormData:
// model, prompt, image(s), mask, n, size, then the provider options; list
// values use `key[]` unless they hold a single element.
func (m *ImageModel) buildCompatEditBody(ctx context.Context, opts *provider.ImageGenerateOptions, args map[string]interface{}) (io.Reader, string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			// TS {model, prompt, image, mask, n, size, ...args}: a provider
			// option with one of these keys replaces the value in place (and
			// a null one removes it); the other options follow in sorted
			// order (a Go map has no insertion order).
			override := func(key string) (bool, error) {
				v, ok := args[key]
				if !ok {
					return false, nil
				}
				return true, writeFormValue(writer, key, v)
			}
			if done, err := override("model"); err != nil {
				return err
			} else if !done {
				if err := providerutils.WriteFormDataField(writer, "model", m.modelID); err != nil {
					return err
				}
			}
			if done, err := override("prompt"); err != nil {
				return err
			} else if !done && opts.Prompt != "" {
				if err := providerutils.WriteFormDataField(writer, "prompt", opts.Prompt); err != nil {
					return err
				}
			}
			if done, err := override("image"); err != nil {
				return err
			} else if !done {
				field := "image"
				if len(opts.Files) > 1 {
					field = "image[]"
				}
				for _, f := range opts.Files {
					if err := m.writeImageFilePart(ctx, writer, field, "", f); err != nil {
						return err
					}
				}
			}
			if done, err := override("mask"); err != nil {
				return err
			} else if !done && opts.Mask != nil {
				if err := m.writeImageFilePart(ctx, writer, "mask", "", *opts.Mask); err != nil {
					return err
				}
			}
			if done, err := override("n"); err != nil {
				return err
			} else if !done && opts.N != nil {
				if err := providerutils.WriteFormDataField(writer, "n", fmt.Sprint(*opts.N)); err != nil {
					return err
				}
			}
			if done, err := override("size"); err != nil {
				return err
			} else if !done && opts.Size != "" {
				if err := providerutils.WriteFormDataField(writer, "size", opts.Size); err != nil {
					return err
				}
			}
			keys := make([]string, 0, len(args))
			for k := range args {
				switch k {
				case "model", "prompt", "image", "mask", "n", "size":
					continue
				}
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if err := writeFormValue(writer, k, args[k]); err != nil {
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

// writeFormValue appends a form value like TS convertToFormData: a null
// value is skipped, a single-element list keeps the bare key, a longer list
// is repeated `key[]`, and each appended value is FormData.append's
// String(value) (JS number text, "null", "[object Object]", nested lists
// joined with ",").
func writeFormValue(w *multipart.Writer, key string, v interface{}) error {
	switch x := v.(type) {
	case nil:
		return nil
	case []interface{}:
		if len(x) == 1 {
			return providerutils.WriteFormDataField(w, key, providerutils.JSString(x[0]))
		}
		for _, e := range x {
			if err := providerutils.WriteFormDataField(w, key+"[]", providerutils.JSString(e)); err != nil {
				return err
			}
		}
		return nil
	default:
		return providerutils.WriteFormDataField(w, key, providerutils.JSString(x))
	}
}
