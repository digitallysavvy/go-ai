package anthropic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsdate"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type FilesAPI struct {
	provider *Provider
}

func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	if opts.Data.Type == types.FileDataTypeStream {
		// TS convertInlineFileDataToUint8Array: streaming upload is not
		// supported; the stream is released and never read.
		if closer, ok := opts.Data.Stream.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "streaming file upload"}
	}
	content, err := inlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// The part is TS's typed Blob appended to a FormData (filename "blob"
	// when none is given).
	part, err := providerutils.CreateFormDataFile(writer, "file", chooseFilename(opts.Filename), opts.MediaType)
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
			"Content-Type": writer.FormDataContentType(),
		}, filesHeaders(opts.Headers)),
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS: a rejected fetch or header resolution is thrown as is
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, anthropicHTTPError(f.provider.Name(), resp.StatusCode, resp.Headers, resp.Body, nil)
	}

	// TS createJsonResponseHandler(anthropicUploadFileResponseSchema).
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text() before JSON.parse
	if err := anthropicSchemaValidator(f.provider.Name(), zFileUpload)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var out anthropicFileResponse
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}

	// TS `response.filename ?? filename`, `response.mime_type ?? mediaType`.
	filename := opts.Filename
	if out.Filename != nil {
		filename = *out.Filename
	}
	mediaType := opts.MediaType
	if out.MimeType != nil {
		mediaType = *out.MimeType
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"anthropic": out.ID},
		MediaType:         mediaType,
		Filename:          filename,
		ByteSize:          out.byteSize(),
		CreatedAt:         anthropicFileTime(out.CreatedAt),
		ExpiresAt:         anthropicFileTime(out.ExpiresAt),
		ProviderMetadata:  map[string]interface{}{"anthropic": out.metadata()},
		Warnings:          []types.Warning{},
	}, nil
}

// anthropicFileResponse is anthropicUploadFileResponseSchema: id is
// required, every other field nullish.
type anthropicFileResponse struct {
	ID           string   `json:"id"`
	Filename     *string  `json:"filename"`
	MimeType     *string  `json:"mime_type"`
	SizeBytes    *float64 `json:"size_bytes"`
	CreatedAt    *string  `json:"created_at"`
	Downloadable *bool    `json:"downloadable"`
	ExpiresAt    *string  `json:"expires_at"`
}

// byteSize is the ByteSize result field: size_bytes truncated to an integer
// (a fractional size, which TS z.number() accepts, cannot be represented in
// the int64 field; providerMetadata keeps the exact number).
func (r anthropicFileResponse) byteSize() *int64 {
	if r.SizeBytes == nil || math.IsNaN(*r.SizeBytes) || math.Abs(*r.SizeBytes) >= math.MaxInt64 {
		return nil
	}
	n := int64(*r.SizeBytes)
	return &n
}

// metadata is TS toFileMetadata: each field only when the API sent it.
func (r anthropicFileResponse) metadata() map[string]interface{} {
	meta := map[string]interface{}{}
	if r.Filename != nil {
		meta["filename"] = *r.Filename
	}
	if r.MimeType != nil {
		meta["mimeType"] = *r.MimeType
	}
	if r.SizeBytes != nil {
		// An integral size keeps the int64 type of v0.5; TS z.number() also
		// takes a fraction, which is kept as is.
		if n := r.byteSize(); n != nil && float64(*n) == *r.SizeBytes {
			meta["sizeBytes"] = *n
		} else {
			meta["sizeBytes"] = *r.SizeBytes
		}
	}
	if r.CreatedAt != nil {
		meta["createdAt"] = *r.CreatedAt
	}
	if r.Downloadable != nil {
		meta["downloadable"] = *r.Downloadable
	}
	if r.ExpiresAt != nil {
		meta["expiresAt"] = *r.ExpiresAt
	}
	return meta
}

