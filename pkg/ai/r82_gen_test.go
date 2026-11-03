package ai

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// r82genNilSchemaValidator is a Schema whose Validator is nil.
type r82genNilSchemaValidator struct{}

func (r82genNilSchemaValidator) Validator() schema.Validator { return nil }

// TS parseToolCall's repair inputSchema callback is
// asSchema(getOwn(tools, toolName)?.inputSchema).jsonSchema: a tool without an
// input schema has the empty object schema. A nil schema value (a typed nil
// Schema or Validator pointer, a Schema whose Validator is nil, a nil map) is
// no schema when a call is parsed; the repair callback dereferenced it and
// panicked.
func TestR82genRepairInputSchemaNilSchemaKinds(t *testing.T) {
	cases := []struct {
		name   string
		params interface{}
	}{
		{"typed nil SimpleJSONSchema", (*schema.SimpleJSONSchema)(nil)},
		{"typed nil SimpleStructSchema", (*schema.SimpleStructSchema)(nil)},
		{"typed nil JSONSchemaValidator", (*schema.JSONSchemaValidator)(nil)},
		{"typed nil OrderedSchema", (*schema.OrderedSchema)(nil)},
		{"zero SimpleJSONSchema", &schema.SimpleJSONSchema{}},
		{"schema with nil validator", r82genNilSchemaValidator{}},
		{"nil map", map[string]interface{}(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := types.Tool{Name: "weather", Parameters: tc.params}
			var got map[string]interface{}
			var schemaErr error
			_, err := ParseToolCall(context.Background(), ParseToolCallOptions{
				ToolCall: types.ToolCall{ID: "call-1", ToolName: "wether", RawArguments: `{}`},
				Tools:    []types.Tool{tool},
				RepairToolCall: func(_ context.Context, opts ToolCallRepairOptions) (*types.ToolCall, error) {
					got, schemaErr = opts.InputSchema("weather")
					return nil, nil
				},
			})
			if err != nil {
				t.Fatalf("ParseToolCall: %v", err)
			}
			if schemaErr != nil {
				t.Fatalf("InputSchema: %v", schemaErr)
			}
			if !reflect.DeepEqual(got, emptyToolInputJSONSchema()) {
				t.Fatalf("InputSchema = %v, want the empty object schema", got)
			}
		})
	}
}

// r82genApprovalModel answers the first call with tool calls of tool1 (one
// per input value) and every later call with "Hello, world!", recording the
// prompts.
func r82genApprovalModel(prompts *[][]types.Message, values ...string) *testutil.MockLanguageModel {
	var mu sync.Mutex
	step := 0
	return &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		defer mu.Unlock()
		*prompts = append(*prompts, o.Prompt.Messages)
		step++
		if step == 1 {
			res := &types.GenerateResult{FinishReason: types.FinishReasonToolCalls}
			for i, v := range values {
				id := fmt.Sprintf("call-%d", i+1)
				raw := fmt.Sprintf(`{ "value": %q }`, v)
				res.Content = append(res.Content, types.ToolCallContent{ToolCallID: id, ToolName: "tool1", Input: raw})
				res.ToolCalls = append(res.ToolCalls, types.ToolCall{ID: id, ToolName: "tool1", RawArguments: raw, Arguments: map[string]interface{}{"value": v}})
			}
			return res, nil
		}
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Hello, world!"}}, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}, nil
	}}
}

// r82genPromptsJSON is the JSON of each recorded prompt (r71genJSON per
// prompt, so the Go-only tool_calls field is dropped at every level).
func r82genPromptsJSON(t *testing.T, prompts [][]types.Message) []interface{} {
	out := make([]interface{}, len(prompts))
	for i, p := range prompts {
		out[i] = r71genJSON(t, p)
	}
	return out
}

// r82genMockID is TS mockId({prefix: 'id'}): id-0, id-1, ... (the response
// ID of a step without one takes id-0).
func r82genMockID() IDGenerator {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		id := fmt.Sprintf("id-%d", n)
		n++
		return id
	}
}

func r82genValueTool(executed *int32, execute func(map[string]interface{}) interface{}) types.Tool {
	return types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			atomic.AddInt32(executed, 1)
			return execute(in), nil
		},
	}
}

