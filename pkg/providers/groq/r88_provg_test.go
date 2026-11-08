package groq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

type r88provgGroqServer struct {
	mu     sync.Mutex
	calls  int
	fields map[string][]string
	files  []string
}

func r88provgGroqStart(t *testing.T, status int, contentType, body string) (*r88provgGroqServer, provider.TranscriptionModel) {
	t.Helper()
	rec := &r88provgGroqServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.calls++
		rec.fields = map[string][]string{}
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				rec.files = append(rec.files, p.FormName())
				continue
			}
			rec.fields[p.FormName()] = append(rec.fields[p.FormName()], string(b))
		}
		rec.mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("whisper-large-v3-turbo")
	if err != nil {
		t.Fatal(err)
	}
	return rec, m
}

const r88provgGroqVerbose = `{"task":"transcribe","language":"English","duration":2.5,"text":"Hello world!","segments":[],"x_groq":{"id":"req_1"}}`

// Ports the groq-transcription-model.test.ts native URL tests (TS
// 36802d48af).
func TestR88provgGroqTranscribeFromURL(t *testing.T) {
	rec, m := r88provgGroqStart(t, 200, "application/json", r88provgGroqVerbose)
	um := m.(provider.TranscriptionURLModel)
	res, err := um.DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{
		AudioURL: "https://storage.example.com/large-audio.mp3",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{
			"responseFormat": "verbose_json", "language": "en", "prompt": "Galileo", "temperature": 0.0,
			"timestampGranularities": []interface{}{"word", "segment"},
		}},
	})
	if err != nil || res == nil {
		t.Fatalf("res %+v err %v", res, err)
	}
	if res.Text != "Hello world!" || res.Language != "English" || res.DurationInSeconds == nil || *res.DurationInSeconds != 2.5 {
		t.Fatalf("result = %+v", res)
	}
	rec.mu.Lock()
	if rec.calls != 1 || len(rec.files) != 0 || rec.fields["url"][0] != "https://storage.example.com/large-audio.mp3" ||
		rec.fields["model"][0] != "whisper-large-v3-turbo" || rec.fields["temperature"][0] != "0" ||
		strings.Join(rec.fields["timestamp_granularities[]"], ",") != "word,segment" {
		t.Fatalf("calls %d files %v fields %v", rec.calls, rec.files, rec.fields)
	}
	rec.mu.Unlock()

	// The fragment is not sent.
	rec, m = r88provgGroqStart(t, 200, "application/json", r88provgGroqVerbose)
	if _, err := m.(provider.TranscriptionURLModel).DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{AudioURL: "https://example.com/audio.mp3#t=0"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.fields["url"][0]; got != "https://example.com/audio.mp3" {
		t.Fatalf("url = %q", got)
	}

	// Unsupported URL forms fail before any request.
	for _, u := range []string{"ftp://example.com/audio.mp3", "https://user:secret@example.com/audio.mp3"} {
		rec, m = r88provgGroqStart(t, 200, "application/json", r88provgGroqVerbose)
		_, err := m.(provider.TranscriptionURLModel).DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{AudioURL: u})
		var ae *providererrors.InvalidArgumentError
		if !errors.As(err, &ae) || !strings.Contains(err.Error(), "Groq URL transcription requires an HTTP(S) URL without userinfo.") || rec.calls != 0 {
			t.Fatalf("%s: err %v calls %d", u, err, rec.calls)
		}
	}

	// A plain-text response for URL input.
	_, m = r88provgGroqStart(t, 200, "text/plain", "Hello world!")
	res, err = m.(provider.TranscriptionURLModel).DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{
		AudioURL: "https://example.com/audio.mp3", ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"responseFormat": "text"}},
	})
	if err != nil || res.Text != "Hello world!" || res.Response.Body != "Hello world!" {
		t.Fatalf("text: %+v %v", res, err)
	}
}

// A redirect Groq cannot follow declines (nil, nil); other errors are kept.
func TestR88provgGroqTranscribeFromURLDecline(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		body := fmt.Sprintf(`{"error":{"message":"failed to retrieve media: received status code: %d","type":"invalid_request_error"}}`, status)
		_, m := r88provgGroqStart(t, 400, "application/json", body)
		res, err := m.(provider.TranscriptionURLModel).DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{AudioURL: "https://example.com/redirect"})
		if res != nil || err != nil {
			t.Fatalf("%d: res %+v err %v, want decline", status, res, err)
		}
	}
	for _, tc := range []struct {
		status  int
		message string
	}{
		{400, "failed to retrieve media: received status code: 404"},
		{400, "invalid audio format"},
		{401, "invalid API key"},
		{429, "rate limit exceeded"},
	} {
		body := fmt.Sprintf(`{"error":{"message":%q,"type":"invalid_request_error"}}`, tc.message)
		_, m := r88provgGroqStart(t, tc.status, "application/json", body)
		_, err := m.(provider.TranscriptionURLModel).DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{AudioURL: "https://example.com/audio.mp3"})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.StatusCode != tc.status || perr.Message != tc.message {
			t.Fatalf("%v: err %v", tc, err)
		}
	}
}
