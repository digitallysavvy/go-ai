package huggingface

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23814: a stream that ends without response.completed reports interrupted.
func TestHFStreamInterruptedWithoutCompletion(t *testing.T) {
	body := `data: {"type":"response.created","response":{"id":"resp_1","model":"m","created_at":1}}` + "\n\n" +
		"data: [DONE]\n\n"
	s := newHFStream(io.NopCloser(strings.NewReader(body)), nil, "huggingface")
	for {
		chunk, err := s.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if chunk.Type == provider.ChunkTypeFinish {
			if chunk.FinishReason != types.FinishReasonInterrupted {
				t.Fatalf("finish reason = %q", chunk.FinishReason)
			}
			return
		}
	}
}
