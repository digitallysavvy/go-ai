package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r85genValueTool is the TS validate-tool-approvals.test.ts tool1 definition
// (z.object({ value: z.string() })), optionally dynamic and with metadata.
func r85genValueTool(kind string) types.Tool {
	tool := types.Tool{
		Name: "tool1",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"value"},
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	if kind != "without-metadata" {
		tool.Metadata = map[string]interface{}{"annotations": map[string]interface{}{"readOnlyHint": true}}
		tool.Title = "Read value"
	}
	if kind == "dynamic" {
		tool.Type = types.ToolTypeDynamic
	}
	return tool
}

func r85genForgedApproval(toolName string, input map[string]interface{}) CollectedToolApproval {
	call := types.ToolCall{
		ID: "call-1", ToolName: toolName, Arguments: input,
		ToolMetadata: map[string]interface{}{"untrusted": true},
		Dynamic:      true,
		Title:        "Untrusted title",
	}
	return CollectedToolApproval{
		ApprovalRequest:  types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", ToolCall: call},
		ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		ToolCall:         call,
	}
}

// Port of validate-tool-approvals.test.ts 'should restore %s tool definition
// fields before refinement and policy evaluation' (TS 857ca45151): the
// history's ToolMetadata / Dynamic / Title are replaced with the server
// tool's before the refiner and the policy run, and the input approval is
// not mutated.
func TestR85genApprovalRestoresServerToolFields(t *testing.T) {
	for _, kind := range []string{"dynamic", "static", "without-metadata"} {
		t.Run(kind, func(t *testing.T) {
			tool := r85genValueTool(kind)
			approval := r85genForgedApproval("tool1", map[string]interface{}{"value": "test"})
			original := r85genForgedApproval("tool1", map[string]interface{}{"value": "test"})
			var refinerCall, policyCall types.ToolCall
			refiners := map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
				refinerCall = o.ToolCall
				return o.ToolCall.Arguments, nil
			}}
			policy := types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				policyCall = o.ToolCall
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			})
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: []CollectedToolApproval{approval},
				Tools:                 []types.Tool{tool},
				ToolApproval:          policy,
				RefineToolInput:       refiners,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"},
				ToolMetadata: tool.Metadata, Dynamic: kind == "dynamic", Title: tool.Title}
			if len(res.ApprovedToolApprovals) != 1 {
				t.Fatalf("approved = %d, want 1", len(res.ApprovedToolApprovals))
			}
			for name, got := range map[string]types.ToolCall{"policy": policyCall, "refiner": refinerCall, "approved": res.ApprovedToolApprovals[0].ToolCall} {
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s toolCall = %+v, want %+v", name, got, want)
				}
			}
			if !reflect.DeepEqual(approval, original) {
				t.Errorf("input approval mutated: %+v", approval)
			}
		})
	}
}

// Port of 'should clear history metadata when the tool definition is no
// longer available'.
func TestR85genApprovalRemovedToolClearsHistoryFields(t *testing.T) {
	approval := r85genForgedApproval("removedTool", map[string]interface{}{})
	var policyCall types.ToolCall
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			policyCall = o.ToolCall
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := types.ToolCall{ID: "call-1", ToolName: "removedTool", Arguments: map[string]interface{}{}}
	if len(res.ApprovedToolApprovals) != 0 || len(res.DeniedToolApprovals) != 1 {
		t.Fatalf("approved=%d denied=%d", len(res.ApprovedToolApprovals), len(res.DeniedToolApprovals))
	}
	if got := res.DeniedToolApprovals[0].ToolCall; !reflect.DeepEqual(got, want) {
		t.Errorf("denied toolCall = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(policyCall, want) {
		t.Errorf("policy toolCall = %+v, want %+v", policyCall, want)
	}
	if approval.ToolCall.ToolMetadata["untrusted"] != true {
		t.Errorf("input approval mutated")
	}
}

// Port of 'should enforce metadata-based policy denials with approval secret
// %s': the history omits the metadata, the server tool has it, and the
// policy denies on it, signed or not.
func TestR85genApprovalMetadataPolicyDenial(t *testing.T) {
	for _, secret := range [][]byte{nil, []byte("test-secret")} {
		t.Run(string(secret), func(t *testing.T) {
			tool := r85genValueTool("dynamic")
			tool.Metadata = map[string]interface{}{"requiresAdmin": true}
			call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}}
			approval := CollectedToolApproval{
				ApprovalRequest:  types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", ToolCall: call},
				ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
				ToolCall:         call,
			}
			if secret != nil {
				sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", call.Arguments)
				if err != nil {
					t.Fatal(err)
				}
				approval.ApprovalRequest.Signature = sig
			}
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: []CollectedToolApproval{approval},
				Tools:                 []types.Tool{tool},
				ToolApprovalSecret:    secret,
				ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
					if o.ToolCall.ToolMetadata["requiresAdmin"] == true {
						reason := "Admin required"
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
					}
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.ApprovedToolApprovals) != 0 || len(res.DeniedToolApprovals) != 1 {
				t.Fatalf("approved=%d denied=%d", len(res.ApprovedToolApprovals), len(res.DeniedToolApprovals))
			}
			if r := res.DeniedToolApprovals[0].ApprovalResponse; r.Approved || r.Reason != "Admin required" {
				t.Errorf("approval response = %+v", r)
			}
		})
	}
}

