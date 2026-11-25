package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS 6815153d64: a provider-executed preliminary tool-result is forwarded with
// its preliminary flag and is not recorded as a final result in content.
func TestPortStreamPreliminaryProviderToolResult(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "image_generation", RawArguments: "{}", ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "partial-image"}, Preliminary: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "final-image"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var seen []bool
	fs := res.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			seen = append(seen, c.ToolResult.Preliminary)
		}
	}
	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("stream tool-result preliminary flags = %v, want [true false]", seen)
	}
	n := 0
	for _, p := range res.Content() {
		if tr, ok := p.(types.ToolResultContent); ok {
			n++
			if tr.Preliminary || tr.Result.(map[string]interface{})["result"] != "final-image" {
				t.Fatalf("content tool-result = %+v, want final-image", tr)
			}
		}
	}
	if n != 1 {
		t.Fatalf("content tool-results = %d, want 1", n)
	}
}

func portstreamRepeat(n int, elem string) string {
	return "[" + strings.TrimSuffix(strings.Repeat(elem+",", n), ",") + "]"
}

func portstreamValidationMessage(t *testing.T, messages string) string {
	t.Helper()
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(messages)})
	if res.Success {
		t.Fatalf("validation succeeded for %.80s", messages)
	}
	if !providererrors.IsValidationError(res.Error) {
		t.Fatalf("error %T is not a validation error", res.Error)
	}
	msg := res.Error.Error()
	if len(msg) >= 40_000 {
		t.Fatalf("error message is %d bytes, want < 40000", len(msg))
	}
	return msg
}

// TS a3c6e3fef0 "should avoid union fan-out for malformed metadata".
func TestPortStreamValidateUIMessagesNoUnionFanOutForMalformedMetadata(t *testing.T) {
	t.Parallel()
	var meta strings.Builder
	meta.WriteString("{")
	for i := 0; i < 100; i++ {
		if i > 0 {
			meta.WriteString(",")
		}
		fmt.Fprintf(&meta, `"key%d":1`, i)
	}
	meta.WriteString("}")
	for _, part := range []string{
		`"x":1`,
		`"type":"unknown"`,
		`"type":"text"`,
		`"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-available","input":{}`,
		`"type":"tool-foo","toolCallId":"1","state":"input-available","input":{}`,
	} {
		portstreamValidationMessage(t, `[{"id":"1","role":"user","parts":[{`+part+`,"callProviderMetadata":`+meta.String()+`}]}]`)
	}
}

// TS a3c6e3fef0 / 8c5d5345c0 "should stop accumulating structural errors across
// invalid parts, messages and steering parts": 7.5 KB of malformed input stays
// a single bounded issue at the first invalid element.
func TestPortStreamValidateUIMessagesStopsAtFirstInvalidElement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, messages, path string
	}{
		{"parts", `[{"id":"1","role":"user","parts":` + portstreamRepeat(2500, `{}`) + `}]`, "[0].parts[0].type"},
		{"messages", portstreamRepeat(100, `{"id":"1","role":"user","parts":[{}]}`), "[0].parts[0].type"},
		{"steering parts", `[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"2","parts":` + portstreamRepeat(2500, `{}`) + `}]}]`, "[0].parts[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := portstreamValidationMessage(t, tc.messages)
			if !strings.HasPrefix(msg, "Type validation failed") && !strings.Contains(msg, tc.path+":") {
				t.Fatalf("message %.200q does not report path %s", msg, tc.path)
			}
			if strings.Count(msg, "Invalid") > 3 {
				t.Fatalf("message accumulates several issues: %.300q", msg)
			}
		})
	}
}

// TS a3c6e3fef0 "should preserve the index of the first invalid part" and
// "should not validate parts after the first invalid part".
func TestPortStreamValidateUIMessagesFirstInvalidPartIndex(t *testing.T) {
	t.Parallel()
	msg := portstreamValidationMessage(t, `[{"id":"1","role":"user","parts":[{"type":"text","text":"hello"},{},{"type":"nope"}]}]`)
	if !strings.Contains(msg, "[0].parts[1].type") {
		t.Fatalf("message %.200q does not report the first invalid part [0].parts[1].type", msg)
	}
	if strings.Contains(msg, "parts[2]") {
		t.Fatalf("message %.200q reports a later part", msg)
	}
}

// TS a3c6e3fef0 "should continue to accept large valid part arrays".
func TestPortStreamValidateUIMessagesAcceptsLargeValidPartArrays(t *testing.T) {
	t.Parallel()
	msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"user","parts":` + portstreamRepeat(2500, `{"type":"text","text":"hello"}`) + `}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(msgs[0].Parts) != 2500 {
		t.Fatalf("got %d messages / %d parts, want 1 / 2500", len(msgs), len(msgs[0].Parts))
	}
}

