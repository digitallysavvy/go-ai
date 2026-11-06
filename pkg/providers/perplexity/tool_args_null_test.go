package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPerplexityToolCallArguments_NullInputFallsBackToEmptyObject(t *testing.T) {
	if got := perplexityToolCallArguments(types.ToolCallContent{Input: "null"}); got != "{}" {
		t.Fatalf("got %q, want {}", got)
	}
}
