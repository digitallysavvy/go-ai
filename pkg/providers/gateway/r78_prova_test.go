package gateway

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS parseJsonEventStream drops `data: [DONE]` and keeps reading the body.
func TestR78prova_GatewayReadsPastDone(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"text-delta","id":"t","delta":"a"}`, "",
			`data: [DONE]`, "",
			`data: {"type":"text-delta","id":"t","delta":"b"}`, "",
			`data: {"type":"finish","finishReason":"stop"}`, "", "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	text, finishes := "", 0
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
	}
	if text != "ab" || finishes != 1 {
		t.Fatalf("text %q, finishes %d; want \"ab\", 1", text, finishes)
	}
}