var (
	r82genUserPrompt = r71genObj{"role": "user", "content": []interface{}{r71genObj{"type": "text", "text": "test-input"}}}
	r82genHello      = r71genObj{"role": "assistant", "content": []interface{}{r71genObj{"type": "text", "text": "Hello, world!"}}}
)

func r82genCall(id, value string) r71genObj {
	return r71genObj{"type": "tool-call", "toolCallId": id, "toolName": "tool1", "input": r71genObj{"value": value}}
}

func r82genRequest(id, approvalID string, auto bool) r71genObj {
	o := r71genObj{"type": "tool-approval-request", "approvalId": approvalID, "toolCallId": id}
	if auto {
		o["isAutomatic"] = true
	}
	return o
}

func r82genResult(id string, output r71genObj) r71genObj {
	return r71genObj{"type": "tool-result", "toolCallId": id, "toolName": "tool1", "output": output}
}

// generate-text.test.ts 'tool execution approval' > 'when a single tool needs
// approval (user-defined)' 12201 / 12205 / 12209 / 12242 and '(tool-defined)'
// 12307 / 12311 / 12315 / 12348: one step, finish reason tool-calls, the
// content is the call and its approval request id-1, and the response message
// is the assistant call plus the request (no tool message).
func TestR82genSingleToolNeedsApproval(t *testing.T) {
	for _, tc := range []struct {
		name         string
		toolApproval interface{}
		toolDefined  interface{}
	}{
		{"user-defined", map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalStatusUserApproval}, nil},
		{"tool-defined", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prompts [][]types.Message
			var executed int32
			tool := r82genValueTool(&executed, func(map[string]interface{}) interface{} { return "result1" })
			tool.ToolApproval = tc.toolDefined
			opts := GenerateTextOptions{
				Model:    r82genApprovalModel(&prompts, "value"),
				Tools:    []types.Tool{tool},
				StopWhen: []StopCondition{IsStepCount(3)},
				Prompt:   "test-input",
				Internal: &InternalOptions{GenerateID: r82genMockID()},
			}
			if tc.toolApproval != nil {
				opts.ToolApproval = tc.toolApproval
			}
			res, err := GenerateText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Steps) != 1 || res.FinishReason != types.FinishReasonToolCalls || executed != 0 {
				t.Fatalf("steps=%d finish=%s executed=%d", len(res.Steps), res.FinishReason, executed)
			}
			if got := r63ContentShape(res.Content); got != "call:call-1 request:call-1:id-1:auto=false" {
				t.Errorf("content = %s", got)
			}
			r71genCheckJSON(t, "responseMessages", r71genJSON(t, res.ResponseMessages), []interface{}{
				r71genObj{"role": "assistant", "content": []interface{}{r82genCall("call-1", "value"), r82genRequest("call-1", "id-1", false)}},
			})
		})
	}
}

