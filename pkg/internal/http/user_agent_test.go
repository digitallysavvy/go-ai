package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPTransportNoCustomUserAgent verifies that the internal HTTP client does
// not add a custom User-Agent header to outgoing requests. A custom User-Agent is
// a non-simple CORS header that would trigger preflight requests in browsers,
// breaking client-side usage in Safari and Firefox.
func TestHTTPTransportNoCustomUserAgent(t *testing.T) {
	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := NewClient(Config{BaseURL: srv.URL})
	_, err := client.Do(context.Background(), Request{
		Method: http.MethodGet,
		Path:   "/",
	})
	// Ignore errors — we only care about the User-Agent header.
	_ = err

	// Go's net/http sets "Go-http-client/1.1" (or 2.0) automatically.
	// The SDK must NOT override this with a custom value like "go-ai/1.0".
	// We accept any value that is the Go default or empty; we explicitly reject
	// values that look like custom SDK branding.
	if capturedUA != "" &&
		capturedUA != "Go-http-client/1.1" &&
		capturedUA != "Go-http-client/2.0" {
		t.Errorf("unexpected custom User-Agent header: %q; the SDK must not set a custom User-Agent", capturedUA)
	}
}
