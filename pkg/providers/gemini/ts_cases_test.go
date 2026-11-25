package gemini

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestTSCasesPrepareTools replays google-prepare-tools.test.ts.
func TestTSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
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
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice = tsfixture.BuildToolChoice(tc)
		}
		modelID, _ := in["modelId"].(string)
		isVertex, _ := in["isVertexProvider"].(bool)
		out := prepareTools(ts, choice, modelID, isVertex)
		warnings := tsfixture.WarningsToTS(out.warnings)
		return map[string]interface{}{
			"tools":        out.tools,
			"toolConfig":   out.toolConfig,
			"toolWarnings": warnings,
		}, nil, nil
	}, nil)
}

// TestTSCasesConvertUsage replays convert-google-usage.test.ts.
func TestTSCasesConvertUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		raw, _ := json.Marshal(args[0])
		var u UsageMetadata
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertUsage(&u)), nil, nil
	}, nil)
}

// TestTSCasesSanitizeResponseSchema replays sanitize-response-json-schema.test.ts.
func TestTSCasesSanitizeResponseSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/sanitize_schema.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		return sanitizeResponseJSONSchema(args[0]), nil, nil
	}, nil)
}

// TestTSCasesModelPath replays get-model-path.test.ts.
func TestTSCasesModelPath(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/model_path.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		id, _ := args[0].(string)
		return GetModelPath(id), nil, nil
	}, nil)
}
