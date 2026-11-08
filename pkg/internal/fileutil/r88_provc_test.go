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

// r88provcRedirectTransport answers from a fixed table without network: a
// URL with a Location entry gets that redirect status, any other URL a 200.
type r88provcRedirectTransport struct {
	mu        sync.Mutex
	status    int
	locations map[string]string
	requests  []string
}

func (t *r88provcRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.URL.String())
	t.mu.Unlock()
	h := http.Header{}
	status := http.StatusOK
	body := "download"
	if loc, ok := t.locations[req.URL.String()]; ok {
		status = t.status
		h.Set("Location", loc)
		body = "redirect"
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// TS fetch-with-validated-redirects.test.ts (f608c056a2): HTTPS to HTTP
// redirects are blocked before the target is requested, for every redirect
// status, after an HTTP to HTTPS upgrade, and for a trusted-origin target;
// HTTP starts, HTTP to HTTP hops and upgrades keep working.
func TestR88provcHTTPSDowngradeRedirectBlocked(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		tr := &r88provcRedirectTransport{status: status, locations: map[string]string{
			"https://api.example.com/file": "http://cdn.example.net/file",
		}}
		opts := DownloadOptions{MaxSize: 1 << 20, Transport: tr}
		_, err := DownloadWithMetadata(context.Background(), "https://api.example.com/file", opts)
		var de *providererrors.DownloadError
		if !errors.As(err, &de) || de.Message != "Redirect from HTTPS to HTTP was blocked" || de.URL != "https://api.example.com/file" {
			t.Fatalf("status %d: err = %v, want the downgrade DownloadError for the original URL", status, err)
		}
		if len(tr.requests) != 1 {
			t.Fatalf("status %d: requests = %v, want only the HTTPS request", status, tr.requests)
		}
	}

	t.Run("upgrade then downgrade", func(t *testing.T) {
		tr := &r88provcRedirectTransport{status: 302, locations: map[string]string{
			"http://cdn.example.net/upgrade":    "https://api.example.com/downgrade",
			"https://api.example.com/downgrade": "http://cdn.example.net/done",
		}}
		_, err := Download(context.Background(), "http://cdn.example.net/upgrade", DownloadOptions{MaxSize: 1 << 20, Transport: tr})
		if err == nil || !strings.Contains(err.Error(), "Redirect from HTTPS to HTTP was blocked") {
			t.Fatalf("err = %v", err)
		}
		if len(tr.requests) != 2 {
			t.Fatalf("requests = %v", tr.requests)
		}
	})

	t.Run("trusted origin target still blocked", func(t *testing.T) {
		tr := &r88provcRedirectTransport{status: 307, locations: map[string]string{
			"https://cdn.example.net/file": "http://self-hosted.example/file",
		}}
		opts := TrustedOriginDownloadOptions("http://self-hosted.example", tr)
		opts.Transport = tr
		opts.URLValidator = func(string) error { return nil }
		_, err := Download(context.Background(), "https://cdn.example.net/file", opts)
		if err == nil || !strings.Contains(err.Error(), "Redirect from HTTPS to HTTP was blocked") {
			t.Fatalf("err = %v", err)
		}
		if len(tr.requests) != 1 {
			t.Fatalf("requests = %v", tr.requests)
		}
	})

	t.Run("http, http to http and upgrades work", func(t *testing.T) {
		tr := &r88provcRedirectTransport{status: 302, locations: map[string]string{
			"http://a.example/1":  "http://b.example/2",
			"http://b.example/2":  "https://c.example/3",
			"https://c.example/3": "https://d.example/4",
		}}
		data, err := Download(context.Background(), "http://a.example/1", DownloadOptions{MaxSize: 1 << 20, Transport: tr})
		if err != nil || string(data) != "download" || len(tr.requests) != 4 {
			t.Fatalf("data = %q, err = %v, requests = %v", data, err, tr.requests)
		}
	})

	t.Run("downgrade on the last allowed hop wins over the redirect bound", func(t *testing.T) {
		locs := map[string]string{}
		for i := 0; i < maxDownloadRedirects; i++ {
			locs["https://a.example/"+string(rune('a'+i))] = "https://a.example/" + string(rune('a'+i+1))
		}
		last := "https://a.example/" + string(rune('a'+maxDownloadRedirects))
		locs[last] = "http://a.example/plain"
		tr := &r88provcRedirectTransport{status: 302, locations: locs}
		_, err := Download(context.Background(), "https://a.example/a", DownloadOptions{MaxSize: 1 << 20, Transport: tr})
		if err == nil || !strings.Contains(err.Error(), "Redirect from HTTPS to HTTP was blocked") {
			t.Fatalf("err = %v", err)
		}
	})
}
