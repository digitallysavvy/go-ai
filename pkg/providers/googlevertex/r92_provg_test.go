package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS google-vertex-transcription-model.ts and
// google-vertex-gemini-transcription-model.ts read currentDate before the
// request: response.timestamp is not the time the response arrived.
func TestR92provgTranscriptionTimestampBeforeRequest(t *testing.T) {
	var served time.Time
	chirp, err := New(Config{
		Project: "p", Location: "us-central1", AccessToken: "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			served = time.Now()
			time.Sleep(20 * time.Millisecond)
			return jsonResponse(200, `{"results":[{"alternatives":[{"transcript":"hi"}]}]}`), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := chirp.TranscriptionModel("chirp_2")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQID", MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Response.Timestamp.Before(served) {
		t.Fatalf("chirp timestamp %v is not before the request %v", res.Response.Timestamp, served)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = time.Now()
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`))
	}))
	defer srv.Close()
	gm, err := newVertexTestProvider(t, srv.URL).TranscriptionModel(ModelGemini35Transcribe)
	if err != nil {
		t.Fatal(err)
	}
	res, err = gm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQID", MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Response.Timestamp.Before(served) {
		t.Fatalf("gemini timestamp %v is not before the request %v", res.Response.Timestamp, served)
	}
}

// TS google-vertex-gemini-transcription-model.ts posts to
// `${baseURL}/models/${modelId}:generateContent` (no getModelPath), so a
// model id with a slash keeps the models/ prefix.
func TestR92provgGeminiTranscriptionPath(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`))
	}))
	defer srv.Close()
	m := NewGeminiTranscriptionModel(newVertexTestProvider(t, srv.URL), "tuned/gemini-3.5-transcribe")
	if _, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQID", MimeType: "audio/wav"}); err != nil {
		t.Fatal(err)
	}
	if path != "/models/tuned/gemini-3.5-transcribe:generateContent" {
		t.Fatalf("path = %s", path)
	}
}