func portstreamNestedArrays() string {
	elem := strings.Repeat("[", 100) + "[]" + strings.Repeat("]", 100)
	return portstreamRepeat(20, elem)
}

// TS 718824cd59 "valid JSON values in metadata": nested JSON values in provider,
// tool and citation metadata are accepted and keep their shape.
func TestPortStreamValidateUIMessagesAcceptsNestedJSONMetadata(t *testing.T) {
	t.Parallel()
	nested := portstreamNestedArrays()
	for name, part := range map[string]string{
		"provider metadata": `{"type":"text","text":"Hello","providerMetadata":{"provider":{"key":` + nested + `}}}`,
		"tool metadata and call/result provider metadata": `{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{},"output":"result",` +
			`"callProviderMetadata":{"provider":{"key":` + nested + `}},"resultProviderMetadata":{"provider":{"key":` + nested + `}},"toolMetadata":{"key":` + nested + `}}`,
		"citation provider metadata": `{"type":"text","text":"Hello","citations":[{"source":{"type":"source","sourceType":"url","id":"source-1","url":"https://example.com","providerMetadata":{"provider":{"key":` + nested + `}}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[` + part + `]}]`),
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(msgs[0].Parts[0])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), nested) {
				t.Fatalf("validated part lost the nested value: %.200s", out)
			}
		})
	}
}

// TS 718824cd59 "should reject provider metadata that is an array / a provider
// without an object / an undefined provider" and "should reject tool metadata
// that is not an object".
func TestPortStreamValidateUIMessagesRejectsMalformedMetadataShapes(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, part, path string }{
		{"array provider metadata", `{"type":"text","text":"Hello","providerMetadata":[]}`, "[0].parts[0]"},
		{"provider without an object", `{"type":"text","text":"Hello","providerMetadata":{"provider":"value"}}`, "[0].parts[0]"},
		{"null provider", `{"type":"text","text":"Hello","providerMetadata":{"provider":null}}`, "[0].parts[0]"},
		{"tool metadata array", `{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-available","input":{},"toolMetadata":["value"]}`, "[0].parts[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := portstreamValidationMessage(t, `[{"id":"1","role":"assistant","parts":[`+tc.part+`]}]`)
			if !strings.Contains(msg, tc.path) {
				t.Fatalf("message %.200q does not report %s", msg, tc.path)
			}
		})
	}
}

// TS 63593a7f6a: approval requests of user-approval and approved tool calls are
// signed; the request of an automatically denied call is not.
func TestPortStreamApprovalSignatureNotIssuedForDeniedCalls(t *testing.T) {
	t.Parallel()
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			tool := types.Tool{
				Name: "tool1",
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "result", nil
				},
			}
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}}
			maxSteps := 1
			var mu sync.Mutex
			var requests []types.ToolApprovalRequestContent
			res, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "x", Tools: []types.Tool{tool}, MaxSteps: &maxSteps,
				ExperimentalToolApprovalSecret: secret,
				ToolApproval:                   map[string]types.ToolApprovalValue{"tool1": status},
				OnChunk: func(c provider.StreamChunk) {
					if c.Type == provider.ChunkTypeToolApprovalRequest && c.ToolApprovalRequest != nil {
						mu.Lock()
						requests = append(requests, *c.ToolApprovalRequest)
						mu.Unlock()
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := res.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 1 {
				t.Fatalf("approval requests = %d, want 1", len(requests))
			}
			req := requests[0]
			if status == types.ToolApprovalStatusDenied {
				if req.Signature != "" || !req.IsAutomatic {
					t.Fatalf("denied request = %+v, want automatic and unsigned", req)
				}
				return
			}
			want, err := SignToolApproval(secret, req.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "test"})
			if err != nil {
				t.Fatal(err)
			}
			if req.Signature == "" || req.Signature != want {
				t.Fatalf("signature = %q, want %q", req.Signature, want)
			}
		})
	}
}

