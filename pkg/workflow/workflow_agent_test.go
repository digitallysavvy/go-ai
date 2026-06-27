package workflow

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type wfMockModel struct {
	calls int
	opts  []*provider.GenerateOptions
}

func (m *wfMockModel) SpecificationVersion() string   { return "v3" }
func (m *wfMockModel) Provider() string               { return "mock" }
func (m *wfMockModel) ModelID() string                { return "mock-model" }
func (m *wfMockModel) SupportsTools() bool            { return true }
func (m *wfMockModel) SupportsStructuredOutput() bool { return true }
func (m *wfMockModel) SupportsImageInput() bool       { return false }
func (m *wfMockModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	m.opts = append(m.opts, opts)
	if m.calls == 1 {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}}}, nil
	}
	return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
}
func (m *wfMockModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.opts = append(m.opts, opts)
	chunks := []provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "ok"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}
	return testutil.NewMockTextStream(chunks), nil
}

type wfErrorModel struct{ wfMockModel }

func (m *wfErrorModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, errors.New("boom")
}

type wfJSONModel struct {
	wfMockModel
	opts []*provider.GenerateOptions
}

func (m *wfJSONModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.opts = append(m.opts, opts)
	return &types.GenerateResult{Text: `{"ok":true}`, FinishReason: types.FinishReasonStop}, nil
}

func TestNewWorkflowAgentValidation(t *testing.T) {
	if _, err := NewWorkflowAgent(WorkflowAgent{}); err == nil {
		t.Fatal("expected error for nil model")
	}
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(agent.StopWhen) == 0 {
		t.Fatalf("expected default stop condition")
	}
}

func TestWorkflowPromptMessagesValidation(t *testing.T) {
	agent, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	_, err = agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "x", Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "y"}}}}})
	if err == nil {
		t.Fatal("expected mutually exclusive validation error")
	}
}

func TestWorkflowActiveToolsFiltering(t *testing.T) {
	m := &wfMockModel{}
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model: m,
		Tools: []types.Tool{{Name: "t1"}, {Name: "t2"}},
	})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if len(m.opts) == 0 || len(m.opts[0].Tools) != 2 {
		t.Fatalf("expected both tools, got %+v", m.opts)
	}

	m2 := &wfMockModel{}
	agent2, _ := NewWorkflowAgent(WorkflowAgent{Model: m2, Tools: []types.Tool{{Name: "t1"}, {Name: "t2"}}, ActiveTools: []string{"t1"}})
	_, err = agent2.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if len(m2.opts) == 0 || len(m2.opts[0].Tools) != 1 || m2.opts[0].Tools[0].Name != "t1" {
		t.Fatalf("expected filtered tools, got %+v", m2.opts[0].Tools)
	}
}

func TestWorkflowStreamWithOptions(t *testing.T) {
	agent, _ := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	res, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if res == nil || res.StreamTextResult == nil {
		t.Fatalf("expected stream result")
	}
	for {
		_, e := res.Stream().Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream next err: %v", e)
		}
	}
}

func TestWorkflowInstructionsTelemetryAndOutputForwarding(t *testing.T) {
	model := &wfMockModel{}
	telemetry := &ai.TelemetrySettings{FunctionID: "workflow-test"}
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model:        model,
		Instructions: "use the instructions alias",
		Output:       map[string]interface{}{"type": "object"},
		Telemetry:    telemetry,
	})
	res, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if res == nil || res.Output == nil {
		t.Fatalf("expected workflow result output")
	}
	if len(model.opts) == 0 {
		t.Fatal("expected model call")
	}
	if model.opts[0].Prompt.System != "use the instructions alias" {
		t.Fatalf("instructions not forwarded as system: %q", model.opts[0].Prompt.System)
	}
	if model.opts[0].Telemetry != telemetry {
		t.Fatalf("telemetry was not forwarded")
	}
}

func TestWorkflowTelemetryOptionsOverrideConstructor(t *testing.T) {
	constructorTelemetry := &ai.TelemetrySettings{FunctionID: "constructor"}
	generateTelemetry := &ai.TelemetrySettings{FunctionID: "generate"}
	streamTelemetry := &ai.TelemetrySettings{FunctionID: "stream"}

	generateModel := &wfMockModel{}
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model:     generateModel,
		Telemetry: constructorTelemetry,
		Tools: []types.Tool{{Name: "t1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []ai.StopCondition{ai.StepCountIs(1)},
	})
	if _, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello", Telemetry: generateTelemetry}); err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if generateModel.opts[0].Telemetry != generateTelemetry {
		t.Fatalf("generate telemetry = %p, want override %p", generateModel.opts[0].Telemetry, generateTelemetry)
	}

	streamModel := &wfMockModel{}
	streamAgent, _ := NewWorkflowAgent(WorkflowAgent{Model: streamModel, Telemetry: constructorTelemetry})
	stream, err := streamAgent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hello", Telemetry: streamTelemetry})
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if streamModel.opts[0].Telemetry != streamTelemetry {
		t.Fatalf("stream telemetry = %p, want override %p", streamModel.opts[0].Telemetry, streamTelemetry)
	}
}

