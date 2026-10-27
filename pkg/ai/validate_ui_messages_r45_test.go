package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TS uiMessagesSchema declares rawInput (z.string()) and inputEncoding on
// input-streaming tool parts, so validated messages keep the partial input a
// resumed stream continues from. inputEncoding is stripped from other states.
func TestValidateUIMessages_KeepsInputStreamingRawInputAndEncoding(t *testing.T) {
	raw := `[{"id":"m1","role":"assistant","parts":[` +
		`{"type":"tool-t","toolCallId":"c1","state":"input-streaming","rawInput":"{\"a\":","inputEncoding":"text"},` +
		`{"type":"dynamic-tool","toolName":"d","toolCallId":"c2","state":"input-streaming","rawInput":"{\"b\":"},` +
		`{"type":"tool-t","toolCallId":"c3","state":"input-available","input":{},"rawInput":"x","inputEncoding":"text"}]}]`
	messages, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: raw})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(messages[0].Parts)
	if err != nil {
		t.Fatal(err)
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(data, &parts); err != nil {
		t.Fatal(err)
	}
	if parts[0]["rawInput"] != `{"a":` || parts[0]["inputEncoding"] != "text" {
		t.Fatalf("static input-streaming part = %v", parts[0])
	}
	if parts[1]["rawInput"] != `{"b":` {
		t.Fatalf("dynamic input-streaming part = %v", parts[1])
	}
	if _, ok := parts[2]["rawInput"]; ok {
		t.Fatalf("input-available part kept rawInput: %v", parts[2])
	}
	if _, ok := parts[2]["inputEncoding"]; ok {
		t.Fatalf("input-available part kept inputEncoding: %v", parts[2])
	}
	if !strings.Contains(string(data), `"state":"input-streaming"`) {
		t.Fatalf("parts = %s", data)
	}
}
