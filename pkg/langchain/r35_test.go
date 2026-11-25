package langchain

import "testing"

// TS reads the custom event's type and id, and the tools event's toolCallId,
// with `typeof === 'string'`: numbers are ignored.
func TestR35NumericCustomAndToolsFieldsAreIgnored(t *testing.T) {
	got := processCustomEvent(map[string]interface{}{"type": float64(5), "id": float64(7)})
	if len(got) != 1 || got[0]["type"] != "data-custom" || got[0]["transient"] != true {
		t.Fatalf("custom event with numeric type/id = %#v", got)
	}
	if _, has := got[0]["id"]; has {
		t.Fatalf("numeric id must not become a part id: %#v", got[0])
	}
	chunks := processToolsEvent(newEventState(), StreamEvent{Mode: "tools", Data: map[string]interface{}{"event": "on_tool_start", "toolCallId": float64(123), "name": "t"}})
	if len(chunks) != 0 {
		t.Fatalf("numeric toolCallId must be ignored, got %#v", chunks)
	}
}
