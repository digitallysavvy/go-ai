package lmnt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestR93provcNetworkFailureIsConnectError: like every raw-client provider
// site (internalhttp.TransportErrorCtx), a connection that fails before any
// response is the retryable "Cannot connect to API" error; a nil options
// pointer does not panic.
func TestR93provcNetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	m := &SpeechModel{provider: New(Config{APIKey: "k", BaseURL: srv.URL}), modelID: "aurora"}
	_, err := m.DoGenerate(context.Background(), nil)
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}
