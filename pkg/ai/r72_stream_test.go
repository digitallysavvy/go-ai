package ai

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"reflect"
	"strconv"
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
