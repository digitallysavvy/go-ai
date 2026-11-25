package revai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS getFromApi handlers read the body as text (TextDecoder drops a UTF-8
// BOM): a BOM-prefixed job status parses, and a BOM-prefixed failed poll
// gives the Rev.ai error message.
func TestR94provcPollBodyIsText(t *testing.T) {
	oldTimeout, oldInterval := pollTimeout, pollInterval
	pollTimeout, pollInterval = time.Second, time.Millisecond
	t.Cleanup(func() { pollTimeout, pollInterval = oldTimeout, oldInterval })
	for _, failPoll := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost:
				_, _ = w.Write([]byte(`{"id":"j1","status":"in_progress"}`))
			case r.URL.Path == "/speechtotext/v1/jobs/j1" && failPoll:
				w.WriteHeader(500)
				_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"error":{"message":"bad thing","code":5}}`))
			case r.URL.Path == "/speechtotext/v1/jobs/j1":
				_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"id":"j1","status":"transcribed"}`))
			default:
				_, _ = w.Write([]byte(`{"monologues":[{"elements":[{"type":"text","value":"hi","ts":0,"end_ts":1}]}]}`))
			}
		}))
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("machine")
		res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
		srv.Close()
		if failPoll {
			var perr *providererrors.ProviderError
			if !errors.As(err, &perr) || perr.Message != "bad thing" {
				t.Errorf("failed poll: err = %v, want the Rev.ai message", err)
			}
		} else if err != nil || res.Text != "hi" {
			t.Errorf("poll: err = %v, res = %+v", err, res)
		}
	}
}
