package bridge

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

func canonical(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", data, err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestOutboundControlFramesRoundTrip(t *testing.T) {
	goldens := []string{
		`{"type":"bridge-hello","state":"running","lastSeq":12,"capabilities":{"experimental_userMessageResponses":true}}`,
		`{"type":"bridge-hello"}`,
		`{"type":"user-message-response","messageId":"m1","accepted":false,"error":{"message":"turn finished"}}`,
		`{"type":"bridge-stop","data":{"sessionId":"s","nested":[1,2]}}`,
		`{"type":"bridge-stop","data":null}`,
		`{"type":"bridge-thread","threadId":"th_1"}`,
		`{"type":"sandbox-log","source":"claude","stream":"stderr","line":"warn!"}`,
		`{"type":"debug-event","level":"debug","subsystem":"bridge.ws","message":"connected","attrs":{"port":4000},"error":{"name":"Error","message":"x","stack":"s"}}`,
		`{"type":"text-delta","id":"t","delta":"hi"}`,
	}
	for _, golden := range goldens {
		msg, _, err := DecodeOutbound([]byte(golden))
		if err != nil {
			t.Fatalf("decode %s: %v", golden, err)
		}
		encoded, err := MarshalOutbound(msg)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(t, encoded) != canonical(t, []byte(golden)) {
			t.Errorf("round trip mismatch\n got  %s\n want %s", encoded, golden)
		}
	}
}

