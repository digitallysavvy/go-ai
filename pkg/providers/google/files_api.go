package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

	baseURL := f.provider.config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseOrigin := strings.TrimSuffix(baseURL, "/v1beta")
	headers := internalhttp.MergeHeaders(map[string]string{
		"x-goog-api-key": f.provider.APIKey(),
	}, f.provider.config.Headers)

	displayName := ""
	pollIntervalMs := int64(2000)
	pollTimeoutMs := int64(300000)
	if google, ok := opts.ProviderOptions["google"].(map[string]interface{}); ok {
		if v, ok := google["displayName"].(string); ok {
			displayName = v
		}
		if v, ok := google["pollIntervalMs"].(float64); ok && v > 0 {
			pollIntervalMs = int64(v)
		}
		if v, ok := google["pollTimeoutMs"].(float64); ok && v > 0 {
			pollTimeoutMs = int64(v)
		}
	}

	initBody := map[string]interface{}{"file": map[string]interface{}{}}
	if displayName != "" {
		initBody["file"] = map[string]interface{}{"display_name": displayName}
	}
	payload, _ := json.Marshal(initBody)
	initReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseOrigin+"/upload/v1beta/files", bytes.NewReader(payload))
	for k, v := range headers {
		initReq.Header.Set(k, v)
	}
	initReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	initReq.Header.Set("X-Goog-Upload-Command", "start")
	initReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", len(content)))
	initReq.Header.Set("X-Goog-Upload-Header-Content-Type", opts.MediaType)
	initReq.Header.Set("Content-Type", "application/json")
	resp, err := f.provider.client.HTTPClient().Do(initReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to initiate resumable upload: %d %s", resp.StatusCode, string(b))
	}
	uploadURL := resp.Header.Get("x-goog-upload-url")
	if uploadURL == "" {
		return nil, fmt.Errorf("no upload URL returned from initiation request")
	}

	uploadReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(content))
	uploadReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	uploadResp, err := f.provider.client.HTTPClient().Do(uploadReq)
	if err != nil {
		return nil, err
	}
	defer uploadResp.Body.Close() //nolint:errcheck
	if uploadResp.StatusCode >= 400 {
		b, _ := io.ReadAll(uploadResp.Body)
		return nil, fmt.Errorf("failed to upload file data: %d %s", uploadResp.StatusCode, string(b))
	}
	var uploadResult struct {
		File googleFileResource `json:"file"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&uploadResult); err != nil {
		return nil, err
	}
	file := uploadResult.File

	start := time.Now()
	for file.State == "PROCESSING" {
		if time.Since(start) > time.Duration(pollTimeoutMs)*time.Millisecond {
			return nil, fmt.Errorf("file processing timed out after %dms", pollTimeoutMs)
		}
		time.Sleep(time.Duration(pollIntervalMs) * time.Millisecond)

		pollReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+file.Name, nil)
		for k, v := range headers {
			pollReq.Header.Set(k, v)
		}
		pollResp, err := f.provider.client.HTTPClient().Do(pollReq)
		if err != nil {
			return nil, err
		}
		var polled googleFileResource
		if err := json.NewDecoder(pollResp.Body).Decode(&polled); err != nil {
			_ = pollResp.Body.Close()
			return nil, err
		}
		_ = pollResp.Body.Close()
		file = polled
	}
	if file.State == "FAILED" {
		return nil, fmt.Errorf("file processing failed for %s", file.Name)
	}

	warnings := []types.Warning{}
	if opts.Filename != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "filename"})
	}

	meta := map[string]interface{}{
		"name":        file.Name,
		"displayName": file.DisplayName,
		"mimeType":    file.MimeType,
		"sizeBytes":   file.SizeBytes,
		"state":       file.State,
		"uri":         file.URI,
	}
	if file.CreateTime != "" {
		meta["createTime"] = file.CreateTime
	}
	if file.UpdateTime != "" {
		meta["updateTime"] = file.UpdateTime
	}
	if file.ExpirationTime != "" {
		meta["expirationTime"] = file.ExpirationTime
	}
	if file.SHA256Hash != "" {
		meta["sha256Hash"] = file.SHA256Hash
	}

	mediaType := file.MimeType
	if mediaType == "" {
		mediaType = opts.MediaType
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"google": file.URI},
		MediaType:         mediaType,
		ProviderMetadata:  map[string]interface{}{"google": meta},
		Warnings:          warnings,
	}, nil
}

type googleFileResource struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	MimeType       string `json:"mimeType"`
	SizeBytes      string `json:"sizeBytes"`
	CreateTime     string `json:"createTime"`
	UpdateTime     string `json:"updateTime"`
	ExpirationTime string `json:"expirationTime"`
	SHA256Hash     string `json:"sha256Hash"`
	URI            string `json:"uri"`
	State          string `json:"state"`
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
