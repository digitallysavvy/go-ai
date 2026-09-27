package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// TestPrepareTools_StrictExplicitFalseForwardedWhenSupported verifies that
// an explicit Strict=false is forwarded to the wire (not dropped) when the
// model supports strict tools, matching TS anthropic-prepare-tools.ts's
// `...(supportsStrictTools === true && tool.strict != null ? {strict:
// tool.strict} : {})` — which forwards the actual value, not only `true`.
func TestPrepareTools_StrictExplicitFalseForwardedWhenSupported(t *testing.T) {
	t.Parallel()

	toolDef := types.Tool{
		Name:       "non_strict_tool",
		Parameters: map[string]interface{}{"type": "object"},
		Strict:     types.BoolPtr(false),
	}

	result := prepareTools(prepareToolsOptions{
		tools:               []types.Tool{toolDef},
		validator:           prompt.NewAnthropicCacheControlValidator(),
		supportsStrictTools: true,
	})

	if len(result.tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %+v", len(result.tools), result.tools)
	}
	strictVal, ok := result.tools[0]["strict"]
	if !ok {
		t.Fatal("expected 'strict' key to be present for an explicit Strict=false")
	}
	if strictVal != false {
		t.Errorf("strict = %v, want false", strictVal)
	}
	if len(result.warnings) != 0 {
		t.Errorf("expected no warnings when strict tools are supported, got %+v", result.warnings)
	}
}

// TestPrepareTools_StrictExplicitFalseWarnsWhenUnsupported verifies that an
// explicit Strict=false (not just Strict=true) triggers the "not supported"
// warning when the model doesn't support strict tools at all, matching TS's
// `!supportsStrictTools && tool.strict != null` (checks "was set", not "was
// set to true").
func TestPrepareTools_StrictExplicitFalseWarnsWhenUnsupported(t *testing.T) {
	t.Parallel()

	toolDef := types.Tool{
		Name:       "non_strict_tool",
		Parameters: map[string]interface{}{"type": "object"},
		Strict:     types.BoolPtr(false),
	}

	result := prepareTools(prepareToolsOptions{
		tools:               []types.Tool{toolDef},
		validator:           prompt.NewAnthropicCacheControlValidator(),
		supportsStrictTools: false,
	})

	if len(result.warnings) != 1 {
		t.Fatalf("expected 1 warning for an explicit strict setting on an unsupported model, got %d: %+v", len(result.warnings), result.warnings)
	}
	if result.warnings[0].Feature != "strict" {
		t.Errorf("warning feature = %q, want %q", result.warnings[0].Feature, "strict")
	}
	if _, ok := result.tools[0]["strict"]; ok {
		t.Error("strict should not be forwarded when the model doesn't support strict tools")
	}
}

// TestPrepareTools_StrictUnsetNoWarningNoForward verifies that an unset
// Strict neither warns nor forwards a field, regardless of model support.
func TestPrepareTools_StrictUnsetNoWarningNoForward(t *testing.T) {
	t.Parallel()

	toolDef := types.Tool{
		Name:       "unspecified_tool",
		Parameters: map[string]interface{}{"type": "object"},
	}

	result := prepareTools(prepareToolsOptions{
		tools:               []types.Tool{toolDef},
		validator:           prompt.NewAnthropicCacheControlValidator(),
		supportsStrictTools: true,
	})

	if len(result.warnings) != 0 {
		t.Errorf("expected no warnings when Strict is unset, got %+v", result.warnings)
	}
	if _, ok := result.tools[0]["strict"]; ok {
		t.Error("strict should not be forwarded when unset")
	}
}