func TestDecodeOutboundSeqAndValidation(t *testing.T) {
	msg, seq, err := DecodeOutbound([]byte(`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{},"outputTokens":{}},"seq":7}`))
	if err != nil || seq == nil || *seq != 7 {
		t.Fatal(seq, err)
	}
	if _, ok := msg.(StreamPartFrame).Part.(*harness.FinishPart); !ok {
		t.Fatalf("%T", msg)
	}
	for _, bad := range []string{
		`{"type":"sandbox-log","source":"s","stream":"stdin","line":"x"}`,
		`{"type":"debug-event","level":"loud","subsystem":"s","message":"m"}`,
		`{"type":"debug-event","level":"info","subsystem":"s","message":"m","error":{"name":"E"}}`,
		`{"type":"bridge-thread"}`,
		`{"type":"user-message-response","messageId":"m"}`,
		`{"type":"detach"}`,
	} {
		if _, _, err := DecodeOutbound([]byte(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestDiagnosticFromFrame(t *testing.T) {
	d, ok := DiagnosticFromFrame(&SandboxLog{Source: "claude", Stream: "stderr", Line: "oops"}, DiagnosticContext{SessionID: "s1", Timestamp: 5})
	if !ok || d.Level != harness.DebugLevelWarn || d.Subsystem != "sandbox.log.claude" || d.Kind != "log" || d.Source != "claude" || d.Stream != "stderr" || d.SessionID != "s1" || d.Timestamp != 5 {
		t.Fatalf("%+v", d)
	}
	d, _ = DiagnosticFromFrame(&SandboxLog{Source: "x", Stream: "stdout", Line: "l"}, DiagnosticContext{})
	if d.Level != harness.DebugLevelInfo {
		t.Fatal(d.Level)
	}
	d, ok = DiagnosticFromFrame(&DebugEvent{Level: harness.DebugLevelTrace, Subsystem: "a.b", Message: "m", Attrs: map[string]any{"k": 1}}, DiagnosticContext{Timestamp: 1})
	if !ok || d.Kind != "event" || d.Level != harness.DebugLevelTrace || d.Attrs["k"] != 1 {
		t.Fatalf("%+v", d)
	}
	if _, ok := DiagnosticFromFrame(&Thread{}, DiagnosticContext{}); ok {
		t.Fatal("only diagnostics frames convert")
	}
}

// claudeStart mimics an adapter start frame extending StartBase.
type claudeStart struct {
	StartBase
	Thinking        map[string]any `json:"thinking,omitempty"`
	ResumeSessionID string         `json:"resumeSessionId,omitempty"`
}

func TestInboundFrames(t *testing.T) {
	enabled := true
	cases := []struct {
		cmd  InboundCommand
		want string
	}{
		{
			claudeStart{
				StartBase: StartBase{
					Prompt:               "fix it",
					Tools:                []harness.ToolSpec{{Name: "weather", Description: "w", InputSchema: map[string]any{"type": "object"}}},
					Model:                "sonnet",
					Debug:                &harness.DebugConfig{Enabled: &enabled, Level: harness.DebugLevelDebug, Subsystems: []string{"bridge"}},
					PermissionMode:       harness.PermissionModeAllowEdits,
					BuiltinToolFiltering: &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow, ToolNames: []string{"read"}},
					ResponseFormat:       &harness.ResponseFormat{Type: harness.ResponseFormatJSON, Name: "answer", Description: "A structured answer.", Schema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string", "enum": []any{"yes", "no"}}}, "required": []any{"answer"}, "additionalProperties": false}},
				},
				ResumeSessionID: "native-1",
			},
			`{"type":"start","prompt":"fix it","tools":[{"name":"weather","description":"w","inputSchema":{"type":"object"}}],"model":"sonnet","debug":{"enabled":true,"level":"debug","subsystems":["bridge"]},"permissionMode":"allow-edits","builtinToolFiltering":{"mode":"allow","toolNames":["read"]},"responseFormat":{"type":"json","schema":{"type":"object","properties":{"answer":{"type":"string","enum":["yes","no"]}},"required":["answer"],"additionalProperties":false},"name":"answer","description":"A structured answer."},"resumeSessionId":"native-1"}`,
		},
		{StartBase{Prompt: "hi"}, `{"type":"start","prompt":"hi"}`},
		{ToolResultCommand{ToolCallID: "c1", Output: map[string]any{"ok": true}, IsError: true}, `{"type":"tool-result","toolCallId":"c1","output":{"ok":true},"isError":true}`},
		{ToolResultCommand{ToolCallID: "c1"}, `{"type":"tool-result","toolCallId":"c1","output":null}`},
		{ToolApprovalResponseCommand{ApprovalID: "a1", Approved: false, Reason: "no"}, `{"type":"tool-approval-response","approvalId":"a1","approved":false,"reason":"no"}`},
		{UserMessageCommand{Text: "/compact"}, `{"type":"user-message","text":"/compact"}`},
		{UserMessageCommand{MessageID: "message-1", Text: "Change course."}, `{"type":"user-message","messageId":"message-1","text":"Change course."}`},
		{AbortCommand{}, `{"type":"abort"}`},
		{DestroyCommand{}, `{"type":"destroy"}`},
		{ResumeCommand{LastSeenEventID: 12}, `{"type":"resume","lastSeenEventId":12}`},
		{ResumeCommand{}, `{"type":"resume","lastSeenEventId":0}`},
		{StopCommand{}, `{"type":"stop"}`},
	}
	for _, tc := range cases {
		got, err := MarshalInbound(tc.cmd)
		if err != nil {
			t.Fatal(err)
		}
		// Map-valued schemas encode with sorted keys, so compare canonically;
		// struct field order (type first) is still checked by the prefix.
		if canonical(t, got) != canonical(t, []byte(tc.want)) || string(got[:len(`{"type":"`)]) != `{"type":"` {
			t.Errorf("got  %s\nwant %s", got, tc.want)
		}
		if _, ok := tc.cmd.(claudeStart); ok {
			continue
		}
		decoded, err := DecodeInbound(got)
		if err != nil {
			t.Fatalf("decode %s: %v", got, err)
		}
		if decoded.FrameType() != tc.cmd.FrameType() {
			t.Fatal(decoded.FrameType())
		}
	}
}

// TS: harness-v1-bridge-protocol.test.ts user-message cases.
func TestExperimentalUserMessage(t *testing.T) {
	if err := ValidateExperimentalUserMessage(UserMessageCommand{Text: "Change course."}); err == nil {
		t.Fatal("messageId is required")
	}
	if err := ValidateExperimentalUserMessage(UserMessageCommand{MessageID: "message-1", Text: "Change course."}); err != nil {
		t.Fatal(err)
	}
	cmd, err := DecodeInbound([]byte(`{"type":"user-message","text":"/compact"}`))
	if err != nil || cmd.(*UserMessageCommand).Text != "/compact" {
		t.Fatal(cmd, err)
	}
}

func TestBridgeReady(t *testing.T) {
	r, err := DecodeReady([]byte(`{"type":"bridge-ready","port":4123}`))
	if err != nil || r.Port != 4123 {
		t.Fatal(r, err)
	}
	if _, err := DecodeReady([]byte(`{"type":"bridge-ready"}`)); err == nil {
		t.Fatal("port required")
	}
	out, _ := MarshalReady(Ready{Port: 1})
	if string(out) != `{"type":"bridge-ready","port":1}` {
		t.Fatal(string(out))
	}
}
