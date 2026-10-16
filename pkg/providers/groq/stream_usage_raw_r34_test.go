package groq

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// usage.raw of a streamed finish follows the TS zod usage schema: unknown keys
// are stripped, absent keys stay absent and nested details keep only the
// declared members.
func TestGroqStreamUsageRawFollowsSchema(t *testing.T) {
	sse := `data: {"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"x_groq":{"usage":{"prompt_tokens":3,"completion_tokens":2,"queue_time":0.1,"prompt_tokens_details":{"cached_tokens":1,"audio_tokens":9},"completion_tokens_details":null}}}

data: [DONE]

`
	stream := newGroqStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			t.Fatal("no finish chunk")
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Type != provider.ChunkTypeFinish {
			continue
		}
		want := map[string]interface{}{
			"prompt_tokens":             float64(3),
			"completion_tokens":         float64(2),
			"prompt_tokens_details":     map[string]interface{}{"cached_tokens": float64(1)},
			"completion_tokens_details": nil,
		}
		if !reflect.DeepEqual(chunk.Usage.Raw, want) {
			t.Fatalf("usage.raw = %#v, want %#v", chunk.Usage.Raw, want)
		}
		return
	}
}
