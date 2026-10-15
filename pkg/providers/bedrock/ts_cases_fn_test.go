package bedrock

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture/anthropictools"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestTSCasesPrepareTools replays amazon-bedrock-prepare-tools.test.ts.
func TestTSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return anthropictools.ProviderTool(id, args)
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		hasChoice := false
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice, hasChoice = tsfixture.BuildToolChoice(tc), true
		}
		modelID, _ := in["modelId"].(string)
		family, _ := in["modelFamily"].(string)
		var budget *int
		if v, ok := in["reasoningBudgetTokens"].(float64); ok {
			n := int(v)
			budget = &n
		}
		var disable *bool
		if v, ok := in["disableParallelToolUse"].(bool); ok {
			disable = &v
		}
		out := prepareBedrockTools(ts, choice, hasChoice, modelID, family, budget, disable, in["rejectsForcedToolUse"] == true)
		toolConfig := map[string]interface{}{}
		if len(out.ToolConfig.Tools) > 0 {
			toolConfig["tools"] = out.ToolConfig.Tools
			if out.ToolConfig.ToolChoice != nil {
				toolConfig["toolChoice"] = out.ToolConfig.ToolChoice
			}
		}
		warnings := tsfixture.WarningsToTS(out.Warnings)
		betas := out.Betas
		if betas == nil {
			betas = []string{}
		}
		return map[string]interface{}{
			"toolConfig":      toolConfig,
			"additionalTools": out.AdditionalTools,
			"betas":           map[string]interface{}{"__set": betas},
			"toolWarnings":    warnings,
		}, nil, nil
	}, nil)
}

// TestTSCasesConvertUsage replays convert-amazon-bedrock-usage.test.ts.
func TestTSCasesConvertUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		var raw []byte
		if m, ok := args[0].(map[string]interface{}); !ok || m["__undef"] == nil {
			raw, _ = json.Marshal(args[0])
		}
		return tsfixture.UsageToTS(convertBedrockConverseUsage(raw)), nil, nil
	}, nil)
}

// TestTSCasesNormalizeToolCallID replays normalize-tool-call-id.test.ts.
func TestTSCasesNormalizeToolCallID(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/normalize_id.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		id, _ := args[0].(string)
		mistral, _ := args[1].(bool)
		return normalizeToolCallID(id, mistral), nil, nil
	}, nil)
}
