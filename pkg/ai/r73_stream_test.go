package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r73streamPUICase is one `it` case of ui/process-ui-message-stream.test.ts:
// the scripted chunks, the initial streaming state, the processUIMessageStream
// options and the case's assertions (inline snapshots and toEqual/toBe values).
type r73streamPUICase struct {
	Describe    string                        `json:"describe"`
	It          string                        `json:"it"`
	Line        int                           `json:"line"`
	Chunks      []map[string]interface{}      `json:"chunks"`
	MessageID   string                        `json:"messageId"`
	LastMessage map[string]interface{}        `json:"lastMessage"`
	ResetFirst  bool                          `json:"resetFirst"`
	ResetChange bool                          `json:"resetChange"`
	Lookups     map[string]r73streamPUILookup `json:"lookups"`
	Asserts     []r73streamPUIAssert          `json:"asserts"`
}

// r73streamPUILookup is a TS `const x = <source>.message.parts.find(p =>
// p.<field> === '<value>')` (filter when Filter), where the source is the
// final state or the first write whose tool part matches Write.
type r73streamPUILookup struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Filter bool   `json:"filter"`
	Whole  bool   `json:"whole"`
	Write  *struct {
		Field string `json:"field"`
		Value string `json:"value"`
		State string `json:"state"`
	} `json:"write"`
}

type r73streamPUIAssert struct {
	Expr  string      `json:"expr"`
	Kind  string      `json:"kind"`
	Value interface{} `json:"value"`
	Undef bool        `json:"undef"`
}

// r73streamProcessUI runs chunks through the UI message state machine the
// way TS processUIMessageStream does (the TS tests do not set
// resetStateOnMessageIdChange), recording a snapshot for every write().
func r73streamProcessUI(t *testing.T, c r73streamPUICase) (writes []interface{}, final interface{}, errs []error) {
	t.Helper()
	writes = []interface{}{}
	var original []UIMessageChunk
	if c.LastMessage != nil {
		original = []UIMessageChunk{UIMessageChunk(r73streamJSONClone(t, c.LastMessage).(map[string]interface{}))}
	}
	state := newUIMessageCallbackState(original, c.MessageID)
	state.resetOnMessageIDChange = c.ResetChange
	snapshot := func() interface{} {
		msg, err := UIMessageFromChunk(state.responseMessage())
		if err != nil {
			t.Fatalf("UIMessageFromChunk: %v", err)
		}
		return r73streamJSONClone(t, msg)
	}
	for _, raw := range c.Chunks {
		chunk := UIMessageChunk(r73streamJSONClone(t, raw).(map[string]interface{}))
		emptyReset := stringValue(chunk["type"]) == "reset-step" &&
			currentStepStartIndex(uiParts(state.message)) >= len(uiParts(state.message))
		var failed error
		state.apply(chunk, func(err error) string {
			if failed == nil {
				failed = err
			}
			errs = append(errs, err)
			return ""
		})
		if failed != nil {
			if stringValue(chunk["type"]) != "error" {
				break
			}
			continue
		}
		if emptyReset || !uiChunkWritesMessage(chunk) {
			continue
		}
		writes = append(writes, snapshot())
	}
	return writes, snapshot(), errs
}

func r73streamJSONClone(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func r73streamJSON(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}

// r73streamPUIEval evaluates the TS assertion expressions the port
// supports over the recorded writes and final state. ok is false for
// expressions that need JS-only state (Object.prototype, client callbacks).
func r73streamPUIEval(c r73streamPUICase, expr string, writes []interface{}, final interface{}) (interface{}, bool) {
	e := strings.ReplaceAll(strings.ReplaceAll(expr, "!", ""), " ", "")
	finalParts, _ := final.(map[string]interface{})["parts"].([]interface{})
	switch e {
	case "state.message.parts.filter(isToolUIPart).map(part=>({toolCallId:part.toolCallId,state:part.state,toolMetadata:part.toolMetadata,})),":
		out := []interface{}{}
		for _, raw := range finalParts {
			p := raw.(map[string]interface{})
			typ, _ := p["type"].(string)
			if typ != "dynamic-tool" && !strings.HasPrefix(typ, "tool-") {
				continue
			}
			m := map[string]interface{}{"toolCallId": p["toolCallId"], "state": p["state"]}
			if p["toolMetadata"] != nil {
				m["toolMetadata"] = p["toolMetadata"]
			}
			out = append(out, m)
		}
		return out, true
	case "state.message.parts.filter(part=>part.type==='reasoning').map(part=>part.id),":
		out := []interface{}{}
		for _, raw := range finalParts {
			if p := raw.(map[string]interface{}); p["type"] == "reasoning" {
				out = append(out, p["id"])
			}
		}
		return out, true
	case "state.message.parts.some(part=>isToolUIPart(part)&&part.toolCallId==='stale-tool',),":
		for _, raw := range finalParts {
			if p := raw.(map[string]interface{}); p["toolCallId"] == "stale-tool" {
				return true, true
			}
		}
		return false, true
	case "writeCalls.map(call=>call.message.parts.find(isToolUIPart)?.approval).filter(approval=>approval=null),":
		out := []interface{}{}
		for _, w := range writes {
			for _, raw := range w.(map[string]interface{})["parts"].([]interface{}) {
				p := raw.(map[string]interface{})
				typ, _ := p["type"].(string)
				if typ == "dynamic-tool" || strings.HasPrefix(typ, "tool-") {
					if p["approval"] != nil {
						out = append(out, p["approval"])
					}
					break
				}
			}
		}
		return out, true
	case "writeCalls.map(call=>call.message.parts[1])":
		out := []interface{}{}
		for _, w := range writes {
			parts := w.(map[string]interface{})["parts"].([]interface{})
			if len(parts) > 1 {
				out = append(out, parts[1])
			} else {
				out = append(out, nil)
			}
		}
		return out, true
	}
	var cur interface{}
	switch {
	case strings.HasPrefix(e, "writeCalls"):
		list := make([]interface{}, len(writes))
		for i, w := range writes {
			list[i] = map[string]interface{}{"message": w}
		}
		cur, e = list, strings.TrimPrefix(e, "writeCalls")
	case strings.HasPrefix(e, "state.message"):
		cur, e = final, strings.TrimPrefix(e, "state.message")
	default:
		e = strings.TrimSuffix(e, ",")
		name := e
		if n := strings.IndexAny(e, ".["); n >= 0 {
			name = e[:n]
		}
		lk, found := c.Lookups[name]
		if !found {
			return nil, false
		}
		e = strings.TrimPrefix(e, name)
		parts := finalParts
		if lk.Write != nil {
			parts = nil
			for _, w := range writes {
				wp, _ := w.(map[string]interface{})["parts"].([]interface{})
				hit := false
				for _, raw := range wp {
					p := raw.(map[string]interface{})
					if p[lk.Write.Field] == lk.Write.Value && p["state"] == lk.Write.State {
						hit = true
					}
				}
				if hit {
					parts = wp
					if lk.Whole {
						cur = w
					}
					break
				}
			}
		}
		if !lk.Whole {
			var matches []interface{}
			for _, raw := range parts {
				if p := raw.(map[string]interface{}); p[lk.Field] == lk.Value {
					matches = append(matches, p)
				}
			}
			switch {
			case lk.Filter:
				cur = matches
			case len(matches) > 0:
				cur = matches[0]
			}
		}
	}
	for e != "" {
		switch {
		case strings.HasPrefix(e, "."):
			e = e[1:]
			n := strings.IndexAny(e, ".[")
			if n < 0 {
				n = len(e)
			}
			if strings.ContainsAny(e[:n], "()=>,") {
				return nil, false
			}
			m, _ := cur.(map[string]interface{})
			cur, e = m[e[:n]], e[n:]
		case strings.HasPrefix(e, "["):
			n := strings.Index(e, "]")
			idx, err := strconv.Atoi(e[1:n])
			if err != nil {
				return nil, false
			}
			l, _ := cur.([]interface{})
			if idx >= len(l) {
				cur = nil
			} else {
				cur = l[idx]
			}
			e = e[n+1:]
		default:
			return nil, false
		}
	}
	return cur, true
}

// TestR73StreamProcessUIMessageStreamTSCases replays every case of
// ui/process-ui-message-stream.test.ts that scripts a chunk stream: each
// write() snapshot, the final message state and the case's other value
// assertions, compared as JSON values (TS undefined = absent).
func TestR73StreamProcessUIMessageStreamTSCases(t *testing.T) {
	var cases []r73streamPUICase
	if err := json.Unmarshal([]byte(r73streamPUICasesJSON), &cases); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%d %s > %s", c.Line, c.Describe, c.It), func(t *testing.T) {
			if c.ResetFirst {
				t.Skip("resetStateOnFirstMessageStart is a client chat option (chat.ts)")
			}
			writes, final, errs := r73streamProcessUI(t, c)
			for _, a := range c.Asserts {
				if a.Expr == "errors" {
					want := []interface{}{}
					for _, err := range errs {
						want = append(want, err.Error())
					}
					if len(errs) != 1 || errs[0].Error() != "test error" {
						t.Errorf("TS %d errors = %v, want [test error]", c.Line, want)
					}
					checked++
					continue
				}
				got, ok := r73streamPUIEval(c, a.Expr, writes, final)
				if !ok {
					t.Logf("TS %d: expect(%s) not evaluated (JS-only state)", c.Line, a.Expr)
					continue
				}
				checked++
				if a.Kind == "toBeDefined" || a.Kind == "toBeUndefined" {
					if (got == nil) != (a.Kind == "toBeUndefined") {
						t.Errorf("TS %d expect(%s).%s: got %v", c.Line, a.Expr, a.Kind, got)
					}
					continue
				}
				want := r73streamJSONClone(t, a.Value)
				got = r73streamJSONClone(t, got)
				if a.Kind == "toHaveLength" {
					l, _ := got.([]interface{})
					got = float64(len(l))
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("TS %d expect(%s).%s differs\n got: %s\nwant: %s", c.Line, a.Expr, a.Kind, r73streamJSON(got), r73streamJSON(want))
				}
			}
		})
	}
	if checked < 85 {
		t.Fatalf("only %d assertions evaluated", checked)
	}
}

// ---------------------------------------------------------------------------
// stream-text.test.ts ports (round 73).
// ---------------------------------------------------------------------------

// r73streamParts runs StreamText and returns the full stream parts.
func r73streamParts(t *testing.T, opts StreamTextOptions) (*StreamTextResult, []provider.StreamChunk) {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	s := r.FullStream()
	var parts []provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	return r, parts
}

func r73streamNames(parts []provider.StreamChunk) []string {
	var out []string
	for i := range parts {
		if n := tsPartName(&parts[i]); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// TS 2144: tool choice enforcement > should surface an error when a different
// tool is called instead of the required tool.
func TestR73StreamToolChoiceDifferentToolCalled(t *testing.T) {
	var errs []error
	var mu sync.Mutex
	executed := false
	tool2 := r71streamTool1(true, nil, nil)
	tool2.Name = "tool2"
	exec := tool2.Execute
	tool2.Execute = func(ctx context.Context, a map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		executed = true
		return exec(ctx, a, o)
	}
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool2", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r72streamTestUsage},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, step), Prompt: "test-input",
		Tools:      []types.Tool{r71streamTool1(false, nil, nil), tool2},
		ToolChoice: types.SpecificToolChoice("tool1"),
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if len(errs) != 1 {
		t.Fatalf("onError calls = %d (%v), want 1", len(errs), errs)
	}
	var v *ToolChoiceViolationError
	if !errors.As(errs[0], &v) {
		t.Fatalf("onError error = %T %v, want ToolChoiceViolationError", errs[0], errs[0])
	}
	if v.Error() != "Model response did not contain a call to the required tool 'tool1'." {
		t.Errorf("message = %q", v.Error())
	}
	if v.ToolChoice.Type != types.ToolChoiceTool || v.ToolChoice.ToolName != "tool1" || v.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("violation = %+v", v)
	}
	if len(v.Content) != 1 {
		t.Fatalf("content = %#v", v.Content)
	}
	if call, ok := v.Content[0].(types.ToolCallContent); !ok || call.ToolCallID != "call-1" || call.ToolName != "tool2" {
		// TS: { type: 'tool-call', toolCallId: 'call-1', toolName: 'tool2', input }
		t.Errorf("content[0] = %#v", v.Content[0])
	}
	if r.FinishReason() != types.FinishReasonError {
		t.Errorf("finishReason = %q, want error", r.FinishReason())
	}
	if executed {
		t.Error("tool2 was executed")
	}
}

// TS 2205: tool choice enforcement > should enforce the tool choice returned
// by prepareStep.
func TestR73StreamToolChoiceFromPrepareStepEnforced(t *testing.T) {
	var errs []error
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "No tool call."},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, step), Prompt: "test-input",
		Tools:      []types.Tool{r71streamTool1(false, nil, nil)},
		ToolChoice: types.AutoToolChoice(),
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			s.ToolChoice = types.RequiredToolChoice()
			return s
		},
		OnError: func(_ context.Context, err error) { errs = append(errs, err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if len(errs) != 1 {
		t.Fatalf("onError calls = %d (%v), want 1", len(errs), errs)
	}
	var v *ToolChoiceViolationError
	if !errors.As(errs[0], &v) || v.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("onError error = %#v, want required ToolChoiceViolationError", errs[0])
	}
}

// TS 2673: result.stream > should send delayed asynchronous tool results
// (snapshot): the tool result is sent before the step's finish-step part.
func TestR73StreamDelayedAsyncToolResult(t *testing.T) {
	tool := r71streamTool1(true, nil, nil)
	exec := tool.Execute
	tool.Title = "Tool 1"
	tool.Execute = func(ctx context.Context, a map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return exec(ctx, a, o)
	}
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input", Tools: []types.Tool{tool}})
	got := r73streamNames(parts)
	want := []string{"start", "start-step", "tool-call:call-1:tool1", "tool-result:call-1:value-result", "finish-step:stop", "finish:stop"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %v\nwant %v", got, want)
	}
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolCall && p.ToolCall.Title != "Tool 1" {
			t.Errorf("tool-call title = %q, want Tool 1", p.ToolCall.Title)
		}
	}
}

// TS 2569: result.stream > should pass through providerMetadata on
// tool-input-start.
func TestR73StreamToolInputStartProviderMetadata(t *testing.T) {
	pmd := json.RawMessage(`{"testProvider":{"someKey":"someValue"}}`)
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool"}, ProviderMetadata: pmd},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{"value":"test"}`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{"value":"test"}`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input"})
	found := false
	for _, p := range parts {
		if p.Type == provider.ChunkTypeToolInputStart {
			found = true
			if string(p.ProviderMetadata) != string(pmd) {
				t.Errorf("tool-input-start providerMetadata = %s", p.ProviderMetadata)
			}
		}
	}
	if !found {
		t.Fatal("no tool-input-start part")
	}
}

// TS 1591: result.stream > should use fallback response metadata when
// response metadata is not provided: finish-step response id comes from the
// id generator and the model id from the model.
func TestR73StreamFallbackResponseMetadata(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
		Internal: &InternalOptions{GenerateID: func() string { return "id-2000" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
	})
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFinishStep {
			if p.Response == nil || p.Response.ID != "id-2000" || p.Response.ModelID != "mock-model-id" || p.Response.Timestamp.IsZero() {
				t.Fatalf("finish-step response = %+v", p.Response)
			}
			return
		}
	}
	t.Fatal("no finish-step part")
}

