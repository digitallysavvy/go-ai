package ai

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 72 (stream): ports of TS stream-text.test.ts "... in the ui message
// stream" cases with full-sequence assertions. Package-level identifiers are
// prefixed r72stream.

// r72streamUI runs StreamText and returns its UI message stream chunks as
// JSON-normalized maps (and the first stream error).
func r72streamUI(t *testing.T, opts StreamTextOptions, uiOpts ...UIMessageStreamResultOptions) ([]map[string]interface{}, error) {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	ch, errs := r.ToUIMessageStream(context.Background(), uiOpts...)
	var out []map[string]interface{}
	for c := range ch {
		b, mErr := json.Marshal(c)
		if mErr != nil {
			t.Fatal(mErr)
		}
		var m map[string]interface{}
		if uErr := json.Unmarshal(b, &m); uErr != nil {
			t.Fatal(uErr)
		}
		out = append(out, m)
	}
	var first error
	for e := range errs {
		if first == nil {
			first = e
		}
	}
	return out, first
}

func r72streamAssertUI(t *testing.T, opts StreamTextOptions, want string, uiOpts ...UIMessageStreamResultOptions) {
	t.Helper()
	got, err := r72streamUI(t, opts, uiOpts...)
	if err != nil {
		t.Fatalf("ui stream error: %v", err)
	}
	r71streamAssertJSON(t, "ui chunks", got, want)
}

var r72streamTestUsage = &types.Usage{InputTokens: r71streamInt64(3), OutputTokens: r71streamInt64(10), TotalTokens: r71streamInt64(13)}

func r72streamPMD(itemID string) map[string]interface{} {
	return map[string]interface{}{"provider": map[string]interface{}{"itemId": itemID}}
}

// TS 18887: provider-executed tools > single provider-executed tool call and
// result > should include provider-executed tool call and result in the ui
// message stream.
func r72streamProviderExecutedSteps() []provider.StreamChunk {
	isErr := true
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "web_search", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "web_search", Result: `{ "value": "result1" }`, ProviderExecuted: true, ProviderExecutedSet: true, ProviderMetadata: r72streamPMD("result-1")}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "web_search", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-2", ToolName: "web_search", Result: "ERROR", IsError: &isErr, ProviderExecuted: true, ProviderExecutedSet: true, ProviderMetadata: r72streamPMD("error-1")}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
}

var r72streamWebSearch = types.Tool{Name: "web_search", Type: "provider", ProviderID: "test.web_search", ProviderExecuted: true, Parameters: r71streamValueSchema}

func TestR72StreamUIProviderExecutedToolCallAndResult(t *testing.T) {
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, r72streamProviderExecutedSteps()), Prompt: "test-input", Tools: []types.Tool{r72streamWebSearch}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"providerExecuted":true,"toolCallId":"call-1","toolName":"web_search","type":"tool-input-start"},
		{"inputTextDelta":"{ \"value\": \"value\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"input":{"value":"value"},"providerExecuted":true,"toolCallId":"call-1","toolName":"web_search","type":"tool-input-available"},
		{"output":"{ \"value\": \"result1\" }","providerExecuted":true,"providerMetadata":{"provider":{"itemId":"result-1"}},"toolCallId":"call-1","type":"tool-output-available"},
		{"input":{"value":"value"},"providerExecuted":true,"toolCallId":"call-2","toolName":"web_search","type":"tool-input-available"},
		{"errorText":"ERROR","providerExecuted":true,"providerMetadata":{"provider":{"itemId":"error-1"}},"toolCallId":"call-2","type":"tool-output-error"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)
}

