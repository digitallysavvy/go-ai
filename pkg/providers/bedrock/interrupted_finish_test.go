package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23814: a Converse stream that ends without messageStop reports
// interrupted.
func TestConverseStreamInterruptedWithoutMessageStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockStart", `{"contentBlockIndex":0,"start":{}}`},
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"hi"}}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drainChunks(t, stream)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finish = %+v", last)
	}
}
