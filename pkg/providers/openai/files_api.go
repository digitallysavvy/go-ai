package openai

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
	fileBytes, err := inlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	purpose := "assistants"
	var expiresAfter interface{}
	if opts.ProviderOptions != nil {
		if openai, ok := opts.ProviderOptions["openai"].(map[string]interface{}); ok {
			if v, ok := openai["purpose"].(string); ok && v != "" {
				purpose = v
			}
			expiresAfter = openai["expiresAfter"]
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", chooseFilename(opts.Filename))
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, err
	}
	_ = writer.WriteField("purpose", purpose)
	if expiresAfter != nil {
		_ = writer.WriteField("expires_after", fmt.Sprintf("%v", expiresAfter))
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/files",
		Body:   &body,
		Headers: map[string]string{
			"Content-Type": writer.FormDataContentType(),
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai files upload failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out struct {
		ID        string `json:"id"`
		Bytes     *int64 `json:"bytes"`
		CreatedAt *int64 `json:"created_at"`
		Filename  string `json:"filename"`
		Purpose   string `json:"purpose"`
		Status    string `json:"status"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	meta := map[string]interface{}{}
	if out.Filename != "" {
		meta["filename"] = out.Filename
	}
	if out.Purpose != "" {
		meta["purpose"] = out.Purpose
	}
	if out.Bytes != nil {
		meta["bytes"] = *out.Bytes
	}
	if out.CreatedAt != nil {
		meta["createdAt"] = *out.CreatedAt
	}
	if out.Status != "" {
		meta["status"] = out.Status
	}
	if out.ExpiresAt != nil {
		meta["expiresAt"] = *out.ExpiresAt
	}

	filename := out.Filename
	if filename == "" {
		filename = opts.Filename
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"openai": out.ID},
		MediaType:         opts.MediaType,
		Filename:          filename,
		ProviderMetadata:  map[string]interface{}{"openai": meta},
		Warnings:          []types.Warning{},
	}, nil
}

func inlineFileBytes(data types.FileData) ([]byte, error) {
	switch data.Type {
	case types.FileDataTypeData:
		if data.DataString != "" {
			return types.DecodeFileDataString(data.DataString)
		}
		return data.Data, nil
	case types.FileDataTypeText:
		return []byte(data.Text), nil
	default:
		return nil, fmt.Errorf("unsupported file data type %q", data.Type)
	}
}

func chooseFilename(filename string) string {
	if filename == "" {
		return "blob"
	}
	return filename
}
