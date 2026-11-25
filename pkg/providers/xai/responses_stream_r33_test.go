package xai

import (
	"io"
	"strings"
	"testing"
)

// TS keeps costInUsdTicks from an earlier terminal event when a later one has
// no usage (only `if (response.usage)` overwrites it).
func TestStreamKeepsCostTicksWithoutLaterUsageR33(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"cost_in_usd_ticks":42}}}`,
		`data: {"type":"response.done","response":{"object":"response","output":[],"status":"completed","service_tier":"default"}}`,
	}, "\n\n") + "\n\n"
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(body)))
	var meta string
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.ProviderMetadata != nil {
			meta = string(c.ProviderMetadata)
		}
	}
	if !strings.Contains(meta, `"costInUsdTicks":42`) || !strings.Contains(meta, `"serviceTier":"default"`) {
		t.Fatalf("meta = %s", meta)
	}
}

// TS output_item.done for a reasoning item always closes the block, even when
// the item has neither an id nor encrypted content.
func TestStreamReasoningDoneWithoutIDStillEndsBlockR33(t *testing.T) {
	body := `data: {"type":"response.output_item.done","output_index":0,"sequence_number":1,"item":{"type":"reasoning","id":"","summary":[],"status":"completed"}}` + "\n\n"
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(body)))
	var types []string
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		types = append(types, string(c.Type))
	}
	if strings.Join(types, ",") != "reasoning-start,reasoning-end,finish" {
		t.Fatalf("chunks = %v", types)
	}
}
