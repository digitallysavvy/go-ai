package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func portgenCounterTool(validate func(interface{}) (interface{}, error)) types.Tool {
	return types.Tool{
		Name:          "tool1",
		Parameters:    map[string]interface{}{"type": "object"},
		ValidateInput: validate,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
}

func portgenCounterApproval(secret []byte, schemaInput map[string]interface{}) CollectedToolApproval {
	input := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: input}
	request := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", ToolCall: call}
	if schemaInput != nil {
		request.InputSchemaInput = schemaInput
		request.InputSchemaInputSet = true
	}
	if secret != nil {
		sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
		if err != nil {
			panic(err)
		}
		request.Signature = sig
	}
	return CollectedToolApproval{
		ApprovalRequest:  request,
		ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		ToolCall:         call,
	}
}

// Port of validate-tool-approvals.test.ts 'should reject signed approvals
// changed by an in-place schema validator' (TS 3c09e0011c).
func TestPortgenApprovalRejectsInPlaceSchemaValidator(t *testing.T) {
	secret := []byte("test-secret")
	approval := portgenCounterApproval(secret, nil)
	original := portgenCounterApproval(secret, nil)
	tool := portgenCounterTool(func(v interface{}) (interface{}, error) {
		counter := v.(map[string]interface{})["counter"].(map[string]interface{})
		counter["value"] = counter["value"].(float64) + 1
		return v, nil
	})
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{tool},
		ToolApprovalSecret:    secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
		t.Fatalf("approved=%d invalid=%d, want 0 and 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
	}
	if !strings.Contains(res.InvalidToolApprovals[0].Error.Error(), "does not match the validated schema output") {
		t.Errorf("error = %v", res.InvalidToolApprovals[0].Error)
	}
	if res.InvalidToolApprovals[0].Error.ErrorName() != "AI_InvalidToolInputError" {
		t.Errorf("name = %s", res.InvalidToolApprovals[0].Error.ErrorName())
	}
	if !reflect.DeepEqual(approval, original) {
		t.Errorf("input approval mutated: %+v", approval)
	}
}

// Port of tool-approval-follow-up.test.ts 'should isolate in-place refinement
// during signed approval replay (schema input supplied: %s)' at the
// ValidateApprovedToolApprovals level.
func TestPortgenApprovalIsolatesInPlaceRefinement(t *testing.T) {
	for _, hasSchemaInput := range []bool{false, true} {
		name := "without schema input"
		var schemaInput map[string]interface{}
		if hasSchemaInput {
			name = "with schema input"
			schemaInput = map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
		}
		t.Run(name, func(t *testing.T) {
			secret := []byte("test-secret")
			approval := portgenCounterApproval(secret, schemaInput)
			original := portgenCounterApproval(secret, schemaInput)
			refiners := map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
				counter := o.ToolCall.Arguments["counter"].(map[string]interface{})
				counter["value"] = counter["value"].(float64) + 1
				return o.ToolCall.Arguments, nil
			}}
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: []CollectedToolApproval{approval},
				Tools:                 []types.Tool{portgenCounterTool(nil)},
				ToolApprovalSecret:    secret,
				RefineToolInput:       refiners,
			})
			if err != nil {
				t.Fatal(err)
			}
			if hasSchemaInput {
				if len(res.ApprovedToolApprovals) != 1 {
					t.Fatalf("approved=%d invalid=%d", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
				}
				got := res.ApprovedToolApprovals[0].ToolCall.Arguments
				want := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("approved input = %v, want %v", got, want)
				}
			} else {
				if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
					t.Fatalf("approved=%d invalid=%d, want 0 and 1", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
				}
				if !strings.Contains(res.InvalidToolApprovals[0].Error.Error(), "does not match the validated schema output") {
					t.Errorf("error = %v", res.InvalidToolApprovals[0].Error)
				}
			}
			if !reflect.DeepEqual(approval, original) {
				t.Errorf("input approval mutated: %+v", approval)
			}
		})
	}
}

// Port of tool-approval-follow-up.test.ts 'should isolate in-place refinement
// during signed approval replay' for generateText (TS 3c09e0011c): the
// history is not mutated and the approved input is what executes.
func TestPortgenGenerateTextIsolatesInPlaceRefinement(t *testing.T) {
	for _, hasSchemaInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "no schema input", true: "schema input"}[hasSchemaInput], func(t *testing.T) {
			secret := []byte("test-secret")
			input := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
			sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
			if err != nil {
				t.Fatal(err)
			}
			request := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", Signature: sig}
			if hasSchemaInput {
				request.InputSchemaInput = map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
				request.InputSchemaInputSet = true
			}
			messages := []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "call-1", ToolName: "tool1", Arguments: input},
					request,
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true}}},
			}
			wantInput := map[string]interface{}{"counter": map[string]interface{}{"value": float64(2)}}
			wantSchemaInput := map[string]interface{}{"counter": map[string]interface{}{"value": float64(1)}}
			var executed []map[string]interface{}
			tool := portgenCounterTool(nil)
			tool.ToolApproval = types.ToolApprovalStatusUserApproval
			tool.Execute = func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
				executed = append(executed, in)
				return "recorded", nil
			}
			model := newRecordingModel("OK")
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:                          model,
				Tools:                          []types.Tool{tool},
				Messages:                       messages,
				StopWhen:                       []StopCondition{StepCountIs(3)},
				ExperimentalToolApprovalSecret: secret,
				ExperimentalRefineToolInput: map[string]ToolInputRefiner{"tool1": func(_ context.Context, o ToolInputRefinementOptions) (map[string]interface{}, error) {
					counter := o.ToolCall.Arguments["counter"].(map[string]interface{})
					counter["value"] = counter["value"].(float64) + 1
					return o.ToolCall.Arguments, nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = result
			// The refiner also runs when the model's own tool calls are
			// parsed, but this call is a replay: no model tool call exists.
			if hasSchemaInput {
				if len(executed) != 1 || !reflect.DeepEqual(executed[0], wantInput) {
					t.Fatalf("executed = %v, want one call with %v", executed, wantInput)
				}
			} else if len(executed) != 0 {
				t.Fatalf("executed = %v, want none", executed)
			}
			if !reflect.DeepEqual(input, wantInput) {
				t.Errorf("history tool call input mutated: %v", input)
			}
			if hasSchemaInput && !reflect.DeepEqual(request.InputSchemaInput, wantSchemaInput) {
				t.Errorf("history schema input mutated: %v", request.InputSchemaInput)
			}
			tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
			if hasSchemaInput {
				if tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "recorded" {
					t.Errorf("output = %+v", tr.Output)
				}
			} else if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText || !strings.Contains(tr.Output.Value.(string), "does not match the validated schema output") {
				t.Errorf("output = %+v", tr.Output)
			}
		})
	}
}

