package fileutil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DownloadOptions contains options for downloading files
type DownloadOptions struct {
	// Timeout for the download operation
	Timeout time.Duration

	// Headers to include in the request
	Headers map[string]string

	// MaxSize limits the size of the download (in bytes)
	MaxSize int64
}

// DefaultDownloadOptions returns default download options
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		Timeout: 60 * time.Second,
		Headers: make(map[string]string),
		MaxSize: 100 * 1024 * 1024, // 100MB default
	}
}

// Download downloads a file from a URL
func Download(ctx context.Context, url string, opts DownloadOptions) ([]byte, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = 100 * 1024 * 1024 // 100MB
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: opts.Timeout,
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add custom headers
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Check content length if available
	if resp.ContentLength > 0 && resp.ContentLength > opts.MaxSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", resp.ContentLength, opts.MaxSize)
	}

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, opts.MaxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if we hit the size limit
	if int64(len(data)) > opts.MaxSize {
		return nil, fmt.Errorf("file exceeds maximum size: %d bytes", opts.MaxSize)
	}

	return data, nil
}

// DownloadToWriter downloads a file from a URL and writes it to an io.Writer
func DownloadToWriter(ctx context.Context, url string, writer io.Writer, opts DownloadOptions) error {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: opts.Timeout,
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add custom headers
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Copy to writer
	var reader io.Reader = resp.Body
	if opts.MaxSize > 0 {
		reader = io.LimitReader(resp.Body, opts.MaxSize)
	}

	written, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Check if we hit the size limit
	if opts.MaxSize > 0 && written >= opts.MaxSize {
		return fmt.Errorf("file exceeds maximum size: %d bytes", opts.MaxSize)
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
