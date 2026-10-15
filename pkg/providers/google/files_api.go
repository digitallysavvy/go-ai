package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"io"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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
	}, f.provider.config.Headers, opts.Headers)

	displayName := ""
	pollIntervalMs := int64(2000)
	pollTimeoutMs := int64(300000)
	if google, ok := opts.ProviderOptions["google"].(map[string]interface{}); ok {
		if v, ok := google["displayName"].(string); ok {
			displayName = v
		}
		if v, ok := googleFilesNumber(google["pollIntervalMs"]); ok && v > 0 {
			pollIntervalMs = v
		}
		if v, ok := googleFilesNumber(google["pollTimeoutMs"]); ok && v > 0 {
			pollTimeoutMs = v
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
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", fmt.Sprintf("Failed to initiate resumable upload: %d %s", resp.StatusCode, string(b)))
	}
	uploadURL := resp.Header.Get("x-goog-upload-url")
	if uploadURL == "" {
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", "No upload URL returned from initiation request")
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
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", fmt.Sprintf("Failed to upload file data: %d %s", uploadResp.StatusCode, string(b)))
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
			return nil, googleFilesError("GOOGLE_FILES_UPLOAD_TIMEOUT", fmt.Sprintf("File processing timed out after %dms", pollTimeoutMs))
		}
		// select on ctx.Done() during the wait (not a plain time.Sleep), so
		// cancellation/timeout is honored promptly instead of being delayed
		// by up to a full poll interval.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(pollIntervalMs) * time.Millisecond):
		}

		pollReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+googleFilePath(file.Name), nil)
		for k, v := range headers {
			pollReq.Header.Set(k, v)
		}
		pollResp, err := f.provider.client.HTTPClient().Do(pollReq)
		if err != nil {
			return nil, err
		}
		if pollResp.StatusCode >= 400 {
			b, _ := io.ReadAll(pollResp.Body)
			_ = pollResp.Body.Close()
			return nil, f.provider.handleFilesError(pollResp.StatusCode, pollResp.Header, b)
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
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_FAILED", fmt.Sprintf("File processing failed for %s", file.Name))
	}

	warnings := []types.Warning{}
	if opts.Filename != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "filename"})
	}

	// TS reads displayName and sizeBytes as optional: absent ones are left
	// out of the metadata rather than sent as empty strings.
	meta := map[string]interface{}{
		"name":     file.Name,
		"mimeType": file.MimeType,
		"state":    file.State,
		"uri":      file.URI,
	}
	if file.DisplayName != "" {
		meta["displayName"] = file.DisplayName
	}
	if file.SizeBytes != "" {
		meta["sizeBytes"] = file.SizeBytes
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

// googleFilesError is the AISDKError TS throws for a failed upload; Name is
// carried as the error code.
func googleFilesError(name, message string) error {
	return providererrors.NewProviderError("google", 0, name, message, nil)
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

// googleFilePath path-encodes a provider-returned file resource name before it
// is reused in the credentialed polling URL (TS google-files.ts, 7de3612):
// "files/<id>" keeps the collection segment and encodes only the id.
func googleFilePath(name string) string {
	if rest, ok := strings.CutPrefix(name, "files/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "files/" + providerutils.EncodePathSegment(rest)
	}
	return providerutils.EncodePathSegment(name)
}

// googleFilesNumber reads a numeric provider option given as any Go number type.
func googleFilesNumber(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// handleFilesError builds the ProviderError for a failed Files API response
// (TS googleFailedResponseHandler: error.message, status as the code).
func (p *Provider) handleFilesError(status int, headers http.Header, body []byte) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	message := string(body)
	code := ""
	if json.Unmarshal(body, &payload) == nil {
		if payload.Error.Message != "" {
			message = payload.Error.Message
		}
		code = payload.Error.Status
	}
	perr := providererrors.NewProviderError("google", status, code, message, nil)
	perr.ResponseBody = string(body)
	perr.ResponseHeaders = providerutils.ExtractHeaders(headers)
	return perr
}
