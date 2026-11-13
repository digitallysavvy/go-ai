package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97integStillInProgressKeepsPrototypeKey: TS reads the failed poll's
// body with plain JSON.parse (fal-transcription-model.ts:167 clone.json(),
// fal-video-model.ts:289 response.clone().json()), so a "still in progress"
// detail next to a __proto__ key keeps the request pending.
func TestR97integStillInProgressKeepsPrototypeKey(t *testing.T) {
	const pending = `{"__proto__":{"a":1},"detail":"Request is still in progress"}`
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fal-ai/whisper" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
		case strings.HasPrefix(r.URL.Path, "/fal-ai/whisper/requests/"):
			if polls.Add(1) == 1 {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(pending))
				return
			}
			_, _ = w.Write([]byte(`{"text":"hello"}`))
		case r.URL.Path == "/pending":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(pending))
		}
	}))
	defer srv.Close()
	orig := falTranscriptionPollInterval
	falTranscriptionPollInterval = 5 * time.Millisecond
	defer func() { falTranscriptionPollInterval = orig }()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	p.queueHost = srv.URL
	tm, _ := p.TranscriptionModel("whisper")
	if res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"}); err != nil || res.Text != "hello" {
		t.Fatalf("transcription: %v %+v", err, res)
	}
	op, _ := json.Marshal(falVideoOperation{ResponseURL: srv.URL + "/pending", SubmitURL: srv.URL + "/fal-ai/x"})
	res, err := NewVideoModel(p, "luma-dream-machine").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("video: %v %+v", err, res)
	}
}