// 'when a single tool has a needsApproval function' 12433 / 12437 / 12491 /
// 12543: only call-1 needs approval; call-2 runs. The content is both calls,
// call-2's result, then call-1's request; the response messages are the
// assistant calls plus the request, then call-2's tool result; the approval
// function sees each call's input, id, messages and an empty runtime context.
func TestR82genNeedsApprovalFunction(t *testing.T) {
	var prompts [][]types.Message
	var executed int32
	type approvalCall struct {
		input    map[string]interface{}
		id       string
		messages interface{}
		toolCtx  interface{}
	}
	var mu sync.Mutex
	var calls []approvalCall
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r82genApprovalModel(&prompts, "value-needs-approval", "value-no-approval"),
		Tools: []types.Tool{r82genValueTool(&executed, func(in map[string]interface{}) interface{} {
			return fmt.Sprintf("result for %v", in["value"])
		})},
		ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.SingleToolApprovalFunc(func(args map[string]interface{}, o types.SingleToolApprovalOptions) types.ToolApprovalResult {
			mu.Lock()
			calls = append(calls, approvalCall{args, o.ToolCallID, r71genJSON(t, o.Messages), o.ToolContext})
			mu.Unlock()
			if args["value"] == "value-needs-approval" {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		})},
		StopWhen: []StopCondition{IsStepCount(3)},
		Prompt:   "test-input",
		Internal: &InternalOptions{GenerateID: r82genMockID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.FinishReason != types.FinishReasonToolCalls || executed != 1 {
		t.Fatalf("steps=%d finish=%s executed=%d", len(res.Steps), res.FinishReason, executed)
	}
	if got := r63ContentShape(res.Content); got != "call:call-1 call:call-2 result:call-2 request:call-1:id-1:auto=false" {
		t.Errorf("content = %s", got)
	}
	r71genCheckJSON(t, "responseMessages", r71genJSON(t, res.ResponseMessages), []interface{}{
		r71genObj{"role": "assistant", "content": []interface{}{r82genCall("call-1", "value-needs-approval"), r82genCall("call-2", "value-no-approval"), r82genRequest("call-1", "id-1", false)}},
		r71genObj{"role": "tool", "content": []interface{}{r82genResult("call-2", r71genObj{"type": "text", "value": "result for value-no-approval"})}},
	})
	if len(calls) != 2 {
		t.Fatalf("approval calls = %d, want 2", len(calls))
	}
	for i, want := range []struct{ id, value string }{{"call-1", "value-needs-approval"}, {"call-2", "value-no-approval"}} {
		c := calls[i]
		if c.id != want.id || c.input["value"] != want.value || c.toolCtx != nil {
			t.Errorf("approval call %d = %+v", i, c)
		}
		r71genCheckJSON(t, "approval messages", c.messages, []interface{}{r82genUserPrompt})
	}
}

// 'when a single tool is automatically rejected' 13646 / 13652 / 13715 and
// 'automatically approved' 13840 / 13853 / 13916: the run continues to a
// second step; the second prompt holds the call and its execution-denied
// output (rejected) or the tool result (approved), without approval parts;
// the response messages carry the automatic request (isAutomatic) and the
// response with its reason.
func TestR82genAutomaticApproval(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   types.ToolApprovalStatus
		reason   string
		executed int32
		output   r71genObj
	}{
		{"rejected", types.ToolApprovalStatusDenied, "blocked by policy", 0, r71genObj{"type": "execution-denied", "reason": "blocked by policy"}},
		{"approved", types.ToolApprovalStatusApproved, "trusted internal tool", 1, r71genObj{"type": "text", "value": "result1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prompts [][]types.Message
			var executed int32
			reason := tc.reason
			res, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:        r82genApprovalModel(&prompts, "value"),
				Tools:        []types.Tool{r82genValueTool(&executed, func(map[string]interface{}) interface{} { return "result1" })},
				ToolApproval: map[string]types.ToolApprovalValue{"tool1": types.ToolApprovalResult{Status: tc.status, Reason: &reason}},
				StopWhen:     []StopCondition{IsStepCount(3)},
				Prompt:       "test-input",
				Internal:     &InternalOptions{GenerateID: r82genMockID()},
			})
			if err != nil {
				t.Fatal(err)
			}
			if executed != tc.executed || res.Text != "Hello, world!" || len(res.Steps) != 2 {
				t.Fatalf("executed=%d text=%q steps=%d", executed, res.Text, len(res.Steps))
			}
			r71genCheckJSON(t, "prompts", r82genPromptsJSON(t, prompts), []interface{}{
				[]interface{}{r82genUserPrompt},
				[]interface{}{r82genUserPrompt,
					r71genObj{"role": "assistant", "content": []interface{}{r82genCall("call-1", "value")}},
					r71genObj{"role": "tool", "content": []interface{}{r82genResult("call-1", tc.output)}},
				},
			})
			approved := tc.status == types.ToolApprovalStatusApproved
			r71genCheckJSON(t, "responseMessages", r71genJSON(t, res.ResponseMessages), []interface{}{
				r71genObj{"role": "assistant", "content": []interface{}{r82genCall("call-1", "value"), r82genRequest("call-1", "id-1", true)}},
				r71genObj{"role": "tool", "content": []interface{}{
					r71genObj{"type": "tool-approval-response", "approvalId": "id-1", "approved": approved, "reason": tc.reason},
					r82genResult("call-1", tc.output),
				}},
				r82genHello,
			})
		})
	}
}

