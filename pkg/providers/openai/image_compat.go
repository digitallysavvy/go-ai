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
	key = strings.TrimSpace(key)
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
		if raw, ok := providerOptions[k].(map[string]interface{}); ok {
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

	var response openaiImageResponse
	var httpResp *internalhttp.Response
	var err error
	if len(opts.Files) > 0 {
		var body io.Reader
		var contentType string
		body, contentType, err = m.buildCompatEditBody(ctx, opts, args)
		if err != nil {
			return nil, err
		}
		httpResp, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    "/images/edits",
			Query:   m.provider.config.ImageQuery,
			Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
			Body:    body,
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
			Method:  http.MethodPost,
			Path:    "/images/generations",
			Query:   m.provider.config.ImageQuery,
			Body:    body,
			Headers: opts.Headers,
		}, &response)
	}
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	images := make([][]byte, 0, len(response.Data))
	b64 := make([]string, 0, len(response.Data))
	for _, d := range response.Data {
		raw, err := base64.StdEncoding.DecodeString(d.B64JSON)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 image: %w", err)
		}
		images = append(images, raw)
		b64 = append(b64, d.B64JSON)
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

// buildCompatEditBody builds the multipart form of TS convertToFormData:
// model, prompt, image(s), mask, n, size, then the provider options; list
// values use `key[]` unless they hold a single element.
func (m *ImageModel) buildCompatEditBody(ctx context.Context, opts *provider.ImageGenerateOptions, args map[string]interface{}) (io.Reader, string, error) {
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
			field := "image"
			if len(opts.Files) > 1 {
				field = "image[]"
			}
			for _, f := range opts.Files {
				if err := m.writeImageFilePart(ctx, writer, field, "", f); err != nil {
					return err
				}
			}
			if opts.Mask != nil {
				if err := m.writeImageFilePart(ctx, writer, "mask", "", *opts.Mask); err != nil {
					return err
				}
			}
			if opts.N != nil {
				if err := writer.WriteField("n", fmt.Sprint(*opts.N)); err != nil {
					return err
				}
			}
			if opts.Size != "" {
				if err := writer.WriteField("size", opts.Size); err != nil {
					return err
				}
			}
			keys := make([]string, 0, len(args))
			for k := range args {
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

// writeFormValue appends a form value like FormData.append: scalars as their
// string form, lists as repeated `key[]` (a single element keeps the bare key).
func writeFormValue(w *multipart.Writer, key string, v interface{}) error {
	switch x := v.(type) {
	case nil:
		return nil
	case []interface{}:
		if len(x) == 1 {
			return writeFormValue(w, key, x[0])
		}
		for _, e := range x {
			if err := writeFormValue(w, key+"[]", e); err != nil {
				return err
			}
		}
		return nil
	case string:
		return w.WriteField(key, x)
	case float64:
		if x == float64(int64(x)) {
			return w.WriteField(key, fmt.Sprint(int64(x)))
		}
		return w.WriteField(key, fmt.Sprint(x))
	default:
		return w.WriteField(key, fmt.Sprint(x))
	}
}