// r85genFollowUpHistory is the model history of a UI tool part in state
// approval-responded (TS convertToModelMessages): the call carries no
// metadata, dynamic flag or title.
func r85genFollowUpHistory(signature string) []types.Message {
	return []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call", ToolName: "readTheme", Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "approval", ToolCallID: "call", Signature: signature},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval", Approved: true}}},
	}
}

func r85genOKModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: "OK"}}, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"},
			}), nil
		},
	}
}

// Port of tool-approval-follow-up.test.ts 'should enforce the current $field
// policy on continuation (secret: $secret)' for generateText and streamText
// (TS 857ca45151): a policy that denies on the server tool's metadata,
// dynamic flag or title denies the continuation, and the tool never runs.
func TestR85genApprovalFollowUpEnforcesServerFieldPolicy(t *testing.T) {
	for _, api := range []string{"generateText", "streamText"} {
		for _, field := range []string{"toolMetadata", "dynamic", "title"} {
			for _, secret := range [][]byte{nil, []byte("test-secret")} {
				t.Run(api+"/"+field+"/"+string(secret), func(t *testing.T) {
					var mu sync.Mutex
					executions, policyCalls := 0, 0
					tool := types.Tool{
						Name:       "readTheme",
						Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
						Metadata:   map[string]interface{}{"restricted": true},
						Title:      "Read theme",
						Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
							mu.Lock()
							executions++
							mu.Unlock()
							return "dark", nil
						},
					}
					if field == "dynamic" {
						tool.Type = types.ToolTypeDynamic
					}
					signature := ""
					if secret != nil {
						sig, err := SignToolApproval(secret, "approval", "call", "readTheme", map[string]interface{}{})
						if err != nil {
							t.Fatal(err)
						}
						signature = sig
					}
					policy := types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
						mu.Lock()
						policyCalls++
						mu.Unlock()
						var denied bool
						switch field {
						case "toolMetadata":
							denied = o.ToolCall.ToolMetadata["restricted"] == true
						case "dynamic":
							denied = o.ToolCall.Dynamic
						default:
							denied = o.ToolCall.Title == "Read theme"
						}
						if denied {
							reason := "Current policy denies this tool"
							return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
						}
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
					})
					var responseMessages []types.Message
					if api == "generateText" {
						res, err := GenerateText(context.Background(), GenerateTextOptions{
							Model: r85genOKModel(), Tools: []types.Tool{tool}, Messages: r85genFollowUpHistory(signature),
							ExperimentalToolApprovalSecret: secret, ToolApproval: policy,
						})
						if err != nil {
							t.Fatal(err)
						}
						responseMessages = res.ResponseMessages
					} else {
						res, err := StreamText(context.Background(), StreamTextOptions{
							Model: r85genOKModel(), Tools: []types.Tool{tool}, Messages: r85genFollowUpHistory(signature),
							ExperimentalToolApprovalSecret: secret, ToolApproval: policy,
						})
						if err != nil {
							t.Fatal(err)
						}
						if err := res.ConsumeStream(); err != nil {
							t.Fatal(err)
						}
						responseMessages = res.ResponseMessages()
					}
					if executions != 0 {
						t.Errorf("tool executed %d times", executions)
					}
					if policyCalls != 1 {
						t.Errorf("policy calls = %d, want 1", policyCalls)
					}
					var results []types.ToolResultContent
					for _, m := range responseMessages {
						if m.Role != types.RoleTool {
							continue
						}
						for _, p := range m.Content {
							if r, ok := p.(types.ToolResultContent); ok {
								results = append(results, r)
							}
						}
					}
					if len(results) != 1 || results[0].ToolCallID != "call" || results[0].Output == nil ||
						results[0].Output.Type != types.ToolResultOutputExecutionDenied || results[0].Output.Reason != "Current policy denies this tool" {
						t.Fatalf("tool results = %+v", results)
					}
				})
			}
		}
	}
}

