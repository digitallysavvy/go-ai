package providerutils

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 99 gen tests (TS 2ebc78b654, packages/ai/src/generate-text/
// to-response-messages.ts).

// TS `input: part.invalid && typeof part.input !== 'object' ? {} :
// part.input`, where an invalid call's input is the raw text when
// safeParseJSON (secureJsonParse) rejects it. A __proto__ input is such a
// call: it replays as {}, not as the arguments a provider decoded.
func TestR99genInvalidToolCallInputReplay(t *testing.T) {
	proto := map[string]interface{}{"__proto__": map[string]interface{}{"a": 1.0}}
	cases := []struct {
		name string
		call types.ToolCall
		want map[string]interface{}
	}{
		{"proto, provider arguments", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"__proto__":{"a":1}}`, Arguments: proto, Invalid: true}, map[string]interface{}{}},
		{"proto, no arguments", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"__proto__":{"a":1}}`, Invalid: true}, map[string]interface{}{}},
		{"text input", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `"text"`, Arguments: map[string]interface{}{"input": "text"}, Invalid: true}, map[string]interface{}{}},
		{"object input kept", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"a":1}`, Arguments: map[string]interface{}{"a": 1.0}, Invalid: true}, map[string]interface{}{"a": 1.0}},
		{"valid call keeps arguments", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"x":1}`, Arguments: map[string]interface{}{"x": 2.0}}, map[string]interface{}{"x": 2.0}},
		{"undecoded proto", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"__proto__":{"a":1}}`}, map[string]interface{}{}},
		{"undecoded out of range", types.ToolCall{ID: "c", ToolName: "t", RawArguments: `{"v":1e400}`}, map[string]interface{}{"v": math.Inf(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := ConvertToResponseMessage([]types.ToolCall{tc.call}, nil)
			if got := msg.ToolCalls[0].Arguments; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tool call arguments = %#v, want %#v", got, tc.want)
			}
			part := types.ToolCallContent{ToolCallID: tc.call.ID, ToolName: tc.call.ToolName, Input: tc.call.RawArguments, Arguments: tc.call.Arguments, Invalid: tc.call.Invalid}
			msg = ConvertToResponseMessage(nil, []types.ContentPart{part})
			if got := msg.Content[0].(types.ToolCallContent).Arguments; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tool-call part input = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TS passes part.providerMetadata on as the providerOptions object, as
// JSON.parse read it: a number beyond the double range is Infinity (ruling
// 4), not a reason to drop the metadata.
func TestR99genResponseProviderOptionsOutOfRange(t *testing.T) {
	msg := ConvertToResponseMessage(nil, []types.ContentPart{
		types.TextContent{Text: "x", ProviderMetadata: json.RawMessage(`{"p":{"v":1e400,"s":"k"}}`)},
	})
	want := map[string]interface{}{"p": map[string]interface{}{"v": math.Inf(1), "s": "k"}}
	if got := msg.Content[0].(types.TextContent).ProviderOptions; !reflect.DeepEqual(got, want) {
		t.Fatalf("providerOptions = %#v, want %#v", got, want)
	}
}