func TestWorkflowOnStepEndTakesPrecedenceOverDeprecatedOnStepFinish(t *testing.T) {
	model := &wfMockModel{}
	constructorEndCalls := 0
	constructorFinishCalls := 0
	callEndCalls := 0
	callFinishCalls := 0

	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "t1",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
		StopWhen: []ai.StopCondition{ai.StepCountIs(1)},
		OnStepEnd: func(context.Context, ai.OnStepFinishEvent) {
			constructorEndCalls++
		},
		OnStepFinish: func(context.Context, ai.OnStepFinishEvent) {
			constructorFinishCalls++
		},
	})

	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{
		Prompt: "hello",
		OnStepEnd: func(context.Context, ai.OnStepFinishEvent) {
			callEndCalls++
		},
		OnStepFinish: func(context.Context, ai.OnStepFinishEvent) {
			callFinishCalls++
		},
	})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if constructorEndCalls == 0 || callEndCalls == 0 {
		t.Fatalf("expected OnStepEnd callbacks, constructor=%d call=%d", constructorEndCalls, callEndCalls)
	}
	if constructorFinishCalls != 0 || callFinishCalls != 0 {
		t.Fatalf("deprecated OnStepFinish should not run when OnStepEnd is set, constructor=%d call=%d", constructorFinishCalls, callFinishCalls)
	}
}

func TestWorkflowStructuredOutputGenerateAndToolSet(t *testing.T) {
	model := &wfJSONModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model:        model,
		Instructions: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "system message"}}}},
		ToolSet:      map[string]types.Tool{"lookup": {Description: "lookup"}},
		Output:       ai.JSONOutput(ai.JSONOutputOptions{}),
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	res, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if model.opts[0].Prompt.System != "system message" {
		t.Fatalf("unexpected system instructions: %q", model.opts[0].Prompt.System)
	}
	if len(model.opts[0].Tools) != 1 || model.opts[0].Tools[0].Name != "lookup" {
		t.Fatalf("tool set was not converted: %+v", model.opts[0].Tools)
	}
	if model.opts[0].ResponseFormat == nil || model.opts[0].ResponseFormat.Type != "json" {
		t.Fatalf("expected JSON response format, got %+v", model.opts[0].ResponseFormat)
	}
	output, ok := res.Output.(map[string]interface{})
	if !ok || output["ok"] != true {
		t.Fatalf("expected parsed JSON output, got %#v", res.Output)
	}
}

func TestWorkflowToolToModelOutputPreservesRawResult(t *testing.T) {
	model := &wfMockModel{}
	raw := map[string]interface{}{"public": "visible", "secret": "hide me"}
	var gotOptions types.ToModelOutputOptions
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "t1",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return raw, nil
			},
			ToModelOutput: func(_ context.Context, opts types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				gotOptions = opts
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: visible"}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	res, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err != nil {
		t.Fatalf("generate error: %v", err)
	}
	if gotOptions.ToolCall == nil || gotOptions.ToolCall.ID != "1" || gotOptions.ToolCall.ToolName != "t1" {
		t.Fatalf("ToModelOutput options = %+v, want tool call 1/t1", gotOptions)
	}
	if gotOptions.ToolCallID != "1" || !reflect.DeepEqual(gotOptions.Input, map[string]interface{}{}) || !reflect.DeepEqual(gotOptions.Output, raw) {
		t.Fatalf("ToModelOutput TS-shaped options = %+v, want toolCallId/input/output", gotOptions)
	}
	if !reflect.DeepEqual(gotOptions.Result, raw) {
		t.Fatalf("ToModelOutput result alias = %#v, want original map", gotOptions.Result)
	}
	if len(res.ToolResults) != 1 || !reflect.DeepEqual(res.ToolResults[0].Result, raw) {
		t.Fatalf("raw tool results = %#v, want original result", res.ToolResults)
	}
	if len(model.opts) < 2 {
		t.Fatalf("model calls = %d, want continuation call", len(model.opts))
	}
	var toolResult types.ToolResultContent
	found := false
	for _, msg := range model.opts[1].Prompt.Messages {
		for _, part := range msg.Content {
			if tr, ok := part.(types.ToolResultContent); ok && tr.ToolCallID == "1" {
				toolResult = tr
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("missing tool result in continuation messages: %+v", model.opts[1].Prompt.Messages)
	}
	if toolResult.Result != nil || toolResult.Output == nil || toolResult.Output.Type != types.ToolResultOutputText || toolResult.Output.Value != "model sees: visible" {
		t.Fatalf("tool result content = %+v, want converted model output", toolResult)
	}
}

func TestWorkflowRejectsSystemMessagesByDefault(t *testing.T) {
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: &wfJSONModel{}})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "system"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
	}
	if _, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages}); err == nil {
		t.Fatal("expected system message rejection by default")
	}
	if _, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages, AllowSystemInMessages: true}); err != nil {
		t.Fatalf("allowSystemInMessages generate error: %v", err)
	}
	if _, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages}); err == nil {
		t.Fatal("expected stream system message rejection by default")
	}
	if _, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages, AllowSystemInMessages: true}); err != nil {
		t.Fatalf("allowSystemInMessages stream error: %v", err)
	}
}

