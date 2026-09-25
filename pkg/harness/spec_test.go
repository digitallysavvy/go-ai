package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// canonical re-encodes JSON so key order does not matter in comparisons.
func canonical(t *testing.T, data []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", data, err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// Every line is a frame shaped exactly as the TS zod schemas accept it
// (harnessV1*PartSchema). Decoding then re-encoding must be lossless.
var streamPartGoldens = []string{
	`{"type":"stream-start","warnings":[{"type":"unsupported-setting","setting":"temperature","details":"ignored"},{"type":"unsupported-tool","tool":"x"},{"type":"other","message":"m"}],"modelId":"claude-sonnet-4-6"}`,
	`{"type":"stream-start"}`,
	`{"type":"text-start","id":"t1","harnessMetadata":{"claude-code":{"uuid":"u1"}}}`,
	`{"type":"text-delta","id":"t1","delta":"Hello"}`,
	`{"type":"text-end","id":"t1"}`,
	`{"type":"reasoning-start","id":"r1"}`,
	`{"type":"reasoning-delta","id":"r1","delta":"thinking"}`,
	`{"type":"reasoning-end","id":"r1"}`,
	`{"type":"tool-input-start","id":"c1","toolName":"bash","providerExecuted":true,"dynamic":true,"title":"Run"}`,
	`{"type":"tool-input-delta","id":"c1","delta":"{\"command\":"}`,
	`{"type":"tool-input-end","id":"c1","providerMetadata":{"p":{"k":1}}}`,
	`{"type":"tool-call","toolCallId":"c1","toolName":"bash","input":"{\"command\":\"ls\"}","providerExecuted":true,"nativeName":"Bash","stepToolCallCount":2}`,
	`{"type":"tool-approval-request","approvalId":"a1","toolCallId":"c1"}`,
	`{"type":"tool-result","toolCallId":"c1","toolName":"bash","result":{"stdout":"x","n":1.5},"isError":true,"preliminary":true}`,
	`{"type":"tool-result","toolCallId":"c2","toolName":"weather","result":null}`,
	`{"type":"finish-step","finishReason":{"unified":"tool-calls","raw":"tool_use"},"usage":{"inputTokens":{"total":10,"noCache":4,"cacheRead":6},"outputTokens":{"total":5,"text":3,"reasoning":2},"raw":{"input_tokens":10}}}`,
	`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{},"outputTokens":{}}}`,
	`{"type":"file-change","event":"modify","path":"src/a.ts"}`,
	`{"type":"compaction","trigger":"auto","summary":"s","tokensBefore":1000,"tokensAfter":200}`,
	`{"type":"error","error":{"message":"boom"}}`,
	`{"type":"error","error":"plain"}`,
	`{"type":"raw","rawValue":{"anything":[1,"two",null]}}`,
}

func TestStreamPartGoldenRoundTrip(t *testing.T) {
	for _, golden := range streamPartGoldens {
		part, err := DecodeStreamPart([]byte(golden))
		if err != nil {
			t.Fatalf("decode %s: %v", golden, err)
		}
		encoded, err := MarshalStreamPart(part)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(t, encoded) != canonical(t, []byte(golden)) {
			t.Errorf("round trip mismatch\n got  %s\n want %s", encoded, golden)
		}
		if !strings.HasPrefix(string(encoded), `{"type":"`+part.PartType()+`"`) {
			t.Errorf("type must be the first key: %s", encoded)
		}
	}
}

func TestDecodeStreamPartIgnoresSeqAndRejectsInvalid(t *testing.T) {
	part, err := DecodeStreamPart([]byte(`{"type":"text-delta","id":"t","delta":"x","seq":42}`))
	if err != nil {
		t.Fatal(err)
	}
	if d := part.(*TextDeltaPart); d.Delta != "x" {
		t.Fatal(d)
	}
	invalid := []string{
		`{"type":"text-delta","id":"t"}`,
		`{"type":"text-delta","delta":"x"}`,
		`{"type":"tool-call","toolCallId":"c","toolName":"n","input":"{}","stepToolCallCount":0}`,
		`{"type":"tool-result","toolCallId":"c","toolName":"n"}`,
		`{"type":"finish","finishReason":{"unified":"weird"},"totalUsage":{"inputTokens":{},"outputTokens":{}}}`,
		`{"type":"finish","finishReason":{"unified":"stop"}}`,
		`{"type":"file-change","event":"rename","path":"a"}`,
		`{"type":"compaction","trigger":"later","summary":"s"}`,
		`{"type":"nope"}`,
		`{"id":"x"}`,
		`[]`,
	}
	for _, in := range invalid {
		if _, err := DecodeStreamPart([]byte(in)); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
	if _, err := DecodeStreamPart([]byte(`{"type":"nope"}`)); !errors.Is(err, ErrUnknownPartType) {
		t.Fatal(err)
	}
}

func TestToolResultNullResultIsSerialized(t *testing.T) {
	out, _ := MarshalStreamPart(&ToolResultPart{ToolCallID: "c", ToolName: "t"})
	if string(out) != `{"type":"tool-result","toolCallId":"c","toolName":"t","result":null}` {
		t.Fatal(string(out))
	}
}

// A lifecycle payload as persisted by a TS host (JSON.stringify of
// HarnessV1ResumeSessionState with nested continueFrom). A Go host must be able
// to resume it and write it back without loss.
const tsResumeState = `{"type":"resume-session","harnessId":"claude-code","specificationVersion":"harness-v1","data":{"sandboxId":"sbx_1","bridge":{"port":4000,"lastSeq":17,"sessionId":"abc"}},"continueFrom":{"type":"continue-turn","harnessId":"claude-code","specificationVersion":"harness-v1","data":{"cursor":17},"pendingToolApprovals":[{"approvalId":"a1","toolCallId":"c1","toolName":"bash","input":"{\"command\":\"rm -rf x\"}","kind":"builtin","providerExecuted":false,"nativeName":"Bash"}],"pendingToolResults":[{"toolCallId":"c2","toolName":"weather","input":"{\"city\":\"SF\"}","providerOptions":{"openai":{"x":1}},"completedResult":{"output":{"temp":20},"isError":false}}],"turnSettings":{"model":"sonnet","skills":[{"name":"demo","description":"d","content":"c","files":[{"path":"a.md","content":"x"}]}],"instructions":"be nice","tools":[{"name":"weather","description":"w","inputSchema":{"type":"object","properties":{"city":{"type":"string"}}}}]}}}`

func TestLifecycleStateTSCompatibility(t *testing.T) {
	state, err := DecodeLifecycleState([]byte(tsResumeState))
	if err != nil {
		t.Fatal(err)
	}
	resume, ok := state.(*ResumeSessionState)
	if !ok || resume.ContinueFrom == nil || len(resume.ContinueFrom.PendingToolApprovals) != 1 {
		t.Fatalf("state = %#v", state)
	}
	if pe := resume.ContinueFrom.PendingToolApprovals[0].ProviderExecuted; pe == nil || *pe {
		t.Fatal("explicit providerExecuted:false must survive")
	}
	encoded, err := json.Marshal(resume)
	if err != nil {
		t.Fatal(err)
	}
	if canonical(t, encoded) != canonical(t, []byte(tsResumeState)) {
		t.Fatalf("round trip mismatch\n got  %s\n want %s", encoded, tsResumeState)
	}
	// Adapter data survives byte-for-byte.
	if string(resume.Data) != `{"sandboxId":"sbx_1","bridge":{"port":4000,"lastSeq":17,"sessionId":"abc"}}` {
		t.Fatal(string(resume.Data))
	}
}

func TestLifecycleStateGoProduced(t *testing.T) {
	s, err := NewContinueTurnState("codex", map[string]any{"threadId": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	s.TurnSettings = &TurnSettings{}
	out, _ := json.Marshal(s)
	want := `{"type":"continue-turn","harnessId":"codex","specificationVersion":"harness-v1","data":{"threadId":"t1"},"turnSettings":{"skills":[],"tools":[]}}`
	if string(out) != want {
		t.Fatalf("got %s", out)
	}
	r := ResumeSessionState{HarnessID: "x", SpecificationVersion: SpecificationVersion}
	out, _ = json.Marshal(r)
	if string(out) != `{"type":"resume-session","harnessId":"x","specificationVersion":"harness-v1","data":null}` {
		t.Fatal(string(out))
	}

	for _, bad := range []string{
		`{"type":"continue-turn","harnessId":"x","specificationVersion":"harness-v2","data":null}`,
		`{"type":"resume-session","specificationVersion":"harness-v1","data":null}`,
		`{"type":"continue-turn","harnessId":"x","specificationVersion":"harness-v1","data":null,"pendingToolApprovals":[{"approvalId":"a","toolCallId":"c","toolName":"t","input":"{}","kind":"other"}]}`,
		`{"type":"other"}`,
	} {
		if _, err := DecodeLifecycleState([]byte(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestErrors(t *testing.T) {
	cause := errors.New("inner")
	capErr := NewCapabilityUnsupportedError("Claude Code does not support tool approvals", "claude-code", cause)
	authErr := NewSandboxAuthenticationError("Set a sandbox API key", "test-sandbox", cause)
	base := NewHarnessError("Invalid harness state.", nil)

	for _, err := range []error{capErr, authErr, base, fmt.Errorf("wrapped: %w", capErr)} {
		if !IsHarnessError(err) {
			t.Errorf("%T must be a HarnessError", err)
		}
	}
	if !IsCapabilityUnsupportedError(capErr) || IsCapabilityUnsupportedError(errors.New("x")) || IsCapabilityUnsupportedError(nil) {
		t.Fatal("IsCapabilityUnsupportedError")
	}
	if !IsSandboxAuthenticationError(authErr) || IsSandboxAuthenticationError(errors.New("x")) || IsSandboxAuthenticationError(nil) {
		t.Fatal("IsSandboxAuthenticationError")
	}
	if capErr.Error() != "Claude Code does not support tool approvals" || capErr.HarnessID != "claude-code" || !errors.Is(capErr, cause) {
		t.Fatal("capability error fields")
	}
	if authErr.SandboxProviderID != "test-sandbox" || !errors.Is(authErr, cause) {
		t.Fatal("auth error fields")
	}
	if capErr.ErrorName() != CapabilityUnsupportedErrorName || authErr.ErrorName() != SandboxAuthenticationErrorName || base.ErrorName() != HarnessErrorName {
		t.Fatal("error names")
	}

	// getHarnessErrorMessage
	for _, err := range []error{base, NewCapabilityUnsupportedError("This capability is unavailable.", "", nil), NewSandboxAuthenticationError("Configure sandbox credentials.", "test", nil)} {
		if got := GetHarnessErrorMessage(err); got != err.Error() {
			t.Errorf("reviewed message masked: %s", got)
		}
	}
	if GetHarnessErrorMessage(errors.New("secret details")) != "An error occurred." {
		t.Fatal("unknown errors must be masked")
	}
	type unknownHarnessError struct{ HarnessError }
	unreviewed := &unknownHarnessError{HarnessError{Name: "AI_UnknownHarnessError", Message: "unreviewed details"}}
	if !IsHarnessError(unreviewed) || GetHarnessErrorMessage(unreviewed) != "An error occurred." {
		t.Fatal("unreviewed HarnessError subclasses must be masked")
	}
}

func TestBuiltinToolFiltering(t *testing.T) {
	if !IsBuiltinToolIncluded("bash", nil) {
		t.Fatal("nil filtering includes everything")
	}
	allow := &BuiltinToolFiltering{Mode: BuiltinToolFilteringAllow, ToolNames: []string{"read"}}
	deny := &BuiltinToolFiltering{Mode: BuiltinToolFilteringDeny, ToolNames: []string{"read"}}
	if !IsBuiltinToolIncluded("read", allow) || IsBuiltinToolIncluded("bash", allow) || IsBuiltinToolIncluded("read", deny) || !IsBuiltinToolIncluded("bash", deny) {
		t.Fatal("filtering semantics")
	}
	if BuiltinToolFilteringDenialReason("bash") != "Tool 'bash' is inactive due to the HarnessAgent tool filtering policy." {
		t.Fatal("denial reason")
	}
	out, _ := json.Marshal(BuiltinToolFiltering{Mode: BuiltinToolFilteringDeny})
	if string(out) != `{"mode":"deny","toolNames":[]}` {
		t.Fatal(string(out))
	}
}

func TestBuiltinTools(t *testing.T) {
	tools := StandardBuiltinTools()
	if len(tools) != len(BuiltinToolNames) {
		t.Fatal("vocabulary size")
	}
	names := make([]string, len(BuiltinToolNames))
	for i, n := range BuiltinToolNames {
		names[i] = string(n)
		if _, ok := tools[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	if !reflect.DeepEqual(names, []string{"read", "write", "edit", "bash", "grep", "glob", "webSearch", "askUserQuestions"}) {
		t.Fatal(names)
	}

	bash := CommonTool(BuiltinToolBash, CommonToolOptions{
		NativeName:  "Bash",
		ToolUseKind: BuiltinToolUseKindBash,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}, "timeout": map[string]any{"type": "number"}}},
	})
	if bash.NativeName != "Bash" || bash.CommonName != BuiltinToolBash || bash.ToolUseKind != BuiltinToolUseKindBash {
		t.Fatal(bash)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("CommonTool must reject schemas that are not a superset of the standard")
		}
	}()
	CommonTool(BuiltinToolEdit, CommonToolOptions{NativeName: "Edit", InputSchema: map[string]any{"properties": map[string]any{"file_path": map[string]any{}}}})
}

func TestQuestionsTool(t *testing.T) {
	canonicalInput := `{"allowPartialAnswers":true,"questions":[{"id":"framework","question":"Which framework?","header":"Framework","options":[{"id":"react","label":"React","description":"Use React.","preview":"<App />"}],"allowMultiple":false,"allowFreeForm":{"secret":true}}]}`
	in, err := ParseQuestionsToolInput([]byte(canonicalInput))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(in)
	if canonical(t, out) != canonical(t, []byte(canonicalInput)) {
		t.Fatalf("got %s", out)
	}
	if _, err := ParseQuestionsToolInput([]byte(`{"questions":[{"id":"framework","question":"Which framework?"}]}`)); err == nil {
		t.Fatal("allowPartialAnswers is required")
	}
	if _, err := ParseQuestionsToolInput([]byte(`{"allowPartialAnswers":true,"questions":[]}`)); err == nil {
		t.Fatal("at least one question is required")
	}
	if _, err := ParseQuestionsToolInput([]byte(`{"allowPartialAnswers":true,"questions":[{"id":"","question":"q"}]}`)); err == nil {
		t.Fatal("ids must be non-empty")
	}
	boolForm, err := ParseQuestionsToolInput([]byte(`{"allowPartialAnswers":false,"questions":[{"id":"a","question":"b","allowFreeForm":true}]}`))
	if err != nil || boolForm.Questions[0].AllowFreeForm == nil || !boolForm.Questions[0].AllowFreeForm.Enabled {
		t.Fatal("boolean allowFreeForm", err)
	}

	for _, output := range []string{
		`{"action":"answered","answers":{"framework":{"optionIds":["react"],"freeform":"notes"}}}`,
		`{"action":"partially-answered","answers":{"framework":{"optionIds":[]}}}`,
		`{"action":"declined"}`,
		`{"action":"cancelled"}`,
	} {
		parsed, err := ParseQuestionsToolOutput([]byte(output))
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(parsed)
		if canonical(t, encoded) != canonical(t, []byte(output)) {
			t.Errorf("got %s want %s", encoded, output)
		}
	}
	if _, err := ParseQuestionsToolOutput([]byte(`{"action":"answered"}`)); err == nil {
		t.Fatal("answers required")
	}
	if _, err := ParseQuestionsToolOutput([]byte(`{"action":"maybe"}`)); err == nil {
		t.Fatal("unknown action")
	}
}

func TestAuthenticationJSON(t *testing.T) {
	var a Authentication
	if err := json.Unmarshal([]byte(`"ai-gateway"`), &a); err != nil || a.Mode != AuthModeAIGateway || a.IsEnvironment() {
		t.Fatal(a, err)
	}
	if err := json.Unmarshal([]byte(`{"AI_GATEWAY_API_KEY":"k"}`), &a); err != nil || !a.IsEnvironment() || a.Environment["AI_GATEWAY_API_KEY"] != "k" {
		t.Fatal(a, err)
	}
	if err := json.Unmarshal([]byte(`{"gateway":{"apiKey":"k"}}`), &a); !errors.Is(err, ErrInvalidAuthentication) {
		t.Fatal(err)
	}
	if !AuthEnvironment(nil).IsEnvironment() || !(Authentication{}).IsZero() {
		t.Fatal("empty record is still an environment; zero value is unset")
	}
}

func TestPromptJSON(t *testing.T) {
	var p Prompt
	if err := json.Unmarshal([]byte(`"hi"`), &p); err != nil || p.Text != "hi" {
		t.Fatal(p, err)
	}
	out, _ := json.Marshal(TextPrompt("hi"))
	if string(out) != `"hi"` {
		t.Fatal(string(out))
	}
}

func TestCallWarningVariants(t *testing.T) {
	out, _ := json.Marshal([]CallWarning{
		{Type: CallWarningUnsupportedSetting, Setting: "temperature"},
		{Type: CallWarningOther, Message: "m"},
	})
	if string(out) != `[{"type":"unsupported-setting","setting":"temperature"},{"type":"other","message":"m"}]` {
		t.Fatal(string(out))
	}
}
