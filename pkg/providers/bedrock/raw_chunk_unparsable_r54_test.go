package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS safeParseJSON leaves rawValue undefined for an event-stream payload that
// is not valid JSON, so the raw chunk carries nil, not the payload text.
func TestR54IntegratorConverseStreamRawChunkForUnparsablePayloadIsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", `not json`},
			{"messageStop", `{"stopReason":"end_turn"}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: true})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var first *provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if first == nil && c.Type == provider.ChunkTypeRaw {
			first = c
		}
	}
	if first == nil || first.Raw != nil {
		t.Fatalf("first raw chunk = %#v, want one with a nil value", first)
	}
}
