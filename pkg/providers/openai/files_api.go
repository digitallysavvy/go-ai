package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// FilesAPI implements the OpenAI Files API (upload, metadata, download,
// delete). Mirrors TypeScript's OpenAIFiles
// (packages/openai/src/files/openai-files.ts).
type FilesAPI struct {
	provider *Provider
}

// openAIFilesResponse is the wire shape of a file resource, shared by
// uploadFile and getFileMetadata.
type openAIFilesResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     *int64 `json:"bytes"`
	CreatedAt *int64 `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
	ExpiresAt *int64 `json:"expires_at"`
}

func (r openAIFilesResponse) toMetadata() map[string]interface{} {
	meta := map[string]interface{}{}
	if r.Filename != "" {
		meta["filename"] = r.Filename
	}
	if r.Purpose != "" {
		meta["purpose"] = r.Purpose
	}
	if r.Bytes != nil {
		meta["bytes"] = *r.Bytes
	}
	if r.CreatedAt != nil {
		meta["createdAt"] = *r.CreatedAt
	}
	if r.Status != "" {
		meta["status"] = r.Status
	}
	if r.ExpiresAt != nil {
		meta["expiresAt"] = *r.ExpiresAt
	}
	return meta
}

func unixSecondsToTime(v *int64) *time.Time {
	if v == nil {
		return nil
	}
	t := time.Unix(*v, 0)
	return &t
}

// UploadFile uploads a file to OpenAI's Files API (`POST /files`). Both
// inline data and streaming data (types.FileDataTypeStream) are supported;
// streaming uploads are sent via a streaming multipart body so the whole
// file is never buffered in memory, mirroring TypeScript's
// postMultipartStreamToApi path for `{ type: 'stream' }` data.
func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	// Release the caller's stream on any failure path below, mirroring
	// TypeScript's guarantee that a stream-type upload's source is always
	// disposed of on rejection (data.stream.cancel(error) on option-parse
	// failure; postMultipartStreamToApi's body.dispose(error) on every later
	// failure). succeeded is set true only immediately before the final
	// successful return.
	succeeded := false
	defer func() {
		if !succeeded {
			closeStreamOnError(opts.Data)
		}
	}()

	filesOpts, err := parseOpenAIFilesOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	purpose := "assistants"
	if filesOpts.purpose != "" {
		purpose = filesOpts.purpose
	}

	parts := []internalhttp.MultipartStreamPart{
		{Name: "purpose", Value: purpose},
	}
	if filesOpts.expiresAfter != nil {
		parts = append(parts,
			internalhttp.MultipartStreamPart{Name: "expires_after[anchor]", Value: "created_at"},
			internalhttp.MultipartStreamPart{Name: "expires_after[seconds]", Value: strconv.FormatInt(*filesOpts.expiresAfter, 10)},
		)
	}

	filename := chooseFilename(opts.Filename)
	mediaType := opts.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	var content io.Reader
	if opts.Data.Type == types.FileDataTypeStream {
		content = opts.Data.Stream
	} else {
		fileBytes, ferr := inlineFileBytes(opts.Data)
		if ferr != nil {
			return nil, ferr
		}
		content = bytes.NewReader(fileBytes)
	}
	parts = append(parts, internalhttp.MultipartStreamPart{
		IsFile: true, Name: "file", Filename: filename, MediaType: mediaType, Content: content,
	})

	body, contentType, err := internalhttp.NewMultipartStreamBody(parts)
	if err != nil {
		return nil, err
	}
	defer body.Close() //nolint:errcheck

	headers := internalhttp.MergeHeaders(map[string]string{"Content-Type": contentType}, opts.Headers)

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/files",
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai files upload failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out openAIFilesResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	resultFilename := out.Filename
	if resultFilename == "" {
		resultFilename = opts.Filename
	}

	succeeded = true
	return &types.UploadFileResult{
		ProviderReference: types.ProviderReference{"openai": out.ID},
		MediaType:         opts.MediaType,
		Filename:          resultFilename,
		ProviderMetadata:  map[string]interface{}{"openai": out.toMetadata()},
		Warnings:          []types.Warning{},
		ByteSize:          out.Bytes,
		CreatedAt:         unixSecondsToTime(out.CreatedAt),
		ExpiresAt:         unixSecondsToTime(out.ExpiresAt),
	}, nil
}

// GetFileMetadata retrieves a previously uploaded file's metadata
// (`GET /files/{id}`). Mirrors TypeScript's OpenAIFiles.getFileMetadata.
func (f *FilesAPI) GetFileMetadata(ctx context.Context, opts provider.FileMetadataOptions) (*provider.FileMetadataResult, error) {
	fileID, err := openAIFileID(opts.File)
	if err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai files retrieve failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out openAIFilesResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	return &provider.FileMetadataResult{
		ProviderReference: types.ProviderReference{"openai": out.ID},
		Filename:          out.Filename,
		ByteSize:          out.Bytes,
		CreatedAt:         unixSecondsToTime(out.CreatedAt),
		ExpiresAt:         unixSecondsToTime(out.ExpiresAt),
		ProviderMetadata:  map[string]interface{}{"openai": out.toMetadata()},
		Warnings:          []types.Warning{},
	}, nil
}

// DownloadFile streams a previously uploaded file's content
// (`GET /files/{id}/content`) without buffering it in memory. The caller
// owns and must close the returned Content. Mirrors TypeScript's
// OpenAIFiles.downloadFile.
func (f *FilesAPI) DownloadFile(ctx context.Context, opts provider.DownloadFileOptions) (*provider.DownloadFileResult, error) {
	fileID, err := openAIFileID(opts.File)
	if err != nil {
		return nil, err
	}

	httpResp, err := f.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID) + "/content",
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}

	mediaType := ""
	if ct := httpResp.Header.Get("Content-Type"); ct != "" {
		mediaType = strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
	}

	return &provider.DownloadFileResult{
		Content:   httpResp.Body,
		MediaType: mediaType,
		Warnings:  []types.Warning{},
	}, nil
}

// DeleteFile deletes a previously uploaded file (`DELETE /files/{id}`).
// Mirrors TypeScript's OpenAIFiles.deleteFile.
func (f *FilesAPI) DeleteFile(ctx context.Context, opts provider.DeleteFileOptions) (*provider.DeleteFileResult, error) {
	fileID, err := openAIFileID(opts.File)
	if err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  "DELETE",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai files delete failed: %d %s", resp.StatusCode, string(resp.Body))
	}

	var out struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	return &provider.DeleteFileResult{
		ProviderReference: types.ProviderReference{"openai": out.ID},
		Deleted:           out.Deleted,
		Warnings:          []types.Warning{},
	}, nil
}

// openAIFileID extracts the "openai" file ID from a provider reference,
// mirroring TypeScript's OpenAIFiles.getFileId error message exactly.
func openAIFileID(file types.ProviderReference) (string, error) {
	fileID, ok := file["openai"]
	if !ok || strings.TrimSpace(fileID) == "" {
		return "", fmt.Errorf("file reference is missing an 'openai' file id.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return fileID, nil
}

// openAIFilesUploadOptions is the parsed providerOptions.openai shape for
// UploadFile (TS OpenAIFilesOptions: purpose?: string, expiresAfter?: number).
type openAIFilesUploadOptions struct {
	purpose      string
	expiresAfter *int64
}

func parseOpenAIFilesOptions(providerOptions map[string]interface{}) (openAIFilesUploadOptions, error) {
	var out openAIFilesUploadOptions
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return out, nil
	}
	if v, ok := openaiOpts["purpose"]; ok {
		s, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("providerOptions.openai.purpose must be a string")
		}
		out.purpose = s
	}
	if v, ok := openaiOpts["expiresAfter"]; ok && v != nil {
		n, ok := v.(float64)
		if !ok {
			return out, fmt.Errorf("providerOptions.openai.expiresAfter must be a number")
		}
		seconds := int64(n)
		out.expiresAfter = &seconds
	}
	return out, nil
}

// closeStreamOnError releases a stream-type upload's underlying reader when
// preparation fails before any request is made, mirroring TypeScript's
// `data.stream.cancel(error)` in OpenAIFiles.uploadFile's catch block.
func closeStreamOnError(data types.FileData) {
	if data.Type != types.FileDataTypeStream || data.Stream == nil {
		return
	}
	if closer, ok := data.Stream.(io.Closer); ok {
		_ = closer.Close()
	}
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
