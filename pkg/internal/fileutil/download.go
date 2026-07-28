package fileutil

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

	// URLValidator is called before fetching and after each redirect to validate
	// the URL. Return a non-nil error to abort the download. Nil means no validation.
	URLValidator func(string) error
}

// DownloadResult contains downloaded file bytes and response metadata.
type DownloadResult struct {
	Data        []byte
	ContentType string
}

// DefaultDownloadOptions returns default download options
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		Timeout:      60 * time.Second,
		Headers:      make(map[string]string),
		MaxSize:      DefaultMaxDownloadSize,
		URLValidator: ValidateDownloadURL,
	}
}

// Download downloads a file from a URL with size limits to prevent memory exhaustion.
//
// It checks the Content-Length header for early rejection, then reads the body
// incrementally and aborts with a DownloadError when the limit is exceeded.
func Download(ctx context.Context, url string, opts DownloadOptions) ([]byte, error) {
	result, err := DownloadWithMetadata(ctx, url, opts)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

// DownloadWithMetadata downloads a file from a URL with size limits and returns
// response metadata needed by provider wire encoders.
func DownloadWithMetadata(ctx context.Context, url string, opts DownloadOptions) (*DownloadResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}

	if opts.MaxSize == 0 {
		opts.MaxSize = DefaultMaxDownloadSize
	}

	// Validate URL before fetching (SSRF prevention).
	if opts.URLValidator != nil {
		if err := opts.URLValidator(url); err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := decodeDataURL(url)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > opts.MaxSize {
			return nil, providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize), nil)
		}
		return &DownloadResult{Data: data, ContentType: dataURLMediaType(url)}, nil
	}

	// Create HTTP client with timeout and optional post-redirect URL validation.
	client := &http.Client{
		Timeout: opts.Timeout,
	}
	if opts.URLValidator != nil {
		validator := opts.URLValidator
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return providererrors.NewDownloadError(url, 0, "", "Too many redirects (max 10)", nil)
			}
			if err := validator(req.URL.String()); err != nil {
				return err
			}
			return nil
		}
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
		return nil, downloadRequestError(url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			responseStatusText(resp),
			"",
			nil,
		)
	}

	// Early rejection based on Content-Length header
	if contentLength, ok := responseContentLength(resp); ok && contentLength > opts.MaxSize {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).",
				url, opts.MaxSize, contentLength),
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
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize),
			nil,
		)
	}

	return &DownloadResult{Data: data, ContentType: resp.Header.Get("Content-Type")}, nil
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

	// Validate URL before fetching (SSRF prevention).
	if opts.URLValidator != nil {
		if err := opts.URLValidator(url); err != nil {
			return err
		}
	}
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		data, err := decodeDataURL(url)
		if err != nil {
			return err
		}
		if int64(len(data)) > opts.MaxSize {
			return providererrors.NewDownloadError(url, 0, "", fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize), nil)
		}
		_, err = writer.Write(data)
		return err
	}

	// Create HTTP client with timeout and optional post-redirect URL validation.
	client := &http.Client{
		Timeout: opts.Timeout,
	}
	if opts.URLValidator != nil {
		validator := opts.URLValidator
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return providererrors.NewDownloadError(url, 0, "", "Too many redirects (max 10)", nil)
			}
			if err := validator(req.URL.String()); err != nil {
				return err
			}
			return nil
		}
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
		return downloadRequestError(url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return providererrors.NewDownloadError(
			url,
			resp.StatusCode,
			responseStatusText(resp),
			"",
			nil,
		)
	}

	// Early rejection based on Content-Length header
	if contentLength, ok := responseContentLength(resp); ok && contentLength > opts.MaxSize {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return providererrors.NewDownloadError(
			url,
			0,
			"",
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes (Content-Length: %d).",
				url, opts.MaxSize, contentLength),
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
			fmt.Sprintf("Download of %s exceeded maximum size of %d bytes.", url, opts.MaxSize),
			nil,
		)
	}

	return nil
}

func responseStatusText(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	prefix := fmt.Sprintf("%d ", resp.StatusCode)
	if strings.HasPrefix(resp.Status, prefix) {
		return strings.TrimPrefix(resp.Status, prefix)
	}
	return http.StatusText(resp.StatusCode)
}

func responseContentLength(resp *http.Response) (int64, bool) {
	if resp == nil {
		return 0, false
	}
	raw := resp.Header.Get("Content-Length")
	if raw == "" {
		if resp.ContentLength > 0 {
			return resp.ContentLength, true
		}
		return 0, false
	}
	raw = strings.TrimLeft(raw, " \t\n\r\f\v")
	sign := int64(1)
	if strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	} else if strings.HasPrefix(raw, "-") {
		sign = -1
		raw = raw[1:]
	}
	var value int64
	digits := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			break
		}
		digits++
		if value <= (1<<63-1-int64(r-'0'))/10 {
			value = value*10 + int64(r-'0')
		} else {
			value = 1<<63 - 1
		}
	}
	if digits == 0 {
		return 0, false
	}
	return sign * value, true
}

func downloadRequestError(raw string, err error) *providererrors.DownloadError {
	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) {
		return downloadErr
	}
	return providererrors.NewDownloadError(raw, 0, "", "", err)
}

func decodeDataURL(raw string) ([]byte, error) {
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return nil, providererrors.NewDownloadError(raw, 0, "", fmt.Sprintf("Invalid URL: %s", raw), nil)
	}
	meta := raw[len("data:"):comma]
	payload := raw[comma+1:]
	if strings.HasSuffix(strings.ToLower(meta), ";base64") {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, providererrors.NewDownloadError(raw, 0, "", "", err)
		}
		return data, nil
	}
	data, err := url.QueryUnescape(payload)
	if err != nil {
		return nil, providererrors.NewDownloadError(raw, 0, "", "", err)
	}
	return []byte(data), nil
}

func dataURLMediaType(raw string) string {
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return ""
	}
	meta := raw[len("data:"):comma]
	if semi := strings.IndexByte(meta, ';'); semi >= 0 {
		meta = meta[:semi]
	}
	return meta
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
	defer resp.Body.Close() //nolint:errcheck

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
	defer resp.Body.Close() //nolint:errcheck

	return resp.ContentLength, nil
}
