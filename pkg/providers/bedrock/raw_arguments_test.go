package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS emits the tool input as the JSON text the model sent (wire key order),
// for generate and stream.
func TestToolCallsCarryRawArguments(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"output":{"message":{"role":"assistant","content":[{"toolUse":{"toolUseId":"t1","name":"f","input":{"b":1,"a":2}}}]}},"stopReason":"tool_use","usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`))
		}))
		defer server.Close()
		model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
		res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].RawArguments != `{"b":1,"a":2}` {
			t.Fatalf("ToolCalls = %+v, want RawArguments in wire order", res.ToolCalls)
		}
	})
	t.Run("stream", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
				{"contentBlockStart", `{"contentBlockIndex":0,"start":{"toolUse":{"toolUseId":"t1","name":"f"}}}`},
				{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"toolUse":{"input":"{\"b\":1,\"a\":2}"}}}`},
				{"contentBlockStop", `{"contentBlockIndex":0}`},
				{"messageStop", `{"stopReason":"tool_use"}`},
				{"metadata", `{"usage":{"inputTokens":1,"outputTokens":1,"totalTokens":2}}`},
			}))
		}))
		defer server.Close()
		model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-haiku-20240307-v1:0")
		stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close() //nolint:errcheck
		var raw string
		for {
			c, err := stream.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeToolCall {
				raw = c.ToolCall.RawArguments
			}
		}
		if raw != `{"b":1,"a":2}` {
			t.Fatalf("RawArguments = %q", raw)
		}
	})
}