// portgenFindApprovalRequest returns the first approval request in the
// assistant messages.
func portgenFindApprovalRequest(msgs []types.Message) (types.ToolApprovalRequestContent, bool) {
	for _, msg := range msgs {
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.ToolApprovalRequestContent:
				return p, true
			case *types.ToolApprovalRequestContent:
				return *p, true
			}
		}
	}
	return types.ToolApprovalRequestContent{}, false
}

type portgenApprovalCall struct {
	status   func() types.ToolApprovalStatus
	executed *int
	messages []types.Message
}

func portgenCallApprovalTool(c portgenApprovalCall, secret []byte) (*GenerateTextResult, error) {
	step := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step++
			if step == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonToolCalls,
					ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "test"}}},
				}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
	messages := c.messages
	if messages == nil {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}}
	}
	return GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Tools: []types.Tool{{
			Name:       "tool1",
			Parameters: valueStringSchema(),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				*c.executed++
				return "result1", nil
			},
		}},
		ToolApproval: map[string]interface{}{"tool1": types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: c.status()}
		})},
		ExperimentalToolApprovalSecret: secret,
		StopWhen:                       []StopCondition{StepCountIs(3)},
		Messages:                       messages,
	})
}

// Port of generate-text.test.ts 'should sign the approval request when the
// tool approval status is %s' and 'should not sign the approval request when
// the tool call is denied automatically' (TS 63593a7f6a).
func TestPortgenGenerateTextSignsApprovalsExceptAutomaticDenials(t *testing.T) {
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied} {
		t.Run(string(status), func(t *testing.T) {
			executed := 0
			result, err := portgenCallApprovalTool(portgenApprovalCall{status: func() types.ToolApprovalStatus { return status }, executed: &executed}, secret)
			if err != nil {
				t.Fatal(err)
			}
			request, ok := portgenFindApprovalRequest(result.ResponseMessages)
			if !ok {
				t.Fatalf("no approval request in %+v", result.ResponseMessages)
			}
			if status == types.ToolApprovalStatusDenied {
				if request.Signature != "" || !request.IsAutomatic || request.ToolCallID != "call-1" {
					t.Fatalf("request = %+v, want an automatic unsigned request", request)
				}
				return
			}
			want, err := SignToolApproval(secret, request.ApprovalID, "call-1", "tool1", map[string]interface{}{"value": "test"})
			if err != nil {
				t.Fatal(err)
			}
			if request.Signature != want {
				t.Fatalf("signature = %q, want %q", request.Signature, want)
			}
		})
	}
}

// Port of 'should reject an approved response to a tool call that was denied
// automatically' and 'should continue a conversation that ends with an
// automatic denial'.
func TestPortgenGenerateTextAutomaticDenialReplay(t *testing.T) {
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	user := types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test"}}}
	t.Run("approved replay is rejected", func(t *testing.T) {
		executed := 0
		status := types.ToolApprovalStatusDenied
		result, err := portgenCallApprovalTool(portgenApprovalCall{status: func() types.ToolApprovalStatus { return status }, executed: &executed}, secret)
		if err != nil {
			t.Fatal(err)
		}
		request, _ := portgenFindApprovalRequest(result.ResponseMessages)
		// The client omits the history that caused the denial, so the policy
		// no longer denies the call when the approval is checked again.
		status = types.ToolApprovalStatusNotApplicable
		_, err = portgenCallApprovalTool(portgenApprovalCall{
			status:   func() types.ToolApprovalStatus { return status },
			executed: &executed,
			messages: []types.Message{
				user,
				result.ResponseMessages[0],
				{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: request.ApprovalID, Approved: true}}},
			},
		}, secret)
		if err == nil || !IsInvalidToolApprovalSignatureError(err) || !strings.Contains(err.Error(), "missing signature") {
			t.Fatalf("err = %v, want a missing signature error", err)
		}
		if executed != 0 {
			t.Fatalf("executed = %d, want 0", executed)
		}
	})
	t.Run("denied conversation continues", func(t *testing.T) {
		executed := 0
		denied := func() types.ToolApprovalStatus { return types.ToolApprovalStatusDenied }
		result, err := portgenCallApprovalTool(portgenApprovalCall{status: denied, executed: &executed}, secret)
		if err != nil {
			t.Fatal(err)
		}
		continued, err := portgenCallApprovalTool(portgenApprovalCall{
			status:   denied,
			executed: &executed,
			messages: append([]types.Message{user}, result.ResponseMessages[0], result.ResponseMessages[1]),
		}, secret)
		if err != nil {
			t.Fatal(err)
		}
		if continued.Text != "done" || executed != 0 {
			t.Fatalf("text=%q executed=%d", continued.Text, executed)
		}
	})
}

