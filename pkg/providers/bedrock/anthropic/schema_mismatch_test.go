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

// A frame of valid JSON with the wrong shape is an error part (TS
// anthropicChunkSchema validation) and the stream keeps going.
func TestStreamSchemaMismatchIsErrorPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		for _, ev := range []string{
			`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":5}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = w.Write(bedrockChunk(t, ev))
		}
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "t", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text, errs := "", 0
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeError:
			errs++
		}
	}
	if text != "Hello" || errs != 1 {
		t.Fatalf("text=%q errors=%d", text, errs)
	}
}
