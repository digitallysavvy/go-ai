package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type FilesAPI struct {
	provider *Provider
}

func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	content, err := inlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// The part carries the media type like TS's typed Blob; CreateFormFile
	// would label every upload application/octet-stream.
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeMultipartQuotes(chooseFilename(opts.Filename))))
	if opts.MediaType != "" {
		partHeader.Set("Content-Type", opts.MediaType)
	} else {
		partHeader.Set("Content-Type", "application/octet-stream")
	}
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/files",
		Body:   &body,
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Content-Type":   writer.FormDataContentType(),
			"anthropic-beta": BetaHeaderFilesAPI,
		}, opts.Headers),
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, anthropicHTTPError(f.provider.Name(), resp.StatusCode, resp.Headers, resp.Body, nil)
	}

	// TS createJsonResponseHandler(anthropicUploadFileResponseSchema).
	if err := anthropicSchemaValidator(f.provider.Name(), zFileUpload)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var out struct {
		ID           string `json:"id"`
		Filename     string `json:"filename"`
		MimeType     string `json:"mime_type"`
		SizeBytes    int64  `json:"size_bytes"`
		CreatedAt    string `json:"created_at"`
		Downloadable *bool  `json:"downloadable"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	meta := map[string]interface{}{
		"filename":  out.Filename,
		"mimeType":  out.MimeType,
		"sizeBytes": out.SizeBytes,
		"createdAt": out.CreatedAt,
	}
	if out.Downloadable != nil {
		meta["downloadable"] = *out.Downloadable
	}

	filename := out.Filename
	if filename == "" {
		filename = opts.Filename
	}
	mediaType := out.MimeType
	if mediaType == "" {
		mediaType = opts.MediaType
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"anthropic": out.ID},
		MediaType:         mediaType,
		Filename:          filename,
		ProviderMetadata:  map[string]interface{}{"anthropic": meta},
		Warnings:          []types.Warning{},
	}, nil
}

var multipartQuoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeMultipartQuotes(s string) string { return multipartQuoteEscaper.Replace(s) }
