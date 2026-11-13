package moonshot

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

// A Moonshot logprobs object with keys outside moonshotAIChatLogprobsSchema.
const r93provbMoonshotLogprobs = `{"content":[{"token":"Hi","logprob":-0.1,"bytes":[72,105],"extra":1,"top_logprobs":[{"token":"Hi","logprob":-0.1,"bytes":null,"extra":2}]}],"refusal":null}`

// TS moonshotAIChatLogprobsSchema: the zod objects keep content and, per
// entry, token / logprob / bytes / top_logprobs, so providerMetadata
// logprobs drop the other wire keys (generate and stream).
func TestR93provbMoonshotLogprobsSchemaKeys(t *testing.T) {
	const wantEntry = `{"token":"Hi","logprob":-0.1,"bytes":[72,105],"top_logprobs":[{"token":"Hi","logprob":-0.1,"bytes":null}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"kimi-k2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"logprobs":`+r93provbMoonshotLogprobs+`,"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"kimi-k2","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"logprobs":`+r93provbMoonshotLogprobs+`,"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("kimi-k2")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	res, err := lm.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(res.ProviderMetadata["moonshotai"].(map[string]json.RawMessage)["logprobs"])
	if want := `{"content":[` + wantEntry + `]}`; string(got) != want {
		t.Errorf("generate logprobs = %s, want %s", got, want)
	}

	s, err := lm.DoStream(context.Background(), opts)
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
		Moonshot struct {
			Logprobs json.RawMessage `json:"logprobs"`
		} `json:"moonshotai"`
	}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if want := `{"content":[` + wantEntry + `]}`; string(meta.Moonshot.Logprobs) != want {
		t.Errorf("stream logprobs = %s, want %s", meta.Moonshot.Logprobs, want)
	}
}

func TestR93provbMoonshotLogprobsNullAndAbsentContent(t *testing.T) {
	for in, want := range map[string]string{
		`{"content":null,"refusal":null}`: `{"content":null}`,
		`{"refusal":null}`:                `{}`,
	} {
		if got := string(moonshotLogprobsObject(json.RawMessage(in))); got != want {
			t.Errorf("moonshotLogprobsObject(%s) = %s, want %s", in, got, want)
		}
	}
}
