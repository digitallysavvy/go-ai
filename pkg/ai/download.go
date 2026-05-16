package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

var downloadURLValidator = validateDownloadURL

// DownloadRequest describes one remote file URL discovered during prompt
// preparation. IsURLSupportedByModel indicates whether the selected model can
// consume the URL directly; returning nil for that request leaves the URL in the
// prompt, matching the TypeScript SDK's experimental_download contract.
type DownloadRequest = promptutils.DownloadRequest

// DownloadResult contains downloaded file bytes and an optional media type.
type DownloadResult = promptutils.DownloadResult

// DownloadFunction receives all remote file URLs planned for a prompt and
// returns one result per request. A nil result leaves the original URL intact.
type DownloadFunction = promptutils.DownloadFunction

// URLDownloadFunction downloads a single URL. It is useful for non-prompt APIs
// and for adapting existing single-file download implementations.
type URLDownloadFunction func(ctx context.Context, url string) ([]byte, error)

// DownloadOptions contains options for creating a custom download function.
type DownloadOptions struct {
	// MaxBytes is the maximum allowed download size in bytes.
	// Default: 2 GiB (fileutil.DefaultMaxDownloadSize)
	MaxBytes int64

	// Headers are additional HTTP headers to include in download requests.
	Headers map[string]string
}

// CreateURLDownload creates a single-URL download function with configurable
// options.
//
// The default URL download function enforces a 2 GiB size limit to prevent
// memory exhaustion from unbounded downloads. You can customize the
// size limit by passing DownloadOptions.
func CreateURLDownload(options *DownloadOptions) URLDownloadFunction {
	if options == nil {
		options = &DownloadOptions{}
	}

	return func(ctx context.Context, url string) ([]byte, error) {
		opts := fileutil.DefaultDownloadOptions()

		if options.MaxBytes > 0 {
			opts.MaxSize = options.MaxBytes
		}

		if options.Headers != nil {
			opts.Headers = options.Headers
		}

		opts.URLValidator = downloadURLValidator

		return fileutil.Download(ctx, url, opts)
	}
}

// CreateDownload creates a TypeScript-compatible batch download function.
//
// Example:
//
//	// Create a custom download function with 100 MB limit
//	customDownload := ai.CreateDownload(&ai.DownloadOptions{
//	    MaxBytes: 100 * 1024 * 1024, // 100 MB
//	})
//
//	// Use with generate functions that support custom downloads
//	result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
//	    Model: model,
//	    Prompt: prompt,
//	    ExperimentalDownload: customDownload,
//	})
func CreateDownload(options *DownloadOptions) DownloadFunction {
	urlDownload := CreateURLDownload(options)
	return func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
		results := make([]*DownloadResult, len(requests))
		for i, request := range requests {
			if request.IsURLSupportedByModel {
				continue
			}
			data, err := urlDownload(ctx, request.URL)
			if err != nil {
				return nil, err
			}
			results[i] = &DownloadResult{Data: data}
		}
		return results, nil
	}
}

// DefaultDownload is the default download function with 2 GiB limit.
var DefaultDownload = CreateDownload(nil)