// 'provider-executed tool (MCP) approval' > 'when a provider-executed tool
// emits tool-approval-request' 14226 / 14230 / 14234 / 14267: with the MCP
// tool registered (provider tool, input schema), the run stops after one step
// with finish reason tool-calls; the content is the provider-executed call
// and the provider's request carrying that call (not dynamic: the tool is
// registered); the response message is the call (providerExecuted) and the
// request.
func TestR82genProviderExecutedApprovalRequest(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Input: `{ "query": "test" }`, ProviderExecuted: true},
				types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"},
			},
			ToolCalls:    []types.ToolCall{{ID: "mcp-call-1", ToolName: "mcp_tool", RawArguments: `{ "query": "test" }`, ProviderExecuted: true, ProviderExecutedSet: true}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Tools: []types.Tool{{Name: "mcp_tool", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, ProviderID: "test.mcp_tool",
			Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []interface{}{"query"}}}},
		StopWhen: []StopCondition{IsStepCount(3)},
		Prompt:   "test-input",
		Internal: &InternalOptions{GenerateID: r82genMockID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("steps=%d finish=%s", len(res.Steps), res.FinishReason)
	}
	if got := r63ContentShape(res.Content); got != "call:mcp-call-1 request:mcp-call-1:mcp-approval-1:auto=false" {
		t.Errorf("content = %s", got)
	}
	for _, part := range res.Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			if !p.ProviderExecuted || p.Dynamic {
				t.Errorf("call = %+v, want providerExecuted and not dynamic", p)
			}
		case types.ToolApprovalRequestContent:
			if p.ToolCall.ID != "mcp-call-1" || !p.ToolCall.ProviderExecuted || p.ToolCall.Dynamic || p.ToolCall.Arguments["query"] != "test" {
				t.Errorf("request call = %+v", p.ToolCall)
			}
		}
	}
	call := r71genObj{"type": "tool-call", "toolCallId": "mcp-call-1", "toolName": "mcp_tool", "input": r71genObj{"query": "test"}, "providerExecuted": true}
	r71genCheckJSON(t, "responseMessages", r71genJSON(t, res.ResponseMessages), []interface{}{
		r71genObj{"role": "assistant", "content": []interface{}{call, r82genRequest("mcp-call-1", "mcp-approval-1", false)}},
	})
}

// 'result.request' 2786 'should accept deprecated experimental_include as an
// alias for include' and 2807 'should prefer include over deprecated
// experimental_include': the request body is kept only when the effective
// include asks for it, and Include wins over ExperimentalInclude.
func TestR82genIncludeAlias(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Hello, world!"}}, FinishReason: types.FinishReasonStop, RawRequest: "test body"}, nil
	}}
	for _, tc := range []struct {
		name                  string
		include, experimental *IncludeOptions
		want                  interface{}
	}{
		{"experimental alias", nil, &IncludeOptions{RequestBody: true}, "test body"},
		{"include wins", &IncludeOptions{RequestBody: false}, &IncludeOptions{RequestBody: true}, nil},
		{"default", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "prompt", Include: tc.include, ExperimentalInclude: tc.experimental})
			if err != nil {
				t.Fatal(err)
			}
			if res.Request.Body != tc.want || res.Request.Messages != nil {
				t.Fatalf("request = %+v, want body %v and no messages", res.Request, tc.want)
			}
			if res.FinalStep.Request.Body != tc.want {
				t.Fatalf("finalStep request body = %v", res.FinalStep.Request.Body)
			}
		})
	}
}

// r82genIntegration records its lifecycle events under a label; panics when
// panicking is set (TS "integration listener throws").
type r82genIntegration struct {
	telemetry.NoopTelemetryIntegration
	label    string
	panicky  bool
	mu       *sync.Mutex
	events   *[]string
	disabled map[string]bool
}

func (i r82genIntegration) record(name string) {
	if i.disabled[name] {
		return
	}
	i.mu.Lock()
	*i.events = append(*i.events, i.label+name)
	i.mu.Unlock()
	if i.panicky {
		panic("integration error")
	}
}

func (i r82genIntegration) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	i.record("onStart")
	return ctx
}

func (i r82genIntegration) OnStepStart(ctx context.Context, _ telemetry.TelemetryStepStartEvent) context.Context {
	i.record("onStepStart")
	return ctx
}

func (i r82genIntegration) OnToolExecutionStart(ctx context.Context, _ telemetry.TelemetryToolCallStartEvent) context.Context {
	i.record("onToolExecutionStart")
	return ctx
}

func (i r82genIntegration) OnToolExecutionEnd(context.Context, telemetry.TelemetryToolCallFinishEvent) {
	i.record("onToolExecutionEnd")
}

