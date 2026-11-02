package openai

import (
	"encoding/json"
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

// TestR77ProvbTSCasesChatUsage replays convert-openai-chat-usage.test.ts
// through convertOpenAIUsage (the TS wire usage object decoded as Go reads
// the response).
func TestR77ProvbTSCasesChatUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		var u openAIUsage
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertOpenAIUsage(u)), nil, nil
	}, nil)
}

// TestR77ProvbTSCasesNormalizeJSONSchema replays
// normalize-openai-json-schema.test.ts through NormalizeOpenAIJSONSchema
// ({schema, warnings} in the TS shape; a rejected schema is an error).
func TestR77ProvbTSCasesNormalizeJSONSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/normalize_json_schema.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		m, ok := args[0].(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("%w: schema %v", tsfixture.ErrUnsupported, args[0])
		}
		schema, warnings, err := NormalizeOpenAIJSONSchema(m)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{"schema": schema, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}
