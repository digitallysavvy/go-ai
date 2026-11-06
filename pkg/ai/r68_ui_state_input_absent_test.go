package ai

import "testing"

// TestR68ToolPartInputStaysAbsent: TS processUIMessageStream sets the tool
// part's input to toolInvocation.input (output chunks) or chunk.input
// (tool-input-error), so an input that was never received stays undefined
// (absent from the JSON part). Go set the key to nil, which marshals as null.
func TestR68ToolPartInputStaysAbsent(t *testing.T) {
	cases := []struct {
		name   string
		chunks []UIMessageChunk
	}{
		{"output-available after input-start", []UIMessageChunk{
			{"type": "tool-input-start", "toolCallId": "c1", "toolName": "t"},
			{"type": "tool-output-available", "toolCallId": "c1", "output": "ok"},
		}},
		{"output-error after unparseable input delta", []UIMessageChunk{
			{"type": "tool-input-start", "toolCallId": "c1", "toolName": "t", "dynamic": true},
			{"type": "tool-input-delta", "toolCallId": "c1", "inputTextDelta": "}"},
			{"type": "tool-output-error", "toolCallId": "c1", "errorText": "boom"},
		}},
		{"tool-input-error without input", []UIMessageChunk{
			{"type": "tool-input-error", "toolCallId": "c1", "toolName": "t", "errorText": "bad"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := newUIMessageCallbackState(nil, "m1")
			for _, chunk := range tc.chunks {
				if err := applyUIStateChunk(state, chunk, func(error) string { return "" }); err != nil {
					t.Fatalf("apply %v: %v", chunk["type"], err)
				}
			}
			parts := uiParts(state.message)
			if len(parts) != 1 {
				t.Fatalf("%d parts, want 1", len(parts))
			}
			part := asUIPartChunk(parts[0])
			if v, ok := part["input"]; ok {
				t.Fatalf("input = %#v present, want absent (TS undefined)", v)
			}
		})
	}
}
