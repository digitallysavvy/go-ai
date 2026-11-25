package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const rawChunksSSE = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":3}}}\n\n" +
	"event: ping\ndata: {\"type\":\"ping\"}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func collectStream(t *testing.T, includeRaw bool) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, rawChunksSSE)
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model, _ := p.LanguageModel(ClaudeSonnet4_5)
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "x"}, IncludeRawChunks: includeRaw,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

// TS emits {type:'raw', rawValue} for every parsed chunk (pings included)
// ahead of the parts the chunk produces, only when includeRawChunks is set.
func TestStreamIncludeRawChunks(t *testing.T) {
	without := collectStream(t, false)
	with := collectStream(t, true)

	raws := 0
	var rest []*provider.StreamChunk
	for _, c := range with {
		if c.Type == provider.ChunkTypeRaw {
			raws++
			m, ok := c.Raw.(map[string]interface{})
			if !ok || m["type"] == nil {
				t.Fatalf("raw value = %#v, want the parsed event object", c.Raw)
			}
			continue
		}
		rest = append(rest, c)
	}
	if raws != 7 {
		t.Fatalf("raw chunks = %d, want 7 (one per SSE event)", raws)
	}
	for _, c := range without {
		if c.Type == provider.ChunkTypeRaw {
			t.Fatal("raw chunk emitted without includeRawChunks")
		}
	}
	if len(rest) != len(without) {
		t.Fatalf("non-raw chunks differ: %d vs %d", len(rest), len(without))
	}
}
