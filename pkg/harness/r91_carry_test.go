package harness

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func r91carryFinishScript(func(string, interface{})) []StreamPart {
	return []StreamPart{
		&StreamStartPart{},
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
	}
}

// harness-01: TS harness-agent.test.ts "validates custom call options
// before preparing a turn" (harness-agent.ts 1042): invalid options fail
// with a TypeValidationError and PrepareCall is not called; valid options
// reach PrepareCall as the validated (defaulted) value.
func TestR91carryHarnessCallOptionsSchema(t *testing.T) {
	callOptionsSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tenant": map[string]interface{}{"type": "string"},
			"tier":   map[string]interface{}{"type": "string", "default": "free"},
		},
		"required": []interface{}{"tenant"},
	})
	var prepared []interface{}
	mock := newMockHarness(mockHarnessOptions{script: r91carryFinishScript})
	a, err := NewAgent(AgentSettings{
		Harness:           mock.harness,
		CallOptionsSchema: callOptionsSchema,
		PrepareCall: func(_ context.Context, opts PrepareCallOptions) (PrepareCallResult, error) {
			prepared = append(prepared, opts.CallOptions)
			return PrepareCallResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "Hello", HarnessSession: session, CallOptions: map[string]interface{}{"tenant": 123}})
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) || ve.Context == nil || ve.Context.Field != "options" {
		t.Fatalf("err = %v, want a ValidationError for field options", err)
	}
	if len(prepared) != 0 {
		t.Fatalf("PrepareCall ran with invalid options: %v", prepared)
	}

	type opts struct {
		Tenant string `json:"tenant"`
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "Hello", HarnessSession: session, CallOptions: opts{Tenant: "acme"}}); err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 {
		t.Fatalf("PrepareCall calls = %d", len(prepared))
	}
	got, _ := prepared[0].(map[string]interface{})
	if got["tenant"] != "acme" || got["tier"] != "free" {
		t.Fatalf("PrepareCall CallOptions = %#v, want the validated value with defaults", prepared[0])
	}
}

// harness-03: an inline image file part keeps its filename (TS
// normalize-harness-tool-model-output.ts 52: `{...file, mediaType, data}`),
// and ConvertToolModelOutput still accepts the normalized part.
func TestR91carryNormalizeToolModelOutputKeepsFilename(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	out, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{Data: png, MediaType: "image", Filename: "a.png", ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"k": 1}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"content","value":[{"type":"file","data":{"type":"data","data":"iVBORw0KGgo="},"mediaType":"image/png","filename":"a.png","providerOptions":{"p":{"k":1}}}]}`; string(b) != want {
		t.Fatalf("normalized = %s\nwant %s", b, want)
	}
	parts, isErr, err := ConvertToolModelOutput(out)
	if err != nil || isErr || len(parts) != 1 || parts[0].Type != ToolModelOutputContentImage || parts[0].MediaType != "image/png" {
		t.Fatalf("convert = %+v %v %v", parts, isErr, err)
	}
}

// TS 7f1c01e8a2 (harness-session-data-directory-path.test.ts): every
// session ID is exactly one path segment below the parent, and distinct IDs
// never alias.
func TestR91carrySessionDataDirectoryContainment(t *testing.T) {
	const state = "/home/agent/.ai-sdk-harness"
	runs := state + "/.agent-runs"
	cases := [][2]string{
		{"../../../../work/project", "..%2F..%2F..%2F..%2Fwork%2Fproject"},
		{"/work/project", "%2Fwork%2Fproject"},
		{"..", "%2E%2E"},
		{".", "%2E"},
		{"", "%"},
		{"%", "%25"},
		{"a/b", "a%2Fb"},
		{"a%2Fb", "a%252Fb"},
		{"a\\..", "a%5C.."},
		{"a\x00b", "a%00b"},
	}
	for _, c := range cases {
		if got := EncodePathSegment(c[0]); got != c[1] {
			t.Errorf("EncodePathSegment(%q) = %q, want %q", c[0], got, c[1])
		}
		dir := SessionDataDirectoryPath(state, c[0])
		if dir != runs+"/"+c[1] || posixpath.Dirname(dir) != runs {
			t.Errorf("SessionDataDirectoryPath(%q) = %q", c[0], dir)
		}
	}
	if SessionDataDirectoryPath(state, "s1-abc") != runs+"/s1-abc" {
		t.Error("ordinary IDs stay readable")
	}
	if EncodePathSegment("a/b") == EncodePathSegment("a-b") || EncodePathSegment(".") == EncodePathSegment("%2E") {
		t.Error("distinct values must give distinct segments")
	}
}

// TS 8585d7e56f (run-prompt.test.ts "runPrompt host tool input
// validation"): an invalid host tool call is reported in the UI stream once,
// as a tool-input-error with the onError text, and not again as a
// tool-output-error.
func TestR91carryInvalidHostToolInputUIStreamReportsOnce(t *testing.T) {
	restricted := types.Tool{
		Name:       "restricted",
		Parameters: restrictedSchema(),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "not reached", nil
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "c1", ToolName: "restricted", Input: `{"city":42}`},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"restricted": restricted}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(),
	})
	chunks := drainRunPrompt(t, out)
	onErrorCalls := 0
	uiCh, errCh := ai.ToUIMessageStream(context.Background(), testutil.NewMockTextStream(chunks), ai.UIMessageStreamResultOptions{
		OnError: func(error) string { onErrorCalls++; return "Tool failed" },
	})
	var inputErrors, outputErrors int
	for c := range uiCh {
		switch c["type"] {
		case "tool-input-error":
			inputErrors++
			if c["toolCallId"] != "c1" || c["errorText"] != "Tool failed" {
				t.Errorf("tool-input-error = %v", c)
			}
		case "tool-output-error":
			outputErrors++
		case "tool-output-available":
			t.Errorf("unexpected %v", c)
		}
	}
	<-errCh
	if inputErrors != 1 || outputErrors != 0 || onErrorCalls != 1 {
		t.Fatalf("tool-input-error %d, tool-output-error %d, onError calls %d; want 1, 0, 1", inputErrors, outputErrors, onErrorCalls)
	}
}
