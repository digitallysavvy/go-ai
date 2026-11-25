package streaming

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR96provbInfRawChunkAndUsageRaw: a number beyond the double range in a
// raw chunk and in a usage object reads as +Inf, as JSON.parse does (main
// ruling 4, read side).
func TestR96provbInfRawChunkAndUsageRaw(t *testing.T) {
	raw, _ := RawChunkValue(`{"x":1e400}`).(map[string]interface{})
	if f, _ := raw["x"].(float64); !math.IsInf(f, 1) {
		t.Fatalf("raw chunk x = %#v, want +Inf", raw["x"])
	}
	s := NewOpenAICompatStream(io.NopCloser(strings.NewReader(
		"data: {\"id\":\"a\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"cost\":1e400}}\n\n")),
		providerutils.MapOpenAIFinishReason)
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no finish chunk")
		}
		if c.Type == provider.ChunkTypeFinish {
			if f, _ := c.Usage.Raw["cost"].(float64); !math.IsInf(f, 1) {
				t.Fatalf("usage raw cost = %#v, want +Inf", c.Usage.Raw["cost"])
			}
			return
		}
	}
}
