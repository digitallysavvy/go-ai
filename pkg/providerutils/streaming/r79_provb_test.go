package streaming_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// TS openai-compatible chunk schema: created is z.number().nullish(), and the
// timestamp new Date(created * 1000); a fractional created no longer fails
// the chunk parse.
func TestR79provbCompatStreamFractionalCreated(t *testing.T) {
	body := `data: {"id":"c1","object":"chat.completion.chunk","created":1700000000.25,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	s := streaming.NewOpenAICompatStream(io.NopCloser(strings.NewReader(body)), providerutils.MapOpenAIFinishReason)
	defer func() { _ = s.Close() }()
	var meta *provider.ResponseMetadata
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %+v", c)
		}
		if c.Type == provider.ChunkTypeResponseMetadata {
			meta = c.ResponseMetadata
		}
	}
	if meta == nil || !meta.Timestamp.Equal(time.UnixMilli(1700000000250)) {
		t.Fatalf("metadata = %+v", meta)
	}
}
