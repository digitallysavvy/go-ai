package fileutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestDownload_Success(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	data, err := Download(context.Background(), server.URL, DefaultDownloadOptions())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if string(data) != string(content) {
		t.Fatalf("expected %q, got %q", content, data)
	}
}

func TestDownload_DefaultLimit2GiB(t *testing.T) {
	opts := DefaultDownloadOptions()
	if opts.MaxSize != DefaultMaxDownloadSize {
		t.Fatalf("expected default max size %d, got %d", DefaultMaxDownloadSize, opts.MaxSize)
	}
	if opts.MaxSize != 2*1024*1024*1024 {
		t.Fatalf("expected 2 GiB (%d), got %d", 2*1024*1024*1024, opts.MaxSize)
	}
}

func TestDownload_ContentLengthExceedsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test"))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.MaxSize = 500 // Set limit to 500 bytes

	_, err := Download(context.Background(), server.URL, opts)
	if err == nil {
		t.Fatal("expected error for content-length exceeding limit")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}

	if !strings.Contains(err.Error(), "exceeded maximum size") {
		t.Fatalf("expected error message about exceeding size, got: %v", err)
	}
}

func TestDownload_BodyExceedsLimit(t *testing.T) {
	// Server sends more data than Content-Length or doesn't set Content-Length
	largeContent := strings.Repeat("x", 1001)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Don't set Content-Length to test streaming detection
		_, _ = w.Write([]byte(largeContent))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.MaxSize = 1000 // Set limit to 1000 bytes

	_, err := Download(context.Background(), server.URL, opts)
	if err == nil {
		t.Fatal("expected error for body exceeding limit")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}

	if !strings.Contains(err.Error(), "exceeded maximum size") {
		t.Fatalf("expected error message about exceeding size, got: %v", err)
	}
}

func TestDownload_ExactlyAtLimit(t *testing.T) {
	content := strings.Repeat("x", 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.MaxSize = 1000

	data, err := Download(context.Background(), server.URL, opts)
	if err != nil {
		t.Fatalf("expected no error for content exactly at limit, got %v", err)
	}

	if len(data) != 1000 {
		t.Fatalf("expected %d bytes, got %d", 1000, len(data))
	}
}

func TestDownload_JustOverLimit(t *testing.T) {
	content := strings.Repeat("x", 1001)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.MaxSize = 1000

	_, err := Download(context.Background(), server.URL, opts)
	if err == nil {
		t.Fatal("expected error for content just over limit")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
}

func TestDownload_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	_, err := Download(context.Background(), server.URL, DefaultDownloadOptions())
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}

	if downloadErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status code %d, got %d", http.StatusNotFound, downloadErr.StatusCode)
	}
}

func TestDownload_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := Download(ctx, server.URL, DefaultDownloadOptions())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	if !errors.Is(err, context.Canceled) && !providererrors.IsDownloadError(err) {
		t.Fatalf("expected context.Canceled or DownloadError, got %T: %v", err, err)
	}
}

func TestDownload_ContextTimeout(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("delayed"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := Download(ctx, server.URL, DefaultDownloadOptions())
	if err == nil {
		t.Fatal("expected error for timeout")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !providererrors.IsDownloadError(err) {
		t.Fatalf("expected context.DeadlineExceeded or DownloadError, got %T: %v", err, err)
	}
}

func TestDownload_CustomHeaders(t *testing.T) {
	expectedValue := "custom-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != expectedValue {
			t.Errorf("expected custom header %q, got %q", expectedValue, r.Header.Get("X-Custom-Header"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.Headers = map[string]string{
		"X-Custom-Header": expectedValue,
	}

	_, err := Download(context.Background(), server.URL, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestDownload_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Empty body
	}))
	defer server.Close()

	data, err := Download(context.Background(), server.URL, DefaultDownloadOptions())
	if err != nil {
		t.Fatalf("expected no error for empty response, got %v", err)
	}

	if len(data) != 0 {
		t.Fatalf("expected empty data, got %d bytes", len(data))
	}
}

func TestDownload_InvalidURL(t *testing.T) {
	_, err := Download(context.Background(), "://invalid-url", DefaultDownloadOptions())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
}

func TestDownload_NetworkError(t *testing.T) {
	// Use a URL that will fail to connect
	_, err := Download(context.Background(), "http://localhost:1", DefaultDownloadOptions())
	if err == nil {
		t.Fatal("expected error for network failure")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
}

func TestDownloadToWriter_Success(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer server.Close()

	var buf strings.Builder
	err := DownloadToWriter(context.Background(), server.URL, &buf, DefaultDownloadOptions())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if buf.String() != string(content) {
		t.Fatalf("expected %q, got %q", content, buf.String())
	}
}

func TestDownloadToWriter_ExceedsLimit(t *testing.T) {
	content := strings.Repeat("x", 1001)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.MaxSize = 1000

	var buf strings.Builder
	err := DownloadToWriter(context.Background(), server.URL, &buf, opts)
	if err == nil {
		t.Fatal("expected error for content exceeding limit")
	}

	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
}

func TestDownloadError_IsInstance(t *testing.T) {
	err := providererrors.NewDownloadError("http://example.com", 404, "Not Found", "", nil)

	if !providererrors.IsDownloadError(err) {
		t.Fatal("expected IsDownloadError to return true")
	}

	// Test with wrapped error
	wrappedErr := fmt.Errorf("wrapped: %w", err)
	if !providererrors.IsDownloadError(wrappedErr) {
		t.Fatal("expected IsDownloadError to return true for wrapped error")
	}

	// Test with non-DownloadError
	otherErr := errors.New("other error")
	if providererrors.IsDownloadError(otherErr) {
		t.Fatal("expected IsDownloadError to return false for non-DownloadError")
	}
}

func TestDownloadError_ErrorMessage(t *testing.T) {
	tests := []struct {
		name           string
		err            *providererrors.DownloadError
		expectedSubstr string
	}{
		{
			name: "with status code",
			err: providererrors.NewDownloadError(
				"http://example.com/file.jpg",
				404,
				"Not Found",
				"",
				nil,
			),
			expectedSubstr: "404",
		},
		{
			name: "with custom message",
			err: providererrors.NewDownloadError(
				"http://example.com/file.jpg",
				0,
				"",
				"download of http://example.com/file.jpg exceeded maximum size",
				nil,
			),
			expectedSubstr: "exceeded maximum size",
		},
		{
			name: "with cause",
			err: providererrors.NewDownloadError(
				"http://example.com/file.jpg",
				0,
				"",
				"",
				errors.New("network error"),
			),
			expectedSubstr: "network error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errMsg := tt.err.Error()
			if !strings.Contains(errMsg, tt.expectedSubstr) {
				t.Fatalf("expected error message to contain %q, got: %s", tt.expectedSubstr, errMsg)
			}
			if !strings.Contains(errMsg, "http://example.com/file.jpg") {
				t.Fatalf("expected error message to contain URL, got: %s", errMsg)
			}
		})
	}
}
