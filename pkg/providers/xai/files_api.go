package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// FilesAPI implements provider.FilesAPI (plus the optional
// FileMetadataGetter/FileDownloader/FileDeleter capabilities) for xAI's
// `/files` endpoint. Mirrors TypeScript's XaiFiles
// (packages/xai/src/files/xai-files.ts).
type FilesAPI struct {
	provider *Provider
}

func (f *FilesAPI) SpecificationVersion() string {
	return "v4"
}

func (f *FilesAPI) Provider() string {
	return "xai.files"
}

// xaiFilesOptions mirrors TS xaiFilesOptionsSchema's behaviorally-relevant
// fields (teamId, expiresAfter). `filePath` is accepted by the TS schema but
// unused by XaiFiles' request logic, so it is omitted here.
type xaiFilesOptions struct {
	TeamID       *string `json:"teamId,omitempty"`
	ExpiresAfter *int    `json:"expiresAfter,omitempty"`
}

func extractXAIFilesOptions(providerOptions map[string]interface{}) (xaiFilesOptions, error) {
	var opts xaiFilesOptions
	raw, ok := providerOptions["xai"]
	if !ok || raw == nil {
		return opts, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if opts.ExpiresAfter != nil && (*opts.ExpiresAfter < 3600 || *opts.ExpiresAfter > 2_592_000) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("expiresAfter must be between 3600 and 2592000"))
	}
	return opts, nil
}

// UploadFile uploads a file to xAI's Files API (`POST /files`). Both inline
// data and streaming data (types.FileDataTypeStream) are supported;
// streaming uploads are sent via a streaming multipart body so the whole
// file is never buffered in memory, mirroring TypeScript's
// postMultipartStreamToApi path for `{ type: 'stream' }` data in
// XaiFiles#uploadFile.
func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	// Release the caller's stream on any failure path below, mirroring TS's
	// `data.stream.cancel(error)` guarantee.
	succeeded := false
	defer func() {
		if !succeeded {
			closeXAIStreamOnError(opts.Data)
		}
	}()

	xaiOpts, err := extractXAIFilesOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	// xAI rejects uploads where expires_after (or team_id) arrives after the
	// file part, so all fields precede the file, mirroring TS's FormData
	// append order / MultipartStreamPart ordering.
	var parts []internalhttp.MultipartStreamPart
	if xaiOpts.ExpiresAfter != nil {
		parts = append(parts, internalhttp.MultipartStreamPart{Name: "expires_after", Value: strconv.Itoa(*xaiOpts.ExpiresAfter)})
	}
	if xaiOpts.TeamID != nil {
		parts = append(parts, internalhttp.MultipartStreamPart{Name: "team_id", Value: *xaiOpts.TeamID})
	}

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
		IsFile: true, Name: "file", Filename: chooseFilename(opts.Filename), MediaType: mediaType, Content: content,
	})

	body, contentType, err := internalhttp.NewMultipartStreamBody(parts)
	if err != nil {
		return nil, err
	}
	defer body.Close() //nolint:errcheck

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method: stdhttp.MethodPost,
		Path:   "/files",
		Body:   body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": contentType,
		}),
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newXAIProviderError(f.Provider(), resp.StatusCode, resp.Body)
	}

	var out xaiFileResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	filename := out.Filename
	if filename == "" {
		filename = opts.Filename
	}

	succeeded = true
	return &types.UploadFileResult{
		ProviderReference: types.ProviderReference{"xai": out.ID},
		MediaType:         opts.MediaType,
		Filename:          filename,
		ByteSize:          out.Bytes,
		CreatedAt:         xaiUnixSecondsToTime(out.CreatedAt),
		ExpiresAt:         xaiUnixSecondsToTime(out.ExpiresAt),
		ProviderMetadata:  map[string]interface{}{"xai": xaiFileMetadata(out)},
		Warnings:          []types.Warning{},
	}, nil
}

