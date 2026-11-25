package deepgram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcBase64AudioSentAsGiven: TS postToApi sends options.audio as
// the request content, so a base64 string audio goes out as that string.
// Before, Go sent an empty body when only AudioBase64 was set.
func TestR96provcBase64AudioSentAsGiven(t *testing.T) {
	var mu sync.Mutex
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"duration":1},"results":{"channels":[{"alternatives":[{"transcript":"hi","words":[]}]}]}}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "nova-3")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "QUJD", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if sent != "QUJD" {
		t.Fatalf("sent %q, want the base64 string as given", sent)
	}
}

// TestR96provcStreamingAuthorizationTrimmed: TS reads the authorization
// header through new Headers(...).get(), which strips leading and trailing
// HTTP whitespace, so " Bearer tok\t" still gives the bearer subprotocols.
func TestR96provcStreamingAuthorizationTrimmed(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "nova-3")
	_, protocols, err := m.streamingHandshake(map[string]string{"Authorization": " Bearer tok\t"})
	if err != nil || len(protocols) != 2 || protocols[0] != "bearer" || protocols[1] != "tok" {
		t.Fatalf("protocols = %v, err = %v", protocols, err)
	}
}

// TestR96provcUndeclaredResponseFieldsIgnored: deepgramTranscriptionResponseSchema
// declares only metadata.duration and the transcript / words fields, so zod
// strips confidence, transaction_key and request_id whatever their type.
// Go decoded them into typed fields, so an unexpected type failed the call.
func TestR96provcUndeclaredResponseFieldsIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"duration":2,"transaction_key":7,"request_id":false},"results":{"channels":[{"alternatives":[{"transcript":"hi","confidence":"high","words":[{"word":"hi","start":0,"end":1,"confidence":null}]}]}]}}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "nova-3")
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil || res.Text != "hi" || len(res.Segments) != 1 || res.DurationInSeconds == nil || *res.DurationInSeconds != 2 {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
}
