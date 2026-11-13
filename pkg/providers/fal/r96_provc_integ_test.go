package fal

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provcTranscriptionRawBodyOutOfRangeNumber: the raw response body
// keeps a number beyond the double range as +Inf (JSON.parse), instead of
// dropping the whole body.
func TestR96provcTranscriptionRawBodyOutOfRangeNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"text":"hi","extra":1e400}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k"})
	p.queueHost = strings.TrimRight(srv.URL, "/")
	tm, err := p.TranscriptionModel("whisper")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil || res.Text != "hi" {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	body, _ := res.Response.Body.(map[string]interface{})
	if f, ok := body["extra"].(float64); !ok || !math.IsInf(f, 1) {
		t.Fatalf("raw body = %#v, want extra +Inf", res.Response.Body)
	}
}
