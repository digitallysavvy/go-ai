package responses

import (
	"encoding/json"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// TestR74ProvbToolArgsTypeValidationText: TS prepareResponsesTools calls
// validateTypes without a context, so invalid provider tool args throw
// TypeValidationError "Type validation failed: Value: <JSON.stringify(args)>.
// \nError message: <zod issues>". The mcpArgsSchema refine issue is zod's
// custom issue exactly (zod run on the TS schema); v0.5/r73 Go said
// "validation error on field 'tools': Type validation failed for <tool>: ...".
func TestR74ProvbToolArgsTypeValidationText(t *testing.T) {
	_, err := PrepareToolsWithError([]types.Tool{openaitool.MCP(openaitool.MCPConfig{ServerLabel: "x"})})
	want := "Type validation failed: Value: {\"serverLabel\":\"x\"}.\nError message: [\n  {\n    \"code\": \"custom\",\n    \"path\": [],\n    \"message\": \"One of serverUrl or connectorId must be provided.\"\n  }\n]"
	var tv *providererrors.TypeValidationError
	if !errors.As(err, &tv) || err.Error() != want {
		t.Fatalf("mcp refine err =\n%v\nwant\n%s", err, want)
	}
	// A hand-built tool (typed config only) reports its config as the value.
	_, err = PrepareToolsWithError([]types.Tool{{Name: "openai.mcp", ProviderOptions: openaitool.MCPConfig{ServerLabel: "x"}}})
	if err == nil || err.Error() != want {
		t.Fatalf("hand-built mcp refine err =\n%v\nwant\n%s", err, want)
	}

	_, err = PrepareToolsWithError([]types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{SearchContextSize: "huge"})})
	const prefix = "Type validation failed: Value: {\"searchContextSize\":\"huge\"}.\nError message: "
	if !errors.As(err, &tv) || len(err.Error()) <= len(prefix) || err.Error()[:len(prefix)] != prefix {
		t.Fatalf("web search err = %v, want prefix %q", err, prefix)
	}
}

// TestR74ProvbTSShapedShellEnvironment: a TS-shaped openai.shell tool
// (e.g. restored from a workflow) is mapped as TS mapShellEnvironment does:
// a skillReference resolves providerReference.openai (TS
// resolveProviderReference throws NoSuchProviderReferenceError without it;
// Go sent a skill without skill_id), and a disabled network policy is sent as
// {type: "disabled"} only.
func TestR74ProvbTSShapedShellEnvironment(t *testing.T) {
	shell := func(env map[string]interface{}) types.Tool {
		return types.Tool{Name: "shell", Type: types.ToolTypeProviderDefined, ProviderID: "openai.shell", ProviderArgs: map[string]interface{}{"environment": env}}
	}
	_, err := PrepareToolsWithError([]types.Tool{shell(map[string]interface{}{
		"type":   "containerAuto",
		"skills": []interface{}{map[string]interface{}{"type": "skillReference", "providerReference": map[string]interface{}{"anthropic": "sk_a"}}},
	})})
	var nsr *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &nsr) || err.Error() != "No provider reference found for provider 'openai'. Available providers: anthropic" {
		t.Fatalf("missing openai reference err = %v", err)
	}

	got, err := PrepareToolsWithError([]types.Tool{shell(map[string]interface{}{
		"type":          "containerAuto",
		"networkPolicy": map[string]interface{}{"type": "disabled", "allowedDomains": []interface{}{"x.com"}},
		"skills":        []interface{}{map[string]interface{}{"type": "skillReference", "providerReference": map[string]interface{}{"openai": "sk_1"}}},
	})})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got[0])
	want := `{"type":"shell","environment":{"type":"container_auto","network_policy":{"type":"disabled"},"skills":[{"type":"skill_reference","skill_id":"sk_1","version":"latest"}]}}`
	if string(b) != want {
		t.Fatalf("shell tool =\n%s\nwant\n%s", b, want)
	}
}
