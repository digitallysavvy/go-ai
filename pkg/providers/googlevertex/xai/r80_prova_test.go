package xai

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertGoogleVertexXaiUsage reads z.number() counts, so a fractional
// count parses; Go keeps the usage (integer part) in generate and stream.
func TestR80provaVertexXaiFractionalUsage(t *testing.T) {
	const usage = `{"prompt_tokens":10.5,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":2.5},"completion_tokens_details":{"reasoning_tokens":1}}`
	check := func(name string, u *types.Usage) {
		t.Helper()
		if u == nil || u.InputTokens == nil || *u.InputTokens != 10 || u.OutputTokens == nil || *u.OutputTokens != 5 ||
			u.InputDetails == nil || u.InputDetails.CacheReadTokens == nil || *u.InputDetails.CacheReadTokens != 2 {
			t.Fatalf("%s usage = %+v", name, u)
		}
	}

	gen, closeGen := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],"usage":` + usage + `}`))
	})
	defer closeGen()
	res, err := gen.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	check("generate", &res.Usage)

	st, closeSt := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[],\"usage\":" + usage + "}\n\ndata: [DONE]\n\n"))
	})
	defer closeSt()
	s, err := st.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
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
	check("stream", finish.Usage)
}
