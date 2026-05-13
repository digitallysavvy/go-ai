package ai

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateDownloadWithNilOptionsRejectsUnsafeURL(t *testing.T) {
	download := CreateDownload(nil)
	if download == nil {
		t.Fatal("CreateDownload(nil) returned nil")
	}

	_, err := download(context.Background(), "http://127.0.0.1/private")
	if err == nil {
		t.Fatal("expected unsafe URL validation error")
	}
	if !strings.Contains(err.Error(), "private network") && !strings.Contains(err.Error(), "blocked") && !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("expected SSRF validation error, got: %v", err)
	}
}

func TestCreateDownloadWithCustomOptionsAndHeadersPath(t *testing.T) {
	download := CreateDownload(&DownloadOptions{
		MaxBytes: 64,
		Headers: map[string]string{
			"X-Test-Header": "value",
		},
	})

	_, err := download(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("expected blocked file:// URL")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected scheme validation error, got: %v", err)
	}
}

func TestDefaultDownloadUsesCreateDownloadBehavior(t *testing.T) {
	_, err := DefaultDownload(context.Background(), "http://localhost:8080/file")
	if err == nil {
		t.Fatal("expected localhost to be blocked")
	}
	if !strings.Contains(err.Error(), "localhost") && !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateDownloadSuccessfulFetchForwardsHeaders(t *testing.T) {
	restore := downloadURLValidator
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadURLValidator = restore })

	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test-Header"); got != "value" {
			t.Fatalf("X-Test-Header = %q, want value", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	download := CreateDownload(&DownloadOptions{
		Headers: map[string]string{"X-Test-Header": "value"},
	})

	data, err := download(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("download error = %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("download data = %q, want ok", string(data))
	}
}

func TestCreateDownloadHonorsMaxBytes(t *testing.T) {
	restore := downloadURLValidator
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadURLValidator = restore })

	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 20)))
	}))
	defer server.Close()

	download := CreateDownload(&DownloadOptions{MaxBytes: 10})
	_, err := download(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected max-bytes error")
	}
	if !strings.Contains(err.Error(), "exceeded maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen tcp4 error = %v", err)
	}
	server.Listener = listener
	server.Start()
	return server
}