// Port of generate-text.test.ts 'messages array passed to the approval
// function' (TS 0b8cbfadb8): the approval function gets one messages slice
// per step, shared by every tool call of that step and never reused by
// another step, also when PrepareStep returns the same slice every time.
func portgenCollectApprovalMessages(t *testing.T, prepare func(context.Context, PrepareStepOptions) PrepareStepOptions) [][]types.Message {
	t.Helper()
	var collected [][]types.Message
	responses := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			step := responses
			responses++
			if step < 2 {
				calls := make([]types.ToolCall, 0, 2)
				for _, index := range []string{"1", "2"} {
					calls = append(calls, types.ToolCall{ID: "call-" + string(rune('0'+step)) + "-" + index, ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}})
				}
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: calls}, nil
			}
			return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model,
		Tools: []types.Tool{{
			Name:       "tool1",
			Parameters: valueStringSchema(),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "result1", nil
			},
		}},
		ToolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			collected = append(collected, o.Messages)
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}),
		PrepareStep: prepare,
		StopWhen:    []StopCondition{StepCountIs(3)},
		Prompt:      "test-input",
	})
	if err != nil {
		t.Fatal(err)
	}
	return collected
}

func portgenSameSlice(a, b []types.Message) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

func TestPortgenApprovalMessagesSliceIsPerStep(t *testing.T) {
	t.Run("same for the calls of a step and new for every step", func(t *testing.T) {
		got := portgenCollectApprovalMessages(t, nil)
		if len(got) != 4 {
			t.Fatalf("approval calls = %d, want 4", len(got))
		}
		if !portgenSameSlice(got[0], got[1]) || !portgenSameSlice(got[2], got[3]) || portgenSameSlice(got[0], got[2]) {
			t.Fatalf("identity: 0=1 %v, 2=3 %v, 0=2 %v", portgenSameSlice(got[0], got[1]), portgenSameSlice(got[2], got[3]), portgenSameSlice(got[0], got[2]))
		}
	})
	t.Run("new for every step when PrepareStep returns the same slice", func(t *testing.T) {
		prepared := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prepared-input"}}}}
		got := portgenCollectApprovalMessages(t, func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			step.Messages = prepared
			return step
		})
		if len(got) != 4 {
			t.Fatalf("approval calls = %d, want 4", len(got))
		}
		for i, msgs := range got {
			if !reflect.DeepEqual(msgs, prepared) {
				t.Errorf("call %d messages = %+v, want the prepared messages", i, msgs)
			}
			if portgenSameSlice(msgs, prepared) {
				t.Errorf("call %d got the slice PrepareStep returned", i)
			}
		}
		if !portgenSameSlice(got[0], got[1]) || !portgenSameSlice(got[2], got[3]) || portgenSameSlice(got[0], got[2]) {
			t.Fatalf("identity: 0=1 %v, 2=3 %v, 0=2 %v", portgenSameSlice(got[0], got[1]), portgenSameSlice(got[2], got[3]), portgenSameSlice(got[0], got[2]))
		}
	})
}

// Port of decide.test.ts 'downloads image URLs and sends their bytes to the
// decision model' (TS 79d2004947): the call's download hook is the only
// downloader.
func TestPortgenDecideDownloadsFileStateThroughTheHook(t *testing.T) {
	model := stateTestModel()
	var requested []DownloadRequest
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, Questions: stateTestQuestions(),
		ExperimentalDownload: func(_ context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
			requested = append(requested, requests...)
			return []*DownloadResult{{Data: png, MediaType: "image/png"}}, nil
		},
		State: []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
			MediaType: "image/png",
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/package.png"},
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 1 || requested[0].URL != "https://example.com/package.png" {
		t.Fatalf("requested = %+v", requested)
	}
	parts, ok := model.calls[0].State.([]provider.DecisionStatePart)
	if !ok || len(parts) != 1 || parts[0].Type != "file" || parts[0].File.FileData.Type != types.FileDataTypeData || !reflect.DeepEqual(parts[0].File.FileData.Data, png) {
		t.Fatalf("model state = %#v", model.calls[0].State)
	}
}

// Port of 'rejects remote file state without a download hook'.
func TestPortgenDecideRejectsRemoteFileStateWithoutDownloadHook(t *testing.T) {
	model := stateTestModel()
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, Questions: stateTestQuestions(),
		State: []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
			MediaType: "image/png",
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "https://attacker.example/package.png"},
		})},
	})
	var downloadErr *providererrors.DownloadError
	if !errors.As(err, &downloadErr) {
		t.Fatalf("err = %v, want a DownloadError", err)
	}
	if !strings.Contains(err.Error(), "experimental_download") || downloadErr.URL != "https://attacker.example/package.png" {
		t.Fatalf("err = %v (url %q)", err, downloadErr.URL)
	}
	if len(model.calls) != 0 {
		t.Fatal("model was called")
	}
	// Inline data needs no hook.
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, Questions: stateTestQuestions(),
		State: []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
			MediaType: "image/png",
			FileData:  types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		})},
	}); err != nil {
		t.Fatalf("inline data: %v", err)
	}
}

// Ports steering.test.ts 'should report why the run ended for messages sent
// after a run that ended with %s' (TS 28d17e6fec).
func TestPortgenSteeringReportsWhyTheRunEnded(t *testing.T) {
	for _, reason := range []SteeringNotDeliveredReason{SteeringFinished, SteeringStopCondition, SteeringWaitingForToolResults, SteeringAborted, SteeringError} {
		t.Run(string(reason), func(t *testing.T) {
			steering := NewSteering()
			run, err := startSteeringRun(steering)
			if err != nil {
				t.Fatal(err)
			}
			run.end(reason)
			if o := waitOutcome(t, steering.Steer("steered after the run")); o.Delivered || o.Reason != reason {
				t.Fatalf("steer outcome = %+v, want %s", o, reason)
			}
			if o := waitOutcome(t, steering.Queue("queued after the run")); o.Delivered || o.Reason != reason {
				t.Fatalf("queue outcome = %+v, want %s", o, reason)
			}
		})
	}
}

func portgenResolveSteer(t *testing.T, message types.Message, sent types.Message) SteeringOutcome {
	t.Helper()
	steering := NewSteering()
	handle, err := steering.SteerMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	run, err := startSteeringRun(steering)
	if err != nil {
		t.Fatal(err)
	}
	run.takeMessages()
	run.resolveTakenMessages(0, []types.Message{sent})
	return waitOutcome(t, handle)
}

