package mistral

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r91provgCapture serves a fixed body and records the request body.
func r91provgCapture(t *testing.T, contentType, resp string) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

// provg2-01: a System-only prompt must not panic; TS
// convert-to-mistral-chat-messages.ts:54 sends [{role:system}].
func TestR91provgSystemOnlyPrompt(t *testing.T) {
	server, got := r91provgCapture(t, "application/json", `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("mistral-small-latest")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{System: "be brief"}}); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(*got, &body)
	if string(body.Messages) != `[{"content":"be brief","role":"system"}]` {
		t.Fatalf("messages = %s", body.Messages)
	}
}

// provg2-05: a missing API key is the LoadAPIKeyError itself (TS
// mistral-provider.ts:144 loadApiKey), not a wrapping ProviderError.
func TestR91provgMissingAPIKeyIsLoadAPIKeyError(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	p := New(Config{})
	em, _ := p.EmbeddingModel("mistral-embed")
	_, err := em.DoEmbed(context.Background(), "hi", nil)
	r91provgWantLoadKeyError(t, "embedding", err)
	sm, _ := p.SpeechModel("voxtral-mini-tts-2603")
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	r91provgWantLoadKeyError(t, "speech", err)
	tm, _ := p.TranscriptionModel("voxtral-mini-latest")
	_, err = tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	r91provgWantLoadKeyError(t, "transcription", err)
}

func r91provgWantLoadKeyError(t *testing.T, what string, err error) {
	t.Helper()
	if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
		t.Fatalf("%s: error = %T %v", what, err, err)
	}
}

// provg2-10: the speech body goes out as JSON.stringify of the TS literal
// (model, input, voice_id, ..., response_format, stream).
func TestR91provgSpeechBodyKeyOrder(t *testing.T) {
	server, got := r91provgCapture(t, "application/json", `{"audio_data":"AAEC"}`)
	sm, _ := New(Config{APIKey: "k", BaseURL: server.URL}).SpeechModel("voxtral-mini-tts-2603")
	if _, err := sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v1"}); err != nil {
		t.Fatal(err)
	}
	if want := `{"model":"voxtral-mini-tts-2603","input":"hi","voice_id":"v1","response_format":"mp3","stream":false}`; string(*got) != want {
		t.Fatalf("body = %s, want %s", *got, want)
	}
}

// provg2-08: created_at is read with Date.parse semantics; a zone-less
// date-time is local time (mistral-conversation-language-model.ts:399).
func TestR91provgConversationCreatedAtJSDate(t *testing.T) {
	server, _ := r91provgCapture(t, "text/event-stream", "data: {\"type\":\"conversation.response.started\",\"conversation_id\":\"c\",\"created_at\":\"2026-01-01T00:00:00\"}\n\n")
	m := New(Config{APIKey: "k", BaseURL: server.URL}).ConversationModel("mistral-medium-latest")
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	for {
		c, err := stream.Next()
		if err != nil {
			t.Fatal("no response-metadata")
		}
		if c.Type == provider.ChunkTypeResponseMetadata {
			if !c.ResponseMetadata.Timestamp.Equal(want) {
				t.Fatalf("timestamp = %v, want %v", c.ResponseMetadata.Timestamp, want)
			}
			return
		}
	}
}

// provg2-09: an object function.call arguments is JSON.stringify of the
// parsed value (mistral-conversation-language-model.ts:288).
func TestR91provgConversationObjectArgumentsStringify(t *testing.T) {
	server, _ := r91provgCapture(t, "application/json", `{"conversation_id":"c","outputs":[{"type":"function.call","tool_call_id":"t","name":"f","arguments":{"b":1.0,"2":"é","c":1e2}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	m := New(Config{APIKey: "k", BaseURL: server.URL}).ConversationModel("mistral-medium-latest")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Content {
		if tc, ok := c.(types.ToolCallContent); ok {
			if want := `{"2":"é","b":1,"c":100}`; tc.Input != want {
				t.Fatalf("input = %s, want %s", tc.Input, want)
			}
			return
		}
	}
	t.Fatalf("no tool call in %#v", res.Content)
}
