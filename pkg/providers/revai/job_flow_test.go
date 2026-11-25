package revai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS polls the job only while it is not "transcribed", reports the
// submission response's language, and words its failures as AISDKErrors.
func TestJobFlowMatchesTS(t *testing.T) {
	transcript := `{"monologues":[{"elements":[{"type":"text","value":"hi","ts":0,"end_ts":1}]}]}`
	var paths []string
	serve := func(submit string, poll string) *httptest.Server {
		paths = nil
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.Method+" "+r.URL.Path)
			switch {
			case r.Method == http.MethodPost:
				_, _ = w.Write([]byte(submit))
			case strings.HasSuffix(r.URL.Path, "/transcript"):
				_, _ = w.Write([]byte(transcript))
			default:
				_, _ = w.Write([]byte(poll))
			}
		}))
	}
	call := func(srv *httptest.Server) (string, error) {
		m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("machine")
		res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
		if err != nil {
			return "", err
		}
		return res.Language, nil
	}

	srv := serve(`{"id":"j1","status":"transcribed","language":"en"}`, `{"id":"j1","status":"transcribed","language":"fr"}`)
	lang, err := call(srv)
	srv.Close()
	if err != nil || lang != "en" {
		t.Fatalf("transcribed submission: lang=%q err=%v", lang, err)
	}
	for _, p := range paths {
		if p == "GET /speechtotext/v1/jobs/j1" {
			t.Fatalf("polled a job that was already transcribed: %v", paths)
		}
	}

	srv = serve(`{"id":"j1","status":"failed"}`, `{}`)
	_, err = call(srv)
	srv.Close()
	if err == nil || !strings.Contains(err.Error(), "Failed to submit transcription job to Rev.ai") {
		t.Fatalf("failed submission: %v", err)
	}

	srv = serve(`{"id":"j1","status":"in_progress"}`, `{"id":"j1","status":"failed"}`)
	_, err = call(srv)
	srv.Close()
	if err == nil || !strings.Contains(err.Error(), "Transcription job failed") {
		t.Fatalf("failed poll: %v", err)
	}

	srv = serve(`{"id":"j1","status":"in_progress"}`, `{"id":2}`)
	_, err = call(srv)
	srv.Close()
	if !providerutils.IsInvalidJSONResponse(err) {
		t.Fatalf("invalid poll body: %v", err)
	}
}
