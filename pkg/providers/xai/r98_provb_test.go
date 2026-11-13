package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR98provbCallerToolArgsKeepPrototypeKeys: TS validateTypes checks the
// caller's tool args object without a JSON parse, so a __proto__ key is an
// ordinary (stripped) property, not a "forbidden prototype property" error.
func TestR98provbCallerToolArgsKeepPrototypeKeys(t *testing.T) {
	tool := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "xai.web_search", Name: "web_search",
		ProviderArgs: map[string]interface{}{"__proto__": map[string]interface{}{"x": 1}}}
	if err := validateXAIResponsesToolArgs([]types.Tool{tool}); err != nil {
		t.Fatalf("err = %v, want valid args", err)
	}
}
