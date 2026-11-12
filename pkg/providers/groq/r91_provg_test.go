package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg2-01: a System-only prompt must not panic; TS
// convert-to-groq-chat-messages.ts:33 sends [{role:system}].
func TestR91provgSystemOnlyPrompt(t *testing.T) {
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("llama-3.3-70b-versatile")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{System: "be brief"}}); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(got, &body)
	if string(body.Messages) != `[{"content":"be brief","role":"system"}]` {
		t.Fatalf("messages = %s", body.Messages)
	}
}

// provg2-05: a missing API key is the LoadAPIKeyError itself.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	tm, _ := New(Config{}).TranscriptionModel("whisper-large-v3")
	_, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}})
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("error = %T %v", err, err)
	}
}

// provg2-07: TS getArgs parses providerOptions.groq before the audio URL.
func TestR91provgTranscriptionValidatesOptionsFirst(t *testing.T) {
	tm, _ := New(Config{APIKey: "k"}).TranscriptionModel("whisper-large-v3")
	urlModel := tm.(*TranscriptionModel)
	_, err := urlModel.DoTranscribeFromURL(context.Background(), &provider.TranscriptionURLOptions{
		AudioURL:        "ftp://x",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"temperature": 5}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid groq provider options") {
		t.Fatalf("error = %v", err)
	}
}

// Typed-option ruling probe: a Go-typed groq transcription namespace.
func TestR91provgTranscriptionTypedOptions(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hi","x_groq":{"id":"x"}}`)
	}))
	defer server.Close()
	tm, _ := New(Config{APIKey: "k", BaseURL: server.URL}).TranscriptionModel("whisper-large-v3")
	if _, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, ProviderOptions: map[string]interface{}{"groq": map[string]string{"language": "de"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "name=\"language\"\r\n\r\nde") {
		t.Fatalf("multipart body = %q", body)
	}
}
