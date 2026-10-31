package google

import (
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestTSCasesInteractionsConvertInput replays
// interactions/convert-to-google-interactions-input.test.ts.
func TestTSCasesInteractionsConvertInput(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-2.5-flash")
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := GoogleInteractionsProviderOptions{}
		opts.PreviousInteractionID, _ = in["previousInteractionId"].(string)
		opts.MediaResolution, _ = in["mediaResolution"].(string)
		if v, ok := in["store"].(bool); ok {
			opts.Store = &v
		}
		steps, system, warnings, err := m.convertPrompt(types.Prompt{Messages: msgs}, opts)
		if err != nil {
			return nil, nil, err
		}
		res := map[string]interface{}{"input": steps, "warnings": tsfixture.WarningsToTS(warnings)}
		if system != "" {
			res["systemInstruction"] = system
		}
		return res, nil, nil
	}, nil)
}

// TestTSCasesInteractionsFinishReason replays
// interactions/map-google-interactions-finish-reason.test.ts.
func TestTSCasesInteractionsFinishReason(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_finish_reason.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		status, _ := in["status"].(string)
		hasFn, _ := in["hasFunctionCall"].(bool)
		return string(mapInteractionsFinishReason(status, hasFn)), nil, nil
	}, nil)
}

// TestTSCasesInteractionsPrepareTools replays
// interactions/prepare-google-interactions-tools.test.ts.
func TestTSCasesInteractionsPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_prepare_tools.json.gz")
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
		tools, toolChoice, warnings := prepareInteractionsTools(ts, choice)
		return map[string]interface{}{
			"tools":        tools,
			"toolChoice":   toolChoice,
			"toolWarnings": tsfixture.WarningsToTS(warnings),
		}, nil, nil
	}, nil)
}
