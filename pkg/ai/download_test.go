package ai

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestCreateDownloadWithNilOptionsRejectsUnsafeURL(t *testing.T) {
	download := CreateDownload(nil)
	if download == nil {
		t.Fatal("CreateDownload(nil) returned nil")
	}

	_, err := download(context.Background(), []DownloadRequest{{URL: "http://127.0.0.1/private"}})
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

	_, err := download(context.Background(), []DownloadRequest{{URL: "file:///etc/passwd"}})
	if err == nil {
		t.Fatal("expected blocked file:// URL")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected scheme validation error, got: %v", err)
	}
}

func TestDefaultDownloadUsesCreateDownloadBehavior(t *testing.T) {
	_, err := DefaultDownload(context.Background(), []DownloadRequest{{URL: "http://localhost:8080/file"}})
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

	results, err := download(context.Background(), []DownloadRequest{{URL: server.URL}})
	if err != nil {
		t.Fatalf("download error = %v", err)
	}
	if len(results) != 1 || results[0] == nil || string(results[0].Data) != "ok" {
		t.Fatalf("download results = %#v, want ok data", results)
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
	_, err := download(context.Background(), []DownloadRequest{{URL: server.URL}})
	if err == nil {
		t.Fatal("expected max-bytes error")
	}
	if !strings.Contains(err.Error(), "exceeded maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateDownloadLeavesSupportedURLAsNilResult(t *testing.T) {
	download := CreateDownload(nil)
	results, err := download(context.Background(), []DownloadRequest{{
		URL:                   "http://127.0.0.1/private",
		IsURLSupportedByModel: true,
	}})
	if err != nil {
		t.Fatalf("download error = %v", err)
	}
	if len(results) != 1 || results[0] != nil {
		t.Fatalf("results = %#v, want single nil pass-through result", results)
	}
}

func TestCreateDownload_BlocksOpenRedirectChainViaPostRedirectValidation(t *testing.T) {
	finalURL := "http://169.254.169.254/latest/meta-data"
	redirect2 := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalURL, http.StatusFound)
	}))
	defer redirect2.Close()
	redirect1 := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect2.URL, http.StatusFound)
	}))
	defer redirect1.Close()

	restore := downloadURLValidator
	downloadURLValidator = func(raw string) error {
		// Allow our local redirect fixtures as the initial trusted URLs.
		if raw == redirect1.URL || raw == redirect2.URL {
			return nil
		}
		if strings.Contains(raw, "169.254.169.254") {
			return providererrors.NewSSRFError(raw, "blocked redirect target: "+raw, nil)
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		if parsed.Host == "" {
			return providererrors.NewSSRFError(raw, "invalid redirect host", nil)
		}
		return nil
	}
	t.Cleanup(func() { downloadURLValidator = restore })

	download := CreateDownload(nil)
	_, err := download(context.Background(), []DownloadRequest{{URL: redirect1.URL}})
	if err == nil {
		t.Fatal("expected redirect chain to private target to be blocked")
	}
	if !strings.Contains(err.Error(), "blocked redirect target") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !providererrors.IsSSRFError(err) {
		t.Fatalf("expected SSRFError in error chain, got: %T %[1]v", err)
	}
}

func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen tcp4 error = %v", err)
	}
	server := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: handler},
	}
	server.Start()
	return server
}
