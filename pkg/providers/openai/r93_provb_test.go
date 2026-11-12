package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The Completions API logprobs object as OpenAI returns it, text_offset
// included.
const r93provbCompletionLogprobs = `{"tokens":[" ever"],"token_logprobs":[-0.06],"top_logprobs":[{" ever":-0.06}],"text_offset":[5]}`

// TS openaiCompletionResponseSchema / openaiCompletionChunkSchema: the
// logprobs zod object keeps tokens, token_logprobs and top_logprobs, so
// providerMetadata.openai.logprobs has no text_offset.
func TestR93provbCompletionLogprobsSchemaKeys(t *testing.T) {
	const want = `{"tokens":[" ever"],"token_logprobs":[-0.06],"top_logprobs":[{" ever":-0.06}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"id":"c1","created":1711363440,"model":"m","choices":[{"text":"Hi","index":0,"logprobs":`+r93provbCompletionLogprobs+`,"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","created":1711363440,"model":"m","choices":[{"text":"Hi","index":0,"logprobs":`+r93provbCompletionLogprobs+`,"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": 1}},
	}

	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(res.ProviderMetadata["openai"].(map[string]interface{})["logprobs"])
	if string(got) != want {
		t.Errorf("generate logprobs = %s, want %s", got, want)
	}

	s, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish part")
	}
	var meta struct {
		OpenAI struct {
			Logprobs json.RawMessage `json:"logprobs"`
		} `json:"openai"`
	}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if string(meta.OpenAI.Logprobs) != want {
		t.Errorf("stream logprobs = %s, want %s", meta.OpenAI.Logprobs, want)
	}
}

func TestR93provbCompletionLogprobsNullTopLogprobs(t *testing.T) {
	for in, want := range map[string]string{
		`{"tokens":["a"],"token_logprobs":[-1],"top_logprobs":null,"text_offset":[0]}`: `{"tokens":["a"],"token_logprobs":[-1],"top_logprobs":null}`,
		`{"tokens":["a"],"token_logprobs":[-1]}`:                                       `{"tokens":["a"],"token_logprobs":[-1]}`,
	} {
		if got := string(completionLogprobs(json.RawMessage(in))); got != want {
			t.Errorf("completionLogprobs(%s) = %s, want %s", in, got, want)
		}
	}
}