// r85genLastUserTexts is the last user text of each recorded prompt (TS
// lastUserText over model.doGenerateCalls).
func r85genLastUserTexts(prompts [][]string) []string {
	out := make([]string, len(prompts))
	for i, p := range prompts {
		if len(p) > 0 {
			out[i] = p[len(p)-1]
		}
	}
	return out
}

func r85genTextAnswers(answers ...string) *steerModel {
	return &steerModel{next: func(call int) *types.GenerateResult {
		return &types.GenerateResult{Text: answers[call-1], Content: []types.ContentPart{types.TextContent{Text: answers[call-1]}}, FinishReason: types.FinishReasonStop}
	}}
}

// Ports of steering.test.ts (TS cf2796985a): queued messages wait until they
// are promoted, promotion with nothing queued is false, ending the run
// resolves queued messages as not delivered, and ids are shared with Steer.
func TestR85genSteeringQueueRun(t *testing.T) {
	steering := NewSteering()
	run, err := startSteeringRun(steering)
	if err != nil {
		t.Fatal(err)
	}
	steering.Queue("first", WithSteerID("queued-1"))
	steering.Queue("second", WithSteerID("queued-2"))
	if run.hasPendingMessages() {
		t.Fatal("queued messages are pending")
	}
	if got := run.takeMessages(); len(got) != 0 {
		t.Fatalf("takeMessages = %v, want none", got)
	}
	if !run.promoteQueuedMessage() {
		t.Fatal("promoteQueuedMessage = false")
	}
	got := run.takeMessages()
	if len(got) != 1 || userText(got[0]) != "first" {
		t.Fatalf("takeMessages = %+v, want [first]", got)
	}

	empty, _ := startSteeringRun(NewSteering())
	if empty.promoteQueuedMessage() {
		t.Fatal("promoteQueuedMessage with nothing queued = true")
	}
	var nilRun *steeringRun
	if nilRun.promoteQueuedMessage() {
		t.Fatal("nil run promoted")
	}

	ending := NewSteering()
	endRun, _ := startSteeringRun(ending)
	h := ending.Queue("too late", WithSteerID("queued-1"))
	endRun.end(SteeringStopCondition)
	if o := waitOutcome(t, h); o.Delivered || o.ID != "queued-1" || o.Reason != SteeringStopCondition {
		t.Fatalf("outcome = %+v", o)
	}
	if endRun.promoteQueuedMessage() {
		t.Fatal("promoted after end")
	}

	shared := NewSteering()
	steered := shared.Steer("first", WithSteerID("message-1"))
	if shared.Queue("retry", WithSteerID("message-1")) != steered {
		t.Fatal("queue with a steered id returned a new handle")
	}
	if _, err := shared.QueueMessage(types.Message{Role: types.RoleAssistant}); err == nil {
		t.Fatal("QueueMessage accepted an assistant message")
	}
}

// Port of generate-text-steering.test.ts 'queue' > 'should wait until the
// current task is done to deliver a queued message'.
func TestR85genQueueWaitsForTask(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(call int) *types.GenerateResult {
		switch call {
		case 1:
			return toolCallResult()
		case 2:
			return &types.GenerateResult{Text: "It is sunny in Paris.", FinishReason: types.FinishReasonStop}
		}
		return &types.GenerateResult{Text: "The readme is updated.", FinishReason: types.FinishReasonStop}
	}}
	tools := []types.Tool{{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		handle = steering.Queue("Then update the readme.")
		return "sunny", nil
	}}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "What is the weather in Paris?", Tools: tools,
		StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"What is the weather in Paris?", "What is the weather in Paris?", "Then update the readme."}
	if got := r85genLastUserTexts(sm.prompts); !reflect.DeepEqual(got, want) {
		t.Fatalf("last user texts = %q, want %q", got, want)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 2 {
		t.Fatalf("outcome = %+v", o)
	}
	if result.Text != "The readme is updated." {
		t.Fatalf("text = %q", result.Text)
	}
}

