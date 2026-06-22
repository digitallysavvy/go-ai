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

func insecureDownloadOptions() DownloadOptions {
	opts := DefaultDownloadOptions()
	opts.URLValidator = nil
	return opts
}

func TestDownload_Success(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	data, err := Download(context.Background(), server.URL, insecureDownloadOptions())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if string(data) != string(content) {
		t.Fatalf("expected %q, got %q", content, data)
	}
}

func TestDownload_DefaultLimit2GiB(t *testing.T) {
	opts := insecureDownloadOptions()
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

	opts := insecureDownloadOptions()
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

func TestResponseContentLengthMatchesTSParseInt(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
		ok   bool
	}{
		{name: "plain", raw: "1000", want: 1000, ok: true},
		{name: "leading whitespace", raw: " \t1000", want: 1000, ok: true},
		{name: "leading plus", raw: "+1000", want: 1000, ok: true},
		{name: "leading numeric garbage", raw: "1000abc", want: 1000, ok: true},
		{name: "comma suffix", raw: "1000, 1000", want: 1000, ok: true},
		{name: "negative", raw: "-1", want: -1, ok: true},
		{name: "not numeric", raw: "abc1000", ok: false},
		{name: "empty", raw: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: make(http.Header)}
			if tt.raw != "" {
				resp.Header.Set("Content-Length", tt.raw)
			}
			got, ok := responseContentLength(resp)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("responseContentLength(%q) = (%d, %v), want (%d, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
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

	opts := insecureDownloadOptions()
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
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	opts := insecureDownloadOptions()
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
		_, _ = w.Write([]byte(content))
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
		_, _ = w.Write([]byte("not found"))
	}))
	defer server.Close()

	_, err := Download(context.Background(), server.URL, insecureDownloadOptions())
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
	if downloadErr.StatusText != "Not Found" {
		t.Fatalf("expected status text %q, got %q", "Not Found", downloadErr.StatusText)
	}
	if got, want := downloadErr.Error(), "Failed to download "+server.URL+": 404 Not Found"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestResponseStatusTextPreservesCustomReasonPhrase(t *testing.T) {
	resp := &http.Response{
		StatusCode: 499,
		Status:     "499 Custom Client Closed Request",
	}
	if got, want := responseStatusText(resp), "Custom Client Closed Request"; got != want {
		t.Fatalf("status text = %q, want %q", got, want)
	}

	resp = &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
	}
	if got, want := responseStatusText(resp), "Not Found"; got != want {
		t.Fatalf("status text = %q, want %q", got, want)
	}
}

func TestDownload_AcceptsAny2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	data, err := Download(context.Background(), server.URL, insecureDownloadOptions())
	if err != nil {
		t.Fatalf("expected no error for 204 response, got %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("data len = %d, want 0", len(data))
	}

	var buf strings.Builder
	if err := DownloadToWriter(context.Background(), server.URL, &buf, insecureDownloadOptions()); err != nil {
		t.Fatalf("DownloadToWriter expected no error for 204 response, got %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("writer len = %d, want 0", buf.Len())
	}
}

func TestDownload_ContextCancellation(t *testing.T) {
	// Server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("delayed"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := Download(ctx, server.URL, insecureDownloadOptions())
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
		_, _ = w.Write([]byte("delayed"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := Download(ctx, server.URL, insecureDownloadOptions())
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
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	opts := insecureDownloadOptions()
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

	data, err := Download(context.Background(), server.URL, insecureDownloadOptions())
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

func TestValidateDownloadURLBlocksUnsafeHosts(t *testing.T) {
	blocked := []string{
		"file:///etc/passwd",
		"ftp://example.com/file",
		"javascript:alert(1)",
		"http://localhost/file",
		"http://localhost./file",
		"http://app.localhost/file",
		"http://example.local/file",
		"http://127.0.0.1/file",
		"http://10.0.0.1/file",
		"http://172.16.0.1/file",
		"http://192.168.1.1/file",
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/file",
		"http://2130706433/file",
		"http://0x7f000001/file",
		"http://0177.0.0.1/file",
		"http://%31%32%37.0.0.1/file",
		"http://%30%78%37%66.1/file",
		"http://127.1/file",
		"http://127.0.1/file",
		"http://0x7f.1/file",
		"http://0177.1/file",
		"http://0300.0250.1.1/file",
		"http://192.168.1/file",
		"http://10.1/file",
		"http://[::1]/file",
		"http://[fc00::1]/file",
		"http://[fe80::1]/file",
		"http://[fec0::1]/file",
		"http://[ff02::1]/file",
		"http://[::127.0.0.1]/file",
		"http://[::ffff:127.0.0.1]/file",
		"http://[::ffff:0:127.0.0.1]/file",
		"http://[64:ff9b::127.0.0.1]/file",
		"http://[64:ff9b::169.254.169.254]/file",
		"http://[64:ff9b:1::169.254.169.254]/file",
		"http://100.64.0.1/file",
		"http://100.127.255.255/file",
		"http://198.18.0.1/file",
		"http://198.19.255.255/file",
		"http://192.0.0.1/file",
		"http://240.0.0.1/file",
		"http://255.255.255.255/file",
	}
	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateDownloadURL(raw); err == nil {
				t.Fatal("expected unsafe URL to be blocked")
			}
		})
	}

	allowed := []string{
		"https://example.com/image.png",
		"http://example.com:8080/file",
		"data:text/plain;base64,aGVsbG8=",
		"http://172.15.0.1/file",
		"http://172.32.0.1/file",
		"http://203.0.113.1/file",
		"http://[::ffff:203.0.113.1]/file",
		"http://[64:ff9b::203.0.113.1]/file",
		"http://[2001:db8::1]/file",
		"http://100.63.0.1/file",
		"http://100.128.0.1/file",
		"http://8.8/file",
		"http://127.0.0.1%2eexample.com/file",
		"http://%E3%81%82.com/file",
		"https://example.com./image.png",
	}
	for _, raw := range allowed {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateDownloadURL(raw); err != nil {
				t.Fatalf("expected URL to be allowed, got %v", err)
			}
		})
	}
}

