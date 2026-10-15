package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS flush() enqueues `finish` when the stream ends, whether or not a
// response.completed event arrived (and after a [DONE] marker too).
func TestResponsesStreamFinishesAtEndWithoutTerminalEvent(t *testing.T) {
	for name, tail := range map[string]string{"eof": "", "done": "data: [DONE]\n\n"} {
		t.Run(name, func(t *testing.T) {
			s := newResponsesStream(io.NopCloser(strings.NewReader(
				`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","created_at":1,"model":"m"}}`+"\n\n"+tail)), false)
			var finish *provider.StreamChunk
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeFinish {
					if finish != nil {
						t.Fatal("two finish chunks")
					}
					finish = c
				}
			}
			if finish == nil || finish.FinishReason != types.FinishReasonOther || finish.Usage == nil {
				t.Fatalf("finish = %+v", finish)
			}
			if !strings.Contains(string(finish.ProviderMetadata), `"responseId":"resp_1"`) {
				t.Fatalf("metadata = %s", finish.ProviderMetadata)
			}
		})
	}
}
