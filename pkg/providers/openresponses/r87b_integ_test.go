package openresponses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Integrator check of provb fc98ba93 (V061 35). Expected parts recorded by
// running the TS 3fab50aa9b doStream with bun on the same events (scratch copy
// of packages/open-responses/src): an absent or null delta on an unknown item
// starts the accumulator as undefined / null, and `(acc ?? ”) + delta` then
// gives "null" for fc3; the custom tool input appends "undefined" / "null" and
// the tool call input is JSON.stringify("undefinednullz"). TS forwards the
// tool-input-delta parts with delta undefined / null; Go's typed Delta is ""
// (known difference "Open Responses: field values of an unexpected JSON type").
func TestR87bintegOpenResponsesAbsentNullDeltaAccumulation(t *testing.T) {
	chunks, err := r87provbStream(t,
		`{"type":"response.function_call_arguments.delta","item_id":"fc3"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc3","delta":null}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc3","call_id":"c3","name":"h","arguments":""}}`,
		`{"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"ct1","call_id":"cc1","name":"render","input":""}}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"ct1"}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"ct1","delta":null}`,
		`{"type":"response.custom_tool_call_input.delta","item_id":"ct1","delta":"z"}`,
		`{"type":"response.output_item.done","item":{"type":"custom_tool_call","id":"ct1","call_id":"cc1","name":"render","input":""}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			got = append(got, "tool-input-start "+c.ID+" "+c.ToolCall.ToolName)
		case provider.ChunkTypeToolInputDelta:
			got = append(got, "tool-input-delta "+c.ID+" "+c.Text)
		case provider.ChunkTypeToolInputEnd:
			got = append(got, "tool-input-end "+c.ID)
		case provider.ChunkTypeToolCall:
			got = append(got, "tool-call "+c.ToolCall.ID+" "+c.ToolCall.ToolName+" "+c.ToolCall.RawArguments)
		}
	}
	want := []string{
		"tool-call c3 h null",
		"tool-input-start cc1 render",
		"tool-input-delta cc1 ",
		"tool-input-delta cc1 ",
		"tool-input-delta cc1 z",
		"tool-input-end cc1",
		`tool-call cc1 render "undefinednullz"`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