// portstreamApprovalMessages runs a three-step StreamText whose first two steps
// each call tool1 twice and records the Messages slice every approval function
// call receives.
func portstreamApprovalMessages(t *testing.T, prepare func(context.Context, PrepareStepOptions) PrepareStepOptions) [][]types.Message {
	t.Helper()
	var calls int32
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		step := int(atomic.AddInt32(&calls, 1)) - 1
		if step < 2 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: fmt.Sprintf("call-%d-1", step), ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: fmt.Sprintf("call-%d-2", step), ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	var mu sync.Mutex
	var got [][]types.Message
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "test-input",
		Tools: []types.Tool{{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			mu.Lock()
			got = append(got, o.Messages)
			mu.Unlock()
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
		PrepareStep: prepare,
		StopWhen:    []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return got
}

func portstreamSameSlice(a, b []types.Message) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

// TS 0b8cbfadb8 "should be the same for all tool calls of a step and new for
// every step": a tool approval function can tell the responses apart by the
// Messages slice it receives.
func TestPortStreamApprovalMessagesSliceIsPerStep(t *testing.T) {
	t.Parallel()
	got := portstreamApprovalMessages(t, nil)
	if len(got) != 4 {
		t.Fatalf("approval calls = %d, want 4", len(got))
	}
	if !portstreamSameSlice(got[0], got[1]) || !portstreamSameSlice(got[2], got[3]) {
		t.Fatal("tool calls of one step must receive the same messages slice")
	}
	if portstreamSameSlice(got[0], got[2]) {
		t.Fatal("a later step must receive a new messages slice")
	}
}

// TS 0b8cbfadb8 "should be new for every step when prepareStep returns the same
// messages array".
func TestPortStreamApprovalMessagesSliceWithPrepareStepResult(t *testing.T) {
	t.Parallel()
	prepared := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prepared-input"}}}}
	got := portstreamApprovalMessages(t, func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
		step.Messages = prepared
		return step
	})
	if len(got) != 4 {
		t.Fatalf("approval calls = %d, want 4", len(got))
	}
	for i, msgs := range got {
		if len(msgs) != 1 {
			t.Fatalf("call %d messages = %d, want the prepared messages", i, len(msgs))
		}
		if portstreamSameSlice(msgs, prepared) {
			t.Fatalf("call %d received the slice prepareStep returned", i)
		}
	}
	if !portstreamSameSlice(got[0], got[1]) || !portstreamSameSlice(got[2], got[3]) || portstreamSameSlice(got[0], got[2]) {
		t.Fatal("messages slices must be shared within a step and new for every step")
	}
}

// TS 7760e33531 stoppedBy: why the tool loop stopped, on the result and the
// onEnd event.
func TestPortStreamStoppedBy(t *testing.T) {
	t.Parallel()
	call := func(id string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}}
	}
	withExec := types.Tool{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}
	noExec := types.Tool{Name: "tool1"}
	toolStep := func() []provider.StreamChunk {
		return []provider.StreamChunk{call("c1"), {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls}}
	}
	textStep := func(reason types.FinishReason) []provider.StreamChunk {
		return []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "hi"}, {Type: provider.ChunkTypeFinish, FinishReason: reason}}
	}
	cases := []struct {
		name  string
		steps [][]provider.StreamChunk
		tool  types.Tool
		opts  func(*StreamTextOptions)
		want  StoppedBy
	}{
		{"final answer", [][]provider.StreamChunk{textStep(types.FinishReasonStop)}, withExec, nil, StoppedByModel},
		{"cut off by length", [][]provider.StreamChunk{textStep(types.FinishReasonLength)}, withExec, nil, StoppedByModel},
		{"step limit", [][]provider.StreamChunk{toolStep(), toolStep()}, withExec, func(o *StreamTextOptions) { o.StopWhen = []StopCondition{StepCountIs(1)} }, StoppedByStopCondition},
		{"tool without execute", [][]provider.StreamChunk{toolStep()}, noExec, nil, StoppedByToolWithoutExecute},
		{"needs approval", [][]provider.StreamChunk{toolStep()}, withExec, func(o *StreamTextOptions) {
			o.ToolApproval = map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval}
		}, StoppedByApproval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var n int32
			model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				i := int(atomic.AddInt32(&n, 1)) - 1
				if i >= len(tc.steps) {
					i = len(tc.steps) - 1
				}
				return testutil.NewMockTextStream(tc.steps[i]), nil
			}}
			var mu sync.Mutex
			var onEnd StoppedBy
			opts := StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{tc.tool},
				OnEndEvent: func(_ context.Context, e OnFinishEvent) { mu.Lock(); onEnd = e.StoppedBy; mu.Unlock() }}
			if tc.opts != nil {
				tc.opts(&opts)
			}
			res, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := res.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
			if got := res.StoppedBy(); got != tc.want {
				t.Fatalf("StoppedBy() = %q, want %q", got, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if onEnd != tc.want {
				t.Fatalf("onEnd StoppedBy = %q, want %q", onEnd, tc.want)
			}
		})
	}
}
