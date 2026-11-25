package gateway

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r74provaToolResultOutputJSON(t *testing.T, output types.ToolResultOutput) string {
	t.Helper()
	model := newTestGatewayLanguageModel("openai/gpt-5")
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleTool,
			Content: []types.ContentPart{types.ToolResultContent{
				ToolCallID: "call-1", ToolName: "t", Output: &output,
			}},
		}}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody: %v", err)
	}
	part := body["prompt"].([]map[string]interface{})[0]["content"].([]map[string]interface{})[0]
	b, err := json.Marshal(part["output"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS createToolModelOutput turns a tool that returned nothing into
// {type: "json", value: null}; the gateway's V4 prompt schema requires value.
func TestR74prova_GatewayToolOutputNullValueSent(t *testing.T) {
	cases := []struct {
		name   string
		output types.ToolResultOutput
		want   string
	}{
		{"json null", types.ToolResultOutput{Type: types.ToolResultOutputJSON}, `{"type":"json","value":null}`},
		{"error-json null", types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON}, `{"type":"error-json","value":null}`},
		{"empty text", types.ToolResultOutput{Type: types.ToolResultOutputText, Value: ""}, `{"type":"text","value":""}`},
		{"empty content", types.ToolResultOutput{Type: types.ToolResultOutputContent}, `{"type":"content","value":[]}`},
		{"denied", types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "no"}, `{"reason":"no","type":"execution-denied"}`},
		{"denied without reason", types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}, `{"type":"execution-denied"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r74provaToolResultOutputJSON(t, tc.output); got != tc.want {
				t.Fatalf("output = %s, want %s", got, tc.want)
			}
		})
	}
}

// TS sends the call options as given, so an explicitly empty stopSequences
// list reaches the gateway; nil stays absent.
func TestR74prova_GatewayEmptyStopSequencesSent(t *testing.T) {
	model := newTestGatewayLanguageModel("openai/gpt-5")
	body, err := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["stopSequences"]; !ok || len(v.([]string)) != 0 {
		t.Fatalf("stopSequences = %#v (present %v), want []", v, ok)
	}
	body, err = model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["stopSequences"]; ok {
		t.Fatalf("nil stopSequences sent: %#v", body["stopSequences"])
	}
}
