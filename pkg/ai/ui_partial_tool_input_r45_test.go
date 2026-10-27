package ai

import (
	"reflect"
	"testing"
)

// TS process-ui-message-stream parses a streaming tool input with
// parsePartialJson, so the part shows the input received so far; Go's strict
// json.Unmarshal left it absent until the JSON was complete.
func TestUIMessageStateRepairsPartialToolInput(t *testing.T) {
	state := newUIMessageCallbackState(nil, "m1")
	state.apply(UIMessageChunk{"type": "tool-input-start", "toolCallId": "c1", "toolName": "weather"}, nil)
	state.apply(UIMessageChunk{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": `{"city":"Ber`}, nil)
	part := state.findToolPart("c1")
	if part == nil {
		t.Fatal("no tool part")
	}
	if want := map[string]interface{}{"city": "Ber"}; !reflect.DeepEqual(part["input"], want) {
		t.Fatalf("input = %#v, want %#v", part["input"], want)
	}
	if part["rawInput"] != `{"city":"Ber` {
		t.Fatalf("rawInput = %#v", part["rawInput"])
	}
}