func collectWorkflowApprovalResponses(messages []types.Message) []types.ToolApprovalResponseContent {
	var responses []types.ToolApprovalResponseContent
	for _, msg := range messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.ToolApprovalResponseContent:
				responses = append(responses, p)
			case *types.ToolApprovalResponseContent:
				if p != nil {
					responses = append(responses, *p)
				}
			}
		}
	}
	return responses
}

func approvalResponseByID(responses []types.ToolApprovalResponseContent, id string) (types.ToolApprovalResponseContent, bool) {
	for _, response := range responses {
		if response.ApprovalID == id {
			return response, true
		}
	}
	return types.ToolApprovalResponseContent{}, false
}

func TestWorkflowForwardsApprovedProviderExecutedApprovalOnResume(t *testing.T) {
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: model})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search the docs."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "mcp_search", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: true},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(model.opts) == 0 {
		t.Fatal("expected provider call")
	}
	responses := collectWorkflowApprovalResponses(model.opts[0].Prompt.Messages)
	response, ok := approvalResponseByID(responses, "approval-call-1")
	if !ok || !response.Approved || !response.ProviderExecuted {
		t.Fatalf("provider approval response = %+v, ok=%v; want forwarded approved provider-executed response", response, ok)
	}
}

func TestWorkflowForwardsDeniedProviderExecutedApprovalOnResume(t *testing.T) {
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: model})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Search the docs."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "mcp_search", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: false},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(model.opts) == 0 {
		t.Fatal("expected provider call")
	}
	responses := collectWorkflowApprovalResponses(model.opts[0].Prompt.Messages)
	response, ok := approvalResponseByID(responses, "approval-call-1")
	if !ok || response.Approved || !response.ProviderExecuted {
		t.Fatalf("provider approval response = %+v, ok=%v; want forwarded denied provider-executed response", response, ok)
	}
}

func TestWorkflowExecutesLocalApprovalAndForwardsProviderExecutedApprovalOnResume(t *testing.T) {
	model := &wfMockModel{}
	executions := 0
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(_ context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				executions++
				if opts.ToolCallID != "local-1" || input["city"] != "London" {
					t.Fatalf("execute input=%+v opts=%+v, want local-1 London", input, opts)
				}
				return map[string]interface{}{"city": "London", "temperature": 72}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather in London and search the docs."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "local-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-local-1", ToolCallID: "local-1"},
			types.ToolCallContent{ToolCallID: "provider-1", ToolName: "mcp_search", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "approval-provider-1", ToolCallID: "provider-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-local-1", Approved: true},
			types.ToolApprovalResponseContent{ApprovalID: "approval-provider-1", Approved: true},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if executions != 1 {
		t.Fatalf("local executions = %d, want 1", executions)
	}
	if len(model.opts) == 0 {
		t.Fatal("expected provider call")
	}
	responses := collectWorkflowApprovalResponses(model.opts[0].Prompt.Messages)
	if response, ok := approvalResponseByID(responses, "approval-provider-1"); !ok || !response.Approved || !response.ProviderExecuted {
		t.Fatalf("provider approval response = %+v, ok=%v; want forwarded provider-executed response", response, ok)
	}
	if response, ok := approvalResponseByID(responses, "approval-local-1"); ok {
		t.Fatalf("local approval response leaked to provider: %+v", response)
	}
	foundLocalResult := false
	for _, msg := range model.opts[0].Prompt.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			result, ok := part.(types.ToolResultContent)
			if ok && result.ToolCallID == "local-1" {
				foundLocalResult = true
			}
		}
	}
	if !foundLocalResult {
		t.Fatalf("missing local tool result in provider prompt: %+v", model.opts[0].Prompt.Messages)
	}
}