// Ports of 'should deliver one queued message per task in order' and
// 'should deliver steering messages before queued messages'.
func TestR85genQueueOrder(t *testing.T) {
	steering := NewSteering()
	first := steering.Queue("Then update the readme.")
	second := steering.Queue("Then add a changelog entry.")
	sm := r85genTextAnswers("It is sunny in Paris.", "The readme is updated.", "The changelog is updated.")
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"What is the weather in Paris?", "Then update the readme.", "Then add a changelog entry."}
	if got := r85genLastUserTexts(sm.prompts); !reflect.DeepEqual(got, want) {
		t.Fatalf("last user texts = %q, want %q", got, want)
	}
	if o := waitOutcome(t, first); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("first = %+v", o)
	}
	if o := waitOutcome(t, second); !o.Delivered || o.StepNumber != 2 {
		t.Fatalf("second = %+v", o)
	}

	steering = NewSteering()
	queued := steering.Queue("Then update the readme.")
	sm = &steerModel{next: func(call int) *types.GenerateResult {
		answers := []string{"It is sunny in Paris.", "It is 20 degrees.", "The readme is updated."}
		if call == 1 {
			steering.Steer("Use Celsius.")
		}
		return &types.GenerateResult{Text: answers[call-1], FinishReason: types.FinishReasonStop}
	}}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: sm.model(), Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
	}); err != nil {
		t.Fatal(err)
	}
	want = []string{"What is the weather in Paris?", "Use Celsius.", "Then update the readme."}
	if got := r85genLastUserTexts(sm.prompts); !reflect.DeepEqual(got, want) {
		t.Fatalf("last user texts = %q, want %q", got, want)
	}
	if o := waitOutcome(t, queued); !o.Delivered || o.StepNumber != 2 {
		t.Fatalf("queued = %+v", o)
	}
}

// Ports of 'should not deliver a queued message when a stop condition ends
// the run', '... when the response finished with %s' (length,
// content-filter) and '... after the run finished'.
func TestR85genQueueNotDelivered(t *testing.T) {
	steering := NewSteering()
	h := steering.Queue("Then update the readme.", WithSteerID("queued-1"))
	sm := r85genTextAnswers("It is sunny in Paris.")
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: sm.model(), Prompt: "What is the weather in Paris?", ExperimentalSteering: steering}); err != nil {
		t.Fatal(err)
	}
	if len(sm.prompts) != 1 {
		t.Fatalf("model calls = %d, want 1", len(sm.prompts))
	}
	if o := waitOutcome(t, h); o.Delivered || o.ID != "queued-1" || o.Reason != SteeringStopCondition || userText(o.Message) != "Then update the readme." || o.Message.Role != types.RoleUser {
		t.Fatalf("outcome = %+v", o)
	}
	if o := waitOutcome(t, steering.Queue("Then update the readme.", WithSteerID("late"))); o.Delivered || o.Reason != SteeringFinished {
		t.Fatalf("late outcome = %+v", o)
	}

	for _, reason := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonContentFilter} {
		t.Run(string(reason), func(t *testing.T) {
			steering := NewSteering()
			h := steering.Queue("Then update the readme.")
			sm := &steerModel{next: func(int) *types.GenerateResult {
				return &types.GenerateResult{Text: "It is sunny", FinishReason: reason}
			}}
			if _, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: sm.model(), Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(5)}, ExperimentalSteering: steering,
			}); err != nil {
				t.Fatal(err)
			}
			if len(sm.prompts) != 1 {
				t.Fatalf("model calls = %d, want 1", len(sm.prompts))
			}
			if o := waitOutcome(t, h); o.Delivered || o.Reason != SteeringFinished {
				t.Fatalf("outcome = %+v", o)
			}
		})
	}
}
