package ai

import (
	"context"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ErrFileMetadataNotSupported is returned when the provider's Files API does
// not implement FileMetadataGetter.
var ErrFileMetadataNotSupported = errors.New("the provider does not support file metadata retrieval")

// ErrFileDownloadNotSupported is returned when the provider's Files API does
// not implement FileDownloader.
var ErrFileDownloadNotSupported = errors.New("the provider does not support file downloads")

// ErrFileDeleteNotSupported is returned when the provider's Files API does
// not implement FileDeleter.
var ErrFileDeleteNotSupported = errors.New("the provider does not support file deletion")

// GetFileMetadataOptions are the ai-level options for retrieving metadata of
// a previously uploaded file.
type GetFileMetadataOptions struct {
	// API is a provider.FilesAPI instance or a provider.FilesProvider (a
	// Provider exposing a Files() method).
	API interface{}

	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// GetFileMetadata retrieves metadata for a previously uploaded file. ctx
// carries cancellation/timeout (TS parity: abortSignal).
func GetFileMetadata(ctx context.Context, opts GetFileMetadataOptions) (*provider.FileMetadataResult, error) {
	filesAPI, err := resolveFilesAPI(opts.API)
	if err != nil {
		return nil, err
	}
	getter, ok := filesAPI.(provider.FileMetadataGetter)
	if !ok {
		return nil, ErrFileMetadataNotSupported
	}
	return getter.GetFileMetadata(ctx, provider.FileMetadataOptions{
		File:            opts.File,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	})
}

// DownloadFileOptions are the ai-level options for downloading a previously
// uploaded file's content.
type DownloadFileOptions struct {
	// API is a provider.FilesAPI instance or a provider.FilesProvider (a
	// Provider exposing a Files() method).
	API interface{}

	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// DownloadFile streams the content of a previously uploaded file. The
// returned result's Content must be drained and closed by the caller. ctx
// carries cancellation/timeout (TS parity: abortSignal).
func DownloadFile(ctx context.Context, opts DownloadFileOptions) (*provider.DownloadFileResult, error) {
	filesAPI, err := resolveFilesAPI(opts.API)
	if err != nil {
		return nil, err
	}
	downloader, ok := filesAPI.(provider.FileDownloader)
	if !ok {
		return nil, ErrFileDownloadNotSupported
	}
	return downloader.DownloadFile(ctx, provider.DownloadFileOptions{
		File:            opts.File,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	})
}

// DeleteFileOptions are the ai-level options for deleting a previously
// uploaded file.
type DeleteFileOptions struct {
	// API is a provider.FilesAPI instance or a provider.FilesProvider (a
	// Provider exposing a Files() method).
	API interface{}

	// File is the provider reference of the file, as returned by UploadFile.
	File types.ProviderReference

	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// DeleteFile deletes a previously uploaded file. ctx carries
// cancellation/timeout (TS parity: abortSignal).
func DeleteFile(ctx context.Context, opts DeleteFileOptions) (*provider.DeleteFileResult, error) {
	filesAPI, err := resolveFilesAPI(opts.API)
	if err != nil {
		return nil, err
	}
	deleter, ok := filesAPI.(provider.FileDeleter)
	if !ok {
		return nil, ErrFileDeleteNotSupported
	}
	return deleter.DeleteFile(ctx, provider.DeleteFileOptions{
		File:            opts.File,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	})
}