// Ports steering.test.ts 'should deliver messages whose parts are sent with
// provider options' and 'should not deliver messages whose text prepareStep
// changed'. The two TS string-versus-text-part cases collapse in Go, where
// message content is always a list of parts.
func TestPortgenSteeringMatchesSentMessagesIgnoringPartProviderOptions(t *testing.T) {
	user := func(parts ...types.ContentPart) types.Message {
		return types.Message{Role: types.RoleUser, Content: parts}
	}
	hello := user(types.TextContent{Text: "hello"})
	withOptions := user(types.TextContent{Text: "hello", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}})
	if o := portgenResolveSteer(t, hello, withOptions); !o.Delivered || o.StepNumber != 0 {
		t.Errorf("sent with provider options: %+v", o)
	}
	if o := portgenResolveSteer(t, withOptions, hello); !o.Delivered {
		t.Errorf("steered with provider options, sent without: %+v", o)
	}
	if o := portgenResolveSteer(t, hello, user(types.TextContent{Text: "[12:00] hello"})); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
		t.Errorf("changed text: %+v", o)
	}
	if o := portgenResolveSteer(t, hello, types.Message{Role: types.RoleAssistant, Content: hello.Content}); o.Delivered {
		t.Errorf("assistant message with the same content counted as sent: %+v", o)
	}
}

// Ports generate-text-steering.test.ts 'should deliver messages that
// prepareStep turns into text parts with provider options' and the late
// steer outcomes after an abort and after a failure.
func TestPortgenGenerateTextSteeringWithPrepareStepProviderOptions(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	sm := &steerModel{next: func(call int) *types.GenerateResult {
		if call == 1 {
			return toolCallResult()
		}
		return &types.GenerateResult{Text: "It is sunny in Paris, Texas.", FinishReason: types.FinishReasonStop}
	}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  sm.model(),
		Prompt: "What is the weather in Paris?",
		Tools: []types.Tool{{
			Name: "tool1",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				handle = steering.Steer("I meant Paris, Texas.")
				return "sunny", nil
			},
		}},
		StopWhen:             []StopCondition{IsStepCount(5)},
		ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			mapped := make([]types.Message, len(step.Messages))
			for i, m := range step.Messages {
				if m.Role == types.RoleUser {
					parts := make([]types.ContentPart, len(m.Content))
					for j, p := range m.Content {
						if tc, ok := p.(types.TextContent); ok {
							tc.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}
							p = tc
						}
						parts[j] = p
					}
					m.Content = parts
				}
				mapped[i] = m
			}
			step.Messages = mapped
			return step
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
	if len(result.Steps[1].ExperimentalSteeringMessages) != 1 {
		t.Fatalf("step 1 steering messages = %+v", result.Steps[1].ExperimentalSteeringMessages)
	}
	if got := strings.Join(sm.prompts[1], "|"); got != "What is the weather in Paris?|I meant Paris, Texas." {
		t.Fatalf("second prompt users = %q", got)
	}
}

func TestPortgenGenerateTextSteeringAfterFailureAndAbort(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		steering := NewSteering()
		sm := &steerModel{next: func(int) *types.GenerateResult { return nil }}
		model := sm.model()
		model.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, errors.New("boom")
		}
		if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering, MaxRetries: intPtr(0)}); err == nil {
			t.Fatal("expected an error")
		}
		if o := waitOutcome(t, steering.Steer("after the failure")); o.Delivered || o.Reason != SteeringError {
			t.Fatalf("outcome = %+v, want error", o)
		}
	})
	t.Run("abort", func(t *testing.T) {
		steering := NewSteering()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sm := &steerModel{next: func(int) *types.GenerateResult { return nil }}
		model := sm.model()
		model.DoGenerateFunc = func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			cancel()
			return nil, ctx.Err()
		}
		if _, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering, MaxRetries: intPtr(0)}); err == nil {
			t.Fatal("expected an error")
		}
		if o := waitOutcome(t, steering.Steer("after the abort")); o.Delivered || o.Reason != SteeringAborted {
			t.Fatalf("steer outcome = %+v, want aborted", o)
		}
		if o := waitOutcome(t, steering.Queue("queued after the abort")); o.Delivered || o.Reason != SteeringAborted {
			t.Fatalf("queue outcome = %+v, want aborted", o)
		}
	})
}

// ---- batch structured output (TS 3bea041f28) ----

func portgenBatchOutput() Output[map[string]interface{}, map[string]interface{}] {
	return ObjectOutput[map[string]interface{}](ObjectOutputOptions{
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
			"required":             []interface{}{"text"},
			"additionalProperties": false,
		}),
	})
}

func portgenBatchRecorder(requests *[]provider.BatchV4Request) *mockBatchV4 {
	return &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(_ context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			*requests = append(*requests, opts.Requests...)
			return &provider.BatchV4StartResult{BatchID: "batch-123", BatchV4Status: provider.BatchV4Status{Status: "pending"}}, nil
		},
	}
}

// Port of batch.test.ts 'sends the response format of the output
// specification with every text request'.
func TestPortgenStartBatchSendsOutputResponseFormat(t *testing.T) {
	var requests []provider.BatchV4Request
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: portgenBatchRecorder(&requests),
		Output:   portgenBatchOutput(),
		Requests: []BatchRequest{
			textBatchRequest("request-1", "mock-model-id"),
			textBatchRequest("request-2", "mock-model-id"),
			{Image: &BatchImageRequest{ID: "request-3", Model: "mock-image-model-id", Prompt: "a cat"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %d", len(requests))
	}
	for _, request := range requests[:2] {
		format := request.Text.Options.ResponseFormat
		if format == nil || format.Type != "json" {
			t.Fatalf("response format = %+v", format)
		}
		raw, err := json.Marshal(format.Schema)
		if err != nil || !strings.Contains(string(raw), `"properties":{"text":{"type":"string"}}`) || !strings.Contains(string(raw), `"type":"object"`) {
			t.Fatalf("schema = %s (%v)", raw, err)
		}
	}
	if requests[2].Image == nil {
		t.Fatal("third request is not an image request")
	}
}

// Port of 'does not send a response format without an output specification'.
func TestPortgenStartBatchWithoutOutputSendsNoResponseFormat(t *testing.T) {
	var requests []provider.BatchV4Request
	if _, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: portgenBatchRecorder(&requests),
		Requests: []BatchRequest{textBatchRequest("request-1", "mock-model-id")},
	}); err != nil {
		t.Fatal(err)
	}
	if format := requests[0].Text.Options.ResponseFormat; format != nil {
		t.Fatalf("response format = %+v", format)
	}
}

