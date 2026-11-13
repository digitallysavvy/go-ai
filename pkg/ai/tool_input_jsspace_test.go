package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS parseToolCall checks toolCall.input.trim() === ” with JS whitespace.
func TestToolCallInputArgumentsJSWhitespace(t *testing.T) {
	got, err := toolCallInputArguments(types.ToolCall{RawArguments: "\ufeff\u00a0"})
	if err != nil || len(got) != 0 {
		t.Fatalf("BOM/NBSP-only input is empty: got %v, %v", got, err)
	}
	if _, err := toolCallInputArguments(types.ToolCall{RawArguments: "\u0085"}); err == nil {
		t.Fatal("U+0085 is not JS whitespace; the input must fail to parse")
	}
}

func TestValidateBaseURLJSWhitespace(t *testing.T) {
	if providerutils.ValidateBaseURL("\ufeff") == nil {
		t.Fatal("BOM-only baseURL must be rejected")
	}
	if providerutils.ValidateBaseURL("\u0085") != nil {
		t.Fatal("U+0085 is not JS whitespace")
	}
}