// TS 1408: result.stream > should send files with providerMetadata.
func TestR73StreamFilesWithProviderMetadata(t *testing.T) {
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("Hello World"), ProviderMetadata: json.RawMessage(`{"testProvider":{"signature":"sig-1"}}`)}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/jpeg", Data: []byte("BAUG")}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input"})
	var files []*types.GeneratedFileContent
	for _, p := range parts {
		if p.Type == provider.ChunkTypeFile {
			files = append(files, p.GeneratedFileContent)
		}
	}
	if len(files) != 2 || files[0].MediaType != "text/plain" || string(files[0].ProviderMetadata) != `{"testProvider":{"signature":"sig-1"}}` ||
		files[1].MediaType != "image/jpeg" || files[1].ProviderMetadata != nil {
		t.Fatalf("file parts = %+v", files)
	}
	if got := r.Files(); len(got) != 2 || string(got[0].ProviderMetadata) != `{"testProvider":{"signature":"sig-1"}}` {
		t.Fatalf("Files() = %+v", got)
	}
}

var r73streamWarning0 = types.Warning{Type: "other", Message: "step 0 warning"}
var r73streamWarning1 = types.Warning{Type: "other", Message: "step 1 warning"}

func r73streamWarningSteps() *testutil.MockLanguageModel {
	return r71streamModel(nil,
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{r73streamWarning0}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		},
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{r73streamWarning1}},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Done."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
		})
}

var r73streamEmptyTool = types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}}

// TS 7720 / 7785: result.warnings and onFinish warnings span all steps; the
// final step keeps its own warnings.
func TestR73StreamWarningsFromAllSteps(t *testing.T) {
	var end OnFinishEvent
	r := r71streamRun(t, StreamTextOptions{
		Model: r73streamWarningSteps(), Prompt: "test-input",
		Tools: []types.Tool{r73streamEmptyTool}, StopWhen: []StopCondition{StepCountIs(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e },
	})
	all := []types.Warning{r73streamWarning0, r73streamWarning1}
	if got := r.Warnings(); !reflect.DeepEqual(got, all) {
		t.Errorf("Warnings() = %+v", got)
	}
	if got := r.FinalStep().Warnings; !reflect.DeepEqual(got, []types.Warning{r73streamWarning1}) {
		t.Errorf("FinalStep().Warnings = %+v", got)
	}
	if !reflect.DeepEqual(end.Warnings, all) {
		t.Errorf("onEnd warnings = %+v", end.Warnings)
	}
	if len(end.Steps) != 2 || !reflect.DeepEqual(end.Steps[1].Warnings, []types.Warning{r73streamWarning1}) {
		t.Errorf("onEnd steps = %d, last warnings %+v", len(end.Steps), end.Steps)
	}
}

// TS 7917: result.providerMetadata resolves with the finish part's metadata.
func TestR73StreamProviderMetadataAccessor(t *testing.T) {
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage, ProviderMetadata: json.RawMessage(`{"testProvider":{"testKey":"testValue"}}`)},
	}
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input"})
	var got interface{}
	if err := json.Unmarshal(r.ProviderMetadata(), &got); err != nil {
		t.Fatalf("ProviderMetadata() = %s: %v", r.ProviderMetadata(), err)
	}
	if !reflect.DeepEqual(got, map[string]interface{}{"testProvider": map[string]interface{}{"testKey": "testValue"}}) {
		t.Fatalf("ProviderMetadata() = %v", got)
	}
}

// TS 8375 / 8384: result.sources holds the sources of every step, in order,
// with their provider metadata.
func TestR73StreamSourcesFromAllSteps(t *testing.T) {
	src := func(id, url, title string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: id, URL: url, Title: title, ProviderMetadata: json.RawMessage(`{"provider":{"custom":"` + id + `"}}`)}}
	}
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			src("source-0", "https://example.com/0", "Source 0"),
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		},
		[]provider.StreamChunk{
			src("source-1", "https://example.com/1", "Source 1"),
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Done."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			src("source-2", "https://example.com/2", "Source 2"),
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
		})
	var end OnFinishEvent
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{r73streamEmptyTool}, StopWhen: []StopCondition{StepCountIs(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e }})
	var ids []string
	for _, s := range r.Sources() {
		ids = append(ids, s.ID+"|"+s.URL+"|"+s.Title+"|"+string(s.ProviderMetadata))
	}
	want := []string{
		`source-0|https://example.com/0|Source 0|{"provider":{"custom":"source-0"}}`,
		`source-1|https://example.com/1|Source 1|{"provider":{"custom":"source-1"}}`,
		`source-2|https://example.com/2|Source 2|{"provider":{"custom":"source-2"}}`,
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("Sources() = %v", ids)
	}
	if len(end.Sources) != 3 || len(r.Steps()) != 2 || len(r.Steps()[0].Sources) != 1 || len(r.Steps()[1].Sources) != 2 {
		t.Fatalf("onEnd sources %d, step sources %+v", len(end.Sources), r.Steps())
	}
}

// TS 8563 / 9015 / 13557: files from all steps reach onFinish and the steps.
func TestR73StreamFilesFromAllStepsToOnFinish(t *testing.T) {
	file := func(data, mt string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: mt, Data: []byte(data)}}
	}
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			file("Step 0 file", "text/plain"),
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		},
		[]provider.StreamChunk{
			file("Step 1 file", "text/plain"),
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Done."},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
		})
	var end OnFinishEvent
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{r73streamEmptyTool}, StopWhen: []StopCondition{StepCountIs(3)},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e }})
	if len(end.Files) != 2 || string(end.Files[0].Data) != "Step 0 file" || string(end.Files[1].Data) != "Step 1 file" {
		t.Fatalf("onEnd files = %+v", end.Files)
	}
	steps := r.Steps()
	if len(steps) != 2 || len(steps[0].Files) != 1 || len(steps[1].Files) != 1 || string(steps[1].Files[0].Data) != "Step 1 file" {
		t.Fatalf("step files = %+v", steps)
	}
	// TS result.files flattens the files of every step.
	if got := r.Files(); len(got) != 2 || string(got[1].Data) != "Step 1 file" {
		t.Fatalf("Files() = %+v", got)
	}
}

// TS 9571: toolCallConcurrency > should reject invalid concurrency
// synchronously.
func TestR73StreamInvalidToolCallConcurrency(t *testing.T) {
	zero := 0
	_, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input", ToolCallConcurrency: &zero})
	if err == nil || err.Error() != "Invalid argument for parameter toolCallConcurrency: toolCallConcurrency must be >= 1" {
		t.Fatalf("err = %v", err)
	}
}

// TS 8141: result.request includes the request messages when
// include.requestMessages is set (TS ModelMessage prompt, not the converted
// provider prompt).
func TestR73StreamRequestMessagesRetention(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input", Include: &IncludeOptions{RequestBody: true, RequestMessages: true}})
	want := r71streamUserMessages("test-input")
	if got := r.Request().Messages; !reflect.DeepEqual(got, want) {
		t.Errorf("Request().Messages = %#v", got)
	}
	if got := r.Steps()[0].Request.Messages; !reflect.DeepEqual(got, want) {
		t.Errorf("steps[0].Request.Messages = %#v", got)
	}
}

func r73streamCounterIDs(prefix string) func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		id := prefix + "-" + strconv.Itoa(n)
		n++
		return id
	}
}

func r73streamIDParts(parts []provider.StreamChunk, kinds ...provider.ChunkType) []string {
	var out []string
	for _, p := range parts {
		for _, k := range kinds {
			if p.Type == k {
				out = append(out, string(p.Type)+":"+p.ID+":"+p.Text)
			}
		}
	}
	return out
}

// TS 18454 (and the remap case before it): multi-step part IDs. Unique
// provider IDs are kept; a reasoning ID that repeats in a later step is
// remapped to the next generated ID (TS mockId: id-0 and id-1 are the step
// response ids, so the remapped id is id-2), and the step texts are kept.
func TestR73StreamMultiStepPartIDs(t *testing.T) {
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}}
	call := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}, RawArguments: `{ "value": "test" }`}}
	t.Run("unique ids kept", func(t *testing.T) {
		model := r71streamModel(nil,
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "0"}, {Type: provider.ChunkTypeText, ID: "0", Text: "Hello"}, {Type: provider.ChunkTypeTextEnd, ID: "0"},
				call, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage}},
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "World"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}})
		_, parts := r73streamParts(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(2)}})
		got := r73streamIDParts(parts, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd)
		want := []string{"text-start:0:", "text:0:Hello", "text-end:0:", "text-start:1:", "text:1:World", "text-end:1:"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("text parts = %v\nwant %v", got, want)
		}
	})
	t.Run("repeated reasoning id remapped", func(t *testing.T) {
		model := r71streamModel(nil,
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "0"}, {Type: provider.ChunkTypeReasoning, ID: "0", Text: "thinking"}, {Type: provider.ChunkTypeReasoningEnd, ID: "0"},
				call, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage}},
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "0"}, {Type: provider.ChunkTypeReasoning, ID: "0", Text: "thinking more"}, {Type: provider.ChunkTypeReasoningEnd, ID: "0"},
				{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "It is sunny."}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}})
		r, parts := r73streamParts(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(2)},
			Internal: &InternalOptions{GenerateID: r73streamCounterIDs("id")}})
		for _, p := range parts {
			if p.Type == provider.ChunkTypeError {
				t.Fatalf("error part: %v", p.Err)
			}
		}
		got := r73streamIDParts(parts, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoning, provider.ChunkTypeReasoningEnd)
		want := []string{"reasoning-start:0:", "reasoning:0:thinking", "reasoning-end:0:", "reasoning-start:id-2:", "reasoning:id-2:thinking more", "reasoning-end:id-2:"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("reasoning parts = %v\nwant %v", got, want)
		}
		steps := r.Steps()
		if len(steps) != 2 || steps[0].ReasoningText != "thinking" || steps[1].ReasoningText != "thinking more" {
			t.Fatalf("steps reasoning = %+v", steps)
		}
	})
}

func r73streamDur(d time.Duration) *time.Duration { return &d }

// TS 19961 / 20027 / 20232: step timeouts are cleared when a multi-step run
// completes normally; totalMs, stepMs and chunkMs together run a normal
// stream to completion with a deadline-bearing context.
func TestR73StreamTimeoutsOnNormalCompletion(t *testing.T) {
	t.Run("step timeout cleared in multi-step run", func(t *testing.T) {
		var mu sync.Mutex
		var ctxs []context.Context
		steps := [][]provider.StreamChunk{
			r71streamToolCallStep([2]string{"call-1", "test"}),
			{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Final response"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}},
		}
		model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			defer mu.Unlock()
			ctxs = append(ctxs, ctx)
			return testutil.NewMockTextStream(steps[len(ctxs)-1]), nil
		}}
		var errs []error
		r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input",
			Tools:    []types.Tool{r71streamTool1(true, nil, nil)},
			Timeout:  &TimeoutConfig{PerStep: r73streamDur(50 * time.Millisecond)},
			StopWhen: []StopCondition{StepCountIs(2)},
			OnError:  func(_ context.Context, err error) { errs = append(errs, err) },
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.ConsumeStream(); err != nil {
			t.Fatalf("ConsumeStream: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
		if len(ctxs) != 2 || len(errs) != 0 || r.Text() != "Final response" || r.FinishReason() != types.FinishReasonStop {
			t.Fatalf("calls %d errs %v text %q finish %q", len(ctxs), errs, r.Text(), r.FinishReason())
		}
		for i, c := range ctxs {
			if errors.Is(c.Err(), context.DeadlineExceeded) || errors.Is(context.Cause(c), context.DeadlineExceeded) {
				t.Errorf("step %d context timed out after completion: %v", i, context.Cause(c))
			}
		}
	})
	for _, cfg := range []struct {
		name string
		tc   TimeoutConfig
	}{
		{"total and step", TimeoutConfig{Total: r73streamDur(10 * time.Second), PerStep: r73streamDur(5 * time.Second)}},
		{"chunk, total and step", TimeoutConfig{Total: r73streamDur(30 * time.Second), PerStep: r73streamDur(10 * time.Second), PerChunk: r73streamDur(5 * time.Second)}},
	} {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			var got context.Context
			model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(ctx context.Context, _ *provider.GenerateOptions) (provider.TextStream, error) {
				got = ctx
				return testutil.NewMockTextStream(r71streamHelloWorld), nil
			}}
			r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Timeout: &cfg.tc})
			if got == nil || r.Text() != "Hello, world!" {
				t.Fatalf("ctx %v text %q", got, r.Text())
			}
			if _, ok := got.Deadline(); !ok {
				t.Errorf("doStream context has no deadline")
			}
		})
	}
}

func r73streamTextStep(deltas ...string) []provider.StreamChunk {
	out := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}}
	for _, d := range deltas {
		out = append(out, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	return append(out, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
		provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage})
}

var r73streamValueObjectSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}})

// TS 23305 / 23345: object output > the parsed output is available while an
// onEnd callback is still running (output is not delayed by onEnd), and onEnd
// receives the same parsed value.
func TestR73StreamOutputNotDelayedByOnEnd(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var callbackOutput interface{}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, r73streamTextStep(`{ "value": "Hello, world!" }`)), Prompt: "prompt",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r73streamValueObjectSchema}),
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			callbackOutput = e.Output
			close(started)
			<-release
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = r.ConsumeStream() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("onEnd not called")
	}
	want := map[string]interface{}{"value": "Hello, world!"}
	if got := r.Output(); !reflect.DeepEqual(got, want) {
		t.Errorf("Output() during onEnd = %#v", got)
	}
	close(release)
	_ = r.ConsumeStream()
	if !reflect.DeepEqual(callbackOutput, want) {
		t.Errorf("onEnd output = %#v", callbackOutput)
	}
}

// TS 24087: json output > should stream null and empty string values.
func TestR73StreamJSONOutputNullAndEmptyString(t *testing.T) {
	for _, value := range []interface{}{nil, ""} {
		b, _ := json.Marshal(value)
		r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r73streamTextStep(string(b))), Prompt: "prompt", Output: JSONOutput(JSONOutputOptions{})})
		if err := r.OutputErr(); err != nil {
			t.Fatalf("%s: OutputErr = %v", b, err)
		}
		if got := r.Output(); !reflect.DeepEqual(got, value) {
			t.Errorf("%s: Output() = %#v", b, got)
		}
		if got := r.PartialOutput(); !reflect.DeepEqual(got, value) {
			t.Errorf("%s: PartialOutput() = %#v", b, got)
		}
	}
}

