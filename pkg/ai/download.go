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

// URLDownloadWithMetadataFunction downloads a single URL and returns bytes plus
// an optional media type, matching TypeScript generateVideo's download result.
type URLDownloadWithMetadataFunction func(ctx context.Context, url string) (*DownloadResult, error)

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
	download := CreateURLDownloadWithMetadata(options)
	return func(ctx context.Context, url string) ([]byte, error) {
		result, err := download(ctx, url)
		if err != nil {
			return nil, err
		}
		return result.Data, nil
	}
}

// CreateURLDownloadWithMetadata creates a single-URL download function that
// returns downloaded bytes and response media type metadata.
func CreateURLDownloadWithMetadata(options *DownloadOptions) URLDownloadWithMetadataFunction {
	if options == nil {
		options = &DownloadOptions{}
	}

	return func(ctx context.Context, url string) (*DownloadResult, error) {
		opts := fileutil.DefaultDownloadOptions()

		if options.MaxBytes > 0 {
			opts.MaxSize = options.MaxBytes
		}

		if options.Headers != nil {
			opts.Headers = options.Headers
		}

		opts.URLValidator = downloadURLValidator

		result, err := fileutil.DownloadWithMetadata(ctx, url, opts)
		if err != nil {
			return nil, err
		}
		return &DownloadResult{Data: result.Data, MediaType: result.ContentType}, nil
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
//	// Use with prompt APIs that support batch URL downloads.
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//	    Model: model,
//	    Prompt: prompt,
//	    ExperimentalDownload: customDownload,
//	})
func CreateDownload(options *DownloadOptions) DownloadFunction {
	urlDownload := CreateURLDownloadWithMetadata(options)
	return func(ctx context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
		results := make([]*DownloadResult, len(requests))
		for i, request := range requests {
			if request.IsURLSupportedByModel {
				continue
			}
			result, err := urlDownload(ctx, request.URL)
			if err != nil {
				return nil, err
			}
			results[i] = result
		}
		return results, nil
	}
}

// DefaultDownload is the default download function with 2 GiB limit.
var DefaultDownload = CreateDownload(nil)
