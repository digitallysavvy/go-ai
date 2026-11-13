package xai

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR96provbStreamMetadataJSONStringify: xAI stream metadata built from
// provider values is written like JSON.stringify: <, > and & stay literal
// (main ruling 4, write side).
func TestR96provbStreamMetadataJSONStringify(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.reasoning_summary_part.added","item_id":"<a&b>","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}

`)
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoningStart {
			if !strings.Contains(string(c.ProviderMetadata), `"itemId":"<a&b>"`) {
				t.Fatalf("metadata = %s", c.ProviderMetadata)
			}
			return
		}
	}
	t.Fatal("no reasoning-start chunk")
}