// TS 26308 (abort signal > abort during tool execution): the full stream ends
// with start, start-step, the tool call and the abort part (no tool result,
// no finish-step), onError is not called and onAbort has no steps.
func TestR73StreamAbortDuringToolExecutionFullStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		cancel()
		return "result1", nil
	}}
	var errs []error
	var aborts []GenerateTextAbortEvent
	r, err := StreamText(ctx, StreamTextOptions{
		Model: r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "value"}), r71streamHelloWorld), Prompt: "test-input",
		Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(3)}, Internal: r71streamInternal,
		OnError:      func(_ context.Context, err error) { errs = append(errs, err) },
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) { aborts = append(aborts, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	s := r.FullStream()
	var parts []provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		parts = append(parts, *c)
	}
	got := r73streamNames(parts)
	want := []string{"start", "start-step", "tool-call:call-1:tool1", "abort"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parts = %v\nwant %v", got, want)
	}
	if len(errs) != 0 {
		t.Errorf("onError calls = %v", errs)
	}
	if len(aborts) != 1 || aborts[0].CallID != "test-telemetry-call-id" || len(aborts[0].Steps) != 0 {
		t.Errorf("onAbort = %+v", aborts)
	}
}

// TS 27419 / 27626: provider-executed dynamic tools with input streaming:
// dynamic, providerExecuted and providerMetadata on the full-stream tool parts
// and in the content.
func TestR73StreamProviderExecutedDynamicTool(t *testing.T) {
	pm := json.RawMessage(`{"anthropic":{"serverName":"echo"}}`)
	pmMap := map[string]interface{}{"anthropic": map[string]interface{}{"serverName": "echo"}}
	input := map[string]interface{}{"city": "San Francisco"}
	output := map[string]interface{}{"status": "success", "text": "The weather in San Francisco is 72°F"}
	step := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}, ProviderMetadata: pm},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "city": "San Francisco" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", Arguments: input, RawArguments: `{ "city": "San Francisco" }`, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true, ProviderMetadata: pmMap}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "cityAttractions", Input: input, Result: output, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true, ProviderMetadata: pmMap}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, step), Prompt: "test-input", Internal: r71streamInternal})
	want := []string{"start", "start-step", "tool-input-start", "tool-input-delta", "tool-input-end", "tool-call:call-1:cityAttractions", "tool-result:call-1:" + fmt.Sprint(output), "finish-step:stop", "finish:stop"}
	if got := r73streamNames(parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %v\nwant %v", got, want)
	}
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeToolInputStart:
			if !p.ToolCall.Dynamic || !p.ToolCall.ProviderExecuted || string(p.ProviderMetadata) != string(pm) {
				t.Errorf("tool-input-start = %+v pm %s", p.ToolCall, p.ProviderMetadata)
			}
		case provider.ChunkTypeToolCall:
			if !p.ToolCall.Dynamic || !p.ToolCall.ProviderExecuted || !reflect.DeepEqual(p.ToolCall.ProviderMetadata, pmMap) || !reflect.DeepEqual(p.ToolCall.Arguments, input) {
				t.Errorf("tool-call = %+v", p.ToolCall)
			}
		case provider.ChunkTypeToolResult:
			if !p.ToolResult.Dynamic || !p.ToolResult.ProviderExecuted || !reflect.DeepEqual(p.ToolResult.ProviderMetadata, pmMap) || !reflect.DeepEqual(p.ToolResult.Input, input) || !reflect.DeepEqual(p.ToolResult.Result, output) {
				t.Errorf("tool-result = %+v", p.ToolResult)
			}
		}
	}
	content := r.Content()
	if len(content) != 2 {
		t.Fatalf("content = %#v", content)
	}
	call, ok := content[0].(types.ToolCallContent)
	if !ok || call.ToolCallID != "call-1" || call.ToolName != "cityAttractions" || !call.Dynamic || !call.ProviderExecuted || string(call.ProviderMetadata) != string(pm) || !reflect.DeepEqual(call.Arguments, input) {
		t.Errorf("content[0] = %#v", content[0])
	}
	res, ok := content[1].(types.ToolResultContent)
	// Go content carries the result as Output (TS content output; Result is
	// the deprecated field).
	if !ok || res.ToolCallID != "call-1" || !res.Dynamic || !res.ProviderExecuted || string(res.ProviderMetadata) != string(pm) ||
		res.Output == nil || res.Output.Type != types.ToolResultOutputJSON || !reflect.DeepEqual(res.Output.Value, output) || !reflect.DeepEqual(res.Input, input) {
		t.Errorf("content[1] = %#v", content[1])
	}
}

// TS 29689: logWarnings is called once for each step with that step's
// warnings and the provider/model ids.
func TestR73StreamLogWarningsPerStep(t *testing.T) {
	setupLogWarnings(t)
	var mu sync.Mutex
	var calls []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) {
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
	})
	r71streamRun(t, StreamTextOptions{Model: r73streamWarningSteps(), Prompt: "test-input", Tools: []types.Tool{r73streamEmptyTool}, StopWhen: []StopCondition{StepCountIs(3)}})
	want := []LogWarningsOptions{
		{Warnings: []types.Warning{r73streamWarning0}, Provider: "mock-provider", Model: "mock-model-id"},
		{Warnings: []types.Warning{r73streamWarning1}, Provider: "mock-provider", Model: "mock-model-id"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("logWarnings calls = %+v", calls)
	}
}

// r73streamPromptModel answers every call with steps[i] (last repeats) and
// records each call's prompt messages.
func r73streamPromptModel(prompts *[][]types.Message, steps ...[]provider.StreamChunk) *testutil.MockLanguageModel {
	var mu sync.Mutex
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		i := len(*prompts)
		*prompts = append(*prompts, o.Prompt.Messages)
		if i >= len(steps) {
			i = len(steps) - 1
		}
		return testutil.NewMockTextStream(steps[i]), nil
	}}
}

func r73streamToolMessageOutput(t *testing.T, msgs []types.Message) *types.ToolResultOutput {
	t.Helper()
	for _, m := range msgs {
		if m.Role != types.RoleTool {
			continue
		}
		for _, c := range m.Content {
			if tr, ok := c.(types.ToolResultContent); ok && tr.ToolCallID == "call-1" {
				return tr.Output
			}
		}
	}
	t.Fatalf("no tool-result for call-1 in %#v", msgs)
	return nil
}

// TS 31285: tool execution approval > when a single tool has a user-defined
// approval function > should call the approval function with the correct
// input and options (Go RuntimeContext stays nil, known difference).
func TestR73StreamApprovalFunctionInputAndOptions(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value-needs-approval"}, RawArguments: `{ "value": "value-needs-approval" }`}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value-no-approval"}, RawArguments: `{ "value": "value-no-approval" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	}
	type call struct {
		input map[string]interface{}
		opts  types.SingleToolApprovalOptions
	}
	var mu sync.Mutex
	var calls []call
	approval := types.SingleToolApprovalFunc(func(args map[string]interface{}, o types.SingleToolApprovalOptions) types.ToolApprovalResult {
		mu.Lock()
		calls = append(calls, call{args, o})
		mu.Unlock()
		if args["value"] == "value-needs-approval" {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	})
	r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", StopWhen: []StopCondition{StepCountIs(3)},
		Internal: r72streamIDs(), Tools: []types.Tool{r71streamTool1(true, nil, nil)},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": approval}})
	if len(calls) != 2 {
		t.Fatalf("approval calls = %d", len(calls))
	}
	for i, want := range []struct{ value, id string }{{"value-needs-approval", "call-1"}, {"value-no-approval", "call-2"}} {
		c := calls[i]
		if c.input["value"] != want.value || c.opts.ToolCallID != want.id || c.opts.ToolContext != nil || !reflect.DeepEqual(c.opts.Messages, r71streamUserMessages("test-input")) {
			t.Errorf("call %d = %+v", i, c)
		}
	}
}

// TS 31432 / 31523 / 31839 / 31966 / 32017 / 32048: tool execution approval
// > when a call that needs approval is approved (resume from messages).
func TestR73StreamApprovedResume(t *testing.T) {
	approval := map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval}
	t.Run("sandbox and options reach the executed tool (31432)", func(t *testing.T) {
		sandbox := &struct{ name string }{"test sandbox"}
		var got []types.ToolExecutionOptions
		tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(_ context.Context, args map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			got = append(got, o)
			return "result1", nil
		}}
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, r72streamHelloStep), Messages: r71genApprovalHistory(true, ""),
			StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool}, ToolApproval: approval, ExperimentalSandbox: sandbox})
		if len(got) != 1 || got[0].ToolCallID != "call-1" || got[0].ExperimentalSandbox != sandbox || len(got[0].Messages) == 0 {
			t.Fatalf("execute options = %+v", got)
		}
	})
	t.Run("prepareStep receives the initial tool result (31523)", func(t *testing.T) {
		var seen [][]types.Message
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, r72streamHelloStep), Messages: r71genApprovalHistory(true, ""),
			StopWhen: []StopCondition{StepCountIs(3)}, Tools: []types.Tool{r71streamTool1(true, nil, nil)}, ToolApproval: approval,
			PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
				seen = append(seen, append([]types.Message(nil), s.ResponseMessages...))
				return PrepareStepOptions{}
			}})
		if len(seen) != 1 || len(seen[0]) != 1 || seen[0][0].Role != types.RoleTool {
			t.Fatalf("prepareStep responseMessages = %#v", seen)
		}
		out := r73streamToolMessageOutput(t, seen[0])
		if out == nil || out.Type != types.ToolResultOutputText || out.Value != "value-result" {
			t.Fatalf("tool-result output = %+v", out)
		}
	})
	t.Run("a throwing tool is error-text in the continuation prompt (31839)", func(t *testing.T) {
		var prompts [][]types.Message
		r71streamRun(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, r72streamHelloStep), Messages: r71genApprovalHistory(true, ""),
			StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), ToolApproval: approval,
			Tools: []types.Tool{r71streamTool1(true, errors.New("No valid token for plugin"), nil)}})
		if len(prompts) != 1 || len(prompts[0]) != 3 {
			t.Fatalf("prompts = %#v", prompts)
		}
		if a := prompts[0][1]; a.Role != types.RoleAssistant || len(a.Content) != 1 {
			t.Fatalf("assistant message = %#v (the approval request is not sent to the model)", a)
		}
		out := r73streamToolMessageOutput(t, prompts[0])
		if out == nil || out.Type != types.ToolResultOutputErrorText || out.Value != "Error: No valid token for plugin" {
			t.Fatalf("tool-result output = %+v", out)
		}
	})
	t.Run("preliminary results: prompt, response messages and full stream (31966/32017/32048)", func(t *testing.T) {
		tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: types.StreamingExecutor(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
			return func(yield func(interface{}, error) bool) {
				if !yield("preliminary-result", nil) {
					return
				}
				yield("final-result", nil)
			}
		})}
		var prompts [][]types.Message
		r, parts := r73streamParts(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, r72streamHelloStep), Messages: r71genApprovalHistory(true, ""),
			StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool}, ToolApproval: approval})
		if len(prompts) != 1 || len(prompts[0]) != 3 {
			t.Fatalf("prompts = %#v", prompts)
		}
		if out := r73streamToolMessageOutput(t, prompts[0]); out == nil || out.Type != types.ToolResultOutputText || out.Value != "final-result" {
			t.Fatalf("prompt tool-result output = %+v", out)
		}
		rm := r.ResponseMessages()
		if len(rm) != 2 || rm[0].Role != types.RoleTool || rm[1].Role != types.RoleAssistant {
			t.Fatalf("response messages = %#v", rm)
		}
		if out := r73streamToolMessageOutput(t, rm[:1]); out == nil || out.Value != "final-result" {
			t.Fatalf("response tool-result output = %+v", out)
		}
		var got []string
		for _, p := range parts {
			if p.Type == provider.ChunkTypeToolResult {
				got = append(got, fmt.Sprintf("%v:%v", p.ToolResult.Result, p.ToolResult.Preliminary))
			}
		}
		if want := []string{"preliminary-result:true", "final-result:true", "final-result:false"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("tool results = %v", got)
		}
		names := r73streamNames(parts)
		if len(names) < 5 || names[0] != "start" || names[4] != "start-step" {
			t.Fatalf("parts = %v (tool results come before the first start-step)", names)
		}
	})
}

// TS 32527: tool execution approval > should emit denied output without
// duplicating an existing execution-denied result.
func TestR73StreamDeniedWithExistingExecutionDeniedResult(t *testing.T) {
	executed := false
	tool := r71streamTool1(true, nil, nil)
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		executed = true
		return nil, nil
	}
	history := r71genApprovalHistory(false, "User denied the request", types.ToolResultContent{ToolCallID: "call-1", ToolName: "tool1",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User denied the request"}})
	var prompts [][]types.Message
	opts := StreamTextOptions{Model: r73streamPromptModel(&prompts, r72streamHelloStep), Messages: history,
		StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval}, OnError: func(context.Context, error) {}}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	ch, errs := r.ToUIMessageStream(context.Background())
	var denied []UIMessageChunk
	for c := range ch {
		if c["type"] == "tool-output-denied" {
			denied = append(denied, c)
		}
	}
	for range errs {
	}
	if len(denied) != 1 || denied[0]["toolCallId"] != "call-1" || len(denied[0]) != 2 {
		t.Errorf("denied chunks = %v", denied)
	}
	pb, _ := json.Marshal(prompts)
	if n := strings.Count(string(pb), "execution-denied"); n != 1 {
		t.Errorf("execution-denied in prompts = %d, want 1: %s", n, pb)
	}
	rb, _ := json.Marshal(r.ResponseMessages())
	if n := strings.Count(string(rb), "execution-denied"); n != 0 {
		t.Errorf("execution-denied in response messages = %d, want 0: %s", n, rb)
	}
	if executed {
		t.Error("denied tool was executed")
	}
}

// TS 32986 / 33032 / 33157 / 33203 / 33215: provider-executed tool (MCP)
// approval resumed from messages: the approval response reaches the model
// exactly once (the request is not sent), and the content holds the provider
// result (approved) or only the model's text (denied).
func TestR73StreamProviderExecutedApprovalResume(t *testing.T) {
	mcp := types.Tool{Name: "mcp_tool", Type: types.ToolTypeProviderDefined, ProviderID: "test.mcp_tool", ProviderExecuted: true,
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}}}
	history := func(approved bool, reason string) []types.Message {
		return []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Shorten this URL: https://example.com"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, ProviderExecuted: true},
				types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "mcp-approval-1", Approved: approved, Reason: reason, ProviderExecuted: true, ProviderExecutedSet: true}}},
		}
	}
	checkPrompt := func(t *testing.T, prompt []types.Message, approved bool, reason string) {
		t.Helper()
		if len(prompt) != 3 {
			t.Fatalf("prompt = %#v", prompt)
		}
		if a := prompt[1]; a.Role != types.RoleAssistant || len(a.Content) != 1 {
			t.Fatalf("assistant message = %#v, want only the tool call", a)
		} else if c, ok := a.Content[0].(types.ToolCallContent); !ok || c.ToolCallID != "mcp-call-1" || !c.ProviderExecuted {
			t.Fatalf("assistant content = %#v", a.Content[0])
		}
		tm := prompt[2]
		if tm.Role != types.RoleTool || len(tm.Content) != 1 {
			t.Fatalf("tool message = %#v, want exactly one approval response", tm)
		}
		resp, ok := tm.Content[0].(types.ToolApprovalResponseContent)
		if !ok || resp.ApprovalID != "mcp-approval-1" || resp.Approved != approved || resp.Reason != reason {
			t.Fatalf("tool content = %#v", tm.Content[0])
		}
	}
	t.Run("approved", func(t *testing.T) {
		step := []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, RawArguments: `{ "query": "test" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Result: map[string]interface{}{"shortened_url": "https://short.url/abc"}, ProviderExecuted: true, ProviderExecutedSet: true}},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Here is your shortened URL: https://short.url/abc"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
		}
		var prompts [][]types.Message
		r := r71streamRun(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, step), Messages: history(true, ""), Tools: []types.Tool{mcp}, Internal: r72streamIDs()})
		if len(prompts) != 1 {
			t.Fatalf("model calls = %d", len(prompts))
		}
		checkPrompt(t, prompts[0], true, "")
		content := r.Content()
		if len(content) != 3 {
			t.Fatalf("content = %#v", content)
		}
		if c, ok := content[0].(types.ToolCallContent); !ok || c.ToolCallID != "mcp-call-1" || !c.ProviderExecuted || !reflect.DeepEqual(c.Arguments, map[string]interface{}{"query": "test"}) {
			t.Errorf("content[0] = %#v", content[0])
		}
		if c, ok := content[1].(types.ToolResultContent); !ok || c.ToolCallID != "mcp-call-1" || !c.ProviderExecuted || c.Output == nil ||
			!reflect.DeepEqual(c.Output.Value, map[string]interface{}{"shortened_url": "https://short.url/abc"}) || !reflect.DeepEqual(c.Input, map[string]interface{}{"query": "test"}) {
			t.Errorf("content[1] = %#v", content[1])
		}
		if c, ok := content[2].(types.TextContent); !ok || c.Text != "Here is your shortened URL: https://short.url/abc" {
			t.Errorf("content[2] = %#v", content[2])
		}
	})
	t.Run("denied", func(t *testing.T) {
		step := r73streamTextStep("I understand. The tool execution was not approved.")
		var prompts [][]types.Message
		r := r71streamRun(t, StreamTextOptions{Model: r73streamPromptModel(&prompts, step), Messages: history(false, "User denied the request"), Tools: []types.Tool{mcp}, Internal: r72streamIDs()})
		if len(prompts) != 1 {
			t.Fatalf("model calls = %d", len(prompts))
		}
		checkPrompt(t, prompts[0], false, "User denied the request")
		content := r.Content()
		if len(content) != 1 {
			t.Fatalf("content = %#v", content)
		}
		if c, ok := content[0].(types.TextContent); !ok || c.Text != "I understand. The tool execution was not approved." {
			t.Errorf("content[0] = %#v", content[0])
		}
		if r.FinishReason() != types.FinishReasonStop {
			t.Errorf("finishReason = %q", r.FinishReason())
		}
	})
}

