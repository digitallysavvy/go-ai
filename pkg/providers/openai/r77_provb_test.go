package openai

import (
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// TestR77ProvbTSCasesCapabilities replays openai-language-model-capabilities.test.ts
// (extract.mjs --expand-each --fn=getOpenAILanguageModelCapabilities: one case
// per it.each row) through GetLanguageModelCapabilities, rendered in the TS
// return shape.
func TestR77ProvbTSCasesCapabilities(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/capabilities.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		id, ok := args[0].(string)
		if !ok {
			return nil, nil, fmt.Errorf("%w: model id %v", tsfixture.ErrUnsupported, args[0])
		}
		c := GetLanguageModelCapabilities(id)
		out := map[string]interface{}{
			"isReasoningModel":               c.IsReasoningModel,
			"systemMessageMode":              c.SystemMessageMode,
			"supportsFlexProcessing":         c.SupportsFlexProcessing,
			"supportsPriorityProcessing":     c.SupportsPriorityProcessing,
			"supportsConfigurationUpdate":    c.SupportsConfigurationUpdate,
			"supportsAsyncToolCalling":       c.SupportsAsyncToolCalling,
			"supportsNonReasoningParameters": c.SupportsNonReasoningParameters,
		}
		if c.SupportedReasoningEfforts != nil {
			out["supportedReasoningEfforts"] = c.SupportedReasoningEfforts
		}
		return out, nil, nil
	}, nil)
}