func TestWorkflowDoesNotExecuteApprovedToolWhenResumeInputFailsSchema(t *testing.T) {
	model := &wfMockModel{}
	executions := 0
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "getWeather",
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"city"},
				"properties": map[string]interface{}{
					"city": map[string]interface{}{"type": "string"},
				},
			},
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executions++
				return map[string]interface{}{"ok": true}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	var chunks []provider.StreamChunk
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather in London."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": 42}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: true},
		}},
	}, OnChunk: func(chunk provider.StreamChunk) {
		chunks = append(chunks, chunk)
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if executions != 0 {
		t.Fatalf("local executions = %d, want 0 for forged input", executions)
	}
	var sawRawToolResult, sawFinishBoundary, sawStartBoundary bool
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil && chunk.ToolResult.ToolCallID == "call-1" {
			sawRawToolResult = true
		}
		if chunk.Type == provider.ChunkTypeStreamFinish {
			sawFinishBoundary = true
		}
		if sawFinishBoundary && chunk.Type == provider.ChunkTypeStreamStart {
			sawStartBoundary = true
		}
	}
	if sawRawToolResult {
		t.Fatal("schema-invalid approval emitted raw tool-result chunk; TS only writes model-facing error output")
	}
	if !sawFinishBoundary || !sawStartBoundary {
		t.Fatalf("boundary chunks finish/start = %v/%v, want both", sawFinishBoundary, sawStartBoundary)
	}
	foundErrorResult := false
	for _, msg := range model.opts[0].Prompt.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			result, ok := part.(types.ToolResultContent)
			if ok && result.ToolCallID == "call-1" && result.Output != nil && result.Output.Type == types.ToolResultOutputErrorText {
				foundErrorResult = true
			}
		}
	}
	if !foundErrorResult {
		t.Fatalf("missing schema error tool result in provider prompt: %+v", model.opts[0].Prompt.Messages)
	}
}

func TestWorkflowResumeApprovedToolUsesToModelOutput(t *testing.T) {
	model := &wfMockModel{}
	raw := map[string]interface{}{"public": "weather summary", "secret": "hide me"}
	var gotOptions types.ToModelOutputOptions
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "getWeather",
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return raw, nil
			},
			ToModelOutput: func(_ context.Context, opts types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				gotOptions = opts
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: weather summary"}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather in London."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: true},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}
	if gotOptions.ToolCallID != "call-1" || gotOptions.Input["city"] != "London" || !reflect.DeepEqual(gotOptions.Output, raw) {
		t.Fatalf("ToModelOutput options = %+v, want TS-shaped toolCallId/input/output", gotOptions)
	}
	foundConverted := false
	for _, msg := range model.opts[0].Prompt.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			result, ok := part.(types.ToolResultContent)
			if ok && result.ToolCallID == "call-1" && result.Result == nil && result.Output != nil && result.Output.Value == "model sees: weather summary" {
				foundConverted = true
			}
		}
	}
	if !foundConverted {
		t.Fatalf("missing converted tool result in provider prompt: %+v", model.opts[0].Prompt.Messages)
	}
}

func TestWorkflowResumeDeniedLocalApprovalCreatesDenialResult(t *testing.T) {
	model := &wfMockModel{}
	executions := 0
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name:         "deleteFile",
			ToolApproval: true,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executions++
				return nil, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Delete /etc/passwd."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "deleteFile", Arguments: map[string]interface{}{"path": "/etc/passwd"}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: false, Reason: "Too dangerous"},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}
	if executions != 0 {
		t.Fatalf("local executions = %d, want 0 for denied approval", executions)
	}
	foundDenied := false
	for _, msg := range model.opts[0].Prompt.Messages {
		if msg.Role != types.RoleTool {
			continue
		}
		for _, part := range msg.Content {
			result, ok := part.(types.ToolResultContent)
			if ok && result.ToolCallID == "call-1" && result.Output != nil && result.Output.Type == types.ToolResultOutputExecutionDenied && result.Output.Reason == "Too dangerous" {
				foundDenied = true
			}
		}
	}
	if !foundDenied {
		t.Fatalf("missing denied tool result in provider prompt: %+v", model.opts[0].Prompt.Messages)
	}
}