// closeXAIStreamOnError releases a stream-type upload's underlying reader
// when preparation or the request fails, mirroring TypeScript's
// `data.stream.cancel(error)` in XaiFiles#uploadFile's catch block.
func closeXAIStreamOnError(data types.FileData) {
	if data.Type != types.FileDataTypeStream || data.Stream == nil {
		return
	}
	if closer, ok := data.Stream.(io.Closer); ok {
		_ = closer.Close()
	}
}

// GetFileMetadata retrieves a previously uploaded file's metadata
// (`GET /files/{id}`). Mirrors TS XaiFiles#getFileMetadata.
func (f *FilesAPI) GetFileMetadata(ctx context.Context, opts provider.FileMetadataOptions) (*provider.FileMetadataResult, error) {
	fileID, err := xaiFileID(opts.File)
	if err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newXAIProviderError(f.Provider(), resp.StatusCode, resp.Body)
	}

	var out xaiFileResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	return &provider.FileMetadataResult{
		ProviderReference: types.ProviderReference{"xai": out.ID},
		Filename:          out.Filename,
		ByteSize:          out.Bytes,
		CreatedAt:         xaiUnixSecondsToTime(out.CreatedAt),
		ExpiresAt:         xaiUnixSecondsToTime(out.ExpiresAt),
		ProviderMetadata:  map[string]interface{}{"xai": xaiFileMetadata(out)},
		Warnings:          []types.Warning{},
	}, nil
}

// DownloadFile streams a previously uploaded file's content
// (`GET /files/{id}/content`). Mirrors TS XaiFiles#downloadFile.
func (f *FilesAPI) DownloadFile(ctx context.Context, opts provider.DownloadFileOptions) (*provider.DownloadFileResult, error) {
	fileID, err := xaiFileID(opts.File)
	if err != nil {
		return nil, err
	}

	httpResp, err := f.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/files/" + providerutils.EncodePathSegment(fileID) + "/content",
		Headers: opts.Headers,
	})
	if err != nil {
		var statusErr *internalhttp.HTTPStatusError
		if errors.As(err, &statusErr) {
			return nil, newXAIProviderError(f.Provider(), statusErr.StatusCode, statusErr.Body)
		}
		return nil, err
	}

	// Media type from the content endpoint's Content-Type, without
	// parameters, mirroring TS's responseHeaders['content-type'].split(';')[0].
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
// Mirrors TS XaiFiles#deleteFile.
func (f *FilesAPI) DeleteFile(ctx context.Context, opts provider.DeleteFileOptions) (*provider.DeleteFileResult, error) {
	fileID, err := xaiFileID(opts.File)
	if err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  stdhttp.MethodDelete,
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, newXAIProviderError(f.Provider(), resp.StatusCode, resp.Body)
	}

	var out struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	return &provider.DeleteFileResult{
		ProviderReference: types.ProviderReference{"xai": out.ID},
		Deleted:           out.Deleted,
		Warnings:          []types.Warning{},
	}, nil
}

// xaiFileID extracts the 'xai' entry from a provider file reference,
// mirroring TS XaiFiles#getFileId.
func xaiFileID(ref types.ProviderReference) (string, error) {
	id := ref["xai"]
	if strings.TrimSpace(id) == "" {
		return "", &providererrors.InvalidArgumentError{
			Field:   "file",
			Message: "file reference is missing an 'xai' file id.",
		}
	}
	return id, nil
}

// xaiFileResponse mirrors TS xaiFilesResponseSchema.
type xaiFileResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object,omitempty"`
	Bytes     *int64 `json:"bytes,omitempty"`
	CreatedAt *int64 `json:"created_at,omitempty"`
	ExpiresAt *int64 `json:"expires_at,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	Status    string `json:"status,omitempty"`
}

// xaiFileMetadata mirrors TS XaiFiles#toFileMetadata.
func xaiFileMetadata(r xaiFileResponse) map[string]interface{} {
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

func xaiUnixSecondsToTime(sec *int64) *time.Time {
	if sec == nil {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
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

var (
	_ provider.FileMetadataGetter = (*FilesAPI)(nil)
	_ provider.FileDownloader     = (*FilesAPI)(nil)
	_ provider.FileDeleter        = (*FilesAPI)(nil)
)