func TestPortgenStartBatchRejectsANonOutputSpecification(t *testing.T) {
	var requests []provider.BatchV4Request
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: portgenBatchRecorder(&requests),
		Output:   "not an output",
		Requests: []BatchRequest{textBatchRequest("request-1", "mock-model-id")},
	})
	if !isInvalidArgumentError(err) || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(requests))
	}
}

func portgenTextBatchItem(id string, result types.GenerateResult) provider.BatchV4ItemResult {
	if result.FinishReason == "" {
		result.FinishReason = types.FinishReasonStop
	}
	if result.Usage.InputTokens == nil {
		three := int64(3)
		result.Usage.InputTokens = &three
	}
	return provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: id, Status: provider.BatchItemSucceeded, TextResult: &result}
}

func portgenCollectBatch(t *testing.T, items []provider.BatchV4ItemResult, output interface{}) []*BatchItemResult {
	t.Helper()
	api := &mockBatchV4{
		providerName: "mock.batch",
		resultsFn: func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
			return &mockBatchItemStream{items: items}, nil
		},
	}
	stream, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider:   api,
		Batch:      BatchReference{Version: 2, ID: "batch-123", Provider: "mock.batch"},
		MaxRetries: intPtr(0),
		Output:     output,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	var out []*BatchItemResult
	for {
		item, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
}

// Ports of the getBatchResults 'output' describe block in batch.test.ts.
func TestPortgenGetBatchResultsParsesOutput(t *testing.T) {
	t.Run("parses and validates the text of successful items", func(t *testing.T) {
		results := portgenCollectBatch(t, []provider.BatchV4ItemResult{
			portgenTextBatchItem("ok", types.GenerateResult{Text: `{"text":"hello"}`}),
		}, portgenBatchOutput())
		if len(results) != 1 || results[0].Text.Text != `{"text":"hello"}` || !reflect.DeepEqual(results[0].Output, map[string]interface{}{"text": "hello"}) || results[0].OutputError != nil {
			t.Fatalf("results = %+v", results)
		}
	})
	t.Run("reports parse failures per item without dropping text and usage", func(t *testing.T) {
		results := portgenCollectBatch(t, []provider.BatchV4ItemResult{
			portgenTextBatchItem("not-json", types.GenerateResult{Text: "oops"}),
			portgenTextBatchItem("wrong-shape", types.GenerateResult{Text: `{"text":1}`}),
			portgenTextBatchItem("ok", types.GenerateResult{Text: `{"text":"fine"}`}),
		}, portgenBatchOutput())
		if len(results) != 3 {
			t.Fatalf("results = %d", len(results))
		}
		if r := results[0]; r.Text.Text != "oops" || r.Text.Usage.InputTokens == nil || *r.Text.Usage.InputTokens != 3 || r.Output != nil ||
			r.OutputError == nil || r.OutputError.Type != "AI_NoObjectGeneratedError" || r.OutputError.Message != "No object generated: could not parse the response." {
			t.Errorf("not-json = %+v / %+v", r, r.OutputError)
		}
		if r := results[1]; r.OutputError == nil || r.OutputError.Type != "AI_NoObjectGeneratedError" || r.OutputError.Message != "No object generated: response did not match schema." {
			t.Errorf("wrong-shape = %+v / %+v", r, r.OutputError)
		}
		if r := results[2]; r.OutputError != nil || !reflect.DeepEqual(r.Output, map[string]interface{}{"text": "fine"}) {
			t.Errorf("ok = %+v", r)
		}
	})
	t.Run("prefers structuredOutputText for JSON output", func(t *testing.T) {
		structured := `{"text":"final"}`
		results := portgenCollectBatch(t, []provider.BatchV4ItemResult{
			portgenTextBatchItem("structured", types.GenerateResult{Text: `draft {"text":"final"}`, StructuredOutputText: &structured}),
		}, portgenBatchOutput())
		if !reflect.DeepEqual(results[0].Output, map[string]interface{}{"text": "final"}) {
			t.Fatalf("output = %+v / %+v", results[0].Output, results[0].OutputError)
		}
	})
	t.Run("reports an output error when the item did not finish with a response", func(t *testing.T) {
		results := portgenCollectBatch(t, []provider.BatchV4ItemResult{
			portgenTextBatchItem("tool-call", types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			}),
		}, portgenBatchOutput())
		if r := results[0]; r.OutputError == nil || r.OutputError.Type != "AI_NoOutputGeneratedError" || r.OutputError.Message != "No output generated." {
			t.Fatalf("result = %+v / %+v", r, r.OutputError)
		}
	})
	t.Run("does not parse failed items or add fields without output", func(t *testing.T) {
		items := []provider.BatchV4ItemResult{
			{Type: provider.BatchRequestTypeText, ID: "failed", Status: provider.BatchItemFailed, Error: &provider.BatchError{Message: "boom"}},
			portgenTextBatchItem("plain", types.GenerateResult{Text: `{"text":"x"}`}),
		}
		withOutput := portgenCollectBatch(t, items, portgenBatchOutput())
		if withOutput[0].Output != nil || withOutput[0].OutputError != nil || withOutput[0].Error == nil {
			t.Fatalf("failed item = %+v", withOutput[0])
		}
		withoutOutput := portgenCollectBatch(t, items, nil)
		if withoutOutput[1].Output != nil || withoutOutput[1].OutputError != nil {
			t.Fatalf("plain item = %+v", withoutOutput[1])
		}
	})
}

func TestPortgenGetBatchResultsRejectsANonOutputSpecification(t *testing.T) {
	api := &mockBatchV4{providerName: "mock.batch"}
	_, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: api,
		Batch:    BatchReference{Version: 2, ID: "batch-123", Provider: "mock.batch"},
		Output:   42,
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v", err)
	}
}