func TestWorkflowResumeLocalApprovalEmitsToolResultsAndStepBoundaries(t *testing.T) {
	model := &wfMockModel{}
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{
			{
				Name:         "getWeather",
				ToolApproval: true,
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"city": "London", "temperature": 72}, nil
				},
			},
			{
				Name:         "deleteFile",
				ToolApproval: true,
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					t.Fatal("denied tool should not execute")
					return nil, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	var chunks []provider.StreamChunk
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather and delete."}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "weather-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
				types.ToolApprovalRequestContent{ApprovalID: "approval-weather-1", ToolCallID: "weather-1"},
				types.ToolCallContent{ToolCallID: "delete-1", ToolName: "deleteFile", Arguments: map[string]interface{}{"path": "/etc/passwd"}},
				types.ToolApprovalRequestContent{ApprovalID: "approval-delete-1", ToolCallID: "delete-1"},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolApprovalResponseContent{ApprovalID: "approval-delete-1", Approved: false, Reason: "Too dangerous"},
				types.ToolApprovalResponseContent{ApprovalID: "approval-weather-1", Approved: true},
			}},
		},
		OnChunk: func(chunk provider.StreamChunk) {
			chunks = append(chunks, chunk)
		},
	})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	_ = stream.Steps()
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	var relevant []provider.StreamChunk
	for _, chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkTypeToolResult, provider.ChunkTypeToolOutputDenied, provider.ChunkTypeStreamFinish, provider.ChunkTypeStreamStart, provider.ChunkTypeText:
			relevant = append(relevant, chunk)
		}
	}
	if len(relevant) < 5 {
		t.Fatalf("relevant chunks = %#v, want approval result, denial, boundaries, and model text", relevant)
	}
	if relevant[0].Type != provider.ChunkTypeToolResult || relevant[0].ToolResult == nil || relevant[0].ToolResult.ToolCallID != "weather-1" {
		t.Fatalf("first relevant chunk = %#v, want approved tool-result", relevant[0])
	}
	if got := relevant[0].ToolResult.Result.(map[string]interface{})["temperature"]; got != 72 {
		t.Fatalf("approved tool raw result temperature = %#v, want 72", got)
	}
	if relevant[1].Type != provider.ChunkTypeToolOutputDenied || relevant[1].ToolResult == nil || relevant[1].ToolResult.ToolCallID != "delete-1" {
		t.Fatalf("second relevant chunk = %#v, want tool-output-denied", relevant[1])
	}
	if relevant[2].Type != provider.ChunkTypeStreamFinish || relevant[3].Type != provider.ChunkTypeStreamStart {
		t.Fatalf("boundary chunks = %s/%s, want stream-finish/stream-start", relevant[2].Type, relevant[3].Type)
	}
	if relevant[4].Type != provider.ChunkTypeText {
		t.Fatalf("next relevant chunk = %#v, want next model text after boundaries", relevant[4])
	}
}

func TestWorkflowResumeDoesNotExecuteFabricatedApprovalWhenToolDoesNotRequireApproval(t *testing.T) {
	model := &wfMockModel{}
	executions := 0
	agent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{
			Name: "getWeather",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executions++
				return map[string]interface{}{"ok": true}, nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent error: %v", err)
	}
	stream, err := agent.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Weather in London."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "getWeather", Arguments: map[string]interface{}{"city": "London"}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-call-1", Approved: true},
		}},
	}})
	if err != nil {
		t.Fatalf("StreamWithOptions error: %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("stream ReadAll error: %v", err)
	}
	if executions != 0 {
		t.Fatalf("local executions = %d, want 0 for fabricated approval", executions)
	}
}

func TestWorkflowErrorAndAbortCallbacks(t *testing.T) {
	errCalled := false
	agent, _ := NewWorkflowAgent(WorkflowAgent{
		Model: &wfErrorModel{},
		OnError: func(context.Context, error) {
			errCalled = true
		},
	})
	_, err := agent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hello"})
	if err == nil || !errCalled {
		t.Fatalf("expected error callback and error, called=%v err=%v", errCalled, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	abortCalled := false
	agent2, _ := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		OnAbort: func(context.Context, []types.StepResult) {
			abortCalled = true
		},
	})
	_, err = agent2.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hello"})
	if err == nil || !abortCalled {
		t.Fatalf("expected abort callback and error, called=%v err=%v", abortCalled, err)
	}
}
