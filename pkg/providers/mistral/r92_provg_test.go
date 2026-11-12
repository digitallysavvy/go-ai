package mistral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS loadApiKey throws from the headers before the fetch: a missing key is
// the LoadAPIKeyError itself, not a status-0 ProviderError
// (cr-r92-provb-to-provg-tools).
func TestR92provgRealtimeMissingKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	r := New(Config{BaseURL: "http://127.0.0.1:1"}).ExperimentalRealtime()
	_, err := r.CreateSession(context.Background(), RealtimeCreateSessionOptions{Model: "voxtral-mini-realtime"})
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("CreateSession err = %T %v", err, err)
	}
	if err := r.CloseSession(context.Background(), "s1"); err == nil {
		t.Fatal("CloseSession succeeded")
	} else if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("CloseSession err = %T %v", err, err)
	}
}

// TS mistral-transcription-model.ts filters provider segments with
// `type != null || score != null || speaker_id != null`: a present empty
// speaker id counts.
func TestR92provgTranscriptionSegmentPresence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"voxtral","text":"hi","segments":[{"text":"hi","start":0,"end":1,"speaker_id":""},{"text":"x","start":1,"end":2}]}`))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("voxtral-mini-latest")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.ProviderMetadata)
	if want := `{"mistral":{"segments":[{"endSecond":1,"speakerId":"","startSecond":0,"text":"hi"}]}}`; string(b) != want {
		t.Fatalf("metadata = %s", b)
	}
}
