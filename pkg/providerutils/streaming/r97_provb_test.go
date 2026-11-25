package streaming

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97provbCompatStreamFractionalUsageRawExact: the openai-compatible
// usage counts are z.number() and usage.raw is the loose usage object, so a
// fractional count stays exact in Usage.Raw (Go's raw held the truncated
// retry value).
func TestR97provbCompatStreamFractionalUsageRawExact(t *testing.T) {
	s := NewOpenAICompatStream(io.NopCloser(strings.NewReader(
		"data: {\"id\":\"a\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1.5,\"completion_tokens\":1}}\n\n")),
		providerutils.MapOpenAIFinishReason)
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if c.Type == provider.ChunkTypeFinish {
			if *c.Usage.InputTokens != 1 || c.Usage.Raw["prompt_tokens"] != 1.5 {
				t.Fatalf("input = %v, raw = %#v", *c.Usage.InputTokens, c.Usage.Raw)
			}
			return
		}
	}
}
