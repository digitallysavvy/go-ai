package assemblyai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r91provcServer answers upload, submit and poll; submitted records the
// submit body and polled is closed on the first poll.
func r91provcServer(t *testing.T, mu *sync.Mutex, submitted *string, polled chan struct{}) *httptest.Server {
	t.Helper()
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"upload_url":"https://cdn.example.com/a"}`)
	})
	mux.HandleFunc("/transcript", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		*submitted = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"tr1","status":"queued"}`)
	})
	mux.HandleFunc("/transcript/tr1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"tr1","status":"processing"}`)
		once.Do(func() { close(polled) })
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestR91provcAbortBetweenPolls (provc-08): an abort seen between polls is
// TS Error('Transcription request was aborted'), still matching the
// context error with errors.Is.
func TestR91provcAbortBetweenPolls(t *testing.T) {
	var mu sync.Mutex
	var submitted string
	polled := make(chan struct{})
	srv := r91provcServer(t, &mu, &submitted, polled)
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-polled
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err == nil || err.Error() != "Transcription request was aborted" {
		t.Fatalf("err = %v, want 'Transcription request was aborted'", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Error("errors.Is(err, context.Canceled) = false")
	}
}

// TestR91provcSubmitBodyOrder (provc-07): the submit body keeps the TS
// getArgs assignment order, nested objects included, then audio_url.
func TestR91provcSubmitBodyOrder(t *testing.T) {
	var mu sync.Mutex
	var submitted string
	polled := make(chan struct{})
	srv := r91provcServer(t, &mu, &submitted, polled)
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "universal-3-5-pro")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-polled
		cancel()
	}()
	_, _ = m.DoTranscribe(ctx, &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"assemblyai": map[string]interface{}{
			"speakerLabels": true, "punctuate": false, "autoChapters": true,
			"languageDetectionOptions": map[string]interface{}{"fallbackLanguage": "en", "codeSwitching": true},
		}},
	})
	mu.Lock()
	defer mu.Unlock()
	if want := `{"speech_models":["universal-3-5-pro"],"auto_chapters":true,"punctuate":false,"speaker_labels":true,"language_detection_options":{"fallback_language":"en","code_switching":true},"audio_url":"https://cdn.example.com/a"}`; submitted != want {
		t.Errorf("body = %s\nwant %s", submitted, want)
	}
}
