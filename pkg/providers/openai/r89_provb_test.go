package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-responses-language-model.ts 872-895: the APICallError of a
// response with an error object (not retryable) or without output carries
// responseHeaders and responseBody.
func TestR89provbResponsesEmbeddedErrorKeepsResponse(t *testing.T) {
	for name, body := range map[string]string{
		"error":     `{"id":"r","error":{"message":"boom","code":"server_error","type":"server_error"}}`,
		"no output": `{"id":"r"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-Id", "req_1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-5")
		if err != nil {
			t.Fatal(err)
		}
		_, err = lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.ResponseHeaders["x-request-id"] != "req_1" || pe.ResponseBody != body || pe.IsRetryable() {
			t.Errorf("%s: err = %#v, want response headers and body, not retryable", name, err)
		}
	}
}

// TS openaiFailedResponseHandler for the decision model: headers, body and
// data are kept; an empty message is a message; a non-matching body gives
// the status text.
func TestR89provbDecisionModelAPIError(t *testing.T) {
	m := NewDecisionModel(New(Config{APIKey: "k"}), "gpt-5")
	for body, want := range map[string]string{
		`{"error":{"message":"slow","code":"rate"}}`: "slow",
		`{"error":{"message":""}}`:                   "",
		`<html/>`:                                    "Too Many Requests",
	} {
		cause := &internalhttp.HTTPStatusError{StatusCode: 429, Headers: http.Header{"Retry-After": {"1"}}, Body: []byte(body)}
		var pe *providererrors.ProviderError
		if err := m.handleError(cause); !errors.As(err, &pe) || pe.StatusCode != 429 || pe.Message != want || pe.ResponseHeaders["Retry-After"] != "1" || pe.ResponseBody != body {
			t.Errorf("%s: err = %#v", body, err)
		}
	}
}

// TS openaiFailedResponseHandler: an empty error.message is the message, and
// a body outside openaiErrorDataSchema gives the status text.
func TestR89provbChatErrorMessageFollowsSchema(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":""}}`:            "",
		`{"message":"not the schema"}`:        "Bad Request",
		`{"error":"string error"}`:            "Bad Request",
		`{"error":{"message":"m","code":42}}`: "m",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(body))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		responses, _ := p.LanguageModel("gpt-4o")
		chat, _ := p.ChatModel("gpt-4o")
		for name, lm := range map[string]provider.LanguageModel{"responses": responses, "chat": chat} {
			_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != want || pe.StatusCode != 400 || pe.ResponseHeaders["retry-after"] != "1" || pe.ResponseBody != body {
				t.Errorf("%s %s: err = %#v, want message %q", name, body, err, want)
			}
		}
		srv.Close()
	}
}

// OpenAI-compatible wrappers keep their own error structures (TS
// errorStructure, e.g. Baseten's string error), so the strict OpenAI schema
// applies to the native models only.
func TestR89provbCompatModeKeepsWrapperErrorShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"please check the model you provided"}`))
	}))
	defer srv.Close()
	chat, _ := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true}).ChatModel("m")
	_, err := chat.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "please check the model you provided" {
		t.Fatalf("err = %#v", err)
	}
}
