package xai

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR97integStreamUsageRawKeepsFraction (provb r97 x this hook): the
// OpenAI-compatible stream now keeps a fractional count exactly in the wire
// usage (TS usage.raw), so the Vertex xAI usage hook truncates into its int
// fields with the z.number() rule instead of failing (finish usage was nil).
func TestR97integStreamUsageRawKeepsFraction(t *testing.T) {
	st, closeSt := newTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10.5,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"))
	})
	defer closeSt()
	s, err := st.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var usage *types.Usage
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			usage = c.Usage
		}
	}
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 10 || usage.Raw["prompt_tokens"] != 10.5 {
		t.Fatalf("usage = %+v", usage)
	}
}