func TestValidateDownloadURLMatchesTSErrorMessagesForLiteralHosts(t *testing.T) {
	tests := []struct {
		raw     string
		message string
	}{
		{
			raw:     "http://2130706433/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://0x7f000001/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://0177.0.0.1/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://%31%32%37.0.0.1/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://%30%78%37%66.1/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://127.1/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://0x7f.1/file",
			message: "URL with IP address 127.0.0.1 is not allowed",
		},
		{
			raw:     "http://0300.0250.1.1/file",
			message: "URL with IP address 192.168.1.1 is not allowed",
		},
		{
			raw:     "http://[::1]/file",
			message: "URL with IPv6 address [::1] is not allowed",
		},
		{
			raw:     "http://[::ffff:127.0.0.1]/file",
			message: "URL with IPv6 address [::ffff:7f00:1] is not allowed",
		},
		{
			raw:     "http://[::ffff:0:127.0.0.1]/file",
			message: "URL with IPv6 address [::ffff:0:7f00:1] is not allowed",
		},
		{
			raw:     "http://[64:ff9b::169.254.169.254]/file",
			message: "URL with IPv6 address [64:ff9b::a9fe:a9fe] is not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			err := ValidateDownloadURL(tt.raw)
			if err == nil {
				t.Fatal("expected unsafe URL to be blocked")
			}
			var downloadErr *providererrors.DownloadError
			if !errors.As(err, &downloadErr) {
				t.Fatalf("expected DownloadError, got %T", err)
			}
			if downloadErr.Message != tt.message {
				t.Fatalf("message = %q, want %q", downloadErr.Message, tt.message)
			}
		})
	}
}

func TestValidateDownloadURLMatchesTSInvalidNumericHosts(t *testing.T) {
	tests := []string{
		"http://1.2.3.4.5/file",
		"http://1..2/file",
		"http://08.0.0.1/file",
		"http://09/file",
		"http://%zz/file",
		"http://%5B::1%5D/file",
		"http://example%2f.com/file",
		"http://example%3a80/file",
		"http://example%40evil.com/file",
		"http://%00example.com/file",
		"http://example.com:%38%30/file",
		"http://example.com:99999/file",
		"http://[fe80::1%25eth0]/file",
		"http://169.254.169254/file",
		"http://4294967296/file",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			err := ValidateDownloadURL(raw)
			if err == nil {
				t.Fatal("expected invalid numeric host to be rejected")
			}
			var downloadErr *providererrors.DownloadError
			if !errors.As(err, &downloadErr) {
				t.Fatalf("expected DownloadError, got %T", err)
			}
			if downloadErr.Message != "Invalid URL: "+raw {
				t.Fatalf("message = %q, want %q", downloadErr.Message, "Invalid URL: "+raw)
			}
		})
	}
}

func TestDownloadRedirectTargetValidationBlocksUnsafeTarget(t *testing.T) {
	redirectHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectHit = true
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.URLValidator = func(raw string) error {
		if strings.HasPrefix(raw, server.URL) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}

	_, err := Download(context.Background(), server.URL, opts)
	if err == nil {
		t.Fatal("expected unsafe redirect target to be blocked")
	}
	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
	if downloadErr.URL != "http://127.0.0.1/private" {
		t.Fatalf("download error URL = %q, want rejected redirect target", downloadErr.URL)
	}
	if !redirectHit {
		t.Fatal("expected initial redirect response to be requested")
	}
}

func TestDownloadAllowsTenRedirectsThenRejectsNextHop(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		var hop int
		_, _ = fmt.Sscanf(r.URL.Path, "/hop/%d", &hop)
		if hop < 10 {
			http.Redirect(w, r, fmt.Sprintf("/hop/%d", hop+1), http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	opts := DefaultDownloadOptions()
	opts.URLValidator = func(raw string) error {
		if strings.HasPrefix(raw, server.URL) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}
	data, err := Download(context.Background(), server.URL+"/hop/0", opts)
	if err != nil {
		t.Fatalf("expected ten redirects to succeed, got %v", err)
	}
	if string(data) != "ok" || hits != 11 {
		t.Fatalf("data=%q hits=%d, want ok and 11 requests", data, hits)
	}

	hits = 0
	looping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer looping.Close()
	opts.URLValidator = func(raw string) error {
		if strings.HasPrefix(raw, looping.URL) {
			return nil
		}
		return ValidateDownloadURL(raw)
	}
	_, err = Download(context.Background(), looping.URL+"/loop", opts)
	if err == nil {
		t.Fatal("expected redirect limit error")
	}
	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("expected DownloadError, got %T", err)
	}
	if downloadErr.Message != "Too many redirects (max 10)" {
		t.Fatalf("download error message = %q, want TS redirect limit message", downloadErr.Message)
	}
	if hits != 11 {
		t.Fatalf("hits=%d, want 11 requests before rejecting next hop", hits)
	}
}

func TestDownloadToWriter_Success(t *testing.T) {
	content := []byte("test content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	var buf strings.Builder
	err := DownloadToWriter(context.Background(), server.URL, &buf, insecureDownloadOptions())
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
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	opts := insecureDownloadOptions()
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
				"Download of http://example.com/file.jpg exceeded maximum size",
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