// TS 19446: dynamic tools > single dynamic tool call and result > should
// include dynamic tool call and result in the ui message stream.
func TestR72StreamUIDynamicToolCallAndResult(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool"}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value": "value" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "dynamicTool", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	}
	dyn := types.Tool{Name: "dynamicTool", Type: types.ToolTypeDynamic, Parameters: r71streamValueSchema,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"value": "test-result"}, nil
		}}
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Tools: []types.Tool{dyn}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"dynamic":true,"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-input-start"},
		{"inputTextDelta":"{ \"value\": \"value\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"dynamic":true,"input":{"value":"value"},"toolCallId":"call-1","toolName":"dynamicTool","type":"tool-input-available"},
		{"dynamic":true,"output":{"value":"test-result"},"toolCallId":"call-1","type":"tool-output-available"},
		{"type":"finish-step"},
		{"finishReason":"tool-calls","type":"finish"}]`)
}

// TS 18672 / 18645: the same stream's content and steps. A provider result
// flagged isError (no Go Error set) is a tool-error part whose error is the
// result value, and it is not one of the step's tool results.
func TestR72StreamProviderExecutedIsErrorResultIsToolError(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r72streamProviderExecutedSteps()), Prompt: "test-input", Tools: []types.Tool{r72streamWebSearch}, StopWhen: []StopCondition{StepCountIs(4)}})
	steps := r.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	content := steps[0].Content
	if len(content) != 4 {
		t.Fatalf("content = %#v, want 4 parts", content)
	}
	ok, isResult := content[1].(types.ToolResultContent)
	if !isResult || ok.ToolCallID != "call-1" || ok.Output == nil || ok.Output.Value != `{ "value": "result1" }` || !ok.ProviderExecuted {
		t.Fatalf("content[1] = %#v, want call-1 tool-result", content[1])
	}
	te, isErrPart := content[3].(types.ToolErrorContent)
	if !isErrPart {
		t.Fatalf("content[3] = %#v, want tool-error", content[3])
	}
	if te.ToolCallID != "call-2" || te.ToolName != "web_search" || te.Error != "ERROR" || !te.ProviderExecuted ||
		string(te.ProviderMetadata) != `{"provider":{"itemId":"error-1"}}` || te.Input["value"] != "value" {
		t.Fatalf("tool-error = %#v", te)
	}
	if got := steps[0].ToolResults; len(got) != 1 || got[0].ToolCallID != "call-1" {
		t.Fatalf("step tool results = %#v, want call-1 only", got)
	}
	if got := r.ToolResults(); len(got) != 1 || got[0].ToolCallID != "call-1" {
		t.Fatalf("result tool results = %#v, want call-1 only", got)
	}
}

// TS 20952: tool execution errors > should add tool-error parts to ui
// message stream (the error text is masked by the default onError).
func TestR72StreamUIToolExecutionError(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Tools: []types.Tool{r71streamTool1(true, errors.New("test error"), nil)}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
		{"errorText":"An error occurred.","toolCallId":"call-1","type":"tool-output-error"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)
}

// TS 26143 (and 26070): abort signal > abort in 2nd step > the ui message
// stream and the full stream carry the first step, then the abort part. TS
// also drops the second step's start-step, text-start and text-delta: they
// were still queued inside its pipe chain when the signal cancelled the
// reader (a stream-buffering effect, see the r72 stream report). Go delivers
// the parts the model produced before the abort, so they sit between the
// first step and the abort part here.
func TestR72StreamUIAbortInSecondStep(t *testing.T) {
	for _, view := range []string{"ui", "full"} {
		t.Run(view, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mu sync.Mutex
			call := 0
			model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					mu.Lock()
					call++
					n := call
					mu.Unlock()
					if n == 1 {
						return testutil.NewMockTextStream([]provider.StreamChunk{
							{Type: provider.ChunkTypeStreamStart},
							{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
							{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
						}), nil
					}
					return &abortingStream{ctx: ctx, cancel: cancel, chunks: []provider.StreamChunk{
						{Type: provider.ChunkTypeStreamStart},
						{Type: provider.ChunkTypeTextStart, ID: "1"},
						{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
					}}, nil
				}}
			tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "result1", nil
			}}
			r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, StopWhen: []StopCondition{StepCountIs(3)}, OnError: func(context.Context, error) {}})
			if err != nil {
				t.Fatal(err)
			}
			if view == "full" {
				var got []string
				s := r.FullStream()
				for {
					c, nErr := s.Next()
					if nErr != nil {
						break
					}
					if name := tsPartName(c); name != "" {
						got = append(got, name)
					}
				}
				want := []string{"start", "start-step", "tool-call:call-1:tool1", "tool-result:call-1:result1", "finish-step:tool-calls",
					"start-step", "text-start:1", "text-delta:1:Hello", "abort"}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("full stream = %v, want %v", got, want)
				}
				return
			}
			ch, errs := r.ToUIMessageStream(context.Background())
			var got []UIMessageChunk
			for c := range ch {
				got = append(got, c)
			}
			for range errs {
			}
			if n := len(got); n == 0 || got[n-1]["type"] != "abort" {
				t.Fatalf("ui chunks = %v, want a final abort", got)
			}
			// The abort reason text is Go's; compare the rest.
			delete(got[len(got)-1], "reason")
			r71streamAssertJSON(t, "ui chunks", got, `[
				{"type":"start"},
				{"type":"start-step"},
				{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
				{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},
				{"type":"finish-step"},
				{"type":"start-step"},
				{"id":"1","type":"text-start"},
				{"delta":"Hello","id":"1","type":"text-delta"},
				{"type":"abort"}]`)
		})
	}
}

// TS 26901: invalid tool calls > single invalid tool call > should add tool
// call and result error parts to the ui message stream.
func TestR72StreamUIInvalidToolCall(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions"}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "cities": "San Francisco" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", Arguments: map[string]interface{}{"cities": "San Francisco"}, RawArguments: `{ "cities": "San Francisco" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	city := types.Tool{Name: "cityAttractions", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}}}
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r71streamInternal, Tools: []types.Tool{city}}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-start"},
		{"inputTextDelta":"{ \"cities\": \"San Francisco\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"errorText":"An error occurred.","input":{"cities":"San Francisco"},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-error"},
		{"errorText":"An error occurred.","toolCallId":"call-1","type":"tool-output-error"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)
}