// Port of stopped-by.test.ts 'stoppedBy (generate)' cases (TS 7760e33531).
func TestPortgenGenerateTextStoppedBy(t *testing.T) {
	weatherCall := func(finish types.FinishReason) *types.GenerateResult {
		return &types.GenerateResult{
			FinishReason: finish,
			ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Paris"}, RawArguments: `{ "city": "Paris" }`}},
		}
	}
	answer := func(finish types.FinishReason) *types.GenerateResult {
		return &types.GenerateResult{Text: "It is sunny.", Content: []types.ContentPart{types.TextContent{Text: "It is sunny."}}, FinishReason: finish}
	}
	schemaCity := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}}
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "sunny", nil
	}
	weather := types.Tool{Name: "weather", Parameters: schemaCity, Execute: exec}
	noExecute := types.Tool{Name: "weather", Parameters: schemaCity}
	needsApproval := types.Tool{Name: "weather", Parameters: schemaCity, Execute: exec, ToolApproval: types.ToolApprovalStatusUserApproval}
	mcp := types.Tool{Name: "mcp_tool", ProviderExecuted: true, Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}}}

	cases := []struct {
		name      string
		responses []*types.GenerateResult
		tools     []types.Tool
		stop      []StopCondition
		want      StoppedBy
	}{
		{"model: final answer", []*types.GenerateResult{answer(types.FinishReasonStop)}, nil, nil, StoppedByModel},
		{"model: answers after tool results", []*types.GenerateResult{weatherCall(types.FinishReasonToolCalls), answer(types.FinishReasonStop)}, []types.Tool{weather}, []StopCondition{IsStepCount(5)}, StoppedByModel},
		{"stop-condition", []*types.GenerateResult{weatherCall(types.FinishReasonToolCalls), weatherCall(types.FinishReasonToolCalls), weatherCall(types.FinishReasonToolCalls)}, []types.Tool{weather}, []StopCondition{IsStepCount(2)}, StoppedByStopCondition},
		{"tool-without-execute", []*types.GenerateResult{weatherCall(types.FinishReasonToolCalls)}, []types.Tool{noExecute}, []StopCondition{IsStepCount(5)}, StoppedByToolWithoutExecute},
		{"approval", []*types.GenerateResult{weatherCall(types.FinishReasonToolCalls)}, []types.Tool{needsApproval}, []StopCondition{IsStepCount(5)}, StoppedByApproval},
		{"approval: provider-executed tool", []*types.GenerateResult{{
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls:    []types.ToolCall{{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: map[string]interface{}{"query": "test"}, ProviderExecuted: true}},
			Content: []types.ContentPart{
				types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"},
			},
		}}, []types.Tool{mcp}, []StopCondition{IsStepCount(3)}, StoppedByApproval},
		{"model: output limit cuts off a tool call", []*types.GenerateResult{weatherCall(types.FinishReasonLength)}, []types.Tool{weather}, []StopCondition{IsStepCount(5)}, StoppedByModel},
		{"model: interrupted", []*types.GenerateResult{answer(types.FinishReasonInterrupted)}, nil, nil, StoppedByModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := 0
			model := &testutil.MockLanguageModel{
				ToolSupport: true,
				DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
					r := tc.responses[call]
					call++
					return r, nil
				},
			}
			result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "What is the weather in Paris?", Tools: tc.tools, StopWhen: tc.stop})
			if err != nil {
				t.Fatal(err)
			}
			if result.StoppedBy != tc.want {
				t.Fatalf("StoppedBy = %q, want %q", result.StoppedBy, tc.want)
			}
		})
	}
}

// ---- per-call OnWarning (TS d834c5bb35) ----

func portgenWarnModel(modelWarnings []types.Warning) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id", ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "It is sunny.", FinishReason: types.FinishReasonStop, Warnings: modelWarnings}, nil
		},
	}
}

func TestPortgenOnWarningReplacesTheGlobalLogger(t *testing.T) {
	var global []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) { global = append(global, o) })
	t.Cleanup(func() { SetLogWarnings(nil) })
	note := types.Warning{Type: "other", Message: "Provider note"}

	var got []LogWarningsOptions
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: portgenWarnModel([]types.Warning{note}), Prompt: "q",
		OnWarning: func(o LogWarningsOptions) { got = append(got, o) },
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Provider != "mock-provider" || got[0].Model != "mock-model-id" || !reflect.DeepEqual(got[0].Warnings, []types.Warning{note}) {
		t.Fatalf("onWarning calls = %+v", got)
	}
	if len(global) != 0 {
		t.Fatalf("global logger got %+v", global)
	}

	// Without OnWarning the global logger keeps receiving the warnings.
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: portgenWarnModel([]types.Warning{note}), Prompt: "q"}); err != nil {
		t.Fatal(err)
	}
	if len(global) != 1 || global[0].Provider != "mock-provider" {
		t.Fatalf("global = %+v", global)
	}
}

func TestPortgenPromptWarningsJoinStepWarnings(t *testing.T) {
	note := types.Warning{Type: "other", Message: "Provider note"}
	var got []LogWarningsOptions
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     portgenWarnModel([]types.Warning{note}),
		Messages:  []types.Message{r86genImageMessage(1)},
		OnWarning: func(o LogWarningsOptions) { got = append(got, o) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps[0].Warnings) != 2 || result.Steps[0].Warnings[1] != note {
		t.Fatalf("step warnings = %+v", result.Steps[0].Warnings)
	}
	if result.Steps[0].Warnings[0].Type != "deprecated" {
		t.Fatalf("first step warning = %+v, want the prompt deprecation", result.Steps[0].Warnings[0])
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("result warnings = %+v", result.Warnings)
	}
	// The prompt batch has no provider scope; the model batch has one.
	if len(got) != 2 || got[0].Provider != "" || got[0].Model != "" || len(got[0].Warnings) != 1 || got[1].Provider != "mock-provider" {
		t.Fatalf("onWarning calls = %+v", got)
	}
}