// TS 15323: onFinish exposes the deprecated AI SDK 6 final-step properties
// (reasoning, reasoningText, request, response, providerMetadata) equal to
// the final step's.
func TestR73StreamOnEndFinalStepProperties(t *testing.T) {
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningStart, ID: "r"}, {Type: provider.ChunkTypeReasoning, ID: "r", Text: "thinking"}, {Type: provider.ChunkTypeReasoningEnd, ID: "r"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage, ProviderMetadata: json.RawMessage(`{"p":{"step":0}}`)},
		},
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id", Timestamp: time.Unix(10, 0)}},
			{Type: provider.ChunkTypeReasoningStart, ID: "r"}, {Type: provider.ChunkTypeReasoning, ID: "r", Text: "final thought"}, {Type: provider.ChunkTypeReasoningEnd, ID: "r"},
			{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage, ProviderMetadata: json.RawMessage(`{"p":{"step":1}}`)},
		})
	var end OnFinishEvent
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{r71streamTool1(true, nil, nil)}, StopWhen: []StopCondition{StepCountIs(3)},
		Include:    &IncludeOptions{RequestMessages: true},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) { end = e }})
	final := r.FinalStep()
	if final.ReasoningText != "final thought" {
		t.Fatalf("final step reasoning = %q", final.ReasoningText)
	}
	if !reflect.DeepEqual(end.Reasoning, final.Reasoning) || end.ReasoningText != final.ReasoningText {
		t.Errorf("onEnd reasoning = %+v / %q", end.Reasoning, end.ReasoningText)
	}
	if !reflect.DeepEqual(end.Request.Messages, final.Request.Messages) || !reflect.DeepEqual(end.Request.Body, final.Request.Body) || len(end.Request.Messages) != 3 {
		t.Errorf("onEnd request = %+v, final %+v", end.Request, final.Request)
	}
	if end.Response.ID != "id-1" || end.Response.ID != final.Response.ID || end.Response.ModelID != final.Response.ModelID || !end.Response.Timestamp.Equal(final.Response.Timestamp) {
		t.Errorf("onEnd response = %+v, final %+v", end.Response, final.Response)
	}
	if !reflect.DeepEqual(end.ProviderMetadata, final.ProviderMetadata) || !reflect.DeepEqual(end.ProviderMetadata, map[string]interface{}{"p": map[string]interface{}{"step": float64(1)}}) {
		t.Errorf("onEnd providerMetadata = %v, final %v", end.ProviderMetadata, final.ProviderMetadata)
	}
}

// r73streamCtxRecorder records the runtime and tools context each telemetry
// lifecycle event receives.
type r73streamCtxRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu      sync.Mutex
	runtime []interface{}
	tools   []interface{}
}

func (r *r73streamCtxRecorder) add(rt, tc map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runtime = append(r.runtime, rt)
	r.tools = append(r.tools, tc)
}

func (r *r73streamCtxRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.add(e.RuntimeContext, e.ToolsContext)
	return ctx
}

func (r *r73streamCtxRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.add(e.RuntimeContext, e.ToolsContext)
	return ctx
}

func (r *r73streamCtxRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.add(e.RuntimeContext, e.ToolsContext)
}

func (r *r73streamCtxRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.add(e.RuntimeContext, e.ToolsContext)
}

// TS 9784 and the telemetry context cases after it: callbacks receive the
// full runtimeContext, telemetry only the IncludeRuntimeContext /
// IncludeToolsContext keys (nothing by default).
func TestR73StreamRuntimeAndToolsContextInCallbacksAndTelemetry(t *testing.T) {
	runtimeCtx := map[string]interface{}{"userId": "user-123", "requestId": "request-123"}
	weather := types.Tool{Name: "weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"apiKey": map[string]interface{}{"type": "string"}, "region": map[string]interface{}{"type": "string"}}, "required": []interface{}{"apiKey", "region"}}),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		}}
	toolsCtx := map[string]interface{}{"weather": map[string]interface{}{"apiKey": "secret-api-key", "region": "eu"}}
	for _, tc := range []struct {
		name        string
		settings    TelemetrySettings
		wantRuntime interface{}
		wantTools   interface{}
	}{
		{"included keys", TelemetrySettings{IncludeRuntimeContext: map[string]bool{"requestId": true}, IncludeToolsContext: map[string]map[string]bool{"weather": {"region": true}}},
			map[string]interface{}{"requestId": "request-123"}, map[string]interface{}{"weather": map[string]interface{}{"region": "eu"}}},
		{"excluded by default", TelemetrySettings{}, map[string]interface{}{}, map[string]interface{}{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r73streamCtxRecorder{}
			settings := tc.settings
			settings.Integrations = []telemetry.TelemetryIntegration{rec}
			var mu sync.Mutex
			var callbackCtxs []interface{}
			push := func(v interface{}) {
				mu.Lock()
				callbackCtxs = append(callbackCtxs, v)
				mu.Unlock()
			}
			r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r71streamHelloWorld), Prompt: "test-input",
				RuntimeContext: runtimeCtx, Tools: []types.Tool{weather}, ToolsContext: toolsCtx, Telemetry: &settings,
				OnStart:           func(_ context.Context, e OnStartEvent) { push(e.RuntimeContext) },
				OnStepStart:       func(_ context.Context, e OnStepStartEvent) { push(e.RuntimeContext) },
				OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) { push(e.RuntimeContext) },
				OnEndEvent:        func(_ context.Context, e OnFinishEvent) { push(e.RuntimeContext) },
			})
			want := []interface{}{runtimeCtx, runtimeCtx, runtimeCtx, runtimeCtx}
			if !reflect.DeepEqual(callbackCtxs, want) {
				t.Errorf("callback runtime contexts = %v", callbackCtxs)
			}
			for i := range rec.runtime {
				if !reflect.DeepEqual(r73streamJSONClone(t, rec.runtime[i]), r73streamJSONClone(t, tc.wantRuntime)) {
					t.Errorf("telemetry runtime[%d] = %#v, want %#v", i, rec.runtime[i], tc.wantRuntime)
				}
				if !reflect.DeepEqual(r73streamJSONClone(t, rec.tools[i]), r73streamJSONClone(t, tc.wantTools)) {
					t.Errorf("telemetry tools[%d] = %#v, want %#v", i, rec.tools[i], tc.wantTools)
				}
			}
			if len(rec.runtime) != 4 {
				t.Errorf("telemetry events = %d, want 4", len(rec.runtime))
			}
		})
	}
}

// TS 10597 / 10803: onStepStart exposes providerOptions and runtimeContext,
// and the toolsContext a prepareStep returns reaches later prepareStep calls,
// both step-start events and the tool execution.
func TestR73StreamStepStartContextsAndPrepareStepToolsContext(t *testing.T) {
	label := func(v string) map[string]interface{} {
		return map[string]interface{}{"tool1": map[string]interface{}{"label": v}}
	}
	var mu sync.Mutex
	var prepared []map[string]interface{}
	var starts []OnStepStartEvent
	var recorded interface{}
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema,
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"label": map[string]interface{}{"type": "string"}}, "required": []interface{}{"label"}}),
		Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
			recorded = o.ToolContext
			return "test-result", nil
		}}
	model := r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "test"}), r73streamTextStep("Done."))
	r71streamRun(t, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, ToolsContext: label("initial"), StopWhen: []StopCondition{StepCountIs(3)},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}},
		RuntimeContext:  map[string]interface{}{"userId": "test-user"},
		PrepareStep: func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepared = append(prepared, s.ToolsContext)
			mu.Unlock()
			if s.StepNumber == 0 {
				return PrepareStepOptions{ToolsContext: label("updated")}
			}
			return PrepareStepOptions{}
		},
		OnStepStart: func(_ context.Context, e OnStepStartEvent) {
			mu.Lock()
			starts = append(starts, e)
			mu.Unlock()
		},
	})
	if !reflect.DeepEqual(prepared, []map[string]interface{}{label("initial"), label("updated")}) {
		t.Errorf("prepareStep toolsContexts = %v", prepared)
	}
	if len(starts) != 2 {
		t.Fatalf("step starts = %d", len(starts))
	}
	for i, e := range starts {
		if !reflect.DeepEqual(e.ToolsContext, label("updated")) {
			t.Errorf("step %d toolsContext = %v", i, e.ToolsContext)
		}
		if !reflect.DeepEqual(e.ProviderOptions, map[string]interface{}{"openai": map[string]interface{}{"logprobs": true}}) || !reflect.DeepEqual(e.RuntimeContext, map[string]interface{}{"userId": "test-user"}) {
			t.Errorf("step %d providerOptions %v runtimeContext %v", i, e.ProviderOptions, e.RuntimeContext)
		}
	}
	if !reflect.DeepEqual(recorded, map[string]interface{}{"label": "updated"}) {
		t.Errorf("tool context = %v", recorded)
	}
}

// ---------------------------------------------------------------------------
// ui-message-stream/*.test.ts ports (round 73).
// ---------------------------------------------------------------------------

// get-response-ui-message-id.test.ts: no original messages (no persistence)
// gives no id, a last assistant message is continued, otherwise the
// generator supplies the id.
func TestR73StreamGetResponseUIMessageID(t *testing.T) {
	gen := func() string { return "new-id" }
	cases := []struct {
		name     string
		original []UIMessageChunk
		want     string
	}{
		{"originalMessages null", nil, ""},
		{"last assistant message", []UIMessageChunk{{"id": "msg-1", "role": "user", "parts": []interface{}{}}, {"id": "msg-2", "role": "assistant", "parts": []interface{}{}}}, "msg-2"},
		{"last message from user", []UIMessageChunk{{"id": "msg-1", "role": "assistant", "parts": []interface{}{}}, {"id": "msg-2", "role": "user", "parts": []interface{}{}}}, "new-id"},
		{"empty messages", []UIMessageChunk{}, "new-id"},
	}
	for _, c := range cases {
		if got := responseUIMessageID(UIMessageStreamResultOptions{OriginalMessages: c.original, GenerateMessageID: gen}); got != c.want {
			t.Errorf("%s: id = %q, want %q", c.name, got, c.want)
		}
	}
}

// read-ui-message-stream.test.ts 259: a hydrated partial static tool call
// without raw input accepts a delta and the final input.
func TestR73StreamReadUIMessagesHydratedPartialToolCall(t *testing.T) {
	msg := UIMessage{ID: "msg-123", Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
		ToolUIPart{Type: "tool-createDocument", ToolCallID: "tool-1", State: "input-streaming", Input: map[string]interface{}{"title": "Hel"}},
	}}
	src := make(chan UIMessageChunk, 2)
	src <- UIMessageChunk{"type": "tool-input-delta", "toolCallId": "tool-1", "inputTextDelta": `lo"}`}
	src <- UIMessageChunk{"type": "tool-input-available", "toolCallId": "tool-1", "toolName": "createDocument", "input": map[string]interface{}{"title": "Hello"}}
	close(src)
	out, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Message: &msg, Stream: src, TerminateOnError: true})
	var last UIMessage
	for m := range out {
		last = m
	}
	for err := range errs {
		t.Fatalf("error: %v", err)
	}
	parts := r73streamJSONClone(t, last).(map[string]interface{})["parts"].([]interface{})
	if len(parts) != 1 {
		t.Fatalf("parts = %v", parts)
	}
	p := parts[0].(map[string]interface{})
	if p["type"] != "tool-createDocument" || p["toolCallId"] != "tool-1" || p["state"] != "input-available" || !reflect.DeepEqual(p["input"], map[string]interface{}{"title": "Hello"}) {
		t.Fatalf("part = %v", p)
	}
}

type r73streamErrAfter struct {
	chunks []provider.StreamChunk
	err    error
}

func (s *r73streamErrAfter) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) > 0 {
		c := s.chunks[0]
		s.chunks = s.chunks[1:]
		return &c, nil
	}
	return nil, s.err
}
func (s *r73streamErrAfter) Err() error   { return s.err }
func (s *r73streamErrAfter) Close() error { return nil }

