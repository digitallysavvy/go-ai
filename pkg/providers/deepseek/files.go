package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"mime/multipart"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// maxDeepSeekFileSizeBytes is the largest file DeepSeek accepts for upload.
const maxDeepSeekFileSizeBytes = 64 * 1024 * 1024

// maxDeepSeekFilenameLength is the largest filename DeepSeek accepts,
// measured in Unicode code points (matching the TypeScript SDK's
// `Array.from(filename).length`).
const maxDeepSeekFilenameLength = 512

var deepSeekSupportedFileMediaTypes = map[string]bool{
	"image/gif":  true,
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/webp": true,
}

// deepSeekGenericFileMediaTypes are media types that carry no useful
// information on their own; DeepSeek falls back to sniffing the file bytes or
// checking the filename extension for these.
var deepSeekGenericFileMediaTypes = map[string]bool{
	"":                         true,
	"application/binary":       true,
	"application/octet-stream": true,
	"binary/octet-stream":      true,
	"image":                    true,
	"image/*":                  true,
}

var deepSeekSupportedFilenameExtensions = map[string]bool{
	"gif":  true,
	"jpeg": true,
	"jpg":  true,
	"png":  true,
	"webp": true,
}

// FilesAPI implements provider.FilesAPI for DeepSeek, uploading images that
// can later be referenced from chat messages via a `file_id`.
type FilesAPI struct {
	provider *Provider
}