func TestPortgenOnWarningIsSkippedWithoutWarningsAndSurvivesPanics(t *testing.T) {
	calls := 0
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: portgenWarnModel(nil), Prompt: "q",
		OnWarning: func(LogWarningsOptions) { calls++ },
	}); err != nil || calls != 0 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:     portgenWarnModel([]types.Warning{{Type: "other", Message: "Provider note"}}),
		Messages:  []types.Message{r86genImageMessage(1)},
		OnWarning: func(LogWarningsOptions) { calls++; panic("onWarning failed") },
	})
	if err != nil || calls != 2 || len(result.Steps[0].Warnings) != 2 {
		t.Fatalf("err = %v, calls = %d, result = %+v", err, calls, result)
	}
}

func TestPortgenOnWarningReceivesTheUnsupportedTimeoutWarning(t *testing.T) {
	var global int
	SetLogWarnings(func(LogWarningsOptions) { global++ })
	t.Cleanup(func() { SetLogWarnings(nil) })
	var got []LogWarningsOptions
	chunk := time.Second
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: portgenWarnModel(nil), Prompt: "q", Timeout: &TimeoutConfig{PerChunk: &chunk},
		OnWarning: func(o LogWarningsOptions) { got = append(got, o) },
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Warnings[0].Feature != "timeout.chunkMs" || global != 0 {
		t.Fatalf("got = %+v, global = %d", got, global)
	}
}

// ---- steering interrupt (TS e4039889ba) ----

func portgenUserTexts(opts *provider.GenerateOptions) []string {
	var out []string
	for _, m := range opts.Prompt.Messages {
		if m.Role == types.RoleUser {
			out = append(out, userText(m))
		}
	}
	return out
}

// portgenHangingFirstCall is a model whose first call hangs until its context
// is cancelled; onFirstCall runs inside it. Later calls return responses[i].
type portgenHangingModel struct {
	mu    sync.Mutex
	calls []*provider.GenerateOptions
	ctxs  []context.Context
}

func (m *portgenHangingModel) model(onFirstCall func(), responses ...func(*provider.GenerateOptions) *types.GenerateResult) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			m.mu.Lock()
			m.calls = append(m.calls, opts)
			m.ctxs = append(m.ctxs, ctx)
			n := len(m.calls)
			m.mu.Unlock()
			if n == 1 {
				if onFirstCall != nil {
					onFirstCall()
				}
				<-ctx.Done()
				return nil, context.Cause(ctx)
			}
			return responses[n-2](opts), nil
		},
	}
}

func portgenText(text string, output int64) *types.GenerateResult {
	return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop, Usage: types.Usage{OutputTokens: &output}}
}

func TestPortgenSteeringInterruptRedoesTheStep(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	h := &portgenHangingModel{}
	model := h.model(func() { handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt()) },
		func(*provider.GenerateOptions) *types.GenerateResult {
			return portgenText("It is sunny in Paris, Texas.", 10)
		})
	var stepStarts int
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		OnStepStart: func(context.Context, OnStepStartEvent) { stepStarts++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 2 || h.ctxs[0].Err() == nil {
		t.Fatalf("calls = %d, first cancelled = %v", len(h.calls), h.ctxs[0].Err() != nil)
	}
	if got := portgenUserTexts(h.calls[1]); !reflect.DeepEqual(got, []string{"What is the weather in Paris?", "Use Paris, Texas instead."}) {
		t.Fatalf("second call users = %v", got)
	}
	if len(result.Steps) != 1 || result.Text != "It is sunny in Paris, Texas." || stepStarts != 1 {
		t.Fatalf("steps = %d, text = %q, stepStarts = %d", len(result.Steps), result.Text, stepStarts)
	}
	if len(result.Steps[0].ExperimentalSteeringMessages) != 1 {
		t.Fatalf("steering messages = %+v", result.Steps[0].ExperimentalSteeringMessages)
	}
	// the cancelled call is left out of the usage
	if result.TotalUsage.OutputTokens == nil || *result.TotalUsage.OutputTokens != 10 {
		t.Fatalf("total usage = %+v", result.TotalUsage)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPortgenSteeringInterruptKeepsEarlierMessagesAndRepeats(t *testing.T) {
	steering := NewSteering()
	earlier := steering.Steer("Answer in Celsius.")
	var outcomes []*SteerHandle
	var mu sync.Mutex
	calls := 0
	var secondCallUsers []string
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n <= 2 {
				outcomes = append(outcomes, steering.Steer(fmt.Sprintf("Correction %d.", n), WithSteerInterrupt()))
				<-ctx.Done()
				return nil, context.Cause(ctx)
			}
			secondCallUsers = portgenUserTexts(opts)
			return portgenText("done", 10), nil
		}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(result.Steps) != 1 {
		t.Fatalf("calls = %d, steps = %d", calls, len(result.Steps))
	}
	if !reflect.DeepEqual(secondCallUsers, []string{"q", "Answer in Celsius.", "Correction 1.", "Correction 2."}) {
		t.Fatalf("users = %v", secondCallUsers)
	}
	if len(result.Steps[0].ExperimentalSteeringMessages) != 3 {
		t.Fatalf("steering messages = %+v", result.Steps[0].ExperimentalSteeringMessages)
	}
	for _, h := range append([]*SteerHandle{earlier}, outcomes...) {
		if o := waitOutcome(t, h); !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	}
}

func TestPortgenSteeringInterruptPrepareStepRunsAgainWithStartContexts(t *testing.T) {
	steering := NewSteering()
	h := &portgenHangingModel{}
	model := h.model(func() { steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt()) },
		func(*provider.GenerateOptions) *types.GenerateResult { return portgenText("sunny", 10) })
	var inputs []interface{}
	var startEvents, endEvents int
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "q", ExperimentalSteering: steering,
		RuntimeContext: map[string]interface{}{"prepareStepCalls": 0},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			inputs = append(inputs, step.RuntimeContext)
			step.RuntimeContext = map[string]interface{}{"prepareStepCalls": len(inputs)}
			return step
		},
		OnLanguageModelCallStart: func(context.Context, LanguageModelCallStartEvent) { startEvents++ },
		OnLanguageModelCallEnd:   func(context.Context, LanguageModelCallEndEvent) { endEvents++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"prepareStepCalls": 0}
	if len(inputs) != 2 || !reflect.DeepEqual(inputs[0], want) || !reflect.DeepEqual(inputs[1], want) {
		t.Fatalf("prepareStep runtime contexts = %v", inputs)
	}
	if startEvents != 2 || endEvents != 1 {
		t.Fatalf("model call start/end = %d/%d", startEvents, endEvents)
	}
}

