package fileutil

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type r93provcRefusingTransport struct{}

func (r93provcRefusingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
}

// TestR93provcTransportFailure: only a failure that never reached an HTTP
// response (TS fetch rejecting with "fetch failed") is a transport failure;
// a rejected URL, a blocked redirect hop and a non-2xx status keep their
// DownloadError (TS fetchWithValidatedRedirects / failedResponseHandler).
func TestR93provcTransportFailure(t *testing.T) {
	opts := DefaultDownloadOptions()
	opts.Transport = r93provcRefusingTransport{}
	_, err := DownloadWithMetadata(context.Background(), "https://files.example.com/x", opts)
	cause, ok := TransportFailure(err)
	var netErr *net.OpError
	if !ok || !errors.As(cause, &netErr) {
		t.Errorf("refused connection: TransportFailure(%v) = %v, %v", err, cause, ok)
	}

	_, err = DownloadWithMetadata(context.Background(), "http://127.0.0.1/x", DefaultDownloadOptions())
	if _, ok := TransportFailure(err); err == nil || ok {
		t.Errorf("blocked URL: %v counted as a transport failure", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	local := DefaultDownloadOptions()
	local.URLValidator = TrustedURLValidator(func(raw string) bool { return raw == server.URL+"/redirect" || raw == server.URL+"/missing" })
	local.Transport = http.DefaultTransport
	_, err = DownloadWithMetadata(context.Background(), server.URL+"/redirect", local)
	if _, ok := TransportFailure(err); err == nil || ok {
		t.Errorf("blocked redirect: %v counted as a transport failure", err)
	}
	_, err = DownloadWithMetadata(context.Background(), server.URL+"/missing", local)
	if _, ok := TransportFailure(err); err == nil || ok {
		t.Errorf("404: %v counted as a transport failure", err)
	}
	if _, ok := TransportFailure(errors.New("other")); ok {
		t.Error("a plain error counted as a transport failure")
	}
}
