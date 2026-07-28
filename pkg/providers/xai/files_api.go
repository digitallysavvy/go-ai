package xai

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

func (f *FilesAPI) SpecificationVersion() string {
	return "v4"
}

func (f *FilesAPI) Provider() string {
	return "xai.files"
}

func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	content, err := inlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	var teamID string
	if opts.ProviderOptions != nil {
		if xopts, ok := opts.ProviderOptions["xai"].(map[string]interface{}); ok {
			if v, ok := xopts["teamId"]; ok {
				teamIDValue, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("invalid xai files teamId %T: expected string", v)
				}
				teamID = teamIDValue
			}
		} else if xopts, ok := opts.ProviderOptions["xai"].(map[string]string); ok {
			if v, ok := xopts["teamId"]; ok {
				teamID = v
			}
		}
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
	if teamID != "" {
		_ = writer.WriteField("team_id", teamID)
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
		return nil, newXAIProviderError("xai.files", resp.StatusCode, resp.Body)
	}

	var out struct {
		ID        string `json:"id"`
		Bytes     *int64 `json:"bytes"`
		CreatedAt *int64 `json:"created_at"`
		Filename  string `json:"filename"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	filename := out.Filename
	if filename == "" {
		filename = opts.Filename
	}
	meta := map[string]interface{}{}
	if out.Filename != "" {
		meta["filename"] = out.Filename
	}
	if out.Bytes != nil {
		meta["bytes"] = *out.Bytes
	}
	if out.CreatedAt != nil {
		meta["createdAt"] = *out.CreatedAt
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"xai": out.ID},
		MediaType:         opts.MediaType,
		Filename:          filename,
		ProviderMetadata:  map[string]interface{}{"xai": meta},
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
