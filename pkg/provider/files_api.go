package provider

import (
	"context"
	"io"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// FilesAPI mirrors the TypeScript FilesV4 upload surface. Only UploadFile is
// required; GetFileMetadata, DownloadFile and DeleteFile are optional
// capabilities exposed through the FileMetadataGetter, FileDownloader and
// FileDeleter interfaces below (mirroring the optional-method pattern TS uses
// for FilesV4.getFileMetadata/downloadFile/deleteFile).
type FilesAPI interface {
	UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error)
}

// FilesProvider is a provider that exposes file upload capabilities.
type FilesProvider interface {
	Files() FilesAPI
}

// FileMetadataOptions are options for retrieving metadata of a previously
// uploaded file.
type FileMetadataOptions struct {
	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	// Headers are additional HTTP headers to send with the request. Only
	// applicable for HTTP-based providers.
	Headers map[string]string

	ProviderOptions map[string]interface{}
}

// FileMetadataResult is the result of retrieving file metadata.
type FileMetadataResult struct {
	// ProviderReference contains only the operated provider's entry — when
	// working with a merged multi-provider reference, do not reassign it
	// with this result.
	ProviderReference types.ProviderReference
	Filename          string
	MediaType         string
	ByteSize          *int64
	CreatedAt         *time.Time
	ExpiresAt         *time.Time
	ProviderMetadata  map[string]interface{}
	Warnings          []types.Warning
}

// FileMetadataGetter is an optional FilesAPI capability for retrieving
// metadata of a previously uploaded file. Presence signals that the provider
// supports metadata reads, mirroring TypeScript's FilesV4.getFileMetadata.
type FileMetadataGetter interface {
	GetFileMetadata(ctx context.Context, opts FileMetadataOptions) (*FileMetadataResult, error)
}

// DownloadFileOptions are options for downloading a previously uploaded
// file's content.
type DownloadFileOptions struct {
	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	// Headers are additional HTTP headers to send with the request. Only
	// applicable for HTTP-based providers.
	Headers map[string]string

	ProviderOptions map[string]interface{}
}

// DownloadFileResult is the result of downloading file content.
type DownloadFileResult struct {
	// Content is the file's byte stream. The caller is responsible for
	// draining and closing it.
	Content          io.ReadCloser
	MediaType        string
	ProviderMetadata map[string]interface{}
	Warnings         []types.Warning
}

// FileDownloader is an optional FilesAPI capability for streaming a
// previously uploaded file's content. Presence signals that the provider
// supports content download, mirroring TypeScript's FilesV4.downloadFile.
type FileDownloader interface {
	DownloadFile(ctx context.Context, opts DownloadFileOptions) (*DownloadFileResult, error)
}

// DeleteFileOptions are options for deleting a previously uploaded file.
type DeleteFileOptions struct {
	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	// Headers are additional HTTP headers to send with the request. Only
	// applicable for HTTP-based providers.
	Headers map[string]string

	ProviderOptions map[string]interface{}
}

// DeleteFileResult is the result of deleting a file.
type DeleteFileResult struct {
	// ProviderReference contains only the operated provider's entry — when
	// working with a merged multi-provider reference, do not reassign it
	// with this result.
	ProviderReference types.ProviderReference
	Deleted           bool
	ProviderMetadata  map[string]interface{}
	Warnings          []types.Warning
}

// FileDeleter is an optional FilesAPI capability for deleting a previously
// uploaded file. Presence signals that the provider supports deletion,
// mirroring TypeScript's FilesV4.deleteFile.
type FileDeleter interface {
	DeleteFile(ctx context.Context, opts DeleteFileOptions) (*DeleteFileResult, error)
}
