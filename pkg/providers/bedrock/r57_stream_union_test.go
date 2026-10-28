package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r57: the TS delta union picks the first member that parses. A toolUse
// whose input is not a string is not the toolUse member, so a
// reasoningContent beside it is the matching member and streams as reasoning.
func TestR57BedrockStreamToolUseWithoutStringInputFallsToReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":5},"reasoningContent":{"text":"think"}}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"end_turn"}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var reasoning string
	for _, c := range drainChunks(t, stream) {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeReasoning {
			reasoning += c.Reasoning
		}
	}
	if reasoning != "think" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "think")
	}
}