// to-ui-message-stream.test.ts 634: an earlier finish part does not hide a
// source stream failure: onEnd runs once with a failed outcome carrying the
// source error (Go writes a masked error chunk instead of erroring the
// stream, owner ruling 2026-10-29).
func TestR73StreamToUIFinishThenSourceFailure(t *testing.T) {
	sourceErr := errors.New("source stream failed after finish")
	var mu sync.Mutex
	var ends []map[string]interface{}
	ch, errs := ToUIMessageStream(context.Background(), &r73streamErrAfter{
		chunks: []provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage}},
		err:    sourceErr,
	}, UIMessageStreamResultOptions{OnEnd: func(e map[string]interface{}) {
		mu.Lock()
		ends = append(ends, e)
		mu.Unlock()
	}})
	for range ch {
	}
	for range errs {
	}
	if len(ends) != 1 {
		t.Fatalf("onEnd calls = %d", len(ends))
	}
	outcome, _ := ends[0]["outcome"].(map[string]interface{})
	if outcome["status"] != "failed" || outcome["error"] != sourceErr {
		t.Fatalf("outcome = %#v", outcome)
	}
}

// handle-ui-message-stream-finish.test.ts 22: without onFinish/onStepFinish
// the chunks pass through unchanged (the start chunk already has an id).
func TestR73StreamHandleFinishPassThrough(t *testing.T) {
	input := r71streamUIChunks(t, `[{"type":"start","messageId":"msg-123"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello"},{"type":"text-delta","id":"text-1","delta":" World"},{"type":"text-end","id":"text-1"},{"type":"finish"}]`)
	var errsSeen []error
	got, err := r71streamCreate(t, input, UIMessageStreamOptions{OriginalMessages: []UIMessageChunk{}, OnError: func(e error) string { errsSeen = append(errsSeen, e); return "" }})
	if err != nil || len(errsSeen) != 0 {
		t.Fatalf("err %v, onError %v", err, errsSeen)
	}
	if !reflect.DeepEqual(r73streamJSONClone(t, got), r73streamJSONClone(t, input)) {
		t.Fatalf("chunks = %v", got)
	}
}

// create-ui-message-stream.test.ts 153: writes and two merged streams are
// all forwarded; each source keeps its own order and the stream ends only
// after every merged stream closed. (TS's exact interleaving follows its
// microtask queue; Go merges concurrently.)
func TestR73StreamCreateUIMergesStreamsAndWrites(t *testing.T) {
	s1 := make(chan UIMessageChunk)
	s2 := make(chan UIMessageChunk)
	delta := func(id, d string) UIMessageChunk { return UIMessageChunk{"type": "text-delta", "id": id, "delta": d} }
	chunks, errs := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{Execute: func(w UIMessageStreamWriter) {
		w.Write(delta("1", "data-part-1"))
		w.Merge(s1)
		w.Write(delta("1", "data-part-2"))
		w.Merge(s2)
		w.Write(delta("1", "data-part-3"))
	}})
	go func() {
		s1 <- delta("1", "1a")
		s2 <- delta("2", "2a")
		s1 <- delta("1", "1b")
		s2 <- delta("2", "2b")
		close(s2)
		s1 <- delta("1", "1c")
		close(s1)
	}()
	bySource := map[string][]string{}
	var all []string
	for c := range chunks {
		d := c["delta"].(string)
		all = append(all, d)
		src := "write"
		if !strings.HasPrefix(d, "data-part") {
			src = c["id"].(string)
		}
		bySource[src] = append(bySource[src], d)
	}
	for range errs {
	}
	want := map[string][]string{"write": {"data-part-1", "data-part-2", "data-part-3"}, "1": {"1a", "1b", "1c"}, "2": {"2a", "2b"}}
	if !reflect.DeepEqual(bySource, want) || len(all) != 8 {
		t.Fatalf("chunks = %v", all)
	}
}

