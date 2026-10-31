package anthropic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// TestTSCasesConvertUsage replays convert-anthropic-usage.test.ts.
func TestTSCasesConvertUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		if _, ok := in["rawUsage"]; ok {
			return nil, nil, fmt.Errorf("%w: Go keeps the wire usage object as the raw usage", tsfixture.ErrUnsupported)
		}
		raw, _ := json.Marshal(in["usage"])
		var u anthropicUsage
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertAnthropicUsage(u)), nil, nil
	}, nil)
}

// TestTSCasesSanitizeSchema replays sanitize-json-schema.test.ts.
func TestTSCasesSanitizeSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/sanitize_schema.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		return tool.SanitizeAnthropicSchema(args[0]), nil, nil
	}, map[string]string{
		"recursively sanitizes arrays, definitions, and composition schemas": "the TS test assigns several schemas to one variable before asserting; the harness reads the first call only",
	})
}