// TS 27558: provider-executed dynamic tools > single provider-executed dynamic
// tool with input streaming > should set dynamic and providerExecuted in ui
// message stream (no tools are declared).
func TestR72StreamUIProviderExecutedDynamicTool(t *testing.T) {
	md := map[string]interface{}{"anthropic": map[string]interface{}{"serverName": "echo"}}
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true, ProviderMetadata: md}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "city": "San Francisco" }`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "call-1"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "cityAttractions", Arguments: map[string]interface{}{"city": "San Francisco"}, RawArguments: `{ "city": "San Francisco" }`, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true, ProviderMetadata: md}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "cityAttractions", Result: map[string]interface{}{"status": "success", "text": "The weather in San Francisco is 72°F"}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true, ProviderMetadata: md}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	r72streamAssertUI(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r71streamInternal}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"dynamic":true,"providerExecuted":true,"providerMetadata":{"anthropic":{"serverName":"echo"}},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-start"},
		{"inputTextDelta":"{ \"city\": \"San Francisco\" }","toolCallId":"call-1","type":"tool-input-delta"},
		{"dynamic":true,"input":{"city":"San Francisco"},"providerExecuted":true,"providerMetadata":{"anthropic":{"serverName":"echo"}},"toolCallId":"call-1","toolName":"cityAttractions","type":"tool-input-available"},
		{"dynamic":true,"output":{"status":"success","text":"The weather in San Francisco is 72°F"},"providerExecuted":true,"providerMetadata":{"anthropic":{"serverName":"echo"}},"toolCallId":"call-1","type":"tool-output-available"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)
}

// r72streamIDs is TS mockId({ prefix: 'id' }): id-0, id-1, ...
func r72streamIDs() *InternalOptions {
	var mu sync.Mutex
	n := 0
	return &InternalOptions{GenerateID: func() string {
		mu.Lock()
		defer mu.Unlock()
		id := "id-" + strconv.Itoa(n)
		n++
		return id
	}, GenerateCallID: func() string { return "test-telemetry-call-id" }}
}

var r72streamHelloStep = []provider.StreamChunk{
	{Type: provider.ChunkTypeStreamStart},
	{Type: provider.ChunkTypeTextStart, ID: "1"},
	{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, world!"},
	{Type: provider.ChunkTypeTextEnd, ID: "1"},
	{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
}

var r72streamToolCallStep = []provider.StreamChunk{
	{Type: provider.ChunkTypeStreamStart},
	{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
	{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
}

const r72streamHelloUI = `
		{"type":"start-step"},
		{"id":"1","type":"text-start"},
		{"delta":"Hello, world!","id":"1","type":"text-delta"},
		{"id":"1","type":"text-end"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`

// TS 30594 and 30872: tool execution approval > when a single tool is
// automatically rejected / approved > the automatic approval (with its
// reason) in the ui message stream, then the second step.
func TestR72StreamUIAutomaticApproval(t *testing.T) {
	for _, tc := range []struct {
		status  types.ToolApprovalStatus
		reason  string
		outcome string
	}{
		{types.ToolApprovalStatusDenied, "blocked by policy", `{"approvalId":"id-1","approved":false,"reason":"blocked by policy","type":"tool-approval-response"},
		{"toolCallId":"call-1","type":"tool-output-denied"},`},
		{types.ToolApprovalStatusApproved, "trusted internal tool", `{"approvalId":"id-1","approved":true,"reason":"trusted internal tool","type":"tool-approval-response"},
		{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},`},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			executed := 0
			var mu sync.Mutex
			tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				executed++
				mu.Unlock()
				return "result1", nil
			}}
			reason := tc.reason
			r72streamAssertUI(t, StreamTextOptions{
				Model: r71streamModel(nil, r72streamToolCallStep, r72streamHelloStep), Prompt: "test-input",
				StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool},
				ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalResult{Status: tc.status, Reason: &reason}},
			}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
		{"approvalId":"id-1","isAutomatic":true,"toolCallId":"call-1","type":"tool-approval-request"},
		`+tc.outcome+`
		{"type":"finish-step"},`+r72streamHelloUI)
			mu.Lock()
			defer mu.Unlock()
			if want := map[types.ToolApprovalStatus]int{types.ToolApprovalStatusDenied: 0, types.ToolApprovalStatusApproved: 1}[tc.status]; executed != want {
				t.Fatalf("execute calls = %d, want %d", executed, want)
			}
		})
	}
}

