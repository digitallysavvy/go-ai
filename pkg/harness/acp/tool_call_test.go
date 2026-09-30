package acp

import (
	"encoding/json"
	"testing"
)

// TestToolCallNameField ports the shape TS `ACPToolCall` now allows since
// ACP SDK 1.5 / harness-acp 1.0.64 (be13602: "update to latest ACP SDK and
// use new `tool.name` field when present to determine tool identity"): the
// wire frame may or may not carry a `name`, and when present it is the
// implementation's own programmatic tool identifier, distinct from the
// human-readable Title and the broad Kind category.
func TestToolCallNameField(t *testing.T) {
	t.Run("decodes name when present", func(t *testing.T) {
		var tc ToolCall
		if err := json.Unmarshal([]byte(`{
			"toolCallId": "call-1",
			"name": "bash",
			"title": "Running ls -la",
			"kind": "execute"
		}`), &tc); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if tc.Name != "bash" {
			t.Errorf("Name = %q, want %q", tc.Name, "bash")
		}
		if tc.Title != "Running ls -la" {
			t.Errorf("Title = %q, want %q", tc.Title, "Running ls -la")
		}
		if tc.Kind != "execute" {
			t.Errorf("Kind = %q, want %q", tc.Kind, "execute")
		}
	})

	t.Run("name is absent for older/other implementations", func(t *testing.T) {
		// Mirrors TS's `name?: string | null`: omitted entirely by
		// implementations that predate ACP SDK 1.5, or that never report a
		// programmatic name. Go's zero value ("") must not be mistaken for
		// an explicit empty name by callers.
		var tc ToolCall
		if err := json.Unmarshal([]byte(`{
			"toolCallId": "call-2",
			"title": "Reading file"
		}`), &tc); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if tc.Name != "" {
			t.Errorf("Name = %q, want empty", tc.Name)
		}
	})

	t.Run("name round-trips through the acp-tool-call-candidate frame", func(t *testing.T) {
		// The frame this field actually arrives on (mirrors TS
		// `acpToolCallCandidateSchema`); classifyToolCallCandidate hands the
		// whole ToolCall (Name included) to the caller-supplied classifiers
		// unmodified, so a classifier can prefer Name over Title/Kind for
		// tool identity once an implementation reports it.
		frame := ToolCallCandidateFrame{
			RequestID: "req-1",
			ToolCall: ToolCall{
				ToolCallID: "call-3",
				Name:       "read_tool_result",
				Title:      "Reading captured output",
				Kind:       "read",
			},
		}
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var decoded ToolCallCandidateFrame
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if decoded.ToolCall.Name != "read_tool_result" {
			t.Errorf("decoded.ToolCall.Name = %q, want %q", decoded.ToolCall.Name, "read_tool_result")
		}

		var sawName string
		classifier := func(tc ToolCall) bool {
			sawName = tc.Name
			return false
		}
		if _, _, err := classifyToolCallCandidate(nil, classifier, decoded.ToolCall); err != nil {
			t.Fatalf("classifyToolCallCandidate: %v", err)
		}
		if sawName != "read_tool_result" {
			t.Errorf("classifier saw Name = %q, want %q", sawName, "read_tool_result")
		}
	})
}
