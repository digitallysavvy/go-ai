package cartesia

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97provcClientSecretIsRawFetch: TS doCreateClientSecret calls the fetch
// function itself (this.config.fetch ?? fetch), so a failed request rejects
// as is, not as the retryable "Cannot connect to API" APICallError, and the
// request carries {...config headers, 'Content-Type': 'application/json'}
// with no runtime user-agent tag.
func TestR97provcClientSecretIsRawFetch(t *testing.T) {
	ln := httptest.NewServer(http.NotFoundHandler())
	deadURL := ln.URL
	ln.Close()
	m := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: deadURL}), "ink-2")
	_, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err == nil {
		t.Fatal("want the transport error")
	}
	if strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %v, want the fetch rejection as is", err)
	}
	var retryable interface{ IsRetryable() bool }
	if errors.As(err, &retryable) && retryable.IsRetryable() {
		t.Fatalf("err = %v is retryable; TS raw fetch rejections are not", err)
	}

	var got http.Header
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = io.WriteString(w, `{"token":"t1"}`)
	}))
	defer srv.Close()
	m = NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "ink-2")
	res, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil || res.Token != "t1" {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	if body != `{"grants":{"stt":true}}` {
		t.Errorf("body = %s", body)
	}
	if got.Get("Authorization") != "Bearer k" || got.Get("Cartesia-Version") == "" || got.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", got)
	}
	ua := got.Get("User-Agent")
	if !strings.Contains(ua, "ai-sdk-cartesia/") || strings.Contains(ua, providerutils.RuntimeEnvironmentUserAgent()) {
		t.Errorf("User-Agent = %q, want the provider tag without the runtime tag", ua)
	}
}

// TestR97provcRealtimeParseNullEvent (cr-r97-provb-to-provc): TS
// parseServerEvent reads event.request_id of a JSON null message and throws;
// Go returned a custom event.
func TestR97provcRealtimeParseNullEvent(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "ink-2")
	events, err := m.ParseServerEvent([]byte(" null "))
	if err == nil || err.Error() != "Cannot read properties of null (reading 'request_id')" || events != nil {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	if _, err := m.ParseServerEvent([]byte(`{"type":"connected","request_id":"r"}`)); err != nil {
		t.Fatalf("connected: %v", err)
	}
}