func (i r82genIntegration) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	i.record("onStepEnd")
}

func (i r82genIntegration) OnEnd(context.Context, telemetry.TelemetryFinishEvent) {
	i.record("onEnd")
}

func r82genHelloOnly() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "Hello!"}}, FinishReason: types.FinishReasonStop}, nil
	}}
}

// 'telemetry integrations' 14771 'should call per-call integration listeners
// for all lifecycle events', 14896 'alongside user callbacks', 14941 'should
// not break generation when an integration listener throws' and 14968
// 'multiple per-call integrations as an array'.
func TestR82genTelemetryIntegrations(t *testing.T) {
	t.Run("lifecycle", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "call-1", ToolName: "testTool", Input: `{ "value": "test" }`}},
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "testTool", RawArguments: `{ "value": "test" }`}},
				FinishReason: types.FinishReasonStop,
			}, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model,
			Tools: []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
				Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					return fmt.Sprintf("%v-result", in["value"]), nil
				}}},
			Prompt:    "test-input",
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{r82genIntegration{mu: &mu, events: &events}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Steps) != 1 {
			t.Fatalf("steps = %d", len(res.Steps))
		}
		want := []string{"onStart", "onStepStart", "onToolExecutionStart", "onToolExecutionEnd", "onStepEnd", "onEnd"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
	t.Run("alongside user callbacks", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		push := func(e string) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:        r82genHelloOnly(),
			Prompt:       "test-input",
			OnStart:      func(context.Context, OnStartEvent) { push("user-onStart") },
			OnStepFinish: func(context.Context, types.StepResult, interface{}) { push("user-onStepFinish") },
			OnFinish:     func(context.Context, *GenerateTextResult, interface{}) { push("user-onFinish") },
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{r82genIntegration{label: "integration-", mu: &mu, events: &events,
				disabled: map[string]bool{"onStepStart": true}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		// TS has user-onStart first; Go starts the telemetry root before the
		// user OnStart (known difference "Telemetry start before the start
		// callbacks"). Step end and end run the user callback first, as TS.
		want := []string{"integration-onStart", "user-onStart", "user-onStepFinish", "integration-onStepEnd", "user-onFinish", "integration-onEnd"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
	t.Run("listener panics", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		res, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:     r82genHelloOnly(),
			Prompt:    "test-input",
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{r82genIntegration{panicky: true, mu: &mu, events: &events}}},
		})
		if err != nil || res.Text != "Hello!" {
			t.Fatalf("text = %v, err = %v", res, err)
		}
	})
	t.Run("multiple", func(t *testing.T) {
		var mu sync.Mutex
		var events []string
		only := map[string]bool{"onStepStart": true, "onStepEnd": true, "onEnd": true}
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:  r82genHelloOnly(),
			Prompt: "test-input",
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
				r82genIntegration{label: "first-", mu: &mu, events: &events, disabled: only},
				r82genIntegration{label: "second-", mu: &mu, events: &events, disabled: only},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"first-onStart", "second-onStart"}; !reflect.DeepEqual(events, want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
	})
}

// r82genStepToolChoiceAttr returns the ai.prompt.toolChoice attribute of the
// first span named name.
func r82genStepToolChoiceAttr(spans []sdktrace.ReadOnlySpan, name string) (string, bool) {
	for _, s := range spans {
		if s.Name() != name {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.prompt.toolChoice" {
				return kv.Value.AsString(), true
			}
		}
		return "", false
	}
	return "", false
}

// TS stepToolChoice = prepareToolChoice(prepareStepResult?.toolChoice ??
// toolChoice) is the telemetry step start's tool choice, {"type":"auto"} when
// none is set (legacy-open-telemetry snapshots: "ai.prompt.toolChoice":
// "{"type":"auto"}" on ai.generateText.doGenerate). Go passed the unset
// choice, so the attribute was missing. (carry CR r82.)
func TestR82genTelemetryStepToolChoiceDefaultsToAuto(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	settings := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
	}}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "hi", Telemetry: settings}); err != nil {
		t.Fatal(err)
	}
	if got, ok := r82genStepToolChoiceAttr(rec.Ended(), "ai.generateText.doGenerate"); !ok || got != `{"type":"auto"}` {
		t.Fatalf(`ai.prompt.toolChoice = %q (present %v), want {"type":"auto"}`, got, ok)
	}
}