// UploadFile uploads a file to DeepSeek's Files API (POST /files) with
// purpose=user_data.
func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	fileBytes, err := deepSeekInlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	if err := validateDeepSeekFileUpload(fileBytes, opts.MediaType, opts.Filename); err != nil {
		return nil, err
	}

	// TS deepSeekFilesOptionsSchema: expiresAfter is z.number().int().
	var expiresAfter *int
	if opts.ProviderOptions != nil {
		if deepseekOpts, ok := opts.ProviderOptions["deepseek"].(map[string]interface{}); ok {
			if raw, present := deepseekOpts["expiresAfter"]; present && raw != nil {
				var n float64
				switch v := raw.(type) {
				case int:
					n = float64(v)
				case int64:
					n = float64(v)
				case float64:
					n = v
				default:
					return nil, &providererrors.InvalidArgumentError{
						Field:   "providerOptions.deepseek.expiresAfter",
						Message: "expiresAfter must be an integer between 3600 (1 hour) and 2592000 (30 days) seconds.",
					}
				}
				if n != math.Trunc(n) {
					return nil, &providererrors.InvalidArgumentError{
						Field:   "providerOptions.deepseek.expiresAfter",
						Message: "expiresAfter must be an integer between 3600 (1 hour) and 2592000 (30 days) seconds.",
					}
				}
				iv := int(n)
				expiresAfter = &iv
			}
		}
	}
	if expiresAfter != nil && (*expiresAfter < 3600 || *expiresAfter > 2592000) {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions.deepseek.expiresAfter",
			Message: "expiresAfter must be between 3600 (1 hour) and 2592000 (30 days) seconds.",
		}
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := providerutils.CreateTypedFormFile(writer, "file", deepSeekUploadFilename(opts.Filename), opts.MediaType)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, err
	}
	if err := writer.WriteField("purpose", "user_data"); err != nil {
		return nil, err
	}
	if expiresAfter != nil {
		if err := writer.WriteField("expires_after[anchor]", "created_at"); err != nil {
			return nil, err
		}
		if err := writer.WriteField("expires_after[seconds]", fmt.Sprintf("%d", *expiresAfter)); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := f.provider.client.Do(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/files",
		Body:   &body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": writer.FormDataContentType(),
		}),
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, internalhttp.StatusProviderError("deepseek", resp.StatusCode, resp.Headers, resp.Body)
	}

	if err := providerutils.MediaResponseValidator("deepseek", providerutils.SchemaDeepSeekFile)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}
	var out struct {
		ID        string   `json:"id"`
		Object    string   `json:"object"`
		Bytes     *float64 `json:"bytes"`
		CreatedAt *float64 `json:"created_at"`
		Filename  string   `json:"filename"`
		Purpose   string   `json:"purpose"`
		ExpiresAt *float64 `json:"expires_at"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	if out.Object != "" && out.Object != "file" {
		return nil, providererrors.NewInvalidResponseDataError(out, fmt.Sprintf("Expected a DeepSeek file object, got %q.", out.Object))
	}
	// TS deepSeekFilesResponseSchema: purpose is z.literal('user_data').nullish().
	if out.Purpose != "" && out.Purpose != "user_data" {
		return nil, providererrors.NewInvalidResponseDataError(out, fmt.Sprintf("Expected DeepSeek file purpose %q, got %q.", "user_data", out.Purpose))
	}
	// TS: bytes/created_at/expires_at are each z.number().int().nonnegative().nullish().
	bytesVal, err := deepSeekNonnegativeInt(out.Bytes, "bytes")
	if err != nil {
		return nil, err
	}
	createdAtVal, err := deepSeekNonnegativeInt(out.CreatedAt, "created_at")
	if err != nil {
		return nil, err
	}
	expiresAtVal, err := deepSeekNonnegativeInt(out.ExpiresAt, "expires_at")
	if err != nil {
		return nil, err
	}

	filename := out.Filename
	if filename == "" {
		filename = opts.Filename
	}

	meta := map[string]interface{}{}
	if out.Object != "" {
		meta["object"] = out.Object
	}
	if out.Filename != "" {
		meta["filename"] = out.Filename
	}
	if out.Purpose != "" {
		meta["purpose"] = out.Purpose
	}
	if bytesVal != nil {
		meta["bytes"] = *bytesVal
	}
	if createdAtVal != nil {
		meta["createdAt"] = *createdAtVal
	}
	if expiresAtVal != nil {
		meta["expiresAt"] = *expiresAtVal
	}

	result := &types.UploadFileResult{
		ProviderReference: types.ProviderReference{"deepseek": out.ID},
		MediaType:         opts.MediaType,
		Warnings:          []types.Warning{},
	}
	if filename != "" {
		result.Filename = filename
	}
	if len(meta) > 0 {
		result.ProviderMetadata = map[string]interface{}{"deepseek": meta}
	}
	return result, nil
}

// deepSeekNonnegativeInt validates a decoded numeric file-metadata field
// against TS deepSeekFilesResponseSchema's z.number().int().nonnegative()
// (bytes/created_at/expires_at), returning a typed error for a fractional or
// negative value instead of silently accepting it (a negative int64 would
// otherwise decode without error) or surfacing a raw JSON unmarshal error
// (a non-integer number would fail an *int64 field's decode outright).
func deepSeekNonnegativeInt(v *float64, field string) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	if *v != math.Trunc(*v) || *v < 0 {
		return nil, providererrors.NewInvalidResponseDataError(*v, fmt.Sprintf("Expected DeepSeek file %q to be a nonnegative integer, got %v.", field, *v))
	}
	iv := int64(*v)
	return &iv, nil
}

func deepSeekInlineFileBytes(data types.FileData) ([]byte, error) {
	switch data.Type {
	case types.FileDataTypeData:
		if data.DataString != "" {
			return types.DecodeFileDataString(data.DataString)
		}
		return data.Data, nil
	case types.FileDataTypeText:
		return []byte(data.Text), nil
	default:
		return nil, fmt.Errorf("unsupported file data type %q for deepseek upload", data.Type)
	}
}

func deepSeekUploadFilename(filename string) string {
	if filename == "" {
		return "blob"
	}
	return filename
}

// validateDeepSeekFileUpload mirrors the TypeScript SDK's validateFileUpload:
// it enforces the 64 MiB size limit, the 512 character filename limit, and
// that the media type (or, failing that, the sniffed content / filename
// extension) identifies a supported image format.
func validateDeepSeekFileUpload(fileBytes []byte, mediaType string, filename string) error {
	if len(fileBytes) > maxDeepSeekFileSizeBytes {
		return &providererrors.InvalidArgumentError{
			Field: "data",
			Message: fmt.Sprintf(
				"DeepSeek file uploads must not exceed 64 MiB (%s bytes). Received %s bytes.",
				deepSeekGroupThousands(maxDeepSeekFileSizeBytes), deepSeekGroupThousands(len(fileBytes))),
		}
	}

	if filename != "" {
		filenameLength := utf8.RuneCountInString(filename)
		if filenameLength > maxDeepSeekFilenameLength {
			return &providererrors.InvalidArgumentError{
				Field: "filename",
				Message: fmt.Sprintf(
					"DeepSeek filenames must not exceed %d characters. Received %d characters.",
					maxDeepSeekFilenameLength, filenameLength),
			}
		}
	}

	normalizedMediaType := normalizeDeepSeekMediaType(mediaType)
	detectedMediaType, detected := fileutil.DetectMediaTypeSignature(fileBytes, "")

	if detected && !deepSeekSupportedFileMediaTypes[detectedMediaType] {
		return &providererrors.InvalidArgumentError{
			Field: "data",
			Message: fmt.Sprintf(
				"DeepSeek file uploads support JPEG, PNG, GIF, and WebP images. Detected unsupported file content type %q.",
				detectedMediaType),
		}
	}

	if deepSeekSupportedFileMediaTypes[normalizedMediaType] {
		return nil
	}

	if !deepSeekGenericFileMediaTypes[normalizedMediaType] {
		return &providererrors.InvalidArgumentError{
			Field: "mediaType",
			Message: fmt.Sprintf(
				"DeepSeek file uploads support JPEG, PNG, GIF, and WebP images. Received unsupported media type %q.",
				mediaType),
		}
	}

	if detected || deepSeekHasSupportedFilenameExtension(filename) {
		return nil
	}

	return &providererrors.InvalidArgumentError{
		Field: "mediaType",
		Message: fmt.Sprintf(
			"DeepSeek file uploads support JPEG, PNG, GIF, and WebP images. Provide a supported media type or a filename ending in .jpg, .jpeg, .png, .gif, or .webp. Received %q.",
			mediaType),
	}
}

func normalizeDeepSeekMediaType(mediaType string) string {
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = mediaType[:idx]
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func deepSeekHasSupportedFilenameExtension(filename string) bool {
	if filename == "" {
		return false
	}
	idx := strings.LastIndexByte(filename, '.')
	if idx == -1 {
		return false
	}
	ext := strings.ToLower(filename[idx+1:])
	return deepSeekSupportedFilenameExtensions[ext]
}

// deepSeekGroupThousands mirrors Number#toLocaleString('en-US') for integers.
func deepSeekGroupThousands(n int) string {
	digits := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
