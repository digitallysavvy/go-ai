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

// TestPrepareTools_StrictExplicitFalseOmittedSilentlyWhenUnsupported verifies
// that an explicit Strict=false is omitted without a warning when the model
// doesn't support strict tools, matching TS #22300 (`tool.strict === true`
// gates the warning).
func TestPrepareTools_StrictExplicitFalseOmittedSilentlyWhenUnsupported(t *testing.T) {
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

	if len(result.warnings) != 0 {
		t.Fatalf("expected no warnings for strict:false on an unsupported model, got %+v", result.warnings)
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

// TS #22440: toolChoice "none" keeps the tool definitions (server-tool
// history stays valid) and sends tool_choice {type: "none"}.
func TestPrepareTools_ToolChoiceNonePreservesTools(t *testing.T) {
	t.Parallel()

	result := prepareTools(prepareToolsOptions{
		tools:               []types.Tool{{Name: "testFunction", Description: "Test", Parameters: map[string]interface{}{}}},
		toolChoice:          &types.ToolChoice{Type: types.ToolChoiceNone},
		validator:           prompt.NewAnthropicCacheControlValidator(),
		supportsStrictTools: true,
	})
	if len(result.tools) != 1 || result.tools[0]["name"] != "testFunction" {
		t.Fatalf("tools = %+v, want the function tool preserved", result.tools)
	}
	if result.toolChoice["type"] != "none" || len(result.toolChoice) != 1 {
		t.Fatalf("toolChoice = %+v, want {type: none}", result.toolChoice)
	}
}
