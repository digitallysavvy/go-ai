package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// DownloadFunction is a function that downloads a file from a URL.
type DownloadFunction func(ctx context.Context, url string) ([]byte, error)

// DownloadOptions contains options for creating a custom download function.
type DownloadOptions struct {
	// MaxBytes is the maximum allowed download size in bytes.
	// Default: 2 GiB (fileutil.DefaultMaxDownloadSize)
	MaxBytes int64

	// Headers are additional HTTP headers to include in download requests.
	Headers map[string]string
}

// CreateDownload creates a download function with configurable options.
//
// The default download function enforces a 2 GiB size limit to prevent
// memory exhaustion from unbounded downloads. You can customize the
// size limit by passing DownloadOptions.
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
//	    Download: customDownload,
//	})
func CreateDownload(options *DownloadOptions) DownloadFunction {
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

		return fileutil.Download(ctx, url, opts)
	}
}

// DefaultDownload is the default download function with 2 GiB limit.
var DefaultDownload = CreateDownload(nil)