// TS 31124: tool execution approval > when a single tool has a user-defined
// approval function > should add tool approval requests to the UI message
// stream (the request follows its own call; the other call runs).
func TestR72StreamUIUserDefinedApprovalFunction(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value-needs-approval"}, RawArguments: `{ "value": "value-needs-approval" }`}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value-no-approval"}, RawArguments: `{ "value": "value-no-approval" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
	}
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		return "result for " + args["value"].(string), nil
	}}
	approval := types.SingleToolApprovalFunc(func(args map[string]interface{}, _ types.SingleToolApprovalOptions) types.ToolApprovalResult {
		if args["value"] == "value-needs-approval" {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	})
	r72streamAssertUI(t, StreamTextOptions{
		Model: r71streamModel(nil, steps), Prompt: "test-input", StopWhen: []StopCondition{StepCountIs(3)},
		Internal: r72streamIDs(), Tools: []types.Tool{tool},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": approval},
	}, `[
		{"type":"start"},
		{"type":"start-step"},
		{"input":{"value":"value-needs-approval"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
		{"approvalId":"id-1","toolCallId":"call-1","type":"tool-approval-request"},
		{"input":{"value":"value-no-approval"},"toolCallId":"call-2","toolName":"tool1","type":"tool-input-available"},
		{"output":"result for value-no-approval","toolCallId":"call-2","type":"tool-output-available"},
		{"type":"finish-step"},
		{"finishReason":"tool-calls","type":"finish"}]`)
}

// TS 31717 and 32172: tool execution approval > when a call from a single
// tool (with preliminary results) that needs approval is approved > the tool
// result (preliminary outputs first) comes before the first step.
func TestR72StreamUIApprovedResumeToolResult(t *testing.T) {
	for _, tc := range []struct {
		name    string
		execute types.ToolExecutor
		outputs string
	}{
		{"plain", func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		},
			`{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},`},
		{"preliminary", types.StreamingExecutor(func(context.Context, map[string]interface{}, types.ToolExecutionOptions) iter.Seq2[interface{}, error] {
			return func(yield func(interface{}, error) bool) {
				if !yield("preliminary-result", nil) {
					return
				}
				yield("final-result", nil)
			}
		}), `{"output":"preliminary-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},
		{"output":"final-result","preliminary":true,"toolCallId":"call-1","type":"tool-output-available"},
		{"output":"final-result","toolCallId":"call-1","type":"tool-output-available"},`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: tc.execute}
			r72streamAssertUI(t, StreamTextOptions{
				Model: r71streamModel(nil, r72streamHelloStep), Messages: r71genApprovalHistory(true, ""),
				StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), Tools: []types.Tool{tool},
				ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval},
			}, `[
		{"type":"start"},
		`+tc.outputs+r72streamHelloUI)
		})
	}
}

// r72streamPipe runs StreamText, pipes its UI message stream to an
// httptest recorder, and returns the SSE events (data lines) in order.
func r72streamPipe(t *testing.T, opts StreamTextOptions, uiOpts ...UIMessageStreamResultOptions) []string {
	t.Helper()
	if opts.OnError == nil {
		opts.OnError = func(context.Context, error) {}
	}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatalf("StreamText: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := r.PipeUIMessageStreamToResponse(context.Background(), rec, uiOpts...); err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	for k, v := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "Connection": "keep-alive", "X-Accel-Buffering": "no", "X-Vercel-Ai-Ui-Message-Stream": "v1"} {
		if got := rec.Header().Get(k); got != v {
			t.Fatalf("header %s = %q, want %q", k, got, v)
		}
	}
	var events []string
	for _, block := range strings.Split(rec.Body.String(), "\n\n") {
		if block != "" {
			events = append(events, block)
		}
	}
	return events
}

// r72streamAssertSSE compares SSE events with the TS snapshot's, comparing
// each JSON payload by value (Go writes object keys sorted).
func r72streamAssertSSE(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events = %d, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		g, w := strings.TrimPrefix(got[i], "data: "), strings.TrimPrefix(want[i], "data: ")
		if w == "[DONE]" {
			if g != w {
				t.Fatalf("event %d = %q, want [DONE]", i, got[i])
			}
			continue
		}
		var gv, wv interface{}
		if err := json.Unmarshal([]byte(g), &gv); err != nil {
			t.Fatalf("event %d %q: %v", i, got[i], err)
		}
		if err := json.Unmarshal([]byte(w), &wv); err != nil {
			t.Fatalf("bad want %d: %v", i, err)
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Fatalf("event %d:\n got  %s\n want %s", i, got[i], want[i])
		}
	}
}

// TS 5277 and 5301: result.pipeUIMessageStreamToResponse > should mask error
// messages by default / should support custom error messages (snapshots).
func TestR72StreamPipeErrorMessages(t *testing.T) {
	for _, custom := range []bool{false, true} {
		var uiOpts []UIMessageStreamResultOptions
		text := "An error occurred."
		if custom {
			uiOpts = append(uiOpts, UIMessageStreamResultOptions{OnError: func(err error) string { return "custom error message: " + err.Error() }})
			text = "custom error message: error"
		}
		steps := []provider.StreamChunk{{Type: provider.ChunkTypeError, Err: errors.New("error"), Text: "error"}}
		got := r72streamPipe(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()}, uiOpts...)
		r72streamAssertSSE(t, got,
			`data: {"type":"start"}`,
			`data: {"type":"start-step"}`,
			`data: {"type":"error","errorText":"`+text+`"}`,
			`data: {"type":"finish-step"}`,
			`data: {"type":"finish","finishReason":"error"}`,
			`data: [DONE]`)
	}
}

// TS 5327: should omit message finish event (d:) when sendFinish is false.
func TestR72StreamPipeSendFinishFalse(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, World!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	no := false
	got := r72streamPipe(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()}, UIMessageStreamResultOptions{SendFinish: &no})
	r72streamAssertSSE(t, got,
		`data: {"type":"start"}`,
		`data: {"type":"start-step"}`,
		`data: {"type":"text-start","id":"1"}`,
		`data: {"type":"text-delta","id":"1","delta":"Hello, World!"}`,
		`data: {"type":"text-end","id":"1"}`,
		`data: {"type":"finish-step"}`,
		`data: [DONE]`)
}

func r72streamRawMD(s string) json.RawMessage { return json.RawMessage(s) }

// TS 5377: should write reasoning content to a Node.js response-like object
// (modelWithReasoning, sendReasoning true).
func TestR72StreamPipeReasoning(t *testing.T) {
	sig1 := r72streamRawMD(`{"testProvider":{"signature":"1234567890"}}`)
	sig2 := r72streamRawMD(`{"testProvider":{"signature":"0987654321"}}`)
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeReasoningStart, ID: "1"},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "I will open the conversation"},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " with witty banter."},
		{Type: provider.ChunkTypeReasoning, ID: "1", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
		{Type: provider.ChunkTypeReasoningStart, ID: "2", ProviderMetadata: r72streamRawMD(`{"testProvider":{"redactedData":"redacted-reasoning-data"}}`)},
		{Type: provider.ChunkTypeReasoningEnd, ID: "2"},
		{Type: provider.ChunkTypeReasoningStart, ID: "3"},
		{Type: provider.ChunkTypeReasoning, ID: "3", Reasoning: " Once the user has relaxed,"},
		{Type: provider.ChunkTypeReasoning, ID: "3", Reasoning: " I will pry for valuable information."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "3", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoningStart, ID: "4", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoning, ID: "4", Reasoning: " I need to think about"},
		{Type: provider.ChunkTypeReasoning, ID: "4", Reasoning: " this problem carefully."},
		{Type: provider.ChunkTypeReasoningStart, ID: "5", ProviderMetadata: sig1},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " The best solution"},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " requires careful"},
		{Type: provider.ChunkTypeReasoning, ID: "5", Reasoning: " consideration of all factors."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "4", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeReasoningEnd, ID: "5", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hi"},
		{Type: provider.ChunkTypeText, ID: "1", Text: " there!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1", ProviderMetadata: sig2},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	yes := true
	got := r72streamPipe(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()}, UIMessageStreamResultOptions{SendReasoning: &yes})
	r72streamAssertSSE(t, got,
		`data: {"type":"start"}`,
		`data: {"type":"start-step"}`,
		`data: {"type":"reasoning-start","id":"1"}`,
		`data: {"type":"reasoning-delta","id":"1","delta":"I will open the conversation"}`,
		`data: {"type":"reasoning-delta","id":"1","delta":" with witty banter."}`,
		`data: {"type":"reasoning-delta","id":"1","delta":"","providerMetadata":{"testProvider":{"signature":"1234567890"}}}`,
		`data: {"type":"reasoning-end","id":"1"}`,
		`data: {"type":"reasoning-start","id":"2","providerMetadata":{"testProvider":{"redactedData":"redacted-reasoning-data"}}}`,
		`data: {"type":"reasoning-end","id":"2"}`,
		`data: {"type":"reasoning-start","id":"3"}`,
		`data: {"type":"reasoning-delta","id":"3","delta":" Once the user has relaxed,"}`,
		`data: {"type":"reasoning-delta","id":"3","delta":" I will pry for valuable information."}`,
		`data: {"type":"reasoning-end","id":"3","providerMetadata":{"testProvider":{"signature":"1234567890"}}}`,
		`data: {"type":"reasoning-start","id":"4","providerMetadata":{"testProvider":{"signature":"1234567890"}}}`,
		`data: {"type":"reasoning-delta","id":"4","delta":" I need to think about"}`,
		`data: {"type":"reasoning-delta","id":"4","delta":" this problem carefully."}`,
		`data: {"type":"reasoning-start","id":"5","providerMetadata":{"testProvider":{"signature":"1234567890"}}}`,
		`data: {"type":"reasoning-delta","id":"5","delta":" The best solution"}`,
		`data: {"type":"reasoning-delta","id":"5","delta":" requires careful"}`,
		`data: {"type":"reasoning-delta","id":"5","delta":" consideration of all factors."}`,
		`data: {"type":"reasoning-end","id":"4","providerMetadata":{"testProvider":{"signature":"0987654321"}}}`,
		`data: {"type":"reasoning-end","id":"5","providerMetadata":{"testProvider":{"signature":"0987654321"}}}`,
		`data: {"type":"text-start","id":"1"}`,
		`data: {"type":"text-delta","id":"1","delta":"Hi"}`,
		`data: {"type":"text-delta","id":"1","delta":" there!"}`,
		`data: {"type":"text-end","id":"1","providerMetadata":{"testProvider":{"signature":"0987654321"}}}`,
		`data: {"type":"finish-step"}`,
		`data: {"type":"finish","finishReason":"stop"}`,
		`data: [DONE]`)
}

// TS 5494: should write source content (modelWithSources, sendSources true).
func TestR72StreamPipeSources(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example", ProviderMetadata: r72streamRawMD(`{"provider":{"custom":"value"}}`)}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "456", URL: "https://example.com/2", Title: "Example 2", ProviderMetadata: r72streamRawMD(`{"provider":{"custom":"value2"}}`)}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	yes := true
	got := r72streamPipe(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()}, UIMessageStreamResultOptions{SendSources: &yes})
	r72streamAssertSSE(t, got,
		`data: {"type":"start"}`,
		`data: {"type":"start-step"}`,
		`data: {"type":"source-url","sourceId":"123","url":"https://example.com","title":"Example","providerMetadata":{"provider":{"custom":"value"}}}`,
		`data: {"type":"text-start","id":"1"}`,
		`data: {"type":"text-delta","id":"1","delta":"Hello!"}`,
		`data: {"type":"text-end","id":"1"}`,
		`data: {"type":"source-url","sourceId":"456","url":"https://example.com/2","title":"Example 2","providerMetadata":{"provider":{"custom":"value2"}}}`,
		`data: {"type":"finish-step"}`,
		`data: {"type":"finish","finishReason":"stop"}`,
		`data: [DONE]`)
}

// TS 5554: should write file content (modelWithFiles; the base64 data is
// written as given).
func TestR72StreamPipeFiles(t *testing.T) {
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "Hello World"}}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "QkFVRw=="}}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	got := r72streamPipe(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", Internal: r72streamIDs()})
	r72streamAssertSSE(t, got,
		`data: {"type":"start"}`,
		`data: {"type":"start-step"}`,
		`data: {"type":"file","mediaType":"text/plain","url":"data:text/plain;base64,Hello World"}`,
		`data: {"type":"text-start","id":"1"}`,
		`data: {"type":"text-delta","id":"1","delta":"Hello!"}`,
		`data: {"type":"text-end","id":"1"}`,
		`data: {"type":"file","mediaType":"image/jpeg","url":"data:image/jpeg;base64,QkFVRw=="}`,
		`data: {"type":"finish-step"}`,
		`data: {"type":"finish","finishReason":"stop"}`,
		`data: [DONE]`)
}

// TS 16382 / 17723 (and 16491 responseMessages): options.stopWhen > 2 steps:
// initial, tool-result with transformed tool results. The transform
// upper-cases each static tool-result's output and input; the UI stream,
// the recorded tool results, the response messages and the next step's
// prompt all carry the transformed result, while the tool call keeps its
// input.
func TestR72StreamTransformedToolResultTwoSteps(t *testing.T) {
	var mu sync.Mutex
	var prompts [][]types.Message
	call := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			mu.Lock()
			prompts = append(prompts, o.Prompt.Messages)
			n := call
			call++
			mu.Unlock()
			if n == 0 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
					{Type: provider.ChunkTypeReasoningStart, ID: "id-0"},
					{Type: provider.ChunkTypeReasoning, ID: "id-0", Reasoning: "thinking"},
					{Type: provider.ChunkTypeReasoningEnd, ID: "id-0"},
					{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id"}},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
			}), nil
		}}
	upper := func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && !c.ToolResult.Dynamic {
			tr := *c.ToolResult
			if s, ok := tr.Result.(string); ok {
				tr.Result = strings.ToUpper(s)
			}
			input := map[string]interface{}{}
			for k, v := range tr.Input {
				input[k] = v
			}
			if s, ok := input["value"].(string); ok {
				input["value"] = strings.ToUpper(s)
			}
			tr.Input = input
			c.ToolResult = &tr
		}
		return []provider.StreamChunk{c}
	}
	tool := types.Tool{Name: "tool1", Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "result1", nil
	}}
	opts := StreamTextOptions{Model: model, Prompt: "test-input", Tools: []types.Tool{tool}, ExperimentalTransform: []StreamTransformFunc{upper},
		StopWhen: []StopCondition{StepCountIs(3)}, Internal: r72streamIDs(), OnError: func(context.Context, error) {}}
	r, err := StreamText(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	ch, errs := r.ToUIMessageStream(context.Background())
	var got []UIMessageChunk
	for c := range ch {
		got = append(got, c)
	}
	for e := range errs {
		t.Fatalf("ui error: %v", e)
	}
	r71streamAssertJSON(t, "ui chunks", got, `[
		{"type":"start"},
		{"type":"start-step"},
		{"id":"id-0","type":"reasoning-start"},
		{"delta":"thinking","id":"id-0","type":"reasoning-delta"},
		{"id":"id-0","type":"reasoning-end"},
		{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},
		{"output":"RESULT1","toolCallId":"call-1","type":"tool-output-available"},
		{"type":"finish-step"},
		{"type":"start-step"},
		{"id":"1","type":"text-start"},
		{"delta":"Hello, ","id":"1","type":"text-delta"},
		{"delta":"world!","id":"1","type":"text-delta"},
		{"id":"1","type":"text-end"},
		{"type":"finish-step"},
		{"finishReason":"stop","type":"finish"}]`)

	steps := r.Steps()
	if len(steps) != 2 {
		t.Fatalf("steps = %d", len(steps))
	}
	if tc := steps[0].ToolCalls; len(tc) != 1 || tc[0].Arguments["value"] != "value" {
		t.Fatalf("step 0 tool calls = %+v, want the untransformed input", tc)
	}
	if tr := steps[0].ToolResults; len(tr) != 1 || tr[0].Result != "RESULT1" || tr[0].Input["value"] != "VALUE" {
		t.Fatalf("step 0 tool results = %+v", tr)
	}
	msgs := r.ResponseMessages()
	if len(msgs) != 3 || msgs[0].Role != types.RoleAssistant || msgs[1].Role != types.RoleTool || msgs[2].Role != types.RoleAssistant {
		t.Fatalf("response messages = %+v", msgs)
	}
	toolPart, ok := msgs[1].Content[0].(types.ToolResultContent)
	if !ok || toolPart.ToolCallID != "call-1" || toolPart.Output == nil || toolPart.Output.Type != types.ToolResultOutputText || toolPart.Output.Value != "RESULT1" {
		t.Fatalf("tool message part = %#v, want text output RESULT1", msgs[1].Content[0])
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("model calls = %d", len(prompts))
	}
	last := prompts[1][len(prompts[1])-1]
	sent, ok := last.Content[0].(types.ToolResultContent)
	if last.Role != types.RoleTool || !ok || sent.Output == nil || sent.Output.Value != "RESULT1" {
		t.Fatalf("step 2 prompt tool message = %#v, want RESULT1", last)
	}
}