// r73streamPUICasesJSON was extracted from ai/packages/ai/src/ui/process-ui-message-stream.test.ts
// at e64ed82f27: per case the scripted chunks, the initial state and the
// assertion values (vitest inline snapshots parsed, undefined dropped).
const r73streamPUICasesJSON = `[
{"describe":"finish-step","it":"preserves active text and reasoning parts across interleaved step boundaries","line":137,"chunks":[{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"first "},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"thinking "},{"type":"start-step"},{"type":"finish-step"},{"type":"text-delta","id":"text-1","delta":"second"},{"type":"reasoning-delta","id":"reasoning-1","delta":"continued"},{"type":"text-end","id":"text-1"},{"type":"reasoning-end","id":"reasoning-1"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state.message.parts","kind":"toEqual","value":[{"type":"text","text":"first second","state":"done"},{"type":"reasoning","id":"reasoning-1","text":"thinking continued","state":"done"},{"type":"step-start"}],"undef":false},{"expr":"state.activeTextParts","kind":"toEqual","value":{},"undef":false},{"expr":"state.activeReasoningParts","kind":"toEqual","value":{},"undef":false}]},
{"describe":"reset-step","it":"removes parts from the current step and accepts retried parts","line":196,"chunks":[{"type":"start-step"},{"type":"text-start","id":"completed-text"},{"type":"text-delta","id":"completed-text","delta":"Completed step"},{"type":"text-end","id":"completed-text"},{"type":"finish-step"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"stale-tool","toolName":"deleteFile"},{"type":"tool-input-delta","toolCallId":"stale-tool","inputTextDelta":"{\"path\":\"partial"},{"type":"reset-step"},{"type":"tool-input-start","toolCallId":"retried-tool","toolName":"deleteFile"},{"type":"tool-input-delta","toolCallId":"retried-tool","inputTextDelta":"{\"path\":\"target\"}"},{"type":"tool-input-available","toolCallId":"retried-tool","toolName":"deleteFile","input":{"path":"target"}}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state.message.parts","kind":"toEqual","value":[{"type":"step-start"},{"type":"text","text":"Completed step","state":"done"},{"type":"step-start"},{"type":"tool-deleteFile","toolCallId":"retried-tool","state":"input-available","input":{"path":"target"}}],"undef":false},{"expr":"state.message.parts.some( part => isToolUIPart(part) && part.toolCallId === 'stale-tool', ),","kind":"toBe","value":false,"undef":false}]},
{"describe":"text","it":"should call the update function with the correct arguments","line":309,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello, "},{"type":"text-delta","id":"text-1","delta":"world!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"Hello, ","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"text","it":"should have the correct final message state","line":396,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello, "},{"type":"text-delta","id":"text-1","delta":"world!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"errors","it":"should call the update function with the correct arguments","line":444,"chunks":[{"type":"error","errorText":"test error"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[],"undef":false}]},
{"describe":"errors","it":"should have the correct final message state","line":448,"chunks":[{"type":"error","errorText":"test error"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[],"role":"assistant"},"undef":false}]},
{"describe":"errors","it":"should call the onError function with the correct arguments","line":459,"chunks":[{"type":"error","errorText":"test error"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should throw descriptive error when text-delta is received without text-start","line":469,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-delta","id":"text-1","delta":"Hello"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should throw descriptive error when reasoning-delta is received without reasoning-start","line":500,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-delta","id":"reasoning-1","delta":"Thinking..."}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should throw descriptive error when tool-input-delta is received without tool-input-start","line":531,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-delta","toolCallId":"tool-1","inputTextDelta":"{\"key\":"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should not read Object.prototype for missing text part ids","line":566,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-delta","id":"__proto__","delta":"Hello"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"consumeStream({ stream: processUIMessageStream({ stream, runUpdateMessageJob, onError: error => { throw error; }, }), onError: error => { throw error; }, }), ).rejects.toThrow( 'Received text-delta for missing text part with ID \"__proto__\". ' + 'Ensure a \"text-start\" chunk is sent before any \"text-delta\" chunks.', ); expect(Object.hasOwn(Object.prototype, 'providerMetadata')","kind":"toBe","value":false,"undef":false},{"expr":"Object.hasOwn(Object.prototype, 'text')","kind":"toBe","value":false,"undef":false}]},
{"describe":"malformed stream errors","it":"should not read Object.prototype for missing reasoning part ids","line":606,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-delta","id":"__proto__","delta":"Thinking..."}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"consumeStream({ stream: processUIMessageStream({ stream, runUpdateMessageJob, onError: error => { throw error; }, }), onError: error => { throw error; }, }), ).rejects.toThrow( 'Received reasoning-delta for missing reasoning part with ID \"__proto__\". ' + 'Ensure a \"reasoning-start\" chunk is sent before any \"reasoning-delta\" chunks.', ); expect(Object.hasOwn(Object.prototype, 'providerMetadata')","kind":"toBe","value":false,"undef":false},{"expr":"Object.hasOwn(Object.prototype, 'text')","kind":"toBe","value":false,"undef":false}]},
{"describe":"malformed stream errors","it":"should not read Object.prototype for missing partial tool call ids","line":646,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-delta","toolCallId":"__proto__","inputTextDelta":"{\"key\":"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"consumeStream({ stream: processUIMessageStream({ stream, runUpdateMessageJob, onError: error => { throw error; }, }), onError: error => { throw error; }, }), ).rejects.toThrow( 'Received tool-input-delta for missing tool call with ID \"__proto__\". ' + 'Ensure a \"tool-input-start\" chunk is sent before any \"tool-input-delta\" chunks.', ); expect(Object.hasOwn(Object.prototype, 'input')","kind":"toBe","value":false,"undef":false},{"expr":"Object.hasOwn(Object.prototype, 'text')","kind":"toBe","value":false,"undef":false}]},
{"describe":"malformed stream errors","it":"should throw descriptive error when text-end is received without text-start","line":690,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-end","id":"text-1"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should throw descriptive error when reasoning-end is received without reasoning-start","line":721,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-end","id":"reasoning-1"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"malformed stream errors","it":"should throw UIMessageStreamError with correct properties for text-delta without text-start","line":752,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-delta","id":"missing-id","delta":"Hello"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"UIMessageStreamError.isInstance(caughtError)","kind":"toBe","value":true,"undef":false},{"expr":"(caughtError as UIMessageStreamError).chunkId","kind":"toBe","value":"missing-id","undef":false}]},
{"describe":"malformed stream errors","it":"should throw UIMessageStreamError with correct properties for tool-input-delta without tool-input-start","line":789,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-delta","toolCallId":"missing-tool-id","inputTextDelta":"{\"key\":"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"UIMessageStreamError.isInstance(caughtError)","kind":"toBe","value":true,"undef":false}]},
{"describe":"server-side tool roundtrip","it":"should call the update function with the correct arguments","line":878,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"server-side tool roundtrip","it":"should have the correct final message state","line":1058,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"server-side tool roundtrip with existing assistant message","it":"should call the update function with the correct arguments","line":1157,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"tool-tool-name-original","toolCallId":"tool-call-id-original","state":"output-available","input":{},"output":{"location":"Berlin"}}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"server-side tool roundtrip with existing assistant message","it":"should have the correct final message state","line":1392,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"tool-tool-name-original","toolCallId":"tool-call-id-original","state":"output-available","input":{},"output":{"location":"Berlin"}}]},"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"input":{},"output":{"location":"Berlin"},"state":"output-available","toolCallId":"tool-call-id-original","type":"tool-tool-name-original"},{"type":"step-start"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"server-side tool roundtrip with multiple assistant texts","it":"should call the update function with the correct arguments","line":1492,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"I will "},{"type":"text-delta","id":"text-1","delta":"use a tool to get the weather in London."},{"type":"text-end","id":"text-1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"The weather in London "},{"type":"text-delta","id":"text-2","delta":"is sunny."},{"type":"text-end","id":"text-2"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"I will ","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"I will use a tool to get the weather in London.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"The weather in London ","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"server-side tool roundtrip with multiple assistant texts","it":"should have the correct final message state","line":1817,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"I will "},{"type":"text-delta","id":"text-1","delta":"use a tool to get the weather in London."},{"type":"text-end","id":"text-1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"The weather in London "},{"type":"text-delta","id":"text-2","delta":"is sunny."},{"type":"text-end","id":"text-2"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"I will use a tool to get the weather in London.","type":"text"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"server-side tool roundtrip with multiple assistant reasoning","it":"should call the update function with the correct arguments","line":1934,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"I will ","providerMetadata":{"testProvider":{"signature":"1234567890"}}},{"type":"reasoning-delta","id":"reasoning-1","delta":"use a tool to get the weather in London."},{"type":"reasoning-end","id":"reasoning-1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-2"},{"type":"reasoning-delta","id":"reasoning-2","delta":"I now know the weather in London.","providerMetadata":{"testProvider":{"signature":"abc123"}}},{"type":"reasoning-end","id":"reasoning-2"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","state":"streaming","text":"","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"streaming","text":"I will ","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"streaming","text":"I will use a tool to get the weather in London.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","state":"streaming","text":"","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"streaming","text":"I now know the weather in London.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"I now know the weather in London.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"I now know the weather in London.","type":"reasoning"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"I now know the weather in London.","type":"reasoning"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"I now know the weather in London.","type":"reasoning"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"server-side tool roundtrip with multiple assistant reasoning","it":"should have the correct final message state","line":2445,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"I will ","providerMetadata":{"testProvider":{"signature":"1234567890"}}},{"type":"reasoning-delta","id":"reasoning-1","delta":"use a tool to get the weather in London."},{"type":"reasoning-end","id":"reasoning-1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-available","toolCallId":"tool-call-id","output":{"weather":"sunny"}},{"type":"finish-step"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-2"},{"type":"reasoning-delta","id":"reasoning-2","delta":"I now know the weather in London.","providerMetadata":{"testProvider":{"signature":"abc123"}}},{"type":"reasoning-end","id":"reasoning-2"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will use a tool to get the weather in London.","type":"reasoning"},{"input":{"city":"London"},"output":{"weather":"sunny"},"state":"output-available","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"I now know the weather in London.","type":"reasoning"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"server-side tool roundtrip with output-error","it":"should call the update function with the correct arguments","line":2553,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-error","toolCallId":"tool-call-id","errorText":"error-text"},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"error-text","input":{"city":"London"},"state":"output-error","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"error-text","input":{"city":"London"},"state":"output-error","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"error-text","input":{"city":"London"},"state":"output-error","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"error-text","input":{"city":"London"},"state":"output-error","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"server-side tool roundtrip with output-error","it":"should have the correct final message state","line":2725,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"tool-output-error","toolCallId":"tool-call-id","errorText":"error-text"},{"type":"finish-step"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"error-text","input":{"city":"London"},"state":"output-error","toolCallId":"tool-call-id","type":"tool-tool-name"},{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"message metadata","it":"should call the update function with the correct arguments","line":2818,"chunks":[{"type":"start","messageId":"msg-123","messageMetadata":{"start":"start-1","shared":{"key1":"value-1a","key2":"value-2a"}}},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"message-metadata","messageMetadata":{"metadata":"metadata-1"}},{"type":"text-delta","id":"text-1","delta":"t2"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish","messageMetadata":{"finish":"finish-1","shared":{"key1":"value-1e","key6":"value-6e"}}}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","metadata":{"shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"streaming","text":"t1","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"metadata":"metadata-1","shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"streaming","text":"t1","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"metadata":"metadata-1","shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"streaming","text":"t1t2","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"metadata":"metadata-1","shared":{"key1":"value-1a","key2":"value-2a"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"done","text":"t1t2","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"finish":"finish-1","metadata":"metadata-1","shared":{"key1":"value-1e","key2":"value-2a","key6":"value-6e"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"done","text":"t1t2","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"message metadata","it":"should have the correct final message state","line":2989,"chunks":[{"type":"start","messageId":"msg-123","messageMetadata":{"start":"start-1","shared":{"key1":"value-1a","key2":"value-2a"}}},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"message-metadata","messageMetadata":{"metadata":"metadata-1"}},{"type":"text-delta","id":"text-1","delta":"t2"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish","messageMetadata":{"finish":"finish-1","shared":{"key1":"value-1e","key6":"value-6e"}}}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","metadata":{"finish":"finish-1","metadata":"metadata-1","shared":{"key1":"value-1e","key2":"value-2a","key6":"value-6e"},"start":"start-1"},"parts":[{"type":"step-start"},{"state":"done","text":"t1t2","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"message metadata delayed after finish","it":"should call the update function with the correct arguments","line":3054,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"},{"type":"message-metadata","messageMetadata":{"key1":"value-1"}}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"t1","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"t1","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"key1":"value-1"},"parts":[{"type":"step-start"},{"state":"done","text":"t1","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"message metadata delayed after finish","it":"should have the correct final message state","line":3143,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"},{"type":"message-metadata","messageMetadata":{"key1":"value-1"}}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","metadata":{"key1":"value-1"},"parts":[{"type":"step-start"},{"state":"done","text":"t1","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"message metadata with existing assistant lastMessage","it":"should call the update function with the correct arguments","line":3210,"chunks":[{"type":"start","messageId":"msg-123","messageMetadata":{"key1":"value-1b","key2":"value-2b"}},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","metadata":{"key1":"value-1a","key3":"value-3a"},"parts":[]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","metadata":{"key1":"value-1b","key2":"value-2b","key3":"value-3a"},"parts":[],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"key1":"value-1b","key2":"value-2b","key3":"value-3a"},"parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"key1":"value-1b","key2":"value-2b","key3":"value-3a"},"parts":[{"type":"step-start"},{"state":"streaming","text":"t1","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","metadata":{"key1":"value-1b","key2":"value-2b","key3":"value-3a"},"parts":[{"type":"step-start"},{"state":"done","text":"t1","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"message metadata with existing assistant lastMessage","it":"should have the correct final message state","line":3295,"chunks":[{"type":"start","messageId":"msg-123","messageMetadata":{"key1":"value-1b","key2":"value-2b"}},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"t1"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","metadata":{"key1":"value-1a","key3":"value-3a"},"parts":[]},"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","metadata":{"key1":"value-1b","key2":"value-2b","key3":"value-3a"},"parts":[{"type":"step-start"},{"state":"done","text":"t1","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"tool call streaming","it":"should call the update function with the correct arguments","line":3372,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-0","toolName":"test-tool"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"{\"testArg\":\"t"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"est-value\"}}"},{"type":"tool-input-available","toolCallId":"tool-call-0","toolName":"test-tool","input":{"testArg":"test-value"}},{"type":"tool-output-available","toolCallId":"tool-call-0","output":"test-result"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"input-streaming","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"testArg":"t"},"rawInput":"{\"testArg\":\"t","state":"input-streaming","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"testArg":"test-value"},"rawInput":"{\"testArg\":\"test-value\"}}","state":"input-streaming","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"testArg":"test-value"},"state":"input-available","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"testArg":"test-value"},"output":"test-result","state":"output-available","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool call streaming","it":"should have the correct final message state","line":3515,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-0","toolName":"test-tool"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"{\"testArg\":\"t"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"est-value\"}}"},{"type":"tool-input-available","toolCallId":"tool-call-0","toolName":"test-tool","input":{"testArg":"test-value"}},{"type":"tool-output-available","toolCallId":"tool-call-0","output":"test-result"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"testArg":"test-value"},"output":"test-result","state":"output-available","toolCallId":"tool-call-0","type":"tool-test-tool"}],"role":"assistant"},"undef":false}]},
{"describe":"static tool name changes","it":"uses the completed input tool name for an existing streamed part","line":3546,"chunks":[{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-0","toolName":"originalTool"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"{\"value\":\"test\"}"},{"type":"tool-input-available","toolCallId":"tool-call-0","toolName":"renamedTool","input":{"value":"test"}},{"type":"tool-approval-request","toolCallId":"tool-call-0","approvalId":"approval-0"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"start with message id","it":"should call the update function with the correct arguments","line":3626,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello, "},{"type":"text-delta","id":"text-1","delta":"world!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"Hello, ","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"start with message id","it":"should have the correct final message state","line":3713,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hello, "},{"type":"text-delta","id":"text-1","delta":"world!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"reasoning","it":"should preserve reasoning part ids","line":3803,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"I will open the conversation"},{"type":"reasoning-delta","id":"reasoning-1","delta":" with witty banter. ","providerMetadata":{"testProvider":{"signature":"1234567890"}}},{"type":"reasoning-end","id":"reasoning-1"},{"type":"reasoning-start","id":"reasoning-2"},{"type":"reasoning-delta","id":"reasoning-2","delta":"redacted-data","providerMetadata":{"testProvider":{"isRedacted":true}}},{"type":"reasoning-end","id":"reasoning-2"},{"type":"reasoning-start","id":"reasoning-3"},{"type":"reasoning-delta","id":"reasoning-3","delta":"Once the user has relaxed,"},{"type":"reasoning-delta","id":"reasoning-3","delta":" I will pry for valuable information.","providerMetadata":{"testProvider":{"signature":"abc123"}}},{"type":"reasoning-end","id":"reasoning-3"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hi there!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts .filter(part => part.type === 'reasoning') .map(part => part.id),","kind":"toEqual","value":["reasoning-1","reasoning-2","reasoning-3"],"undef":false}]},
{"describe":"reasoning","it":"should call the update function with the correct arguments","line":3811,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"I will open the conversation"},{"type":"reasoning-delta","id":"reasoning-1","delta":" with witty banter. ","providerMetadata":{"testProvider":{"signature":"1234567890"}}},{"type":"reasoning-end","id":"reasoning-1"},{"type":"reasoning-start","id":"reasoning-2"},{"type":"reasoning-delta","id":"reasoning-2","delta":"redacted-data","providerMetadata":{"testProvider":{"isRedacted":true}}},{"type":"reasoning-end","id":"reasoning-2"},{"type":"reasoning-start","id":"reasoning-3"},{"type":"reasoning-delta","id":"reasoning-3","delta":"Once the user has relaxed,"},{"type":"reasoning-delta","id":"reasoning-3","delta":" I will pry for valuable information.","providerMetadata":{"testProvider":{"signature":"abc123"}}},{"type":"reasoning-end","id":"reasoning-3"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hi there!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","state":"streaming","text":"","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","state":"streaming","text":"I will open the conversation","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"streaming","text":"I will open the conversation with witty banter. ","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","state":"streaming","text":"","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"streaming","text":"redacted-data","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","state":"streaming","text":"","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","state":"streaming","text":"Once the user has relaxed,","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"streaming","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"},{"state":"streaming","text":"Hi there!","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"},{"state":"done","text":"Hi there!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"reasoning","it":"should have the correct final message state","line":4333,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"reasoning-start","id":"reasoning-1"},{"type":"reasoning-delta","id":"reasoning-1","delta":"I will open the conversation"},{"type":"reasoning-delta","id":"reasoning-1","delta":" with witty banter. ","providerMetadata":{"testProvider":{"signature":"1234567890"}}},{"type":"reasoning-end","id":"reasoning-1"},{"type":"reasoning-start","id":"reasoning-2"},{"type":"reasoning-delta","id":"reasoning-2","delta":"redacted-data","providerMetadata":{"testProvider":{"isRedacted":true}}},{"type":"reasoning-end","id":"reasoning-2"},{"type":"reasoning-start","id":"reasoning-3"},{"type":"reasoning-delta","id":"reasoning-3","delta":"Once the user has relaxed,"},{"type":"reasoning-delta","id":"reasoning-3","delta":" I will pry for valuable information.","providerMetadata":{"testProvider":{"signature":"abc123"}}},{"type":"reasoning-end","id":"reasoning-3"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Hi there!"},{"type":"text-end","id":"text-1"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"id":"reasoning-1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"state":"done","text":"I will open the conversation with witty banter. ","type":"reasoning"},{"id":"reasoning-2","providerMetadata":{"testProvider":{"isRedacted":true}},"state":"done","text":"redacted-data","type":"reasoning"},{"id":"reasoning-3","providerMetadata":{"testProvider":{"signature":"abc123"}},"state":"done","text":"Once the user has relaxed, I will pry for valuable information.","type":"reasoning"},{"state":"done","text":"Hi there!","type":"text"}],"role":"assistant"},"undef":false}]},
{"describe":"onToolCall is executed","it":"should call the update function twice with the correct arguments","line":4420,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}}],"undef":false}]},
{"describe":"onToolCall is executed","it":"should have the correct final message state","line":4461,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"city":"London"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"London"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"},"undef":false}]},
{"describe":"sources","it":"should call the update function with the correct arguments","line":4529,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"source-url","sourceId":"source-id","url":"https://example.com","title":"Example"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"},{"sourceId":"source-id","title":"Example","type":"source-url","url":"https://example.com"}],"role":"assistant"}}],"undef":false}]},
{"describe":"sources","it":"should call the onFinish function with the correct arguments","line":4623,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"The weather in London is sunny."},{"type":"text-end","id":"text-1"},{"type":"source-url","sourceId":"source-id","url":"https://example.com","title":"Example"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"The weather in London is sunny.","type":"text"},{"sourceId":"source-id","title":"Example","type":"source-url","url":"https://example.com"}],"role":"assistant"},"undef":false}]},
{"describe":"file parts","it":"should call the update function with the correct arguments","line":4693,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Here is a file:"},{"type":"text-end","id":"text-1"},{"type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ=","mediaType":"text/plain"},{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"And another one:"},{"type":"text-end","id":"text-2"},{"type":"file","url":"data:application/json;base64,eyJrZXkiOiJ2YWx1ZSJ9","mediaType":"application/json"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"streaming","text":"Here is a file:","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"state":"streaming","text":"And another one:","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"state":"done","text":"And another one:","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"state":"done","text":"And another one:","type":"text"},{"mediaType":"application/json","type":"file","url":"data:application/json;base64,eyJrZXkiOiJ2YWx1ZSJ9"}],"role":"assistant"}}],"undef":false}]},
{"describe":"file parts","it":"should have the correct final message state","line":4906,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"text-1"},{"type":"text-delta","id":"text-1","delta":"Here is a file:"},{"type":"text-end","id":"text-1"},{"type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ=","mediaType":"text/plain"},{"type":"text-start","id":"text-2"},{"type":"text-delta","id":"text-2","delta":"And another one:"},{"type":"text-end","id":"text-2"},{"type":"file","url":"data:application/json;base64,eyJrZXkiOiJ2YWx1ZSJ9","mediaType":"application/json"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"done","text":"Here is a file:","type":"text"},{"mediaType":"text/plain","type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"state":"done","text":"And another one:","type":"text"},{"mediaType":"application/json","type":"file","url":"data:application/json;base64,eyJrZXkiOiJ2YWx1ZSJ9"}],"role":"assistant"},"undef":false}]},
{"describe":"file parts with providerMetadata","it":"should have the correct final message state","line":4982,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ=","mediaType":"text/plain","providerMetadata":{"testProvider":{"signature":"sig-1"}}},{"type":"file","url":"data:image/jpeg;base64,QkFVRw==","mediaType":"image/jpeg"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"mediaType":"text/plain","providerMetadata":{"testProvider":{"signature":"sig-1"}},"type":"file","url":"data:text/plain;base64,SGVsbG8gV29ybGQ="},{"mediaType":"image/jpeg","type":"file","url":"data:image/jpeg;base64,QkFVRw=="}],"role":"assistant"},"undef":false}]},
{"describe":"data ui parts (single part)","it":"should call the update function with the correct arguments","line":5049,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"data":"example-data-can-be-anything","type":"data-test"}],"role":"assistant"}}],"undef":false}]},
{"describe":"data ui parts (single part)","it":"should have the correct final message state","line":5080,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"data":"example-data-can-be-anything","type":"data-test"}],"role":"assistant"},"undef":false}]},
{"describe":"data ui parts (single part)","it":"should call the onData callback with the correct arguments","line":5099,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"dataCalls","kind":"toMatchInlineSnapshot","value":[{"data":"example-data-can-be-anything","type":"data-test"}],"undef":false}]},
{"describe":"data ui parts (transient part)","it":"should not call the update function with the transient part","line":5148,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything","transient":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}}],"undef":false}]},
{"describe":"data ui parts (transient part)","it":"should not have the transient part in the final message state","line":5163,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything","transient":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"}],"role":"assistant"},"undef":false}]},
{"describe":"data ui parts (transient part)","it":"should call the onData callback with the transient part","line":5178,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","data":"example-data-can-be-anything","transient":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"dataCalls","kind":"toMatchInlineSnapshot","value":[{"data":"example-data-can-be-anything","transient":true,"type":"data-test"}],"undef":false}]},
{"describe":"data ui parts (single part with id and replacement update)","it":"should call the update function with the correct arguments","line":5226,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","id":"data-part-id","data":"example-data-can-be-anything"},{"type":"data-test","id":"data-part-id","data":"or-something-else"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"data":"example-data-can-be-anything","id":"data-part-id","type":"data-test"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"data":"or-something-else","id":"data-part-id","type":"data-test"}],"role":"assistant"}}],"undef":false}]},
{"describe":"data ui parts (single part with id and replacement update)","it":"should have the correct final message state","line":5275,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","id":"data-part-id","data":"example-data-can-be-anything"},{"type":"data-test","id":"data-part-id","data":"or-something-else"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"data":"or-something-else","id":"data-part-id","type":"data-test"}],"role":"assistant"},"undef":false}]},
{"describe":"data ui parts (single part with id and merge update)","it":"should call the update function with the correct arguments","line":5337,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","id":"data-part-id","data":{"a":"a1","b":"b1"}},{"type":"data-test","id":"data-part-id","data":{"b":"b2","c":"c2"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"data":{"a":"a1","b":"b1"},"id":"data-part-id","type":"data-test"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"data":{"b":"b2","c":"c2"},"id":"data-part-id","type":"data-test"}],"role":"assistant"}}],"undef":false}]},
{"describe":"data ui parts (single part with id and merge update)","it":"should have the correct final message state","line":5392,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"data-test","id":"data-part-id","data":{"a":"a1","b":"b1"}},{"type":"data-test","id":"data-part-id","data":{"b":"b2","c":"c2"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message","kind":"toMatchInlineSnapshot","value":{"id":"msg-123","parts":[{"type":"step-start"},{"data":{"b":"b2","c":"c2"},"id":"data-part-id","type":"data-test"}],"role":"assistant"},"undef":false}]},
{"describe":"provider-executed static tools","it":"should not call onToolCall","line":5485,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"onToolCallInvoked","kind":"toBe","value":false,"undef":false}]},
{"describe":"provider-executed static tools","it":"should call the update function with the correct arguments","line":5489,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerExecuted":true,"state":"input-streaming","toolCallId":"tool-call-1","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"providerExecuted":true,"rawInput":"{ \"query\": \"test\" }","state":"input-streaming","toolCallId":"tool-call-1","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"providerExecuted":true,"state":"input-available","toolCallId":"tool-call-1","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","type":"tool-tool-name"},{"input":{"query":"test"},"providerExecuted":true,"state":"input-available","toolCallId":"tool-call-2","type":"tool-tool-name"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","type":"tool-tool-name"},{"errorText":"error-text","input":{"query":"test"},"providerExecuted":true,"state":"output-error","toolCallId":"tool-call-2","type":"tool-tool-name"}],"role":"assistant"}}],"undef":false}]},
{"describe":"provider-executed static tools","it":"should have the correct final message state","line":5692,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","type":"tool-tool-name"},{"errorText":"error-text","input":{"query":"test"},"providerExecuted":true,"state":"output-error","toolCallId":"tool-call-2","type":"tool-tool-name"}],"undef":false}]},
{"describe":"provider-executed static tools","it":"should preserve separate call and result provider metadata for static tools","line":5732,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"providerMetadata":{"testProvider":{"itemId":"call-item"}}},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true,"providerMetadata":{"testProvider":{"itemId":"result-item"}}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"toolCallId","value":"tool-call-1","filter":false}},"asserts":[{"expr":"toolPart.callProviderMetadata","kind":"toEqual","value":{"testProvider":{"itemId":"call-item"}},"undef":false},{"expr":"toolPart.resultProviderMetadata","kind":"toEqual","value":{"testProvider":{"itemId":"result-item"}},"undef":false}]},
{"describe":"provider-executed dynamic tools","it":"should not call onToolCall","line":5857,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true,"dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true,"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true,"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"onToolCallInvoked","kind":"toBe","value":false,"undef":false}]},
{"describe":"provider-executed dynamic tools","it":"should call the update function with the correct arguments","line":5861,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true,"dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true,"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true,"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerExecuted":true,"state":"input-streaming","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"providerExecuted":true,"rawInput":"{ \"query\": \"test\" }","state":"input-streaming","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"providerExecuted":true,"state":"input-available","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"},{"input":{"query":"test"},"providerExecuted":true,"state":"input-available","toolCallId":"tool-call-2","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"},{"errorText":"error-text","input":{"query":"test"},"providerExecuted":true,"state":"output-error","toolCallId":"tool-call-2","toolName":"tool-name","type":"dynamic-tool"}],"role":"assistant"}}],"undef":false}]},
{"describe":"provider-executed dynamic tools","it":"should have the correct final message state","line":6070,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"tool-name","providerExecuted":true,"dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"providerExecuted":true,"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","providerExecuted":true,"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"providerExecuted":true,"state":"output-available","toolCallId":"tool-call-1","toolName":"tool-name","type":"dynamic-tool"},{"errorText":"error-text","input":{"query":"test"},"providerExecuted":true,"state":"output-error","toolCallId":"tool-call-2","toolName":"tool-name","type":"dynamic-tool"}],"undef":false}]},
{"describe":"provider-executed dynamic tools","it":"should preserve separate call and result provider metadata for dynamic tools","line":6112,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"providerExecuted":true,"dynamic":true,"providerMetadata":{"testProvider":{"itemId":"call-item"}}},{"type":"tool-output-error","toolCallId":"tool-call-1","errorText":"error-text","providerExecuted":true,"dynamic":true,"providerMetadata":{"testProvider":{"itemId":"result-item"}}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"toolCallId","value":"tool-call-1","filter":false}},"asserts":[{"expr":"toolPart.callProviderMetadata","kind":"toEqual","value":{"testProvider":{"itemId":"call-item"}},"undef":false},{"expr":"toolPart.resultProviderMetadata","kind":"toEqual","value":{"testProvider":{"itemId":"result-item"}},"undef":false}]},
{"describe":"provider-executed dynamic tools","it":"should preserve tool metadata on dynamic tool parts","line":6164,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"tool-name","input":{"query":"test"},"dynamic":true,"toolMetadata":{"clientName":"MyMCPClient"}},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"toolCallId","value":"tool-call-1","filter":false}},"asserts":[{"expr":"toolPart","kind":"toMatchInlineSnapshot","value":{"input":{"query":"test"},"output":{"result":"provider-result"},"state":"output-available","toolCallId":"tool-call-1","toolMetadata":{"clientName":"MyMCPClient"},"toolName":"tool-name","type":"dynamic-tool"},"undef":false}]},
{"describe":"","it":"should apply output chunk tool metadata to static and dynamic tool parts","line":6230,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"dynamic-success","toolName":"tool-name","input":{"query":"test"},"dynamic":true},{"type":"tool-output-available","toolCallId":"dynamic-success","output":{"result":"provider-result"},"dynamic":true,"toolMetadata":{"phase":"dynamic-output-available"}},{"type":"tool-input-available","toolCallId":"dynamic-error","toolName":"tool-name","input":{"query":"test"},"dynamic":true,"toolMetadata":{"phase":"dynamic-input"}},{"type":"tool-output-error","toolCallId":"dynamic-error","errorText":"error-text","dynamic":true,"toolMetadata":{"phase":"dynamic-output-error"}},{"type":"tool-input-available","toolCallId":"static-success","toolName":"tool-name","input":{"query":"test"}},{"type":"tool-output-available","toolCallId":"static-success","output":{"result":"provider-result"},"toolMetadata":{"phase":"static-output-available"}},{"type":"tool-input-available","toolCallId":"static-error","toolName":"tool-name","input":{"query":"test"},"toolMetadata":{"phase":"static-input"}},{"type":"tool-output-error","toolCallId":"static-error","errorText":"error-text","toolMetadata":{"phase":"static-output-error"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state.message.parts.filter(isToolUIPart).map(part => ({ toolCallId: part.toolCallId, state: part.state, toolMetadata: part.toolMetadata, })),","kind":"toEqual","value":[{"toolCallId":"dynamic-success","state":"output-available","toolMetadata":{"phase":"dynamic-output-available"}},{"toolCallId":"dynamic-error","state":"output-error","toolMetadata":{"phase":"dynamic-output-error"}},{"toolCallId":"static-success","state":"output-available","toolMetadata":{"phase":"static-output-available"}},{"toolCallId":"static-error","state":"output-error","toolMetadata":{"phase":"static-output-error"}}],"undef":false}]},
{"describe":"","it":"should call onToolCall for client-executed tools","line":6337,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"query":"test"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"onToolCallInvoked","kind":"toBe","value":true,"undef":false},{"expr":"state.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"undef":false}]},
{"describe":"dynamic tools","it":"should invoke onToolCall for dynamic tools","line":6465,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"t1","dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"onToolCallInvoked","kind":"toBe","value":true,"undef":false}]},
{"describe":"dynamic tools","it":"should call the update function with the correct arguments","line":6469,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"t1","dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"input-streaming","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"rawInput":"{ \"query\": \"test\" }","state":"input-streaming","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"},{"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-2","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"state":"output-available","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"},{"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-2","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"state":"output-available","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"},{"errorText":"error-text","input":{"query":"test"},"state":"output-error","toolCallId":"tool-call-2","toolName":"t1","type":"dynamic-tool"}],"role":"assistant"}}],"undef":false}]},
{"describe":"dynamic tools","it":"should have the correct final message state","line":6690,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"t1","dynamic":true},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{ \"query\": \"test\" }"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-input-available","toolCallId":"tool-call-2","toolName":"t1","input":{"query":"test"},"dynamic":true},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":"provider-result"},"dynamic":true},{"type":"tool-output-error","toolCallId":"tool-call-2","errorText":"error-text","dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"input":{"query":"test"},"output":{"result":"provider-result"},"state":"output-available","toolCallId":"tool-call-1","toolName":"t1","type":"dynamic-tool"},{"errorText":"error-text","input":{"query":"test"},"state":"output-error","toolCallId":"tool-call-2","toolName":"t1","type":"dynamic-tool"}],"undef":false}]},
{"describe":"provider metadata","it":"should call the update function with the correct arguments","line":6789,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"1","providerMetadata":{"testProvider":{"signature":"1"}}},{"type":"text-delta","id":"1","delta":"Hello"},{"type":"text-delta","id":"1","delta":", "},{"type":"text-delta","id":"1","delta":"world!"},{"type":"text-end","id":"1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"query":"test"},"providerMetadata":{"testProvider":{"signature":"2"}}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"streaming","text":"Hello","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"streaming","text":"Hello, ","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"done","text":"Hello, world!","type":"text"},{"callProviderMetadata":{"testProvider":{"signature":"2"}},"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"role":"assistant"}}],"undef":false}]},
{"describe":"provider metadata","it":"should have the correct final message state","line":6955,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"text-start","id":"1","providerMetadata":{"testProvider":{"signature":"1"}}},{"type":"text-delta","id":"1","delta":"Hello"},{"type":"text-delta","id":"1","delta":", "},{"type":"text-delta","id":"1","delta":"world!"},{"type":"text-end","id":"1"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"query":"test"},"providerMetadata":{"testProvider":{"signature":"2"}}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"providerMetadata":{"testProvider":{"signature":"1"}},"state":"done","text":"Hello, world!","type":"text"},{"callProviderMetadata":{"testProvider":{"signature":"2"}},"input":{"query":"test"},"state":"input-available","toolCallId":"tool-call-id","type":"tool-tool-name"}],"undef":false}]},
{"describe":"provider metadata during input-streaming","it":"should propagate providerMetadata during input-streaming state","line":7042,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-id","toolName":"tool-name","providerMetadata":{"testProvider":{"someKey":"someValue"}}},{"type":"tool-input-delta","toolCallId":"tool-call-id","inputTextDelta":"{\"query\":"},{"type":"tool-input-delta","toolCallId":"tool-call-id","inputTextDelta":"\"test\"}"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"query":"test"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"inputStreamingUpdate":{"write":{"field":"toolCallId","value":"tool-call-id","state":"input-streaming"},"whole":true},"toolPart":{"field":"toolCallId","value":"tool-call-id","filter":false,"write":{"field":"toolCallId","value":"tool-call-id","state":"input-streaming"}}},"asserts":[{"expr":"inputStreamingUpdate","kind":"toBeDefined","value":null,"undef":false},{"expr":"toolPart.callProviderMetadata","kind":"toEqual","value":{"testProvider":{"someKey":"someValue"}},"undef":false}]},
{"describe":"provider metadata during input-streaming","it":"should retain providerMetadata in final input-available state","line":7062,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-id","toolName":"tool-name","providerMetadata":{"testProvider":{"someKey":"someValue"}}},{"type":"tool-input-delta","toolCallId":"tool-call-id","inputTextDelta":"{\"query\":"},{"type":"tool-input-delta","toolCallId":"tool-call-id","inputTextDelta":"\"test\"}"},{"type":"tool-input-available","toolCallId":"tool-call-id","toolName":"tool-name","input":{"query":"test"}},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"toolCallId","value":"tool-call-id","filter":false}},"asserts":[{"expr":"toolPart.state","kind":"toBe","value":"input-available","undef":false},{"expr":"toolPart.callProviderMetadata","kind":"toEqual","value":{"testProvider":{"someKey":"someValue"}},"undef":false}]},
{"describe":"tool input error","it":"should not warn when creating a static output-error part","line":7134,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-start"},{"inputTextDelta":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-error"},{"errorText":"Invalid input for tool cityAttractions","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[]},
{"describe":"tool input error","it":"should call the update function with the correct arguments","line":7138,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-start"},{"inputTextDelta":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-error"},{"errorText":"Invalid input for tool cityAttractions","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"state":"input-streaming","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"cities":"San Francisco"},"rawInput":"{ \"cities\": \"San Francisco\" }","state":"input-streaming","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","state":"output-error","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","state":"output-error","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool input error","it":"should have the correct final message state","line":7243,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-start"},{"inputTextDelta":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-error"},{"errorText":"Invalid input for tool cityAttractions","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"errorText":"Invalid input for tool cityAttractions","input":"{ \"cities\": \"San Francisco\" }","state":"output-error","toolCallId":"call-1","type":"tool-cityAttractions"}],"undef":false}]},
{"describe":"tool input error with dynamic flag mismatch","it":"should produce exactly one tool part (no duplicate)","line":7415,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-start"},{"inputTextDelta":"{ \"foo\": \"bar\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","input":"{ \"foo\": \"bar\" }","toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-error","dynamic":true},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolParts":{"field":"toolCallId","value":"call-1","filter":true}},"asserts":[{"expr":"toolParts","kind":"toHaveLength","value":1,"undef":false}]},
{"describe":"tool input error with dynamic flag mismatch","it":"should keep the static tool type from tool-input-start","line":7422,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-start"},{"inputTextDelta":"{ \"foo\": \"bar\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","input":"{ \"foo\": \"bar\" }","toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-error","dynamic":true},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"toolCallId","value":"call-1","filter":false}},"asserts":[{"expr":"toolPart.type","kind":"toBe","value":"tool-nonExistentTool","undef":false}]},
{"describe":"tool input error with dynamic flag mismatch","it":"should have the correct final message state","line":7429,"chunks":[{"type":"start"},{"type":"start-step"},{"toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-start"},{"inputTextDelta":"{ \"foo\": \"bar\" }","toolCallId":"call-1","type":"tool-input-delta"},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","input":"{ \"foo\": \"bar\" }","toolCallId":"call-1","toolName":"nonExistentTool","type":"tool-input-error","dynamic":true},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","toolCallId":"call-1","type":"tool-output-error"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"errorText":"Model tried to call unavailable tool 'nonExistentTool'.","input":"{ \"foo\": \"bar\" }","state":"output-error","toolCallId":"call-1","type":"tool-nonExistentTool"}],"undef":false}]},
{"describe":"preliminary tool results","it":"should call the update function with the correct arguments","line":7521,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-available"},{"output":{"status":"loading","text":"Getting weather for San Francisco"},"preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"toolCallId":"call-1","type":"tool-output-available"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"San Francisco"},"state":"input-available","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"San Francisco"},"output":{"status":"loading","text":"Getting weather for San Francisco"},"preliminary":true,"state":"output-available","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"San Francisco"},"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"preliminary":true,"state":"output-available","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"city":"San Francisco"},"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"state":"output-available","toolCallId":"call-1","type":"tool-cityAttractions"}],"role":"assistant"}}],"undef":false}]},
{"describe":"preliminary tool results","it":"should have the correct final message state","line":7643,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"city":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-available"},{"output":{"status":"loading","text":"Getting weather for San Francisco"},"preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"toolCallId":"call-1","type":"tool-output-available"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"input":{"city":"San Francisco"},"output":{"status":"success","temperature":72,"text":"The weather in San Francisco is 72°F"},"state":"output-available","toolCallId":"call-1","type":"tool-cityAttractions"}],"undef":false}]},
{"describe":"tool title support > static tool with title","it":"should include title in tool invocation parts","line":7721,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-0","toolName":"weatherTool","title":"Weather Information"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"{\"location\":\"Paris\"}"},{"type":"tool-input-available","toolCallId":"tool-call-0","toolName":"weatherTool","input":{"location":"Paris"},"title":"Weather Information"},{"type":"tool-output-available","toolCallId":"tool-call-0","output":"Sunny, 22°C"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"tool-weatherTool","filter":false}},"asserts":[{"expr":"toolPart","kind":"toBeDefined","value":null,"undef":false},{"expr":"toolPart.title","kind":"toBe","value":"Weather Information","undef":false}]},
{"describe":"tool title support > static tool with title","it":"should preserve title through all states","line":7730,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-0","toolName":"weatherTool","title":"Weather Information"},{"type":"tool-input-delta","toolCallId":"tool-call-0","inputTextDelta":"{\"location\":\"Paris\"}"},{"type":"tool-input-available","toolCallId":"tool-call-0","toolName":"weatherTool","input":{"location":"Paris"},"title":"Weather Information"},{"type":"tool-output-available","toolCallId":"tool-call-0","output":"Sunny, 22°C"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"inputStreamingWrite":{"write":{"field":"type","value":"tool-weatherTool","state":"input-streaming"},"whole":true},"inputAvailableWrite":{"write":{"field":"type","value":"tool-weatherTool","state":"input-available"},"whole":true},"outputAvailableWrite":{"write":{"field":"type","value":"tool-weatherTool","state":"output-available"},"whole":true},"inputStreamingWrite#tool-weatherTool":{"field":"type","value":"tool-weatherTool","write":{"field":"type","value":"tool-weatherTool","state":"input-streaming"}},"inputAvailableWrite#tool-weatherTool":{"field":"type","value":"tool-weatherTool","write":{"field":"type","value":"tool-weatherTool","state":"input-available"}},"outputAvailableWrite#tool-weatherTool":{"field":"type","value":"tool-weatherTool","write":{"field":"type","value":"tool-weatherTool","state":"output-available"}}},"asserts":[{"expr":"inputStreamingWrite#tool-weatherTool.title,","kind":"toBe","value":"Weather Information","undef":false},{"expr":"inputAvailableWrite#tool-weatherTool.title,","kind":"toBe","value":"Weather Information","undef":false},{"expr":"outputAvailableWrite#tool-weatherTool.title,","kind":"toBe","value":"Weather Information","undef":false}]},
{"describe":"tool title support > dynamic tool with title","it":"should include title in dynamic tool invocation","line":7829,"chunks":[{"type":"start","messageId":"msg-456"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"calculate","dynamic":true,"title":"Calculator"},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{\"a\":5,\"b\":3}"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"calculate","input":{"a":5,"b":3},"dynamic":true,"title":"Calculator"},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":8},"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-456","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"dynamic-tool","filter":false}},"asserts":[{"expr":"toolPart","kind":"toBeDefined","value":null,"undef":false},{"expr":"toolPart.title","kind":"toBe","value":"Calculator","undef":false},{"expr":"toolPart.toolName","kind":"toBe","value":"calculate","undef":false}]},
{"describe":"tool title support > dynamic tool with title","it":"should maintain title through dynamic tool states","line":7839,"chunks":[{"type":"start","messageId":"msg-456"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-1","toolName":"calculate","dynamic":true,"title":"Calculator"},{"type":"tool-input-delta","toolCallId":"tool-call-1","inputTextDelta":"{\"a\":5,\"b\":3}"},{"type":"tool-input-available","toolCallId":"tool-call-1","toolName":"calculate","input":{"a":5,"b":3},"dynamic":true,"title":"Calculator"},{"type":"tool-output-available","toolCallId":"tool-call-1","output":{"result":8},"dynamic":true},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-456","lastMessage":null,"lookups":{"finalToolPart":{"field":"type","value":"dynamic-tool","filter":false}},"asserts":[{"expr":"finalToolPart.state","kind":"toBe","value":"output-available","undef":false},{"expr":"finalToolPart.title","kind":"toBe","value":"Calculator","undef":false},{"expr":"finalToolPart.input","kind":"toEqual","value":{"a":5,"b":3},"undef":false},{"expr":"finalToolPart.output","kind":"toEqual","value":{"result":8},"undef":false}]},
{"describe":"tool title support > tool with title in error state","it":"should preserve title even in error state","line":7894,"chunks":[{"type":"start","messageId":"msg-error"},{"type":"start-step"},{"type":"tool-input-start","toolCallId":"tool-call-error","toolName":"errorTool","title":"Error Tool"},{"type":"tool-input-available","toolCallId":"tool-call-error","toolName":"errorTool","input":{"invalid":"data"},"title":"Error Tool"},{"type":"tool-output-error","toolCallId":"tool-call-error","errorText":"Tool execution failed"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-error","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"tool-errorTool","filter":false}},"asserts":[{"expr":"toolPart","kind":"toBeDefined","value":null,"undef":false},{"expr":"toolPart.title","kind":"toBe","value":"Error Tool","undef":false},{"expr":"toolPart.state","kind":"toBe","value":"output-error","undef":false},{"expr":"toolPart.errorText","kind":"toBe","value":"Tool execution failed","undef":false}]},
{"describe":"tool approval requests (static tool)","it":"should call the update function with the correct arguments","line":7953,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"value":"value"},"state":"input-available","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"approval":{"id":"id-1"},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool approval requests (static tool)","it":"should have the correct final message state","line":8015,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"id":"id-1"},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","type":"tool-tool1"}],"undef":false}]},
{"describe":"tool approval requests (dynamic tool)","it":"should call the update function with the correct arguments","line":8090,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available","dynamic":true},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"input":{"value":"value"},"state":"input-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"msg-123","parts":[{"type":"step-start"},{"approval":{"id":"id-1"},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool approval requests (dynamic tool)","it":"should have the correct final message state","line":8152,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available","dynamic":true},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"id":"id-1"},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"undef":false}]},
{"describe":"tool approval request with signature","it":"should propagate request details into the approval object","line":8228,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request","reason":"requires operator review","signature":"test-sig"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"tool-tool1","filter":false}},"asserts":[{"expr":"toolPart.state","kind":"toBe","value":"approval-requested","undef":false},{"expr":"toolPart.approval","kind":"toEqual","value":{"id":"id-1","requestReason":"requires operator review","signature":"test-sig"},"undef":false}]},
{"describe":"tool approval response with signature","it":"preserves request details separately from the response reason","line":8296,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request","reason":"requires operator review","signature":"test-sig"},{"approvalId":"id-1","approved":true,"reason":"approved by operator","type":"tool-approval-response"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"tool-tool1","filter":false}},"asserts":[{"expr":"toolPart.state","kind":"toBe","value":"approval-responded","undef":false},{"expr":"toolPart.approval","kind":"toEqual","value":{"id":"id-1","approved":true,"requestReason":"requires operator review","reason":"approved by operator","signature":"test-sig"},"undef":false}]},
{"describe":"","it":"preserves approval descriptors through request and response states","line":8312,"chunks":[{"input":{"userId":"user-123"},"toolCallId":"call-1","toolName":"deleteAccount","type":"tool-input-available"},{"approvalDescriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"},"approvalId":"approval-1","toolCallId":"call-1","type":"tool-approval-request"},{"approvalId":"approval-1","approved":true,"type":"tool-approval-response"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls .map(call => call.message.parts.find(isToolUIPart)?.approval) .filter(approval => approval != null),","kind":"toEqual","value":[{"id":"approval-1","descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}},{"id":"approval-1","approved":true,"descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}}],"undef":false}]},
{"describe":"","it":"preserves an approval descriptor restored from a persisted message","line":8372,"chunks":[{"approvalId":"approval-1","approved":true,"type":"tool-approval-response"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"msg-123","parts":[{"type":"tool-deleteAccount","toolCallId":"call-1","state":"approval-requested","input":{"userId":"user-123"},"approval":{"id":"approval-1","descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}}}]},"asserts":[{"expr":"writeCalls .map(call => call.message.parts.find(isToolUIPart)?.approval) .filter(approval => approval != null),","kind":"toEqual","value":[{"id":"approval-1","approved":true,"descriptor":{"action":"deleteAccount","permissions":["account:delete"],"risk":"high"}}],"undef":false}]},
{"describe":"tool approval request without signature","it":"should not include signature in the approval object","line":8476,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"lookups":{"toolPart":{"field":"type","value":"tool-tool1","filter":false}},"asserts":[{"expr":"toolPart.state","kind":"toBe","value":"approval-requested","undef":false},{"expr":"toolPart.approval","kind":"toEqual","value":{"id":"id-1"},"undef":false}]},
{"describe":"automatic tool approval denial (static tool)","it":"should keep automatic approval metadata through denial","line":8538,"chunks":[{"type":"start"},{"type":"start-step"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","isAutomatic":true,"toolCallId":"call-1","type":"tool-approval-request"},{"approvalId":"id-1","approved":false,"reason":"Policy denied execution","type":"tool-approval-response"},{"toolCallId":"call-1","type":"tool-output-denied"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls.map(call => call.message.parts[1])","kind":"toMatchInlineSnapshot","value":[{"input":{"value":"value"},"state":"input-available","toolCallId":"call-1","type":"tool-tool1"},{"approval":{"id":"id-1","isAutomatic":true},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","type":"tool-tool1"},{"approval":{"approved":false,"id":"id-1","isAutomatic":true,"reason":"Policy denied execution"},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"},{"approval":{"approved":false,"id":"id-1","isAutomatic":true,"reason":"Policy denied execution"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"}],"undef":false},{"expr":"state!.message.parts[1]","kind":"toMatchInlineSnapshot","value":{"approval":{"approved":false,"id":"id-1","isAutomatic":true,"reason":"Policy denied execution"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"},"undef":false}]},
{"describe":"automatic tool approval with execution (dynamic tool)","it":"should keep automatic approval metadata through execution","line":8693,"chunks":[{"type":"start"},{"type":"start-step"},{"dynamic":true,"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"approvalId":"id-1","isAutomatic":true,"toolCallId":"call-1","type":"tool-approval-request"},{"approvalId":"id-1","approved":true,"reason":"trusted internal tool","type":"tool-approval-response"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"writeCalls.map(call => call.message.parts[1])","kind":"toMatchInlineSnapshot","value":[{"input":{"value":"value"},"state":"input-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"approval":{"id":"id-1","isAutomatic":true},"input":{"value":"value"},"state":"approval-requested","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"approval":{"approved":true,"id":"id-1","isAutomatic":true,"reason":"trusted internal tool"},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"approval":{"approved":true,"id":"id-1","isAutomatic":true,"reason":"trusted internal tool"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"undef":false},{"expr":"state!.message.parts[1]","kind":"toMatchInlineSnapshot","value":{"approval":{"approved":true,"id":"id-1","isAutomatic":true,"reason":"trusted internal tool"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},"undef":false}]},
{"describe":"initial tool execution after approval (static tool)","it":"should call the update function with the correct arguments","line":8852,"chunks":[{"type":"start"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"initial tool execution after approval (static tool)","it":"should have the correct final message state","line":8998,"chunks":[{"type":"start"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"undef":false}]},
{"describe":"initial tool execution after approval (dynamic tool)","it":"should call the update function with the correct arguments","line":9089,"chunks":[{"type":"start"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available","dynamic":true},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"dynamic-tool","toolName":"tool1"}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"initial tool execution after approval (dynamic tool)","it":"should have the correct final message state","line":9239,"chunks":[{"type":"start"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available","dynamic":true},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"dynamic-tool","toolName":"tool1"}]},"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"result1","state":"output-available","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"undef":false}]},
{"describe":"initial tool execution with preliminary results after approval (static tool)","it":"should call the update function with the correct arguments","line":9342,"chunks":[{"type":"start"},{"output":"preliminary-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":"final-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":"final-result","toolCallId":"call-1","type":"tool-output-available"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"preliminary-result","preliminary":true,"state":"output-available","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","preliminary":true,"state":"output-available","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","state":"output-available","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"Hello, world!","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"initial tool execution with preliminary results after approval (static tool)","it":"should have the correct final message state","line":9544,"chunks":[{"type":"start"},{"output":"preliminary-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":"final-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},{"output":"final-result","toolCallId":"call-1","type":"tool-output-available"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":true},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"approved":true,"id":"id-1"},"input":{"value":"value"},"output":"final-result","state":"output-available","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"Hello, world!","type":"text"}],"undef":false}]},
{"describe":"tool execution denial (static tool)","it":"should call the update function with the correct arguments","line":9629,"chunks":[{"type":"start"},{"toolCallId":"call-1","type":"tool-output-denied"},{"type":"start-step"},{"id":"1","type":"text-start"},{"id":"1","type":"text-delta","delta":"I did not execute the tool."},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":false},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"streaming","text":"I did not execute the tool.","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"I did not execute the tool.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool execution denial (static tool)","it":"should have the correct final message state","line":9759,"chunks":[{"type":"start"},{"toolCallId":"call-1","type":"tool-output-denied"},{"type":"start-step"},{"id":"1","type":"text-start"},{"id":"1","type":"text-delta","delta":"I did not execute the tool."},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":false},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"tool-tool1"}]},"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","type":"tool-tool1"},{"type":"step-start"},{"state":"done","text":"I did not execute the tool.","type":"text"}],"undef":false}]},
{"describe":"tool execution denial (dynamic tool)","it":"should call the update function with the correct arguments","line":9841,"chunks":[{"type":"start"},{"toolCallId":"call-1","type":"tool-output-denied"},{"type":"start-step"},{"id":"1","type":"text-start"},{"id":"1","type":"text-delta","delta":"I did not execute the tool."},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":false},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"dynamic-tool","toolName":"tool1"}]},"asserts":[{"expr":"writeCalls","kind":"toMatchInlineSnapshot","value":[{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"streaming","text":"","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"streaming","text":"I did not execute the tool.","type":"text"}],"role":"assistant"}},{"message":{"id":"original-id","parts":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"done","text":"I did not execute the tool.","type":"text"}],"role":"assistant"}}],"undef":false}]},
{"describe":"tool execution denial (dynamic tool)","it":"should have the correct final message state","line":9975,"chunks":[{"type":"start"},{"toolCallId":"call-1","type":"tool-output-denied"},{"type":"start-step"},{"id":"1","type":"text-start"},{"id":"1","type":"text-delta","delta":"I did not execute the tool."},{"id":"1","type":"text-end"},{"type":"finish-step"},{"type":"finish"}],"messageId":"msg-123","lastMessage":{"role":"assistant","id":"original-id","parts":[{"type":"step-start"},{"approval":{"id":"id-1","approved":false},"input":{"value":"value"},"state":"approval-responded","toolCallId":"call-1","type":"dynamic-tool","toolName":"tool1"}]},"asserts":[{"expr":"state!.message.parts","kind":"toMatchInlineSnapshot","value":[{"type":"step-start"},{"approval":{"approved":false,"id":"id-1"},"input":{"value":"value"},"state":"output-denied","toolCallId":"call-1","toolName":"tool1","type":"dynamic-tool"},{"type":"step-start"},{"state":"done","text":"I did not execute the tool.","type":"text"}],"undef":false}]},
{"describe":"custom","it":"should append custom parts to the message","line":10037,"chunks":[{"type":"start","messageId":"msg-123"},{"type":"custom","kind":"test-provider.compaction","providerMetadata":{"openai":{"itemId":"cmp_123"}}},{"type":"finish"}],"messageId":"msg-123","lastMessage":null,"asserts":[{"expr":"state!.message.parts","kind":"toEqual","value":[{"type":"custom","kind":"test-provider.compaction","providerMetadata":{"openai":{"itemId":"cmp_123"}}}],"undef":false}]}
]`
