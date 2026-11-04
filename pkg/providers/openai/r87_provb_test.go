package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS add01abd2c: the OpenAI-compatible chat model writes verbosity after the
// passthrough only when textVerbosity is set, so a raw verbosity survives.
func TestR87provbCompatRawVerbosityPassthrough(t *testing.T) {
	for _, tc := range []struct {
		opts map[string]interface{}
		want interface{}
	}{
		{map[string]interface{}{"verbosity": "low"}, "low"},
		{map[string]interface{}{"verbosity": "low", "textVerbosity": "high"}, "high"},
		{map[string]interface{}{}, nil},
	} {
		var body map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider", ChatProviderName: "test-provider.chat"})
		m, _ := p.ChatModel("gpt-5")
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          chatTestPrompt(),
			ProviderOptions: map[string]interface{}{"test-provider": tc.opts},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if body["verbosity"] != tc.want {
			t.Errorf("%v: verbosity = %v, want %v", tc.opts, body["verbosity"], tc.want)
		}
	}
}

// TS 934100cad4 (openai-chat-language-model.test.ts "reports the custom HTTP
// request URL for %s"): the chat model reports the URL it posted to. The
// stream reports it through RequestURL(); DoGenerate reports the same
// chatRequestURL() once types.GenerateResult.RequestURL lands (gen).
func TestR87provbChatReportsRequestURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "test-only", BaseURL: srv.URL + "/private/v1"})
	m := NewLanguageModel(p, "test-model")
	want := srv.URL + "/private/v1/chat/completions"
	if got := m.chatRequestURL(); got != want {
		t.Fatalf("chatRequestURL = %q, want %q", got, want)
	}
	st, _, err := m.openChatStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if st.RequestURL() != want || srv.URL+gotPath != want {
		t.Fatalf("stream RequestURL = %q, posted %q, want %q", st.RequestURL(), srv.URL+gotPath, want)
	}

	// Azure-style chat query: the reported URL carries it as posted.
	q := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/openai/v1", ChatQuery: map[string]string{"api-version": "v1"}}), "d")
	st, _, err = q.openChatStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if want := srv.URL + "/openai/v1/chat/completions?api-version=v1"; st.RequestURL() != want || srv.URL+gotPath != want {
		t.Fatalf("azure RequestURL = %q, posted %q, want %q", st.RequestURL(), srv.URL+gotPath, want)
	}

	// TS OpenAICompatibleChatLanguageModel reports no URL.
	c := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "x"}), "m")
	if got := c.chatRequestURL(); got != "" {
		t.Fatalf("compat chatRequestURL = %q, want empty", got)
	}
}