func TestPortgenSteeringInterruptSentDuringPrepareStepOrBeforeTheRun(t *testing.T) {
	t.Run("during prepareStep", func(t *testing.T) {
		// The interrupt waits for the model call and cancels it right away.
		// TS calls doGenerate with the aborted signal (the model rejects),
		// then redoes the step with the message: two model calls, the first
		// one cancelled and without the message.
		steering := NewSteering()
		var handle *SteerHandle
		var users [][]string
		var cancelled []bool
		model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			users = append(users, portgenUserTexts(opts))
			cancelled = append(cancelled, ctx.Err() != nil)
			if ctx.Err() != nil {
				return nil, context.Cause(ctx)
			}
			return portgenText("sunny", 10), nil
		}}
		result, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "q", ExperimentalSteering: steering,
			PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
				if handle == nil {
					handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
				}
				return step
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(users, [][]string{{"q"}, {"q", "Use Paris, Texas instead."}}) || !reflect.DeepEqual(cancelled, []bool{true, false}) || len(result.Steps) != 1 {
			t.Fatalf("users = %v, cancelled = %v, steps = %d", users, cancelled, len(result.Steps))
		}
		if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("outcome = %+v", o)
		}
	})
	t.Run("before the run: no extra call", func(t *testing.T) {
		steering := NewSteering()
		handle := steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
		calls := 0
		model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return portgenText("Sunny.", 1), nil
		}}
		if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering}); err != nil {
			t.Fatal(err)
		}
		if o := waitOutcome(t, handle); calls != 1 || !o.Delivered || o.StepNumber != 0 {
			t.Fatalf("calls = %d, outcome = %+v", calls, o)
		}
	})
}

func TestPortgenSteeringInterruptFallsBackWhenTheModelCallReturned(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return toolCallResult(), nil
			}
			return portgenText("answer", 1), nil
		}}
	executed := 0
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "q", StopWhen: []StopCondition{IsStepCount(3)}, ExperimentalSteering: steering,
		Tools: []types.Tool{{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed++
			handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
			return "ok", nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed != 1 || calls != 2 || len(result.Steps) != 2 {
		t.Fatalf("executed = %d, calls = %d, steps = %d", executed, calls, len(result.Steps))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPortgenSteeringWithoutInterruptDoesNotCancelTheModelCall(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	calls := 0
	cancelled := false
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				handle = steering.Steer("Use Paris, Texas instead.")
				time.Sleep(5 * time.Millisecond)
				cancelled = ctx.Err() != nil
			}
			return portgenText("sunny", 1), nil
		}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", StopWhen: []StopCondition{IsStepCount(3)}, ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled || len(result.Steps) != 2 {
		t.Fatalf("cancelled = %v, steps = %d", cancelled, len(result.Steps))
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPortgenSteeringInterruptCallerAbortIsNotAnInterrupt(t *testing.T) {
	steering := NewSteering()
	ctx, cancel := context.WithCancelCause(context.Background())
	var handle *SteerHandle
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(c context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
			cancel(errors.New("stopped by user"))
			<-c.Done()
			return nil, context.Cause(c)
		}}
	_, err := GenerateText(ctx, GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering, MaxRetries: intPtr(0)})
	if err == nil || !strings.Contains(err.Error(), "stopped by user") || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringAborted {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPortgenSteeringInterruptRepeatedIDInterruptsOnce(t *testing.T) {
	steering := NewSteering()
	h := &portgenHangingModel{}
	send := func() { steering.Steer("Use Paris, Texas instead.", WithSteerID("steer-1"), WithSteerInterrupt()) }
	model := h.model(send, func(*provider.GenerateOptions) *types.GenerateResult {
		send()
		return portgenText("sunny", 1)
	})
	result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", ExperimentalSteering: steering})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 2 || len(result.Steps) != 1 {
		t.Fatalf("calls = %d, steps = %d", len(h.calls), len(result.Steps))
	}
}

// TS e878ab3925 (ruling 5): a schema result with issues is a failure even
// when it carries a value. Go validators return an error or nil
// (schema.Validator.Validate), and Tool.ValidateInput returns (value, error):
// an error always wins over the value on every tool input path.
func TestPortgenToolInputValidationErrorWinsOverValue(t *testing.T) {
	issue := errors.New("input has issues")
	validate := func(v interface{}) (interface{}, error) { return v, issue }
	t.Run("parsing a model tool call", func(t *testing.T) {
		executed := 0
		tool := types.Tool{Name: "tool1", Parameters: map[string]interface{}{"type": "object"}, ValidateInput: validate,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				executed++
				return "ok", nil
			}}
		calls := 0
		model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "tool1", Arguments: map[string]interface{}{"a": 1.0}, RawArguments: `{"a":1}`}}}, nil
			}
			return portgenText("done", 1), nil
		}}
		result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "q", Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(3)}})
		if err != nil {
			t.Fatal(err)
		}
		if executed != 0 || len(result.Steps[0].ToolCalls) != 1 || !result.Steps[0].ToolCalls[0].Invalid {
			t.Fatalf("executed = %d, calls = %+v", executed, result.Steps[0].ToolCalls)
		}
	})
	t.Run("approval replay", func(t *testing.T) {
		approval := portgenCounterApproval(nil, nil)
		res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval},
			Tools:                 []types.Tool{portgenCounterTool(validate)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
			t.Fatalf("approved=%d invalid=%d", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
		}
	})
}
