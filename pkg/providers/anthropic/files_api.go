package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"

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
	part, err := writer.CreateFormFile("file", chooseFilename(opts.Filename))
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
		Path:   "/v1/files",
		Body:   &body,
		Headers: map[string]string{
			"Content-Type":   writer.FormDataContentType(),
			"anthropic-beta": BetaHeaderFilesAPI,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic files upload failed: %d %s", resp.StatusCode, string(resp.Body))
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