// anthropicFileTime is TS `new Date(s)` (Date.parse, millisecond
// precision); a value that does not parse is left unset (TS gives an
// Invalid Date).
func anthropicFileTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	ms, ok := jsdate.Parse(*s)
	if !ok {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

// anthropicFileID is TS getFileId: the "anthropic" entry of the reference,
// non-empty after a JS trim.
func anthropicFileID(file types.ProviderReference) (string, error) {
	id, ok := file["anthropic"]
	if !ok || providerutils.JSTrim(id) == "" {
		return "", providererrors.NewArgumentError("file", "file reference is missing an 'anthropic' file id.", nil)
	}
	return id, nil
}

// filesHeaders is TS getHeaders: the files API beta, then the call headers.
func filesHeaders(headers map[string]string) map[string]string {
	return internalhttp.MergeHeaders(map[string]string{"anthropic-beta": BetaHeaderFilesAPI}, headers)
}

// GetFileMetadata retrieves a file's metadata (`GET /files/{id}`, TS
// AnthropicFiles.getFileMetadata).
func (f *FilesAPI) GetFileMetadata(ctx context.Context, opts provider.FileMetadataOptions) (*provider.FileMetadataResult, error) {
	fileID, err := anthropicFileID(opts.File)
	if err != nil {
		return nil, err
	}
	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: filesHeaders(opts.Headers),
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS: a rejected fetch or header resolution is thrown as is
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, anthropicHTTPError(f.provider.Name(), resp.StatusCode, resp.Headers, resp.Body, nil)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if err := anthropicSchemaValidator(f.provider.Name(), zFileUpload)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var out anthropicFileResponse
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	result := &provider.FileMetadataResult{
		ProviderReference: types.ProviderReference{"anthropic": out.ID},
		ByteSize:          out.byteSize(),
		CreatedAt:         anthropicFileTime(out.CreatedAt),
		ExpiresAt:         anthropicFileTime(out.ExpiresAt),
		ProviderMetadata:  map[string]interface{}{"anthropic": out.metadata()},
		Warnings:          []types.Warning{},
	}
	if out.Filename != nil {
		result.Filename = *out.Filename
	}
	if out.MimeType != nil {
		result.MediaType = *out.MimeType
	}
	return result, nil
}

// DownloadFile streams a file's content (`GET /files/{id}/content`, TS
// AnthropicFiles.downloadFile). The caller owns and must close Content.
func (f *FilesAPI) DownloadFile(ctx context.Context, opts provider.DownloadFileOptions) (*provider.DownloadFileResult, error) {
	fileID, err := anthropicFileID(opts.File)
	if err != nil {
		return nil, err
	}
	httpResp, err := f.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID) + "/content",
		Headers: filesHeaders(opts.Headers),
	})
	if err != nil {
		var statusErr *internalhttp.HTTPStatusError
		if errors.As(err, &statusErr) {
			return nil, anthropicHTTPError(f.provider.Name(), statusErr.StatusCode, statusErr.Headers, statusErr.Body, nil)
		}
		return nil, internalhttp.UnwrapFetchRejection(err)
	}
	mediaType := ""
	if ct := providerutils.FetchHeaderText(httpResp.Header, "Content-Type"); ct != "" {
		mediaType = providerutils.JSTrim(strings.SplitN(ct, ";", 2)[0])
	}
	return &provider.DownloadFileResult{Content: httpResp.Body, MediaType: mediaType, Warnings: []types.Warning{}}, nil
}

// DeleteFile deletes a file (`DELETE /files/{id}`, TS
// AnthropicFiles.deleteFile).
func (f *FilesAPI) DeleteFile(ctx context.Context, opts provider.DeleteFileOptions) (*provider.DeleteFileResult, error) {
	fileID, err := anthropicFileID(opts.File)
	if err != nil {
		return nil, err
	}
	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method:  "DELETE",
		Path:    "/files/" + providerutils.EncodePathSegment(fileID),
		Headers: filesHeaders(opts.Headers),
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS: a rejected fetch or header resolution is thrown as is
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, anthropicHTTPError(f.provider.Name(), resp.StatusCode, resp.Headers, resp.Body, nil)
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body))
	if err := anthropicSchemaValidator(f.provider.Name(), zFileDelete)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := jsonexact.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	return &provider.DeleteFileResult{
		ProviderReference: types.ProviderReference{"anthropic": out.ID},
		Deleted:           true,
		Warnings:          []types.Warning{},
	}, nil
}
