package fileutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const (
	// DefaultMaxDownloadSize is the default maximum download size: 2 GiB.
	//
	// This limit prevents memory exhaustion from unbounded downloads.
	// Very large downloads risk exceeding default memory limits and causing
	// out-of-memory errors. Setting this limit converts an unrecoverable OOM
	// crash into a catchable DownloadError.
	DefaultMaxDownloadSize = 2 * 1024 * 1024 * 1024 // 2 GiB
)

// DownloadOptions contains options for downloading files
type DownloadOptions struct {
	// Timeout for the download operation
	Timeout time.Duration

	// Headers to include in the request
	Headers map[string]string

	// MaxSize limits the size of the download (in bytes)
	// Default: 2 GiB (DefaultMaxDownloadSize)
	MaxSize int64
}

// DefaultDownloadOptions returns default download options
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		Timeout: 60 * time.Second,
		Headers: make(map[string]string),
		MaxSize: DefaultMaxDownloadSize,
	}
}

// Download downloads a file from a URL with size limits to prevent memory exhaustion.
//
// It checks the Content-Length header for early rejection, then reads the body
// incrementally and aborts with a DownloadError when the limit is exceeded.
func Download(ctx context.Context, url string, opts DownloadOptions) ([]byte, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = DefaultMaxDownloadSize
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: opts.Timeout,
	}

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Add custom headers
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, providererrors.NewDownloadError(url, 0, "", "", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			resp.Status,
			"",
			nil,
		)
	}

	// Early rejection based on Content-Length header
	if resp.ContentLength > 0 && resp.ContentLength > opts.MaxSize {
		return nil, providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("download of %s exceeded maximum size of %d bytes (Content-Length: %d)",
				url, opts.MaxSize, resp.ContentLength),
			nil,
		)
	}

	// Read response body with size limit
	// Use MaxSize+1 to detect when limit is exceeded
	limitedReader := io.LimitReader(resp.Body, opts.MaxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Check if we exceeded the size limit during download
	if int64(len(data)) > opts.MaxSize {
		return nil, providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("download of %s exceeded maximum size of %d bytes", url, opts.MaxSize),
			nil,
		)
	}

	return data, nil
}

// DownloadToWriter downloads a file from a URL and writes it to an io.Writer
// with size limits to prevent memory exhaustion.
func DownloadToWriter(ctx context.Context, url string, writer io.Writer, opts DownloadOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = DefaultMaxDownloadSize
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: opts.Timeout,
	}

	// Create request with context for cancellation support
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Add custom headers
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return providererrors.NewDownloadError(url, 0, "", "", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			resp.Status,
			"",
			nil,
		)
	}

	// Early rejection based on Content-Length header
	if resp.ContentLength > 0 && resp.ContentLength > opts.MaxSize {
		return providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("download of %s exceeded maximum size of %d bytes (Content-Length: %d)",
				url, opts.MaxSize, resp.ContentLength),
			nil,
		)
	}

	// Copy to writer with size limit
	reader := io.LimitReader(resp.Body, opts.MaxSize+1)

	written, err := io.Copy(writer, reader)
	if err != nil {
		return providererrors.NewDownloadError(url, 0, "", "", err)
	}

	// Check if we exceeded the size limit during download
	if written > opts.MaxSize {
		return providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("download of %s exceeded maximum size of %d bytes", url, opts.MaxSize),
			nil,
		)
	}

	return nil
}

// GetContentType retrieves the Content-Type header from a URL without downloading the body
func GetContentType(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get headers: %w", err)
	}
	defer resp.Body.Close()

	return resp.Header.Get("Content-Type"), nil
}

// GetContentLength retrieves the Content-Length from a URL without downloading the body
func GetContentLength(ctx context.Context, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to get headers: %w", err)
	}
	defer resp.Body.Close()

	return resp.ContentLength, nil
}
