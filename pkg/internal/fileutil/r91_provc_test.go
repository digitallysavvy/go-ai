package fileutil

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r91provcTransport is an injected transport (TS: an injected fetch such as
// guarded-fetch) that records the URLs it is asked for.
type r91provcTransport struct {
	mu   sync.Mutex
	urls []string
}

func (t *r91provcTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.urls = append(t.urls, req.URL.String())
	t.mu.Unlock()
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("content")), Request: req}, nil
}

// TestR91provcInjectedTransportKeepsURLChecks (TS 2ebc78b654,
// download-blob.test.ts "uses an injected transport while retaining URL and
// redirect checks"): a caller-supplied transport performs the request, and
// the URL validation still rejects a private address before any request.
// Go keeps the DNS-pinned SafeTransport as the default (ruling 1, S2).
func TestR91provcInjectedTransportKeepsURLChecks(t *testing.T) {
	transport := &r91provcTransport{}
	opts := DefaultDownloadOptions()
	opts.Transport = transport

	data, err := Download(context.Background(), "https://files.example.com/file", opts)
	if err != nil || string(data) != "content" {
		t.Fatalf("Download = %q, %v", data, err)
	}
	_, err = Download(context.Background(), "http://127.0.0.1/file", opts)
	var dlErr *providererrors.DownloadError
	if !errors.As(err, &dlErr) {
		t.Fatalf("private URL err = %T %v, want DownloadError", err, err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.urls) != 1 || transport.urls[0] != "https://files.example.com/file" {
		t.Errorf("transport calls = %v", transport.urls)
	}
}

// TestR91provcDefaultTransportStaysDNSPinned (ruling 1): unlike TS after
// 2ebc78b654, the default download options keep connect-time DNS
// validation.
func TestR91provcDefaultTransportStaysDNSPinned(t *testing.T) {
	if DefaultDownloadOptions().Transport == nil {
		t.Fatal("default transport must be the DNS-pinning SafeTransport")
	}
}
