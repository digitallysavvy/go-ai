package deepseek

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

const r93provbDeepSeekLogprob = `{"token":"Hi","logprob":-0.1,"bytes":[72,105],"extra":1,"top_logprobs":[{"token":"Hi","logprob":-0.1,"extra":2}]}`

// TS deepseek-responses-api.ts createLogprobSchema: the zod objects keep
// token, logprob, bytes and top_logprobs ({token, logprob, bytes}), so the
// providerMetadata logprobs drop other wire keys (generate and stream).
func TestR93provbDeepSeekResponsesLogprobsSchemaKeys(t *testing.T) {
	const want = `[{"token":"Hi","logprob":-0.1,"bytes":[72,105],"top_logprobs":[{"token":"Hi","logprob":-0.1}]}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","item_id":"m1","delta":"Hi","logprobs":[`+r93provbDeepSeekLogprob+`]}`+"\n\n"+
				`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r1","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"Hi","logprobs":[`+r93provbDeepSeekLogprob+`]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("deepseek-v4")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	res, err := lm.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(res.ProviderMetadata["deepseek"].(map[string]interface{})["logprobs"])
	if string(got) != want {
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
		DeepSeek struct {
			Logprobs json.RawMessage `json:"logprobs"`
		} `json:"deepseek"`
	}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if string(meta.DeepSeek.Logprobs) != want {
		t.Errorf("stream logprobs = %s, want %s", meta.DeepSeek.Logprobs, want)
	}
}

func TestR93provbDeepSeekResponsesLogprobsNullish(t *testing.T) {
	for in, want := range map[string]string{
		`{"token":"a","logprob":-1,"bytes":null,"top_logprobs":null,"x":0}`: `{"token":"a","logprob":-1,"bytes":null,"top_logprobs":null}`,
		`{"token":"a","logprob":-1}`:                                        `{"token":"a","logprob":-1}`,
	} {
		if got := string(deepseekResponsesLogprobs([]json.RawMessage{json.RawMessage(in)})[0]); got != want {
			t.Errorf("deepseekResponsesLogprobs(%s) = %s, want %s", in, got, want)
		}
	}
}
