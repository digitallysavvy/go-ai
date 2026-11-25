package mistral

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r80provaMistralModel(t *testing.T, contentType, body string) provider.LanguageModel {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("mistral-small-latest")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func r80provaMistralPrompt() *provider.GenerateOptions {
	return &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
}

// TS doGenerate reads response.choices[0].message without a guard: an empty
// choices array passes the schema and then throws a TypeError.
func TestR80provaMistralGenerateEmptyChoicesErrors(t *testing.T) {
	m := r80provaMistralModel(t, "application/json", `{"id":"x","object":"chat.completion","model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)
	res, err := m.DoGenerate(context.Background(), r80provaMistralPrompt())
	if err == nil {
		t.Fatalf("want error, got result %+v", res)
	}
	if got, want := err.Error(), "Cannot read properties of undefined (reading 'message')"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TS doStream reads value.choices[0].delta without a guard: an empty choices
// array errors the whole stream after the response-metadata part, with no
// error part and no finish part.
func TestR80provaMistralStreamEmptyChoicesErrors(t *testing.T) {
	body := "data: {\"id\":\"a\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"he\"}}]}\n\n" +
		"data: {\"choices\":[]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	m := r80provaMistralModel(t, "text/event-stream", body)
	s, err := m.DoStream(context.Background(), r80provaMistralPrompt())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var kinds []provider.ChunkType
	var streamErr error
	for {
		c, err := s.Next()
		if err != nil {
			streamErr = err
			break
		}
		kinds = append(kinds, c.Type)
	}
	if streamErr == nil || streamErr.Error() != "Cannot read properties of undefined (reading 'delta')" {
		t.Fatalf("stream error = %v", streamErr)
	}
	if s.Err() == nil {
		t.Fatal("Err() = nil after a TypeError")
	}
	for _, k := range kinds {
		if k == provider.ChunkTypeFinish || k == provider.ChunkTypeError {
			t.Fatalf("unexpected %s part before the stream error: %v", k, kinds)
		}
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != provider.ChunkTypeText {
		t.Fatalf("parts = %v, want the first chunk's parts then the error", kinds)
	}
}
