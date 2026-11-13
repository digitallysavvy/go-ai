package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS usage.raw is the usage object as parsed: a fractional count stays exact
// while the typed counts truncate (class of cr-r97-provb-to-provg 2). Stream
// chunks and response bodies both decode through UnmarshalChunk.
func TestR97provgUsageRawKeepsFraction(t *testing.T) {
	var w struct {
		Usage *groqUsage `json:"usage"`
	}
	if err := providerutils.UnmarshalChunk([]byte(`{"usage":{"prompt_tokens":1.5,"completion_tokens":2}}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Usage.PromptTokens != 1 {
		t.Fatalf("typed prompt tokens = %v", w.Usage.PromptTokens)
	}
	if got := convertGroqUsage(*w.Usage).Raw["prompt_tokens"]; got != 1.5 {
		t.Fatalf("raw prompt_tokens = %#v, want 1.5", got)
	}
}
