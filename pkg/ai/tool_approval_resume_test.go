package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// ---------------------------------------------------------------------------
// helpers

func valueStringSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"value"},
	}
}

func approvalHistory(toolName string, input map[string]interface{}, signature string, approved bool, reason string) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "test-input"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: toolName, Arguments: input},
			types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", Signature: signature},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: approved, Reason: reason},
		}},
	}
}

type recordingModel struct {
	mu      sync.Mutex
	prompts [][]types.Message
	*testutil.MockLanguageModel
}

func newRecordingModel(text string) *recordingModel {
	m := &recordingModel{}
	m.MockLanguageModel = &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			m.mu.Lock()
			m.prompts = append(m.prompts, opts.Prompt.Messages)
			m.mu.Unlock()
			return &types.GenerateResult{Text: text, Content: []types.ContentPart{types.TextContent{Text: text}}, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			m.mu.Lock()
			m.prompts = append(m.prompts, opts.Prompt.Messages)
			m.mu.Unlock()
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: text},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	return m
}

func (m *recordingModel) lastMessageOfFirstPrompt(t *testing.T) types.Message {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.prompts) == 0 || len(m.prompts[0]) == 0 {
		t.Fatal("model was not called")
	}
	return m.prompts[0][len(m.prompts[0])-1]
}

func singleToolResult(t *testing.T, msg types.Message) types.ToolResultContent {
	t.Helper()
	if msg.Role != types.RoleTool || len(msg.Content) != 1 {
		t.Fatalf("message = %+v, want tool message with one part", msg)
	}
	tr, ok := msg.Content[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("part = %T, want ToolResultContent", msg.Content[0])
	}
	return tr
}

// ---------------------------------------------------------------------------
// CollectToolApprovals (port of collect-tool-approvals.test.ts)

func TestCollectToolApprovals(t *testing.T) {
	call := func(id string) types.ToolCallContent {
		return types.ToolCallContent{ToolCallID: id, ToolName: "tool1", Arguments: map[string]interface{}{"value": id}}
	}
	req := func(approvalID, callID string) types.ToolApprovalRequestContent {
		return types.ToolApprovalRequestContent{ApprovalID: approvalID, ToolCallID: callID}
	}
	resp := func(approvalID string, approved bool) types.ToolApprovalResponseContent {
		return types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: approved}
	}
	result := func(callID string, outputType types.ToolResultOutputType) types.ToolResultContent {
		return types.ToolResultContent{ToolCallID: callID, ToolName: "tool1", Output: &types.ToolResultOutput{Type: outputType}}
	}

	t.Run("last message not a tool message", func(t *testing.T) {
		got, err := CollectToolApprovals([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}})
		if err != nil || len(got.ApprovedToolApprovals)+len(got.DeniedToolApprovals) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("request without response is ignored", func(t *testing.T) {
		got, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{call("call-1"), req("a1", "call-1")}},
			{Role: types.RoleTool},
		})
		if err != nil || len(got.ApprovedToolApprovals)+len(got.DeniedToolApprovals) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("pending, processed and denied approvals", func(t *testing.T) {
		got, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				call("c1"), req("a1", "c1"), call("c2"), req("a2", "c2"), call("c3"), req("a3", "c3"),
				call("c4"), req("a4", "c4"), call("c5"), req("a5", "c5"), call("c6"), req("a6", "c6"),
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{
				resp("a1", true),
				resp("a2", true),
				types.ToolApprovalResponseContent{ApprovalID: "a3", Approved: false, Reason: "test-reason"},
				resp("a4", false),
				resp("a5", true), result("c5", types.ToolResultOutputText), // already processed
				resp("a6", false), result("c6", types.ToolResultOutputExecutionDenied),
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids := func(list []CollectedToolApproval) string {
			var out []string
			for _, a := range list {
				out = append(out, a.ToolCall.ID)
			}
			return strings.Join(out, ",")
		}
		if got := ids(got.ApprovedToolApprovals); got != "c1,c2" {
			t.Fatalf("approved = %s", got)
		}
		if got := ids(got.DeniedToolApprovals); got != "c3,c4,c6" {
			t.Fatalf("denied = %s", got)
		}
		if got.DeniedToolApprovals[0].ApprovalResponse.Reason != "test-reason" {
			t.Fatalf("reason = %q", got.DeniedToolApprovals[0].ApprovalResponse.Reason)
		}
		if got.DeniedToolApprovals[2].ExistingToolResult == nil {
			t.Fatal("expected existing execution-denied result on c6")
		}
	})

	t.Run("denied approval with non-denial tool result is ignored", func(t *testing.T) {
		got, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{call("c1"), req("a1", "c1")}},
			{Role: types.RoleTool, Content: []types.ContentPart{resp("a1", false), result("c1", types.ToolResultOutputText)}},
		})
		if err != nil || len(got.DeniedToolApprovals) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("unknown approval id", func(t *testing.T) {
		_, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{call("c1")}},
			{Role: types.RoleTool, Content: []types.ContentPart{resp("unknown-approval", true)}},
		})
		if !IsInvalidToolApprovalError(err) {
			t.Fatalf("err = %v, want InvalidToolApprovalError", err)
		}
		want := `Tool approval response references unknown approvalId: "unknown-approval". No matching tool-approval-request found in message history.`
		if err.Error() != want {
			t.Fatalf("message = %q", err.Error())
		}
	})

	t.Run("referenced tool call missing", func(t *testing.T) {
		_, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{req("a1", "missing-call")}},
			{Role: types.RoleTool, Content: []types.ContentPart{resp("a1", true)}},
		})
		if !IsToolCallNotFoundForApprovalError(err) {
			t.Fatalf("err = %v, want ToolCallNotFoundForApprovalError", err)
		}
		if err.Error() != `Tool call "missing-call" not found for approval request "a1".` {
			t.Fatalf("message = %q", err.Error())
		}
	})

	t.Run("tool calls from Message.ToolCalls", func(t *testing.T) {
		got, err := CollectToolApprovals([]types.Message{
			{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "tool1"}}, Content: []types.ContentPart{req("a1", "c1")}},
			{Role: types.RoleTool, Content: []types.ContentPart{resp("a1", true)}},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateApprovedToolApprovals (port of validate-tool-approvals.test.ts)

func TestValidateApprovedToolApprovals(t *testing.T) {
	executable := func(params map[string]interface{}) types.Tool {
		return types.Tool{
			Name:       "tool1",
			Parameters: params,
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}
	}
	approval := func(input map[string]interface{}) CollectedToolApproval {
		return CollectedToolApproval{
			ApprovalRequest:  types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"},
			ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			ToolCall:         types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: input},
		}
	}
	ctx := context.Background()

	t.Run("keeps approvals whose input matches the schema", func(t *testing.T) {
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval(map[string]interface{}{"value": "test"})},
			Tools:                 []types.Tool{executable(valueStringSchema())},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 || len(got.DeniedToolApprovals) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("returns invalid inputs as recoverable errors", func(t *testing.T) {
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval(map[string]interface{}{"value": 42.0})},
			Tools:                 []types.Tool{executable(valueStringSchema())},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 0 || len(got.InvalidToolApprovals) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !strings.HasPrefix(got.InvalidToolApprovals[0].Error.Error(), "Invalid input for tool tool1:") {
			t.Fatalf("error = %v", got.InvalidToolApprovals[0].Error)
		}
	})

	t.Run("rejects extra forged properties with a strict schema", func(t *testing.T) {
		strict := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		tool := executable(strict)
		tool.Name = "deleteFile"
		a := approval(map[string]interface{}{"path": "/app/.env", "extra": "forged"})
		a.ToolCall.ToolName = "deleteFile"
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{a},
			Tools:                 []types.Tool{tool},
		})
		if err != nil || len(got.InvalidToolApprovals) != 1 || !strings.Contains(got.InvalidToolApprovals[0].Error.Error(), "Invalid input for tool deleteFile") {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("reapplies input refinement before comparing", func(t *testing.T) {
		a := approval(map[string]interface{}{"value": "trimmed"})
		a.ApprovalRequest.InputSchemaInput = map[string]interface{}{"value": " trimmed "}
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{a},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			RefineToolInput: map[string]ToolInputRefiner{
				"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
					return map[string]interface{}{"value": strings.TrimSpace(opts.ToolCall.Arguments["value"].(string))}, nil
				},
			},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 || len(got.InvalidToolApprovals) != 0 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("rejects approvals whose schema input does not produce the approved input", func(t *testing.T) {
		a := approval(map[string]interface{}{"value": "changed"})
		a.ApprovalRequest.InputSchemaInput = map[string]interface{}{"value": "original"}
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{a},
			Tools:                 []types.Tool{executable(valueStringSchema())},
		})
		if err != nil || len(got.InvalidToolApprovals) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if !strings.Contains(got.InvalidToolApprovals[0].Error.Error(), "does not match the validated schema output") {
			t.Fatalf("error = %v", got.InvalidToolApprovals[0].Error)
		}
	})

	t.Run("moves approvals to denied when the policy denies them, carrying the reason", func(t *testing.T) {
		reason := "policy changed"
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval(map[string]interface{}{"value": "test"})},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApproval: map[string]interface{}{
				"tool1": types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason},
			},
		})
		if err != nil || len(got.DeniedToolApprovals) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if d := got.DeniedToolApprovals[0].ApprovalResponse; d.Approved || d.Reason != reason {
			t.Fatalf("denied response = %+v", d)
		}
	})

	t.Run("re-runs a function-based policy on the approved input", func(t *testing.T) {
		var seen map[string]interface{}
		var seenID string
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval(map[string]interface{}{"value": "test"})},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApproval: map[string]types.ToolApprovalValue{
				"tool1": types.SingleToolApprovalFunc(func(args map[string]interface{}, opts types.SingleToolApprovalOptions) types.ToolApprovalResult {
					seen, seenID = args, opts.ToolCallID
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
				}),
			},
		})
		if err != nil || len(got.DeniedToolApprovals) != 1 || seen["value"] != "test" || seenID != "call-1" {
			t.Fatalf("got %+v, %v, seen=%v id=%s", got, err, seen, seenID)
		}
	})

	t.Run("passes through tools without execute (not validated)", func(t *testing.T) {
		tool := executable(valueStringSchema())
		tool.Execute = nil
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{approval(map[string]interface{}{"value": 42.0})},
			Tools:                 []types.Tool{tool},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("signature verification", func(t *testing.T) {
		secret := []byte("test-secret-for-signature")
		input := map[string]interface{}{"value": "test"}
		sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
		if err != nil {
			t.Fatal(err)
		}

		valid := approval(input)
		valid.ApprovalRequest.Signature = sig
		got, err := ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{valid},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApprovalSecret:    secret,
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 {
			t.Fatalf("valid signature: %+v, %v", got, err)
		}

		// TS only reports "missing signature" for a null/undefined
		// signature, which it can distinguish from an explicit empty
		// string; Go's plain string field cannot represent that
		// distinction, so an empty signature falls through to
		// verification and fails as "invalid signature" like TS reports
		// for an explicit empty string.
		missing := approval(input)
		_, err = ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{missing},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApprovalSecret:    secret,
		})
		var sigErr *InvalidToolApprovalSignatureError
		if !errors.As(err, &sigErr) || sigErr.Reason != "invalid signature" {
			t.Fatalf("missing signature err = %v", err)
		}

		tampered := approval(map[string]interface{}{"value": "tampered"})
		tampered.ApprovalRequest.Signature = sig
		_, err = ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{tampered},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApprovalSecret:    secret,
		})
		if !errors.As(err, &sigErr) || sigErr.Reason != "invalid signature" {
			t.Fatalf("tampered err = %v", err)
		}

		// Replay under a different approval id is rejected too.
		replayed := approval(input)
		replayed.ApprovalRequest.ApprovalID = "approval-2"
		replayed.ApprovalRequest.Signature = sig
		_, err = ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{replayed},
			Tools:                 []types.Tool{executable(valueStringSchema())},
			ToolApprovalSecret:    secret,
		})
		if !IsInvalidToolApprovalSignatureError(err) {
			t.Fatalf("replayed err = %v", err)
		}

		// Without a secret the signature is ignored.
		unsigned := approval(input)
		unsigned.ApprovalRequest.Signature = "garbage"
		got, err = ValidateApprovedToolApprovals(ctx, ValidateApprovedToolApprovalsOptions{
			ApprovedToolApprovals: []CollectedToolApproval{unsigned},
			Tools:                 []types.Tool{executable(valueStringSchema())},
		})
		if err != nil || len(got.ApprovedToolApprovals) != 1 {
			t.Fatalf("no secret: %+v, %v", got, err)
		}
	})
}

// ---------------------------------------------------------------------------
// GenerateText resume (port of generate-text.test.ts "tool execution approval")

func TestGenerateText_ResumeApprovedToolApproval(t *testing.T) {
	var executedWith map[string]interface{}
	var execOpts types.ToolExecutionOptions
	var startEvents, endEvents int
	var mu sync.Mutex
	tool := types.Tool{
		Name:         "tool1",
		Parameters:   valueStringSchema(),
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executedWith, execOpts = input, opts
			return "result1", nil
		},
	}
	model := newRecordingModel("Hello, world!")
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:    model,
		Tools:    []types.Tool{tool},
		Messages: approvalHistory("tool1", map[string]interface{}{"value": "value"}, "", true, ""),
		StopWhen: []StopCondition{StepCountIs(3)},
		OnToolExecutionStart: func(ctx context.Context, e OnToolCallStartEvent) {
			mu.Lock()
			startEvents++
			mu.Unlock()
		},
		OnToolExecutionEnd: func(ctx context.Context, e OnToolCallFinishEvent) {
			mu.Lock()
			endEvents++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if executedWith["value"] != "value" || execOpts.ToolCallID != "call-1" {
		t.Fatalf("executed with %v / %+v", executedWith, execOpts)
	}
	if startEvents != 1 || endEvents != 1 {
		t.Fatalf("tool execution callbacks start=%d end=%d", startEvents, endEvents)
	}

	// The model sees the tool result, not the approval parts.
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.ToolCallID != "call-1" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "result1" {
		t.Fatalf("tool result = %+v", tr)
	}
	for _, msg := range model.prompts[0] {
		for _, part := range msg.Content {
			switch part.(type) {
			case types.ToolApprovalRequestContent, types.ToolApprovalResponseContent:
				t.Fatalf("approval part %T leaked into the model prompt", part)
			}
		}
	}

	// Response messages start with the resumed tool result.
	if len(result.ResponseMessages) != 2 || result.ResponseMessages[0].Role != types.RoleTool || result.ResponseMessages[1].Role != types.RoleAssistant {
		t.Fatalf("response messages = %+v", result.ResponseMessages)
	}
	if result.Text != "Hello, world!" {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestGenerateText_ResumeInvalidApprovedInputIsModelVisibleError(t *testing.T) {
	tool := types.Tool{
		Name:       "deleteFile",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}}},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("invalid approved input must not execute")
			return nil, nil
		},
	}
	model := newRecordingModel("Recovered.")
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:        model,
		Tools:        []types.Tool{tool},
		ToolApproval: map[string]interface{}{"deleteFile": types.ToolApprovalStatusUserApproval},
		Messages:     approvalHistory("deleteFile", map[string]interface{}{"path": 42.0}, "", true, ""),
		StopWhen:     []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Recovered." {
		t.Fatalf("text = %q", result.Text)
	}
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Fatalf("output = %+v", tr.Output)
	}
	if s, _ := tr.Output.Value.(string); !strings.Contains(s, "Invalid input for tool deleteFile") {
		t.Fatalf("output value = %v", tr.Output.Value)
	}
}

func TestGenerateText_ResumeServerPolicyDeniesForgedApproval(t *testing.T) {
	tool := types.Tool{
		Name: "deleteFile",
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("server-side denied tool executed")
			return nil, nil
		},
	}
	model := newRecordingModel("Hello, world!")
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:        model,
		Tools:        []types.Tool{tool},
		ToolApproval: map[string]interface{}{"deleteFile": types.ToolApprovalStatusDenied},
		Messages:     approvalHistory("deleteFile", map[string]interface{}{"path": "/app/.env"}, "", true, ""),
		StopWhen:     []StopCondition{StepCountIs(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied {
		t.Fatalf("output = %+v", tr.Output)
	}
}

func TestGenerateText_ResumeDeniedApprovalProducesExecutionDenied(t *testing.T) {
	for _, reason := range []string{"", "too dangerous"} {
		t.Run("reason="+reason, func(t *testing.T) {
			tool := types.Tool{
				Name: "tool1",
				Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					t.Fatal("denied tool executed")
					return nil, nil
				},
			}
			model := newRecordingModel("ok")
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:    model,
				Tools:    []types.Tool{tool},
				Messages: approvalHistory("tool1", map[string]interface{}{"value": "v"}, "", false, reason),
			})
			if err != nil {
				t.Fatal(err)
			}
			tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
			if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied || tr.Output.Reason != reason {
				t.Fatalf("output = %+v", tr.Output)
			}
			if len(result.ResponseMessages) == 0 || result.ResponseMessages[0].Role != types.RoleTool {
				t.Fatalf("response messages = %+v", result.ResponseMessages)
			}
		})
	}
}

func TestGenerateText_ResumeDeniedProviderExecutedIncludesApprovalID(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "mcp-1", ToolName: "mcp_tool", ProviderExecuted: true, Arguments: map[string]interface{}{}},
			types.ToolApprovalRequestContent{ApprovalID: "approval-mcp", ToolCallID: "mcp-1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "approval-mcp", Approved: false},
		}},
	}
	model := newRecordingModel("ok")
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Messages: messages}); err != nil {
		t.Fatal(err)
	}
	last := model.lastMessageOfFirstPrompt(t)
	var denied *types.ToolResultContent
	for _, part := range last.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			denied = &tr
		}
	}
	if denied == nil || denied.Output == nil || denied.Output.Type != types.ToolResultOutputExecutionDenied {
		t.Fatalf("last prompt message = %+v", last)
	}
	openai, _ := denied.Output.ProviderOptions["openai"].(map[string]interface{})
	if openai["approvalId"] != "approval-mcp" {
		t.Fatalf("providerOptions = %+v", denied.Output.ProviderOptions)
	}
}

func TestGenerateText_ResumeWithApprovalSecret(t *testing.T) {
	secret := []byte("test-hmac-secret-do-not-use-in-production")
	input := map[string]interface{}{"value": "test"}
	sig, err := SignToolApproval(secret, "approval-1", "call-1", "tool1", input)
	if err != nil {
		t.Fatal(err)
	}
	newTool := func(executed *int) types.Tool {
		return types.Tool{
			Name:         "tool1",
			Parameters:   valueStringSchema(),
			ToolApproval: types.ToolApprovalStatusUserApproval,
			Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				*executed++
				return "result1", nil
			},
		}
	}
	failingModel := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, errors.New("model should not be called")
		},
	}

	t.Run("valid signature executes", func(t *testing.T) {
		executed := 0
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:                          newRecordingModel("done"),
			Tools:                          []types.Tool{newTool(&executed)},
			ExperimentalToolApprovalSecret: secret,
			Messages:                       approvalHistory("tool1", input, sig, true, ""),
		})
		if err != nil || executed != 1 {
			t.Fatalf("err=%v executed=%d", err, executed)
		}
	})

	t.Run("missing signature rejected", func(t *testing.T) {
		// An empty signature is Go's only representation of "no signature
		// sent" (a plain string field cannot distinguish absent from
		// empty, unlike TS's null/undefined). It falls through to
		// verification and is rejected as "invalid signature", matching
		// what TS itself reports for an explicit empty-string signature.
		executed := 0
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:                          failingModel,
			Tools:                          []types.Tool{newTool(&executed)},
			ExperimentalToolApprovalSecret: secret,
			Messages:                       approvalHistory("tool1", input, "", true, ""),
		})
		if err == nil || !strings.Contains(err.Error(), "invalid signature") || !IsInvalidToolApprovalSignatureError(err) || executed != 0 {
			t.Fatalf("err=%v executed=%d", err, executed)
		}
	})

	t.Run("input tampered after signing rejected", func(t *testing.T) {
		executed := 0
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:                          failingModel,
			Tools:                          []types.Tool{newTool(&executed)},
			ExperimentalToolApprovalSecret: secret,
			Messages:                       approvalHistory("tool1", map[string]interface{}{"value": "tampered"}, sig, true, ""),
		})
		if err == nil || !strings.Contains(err.Error(), "invalid signature") || executed != 0 {
			t.Fatalf("err=%v executed=%d", err, executed)
		}
	})

	t.Run("no secret is backward compatible", func(t *testing.T) {
		executed := 0
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model:    newRecordingModel("done"),
			Tools:    []types.Tool{newTool(&executed)},
			Messages: approvalHistory("tool1", input, "", true, ""),
		})
		if err != nil || executed != 1 {
			t.Fatalf("err=%v executed=%d", err, executed)
		}
	})
}

// A signature issued by GenerateText round-trips through the response
// messages and verifies on resume.
func TestGenerateText_IssuedApprovalSignatureVerifiesOnResume(t *testing.T) {
	secret := []byte("round-trip-secret")
	executed := 0
	tool := types.Tool{
		Name:         "tool1",
		Parameters:   valueStringSchema(),
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return "ok", nil
		},
	}
	first := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "x"}}},
			}, nil
		},
	}
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: first, Tools: []types.Tool{tool}, Messages: initial, ExperimentalToolApprovalSecret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if executed != 0 || res.FinishReason != types.FinishReasonUserApproval {
		t.Fatalf("executed=%d finish=%s", executed, res.FinishReason)
	}
	var approvalID string
	for _, msg := range res.ResponseMessages {
		for _, part := range msg.Content {
			if req, ok := part.(types.ToolApprovalRequestContent); ok {
				approvalID = req.ApprovalID
				if req.Signature == "" {
					t.Fatal("issued approval request is unsigned")
				}
			}
		}
	}
	if approvalID == "" {
		t.Fatalf("no approval request in %+v", res.ResponseMessages)
	}
	history := append(append(initial, res.ResponseMessages...), types.Message{
		Role:    types.RoleTool,
		Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: true}},
	})
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: newRecordingModel("done"), Tools: []types.Tool{tool}, Messages: history, ExperimentalToolApprovalSecret: secret,
	}); err != nil {
		t.Fatal(err)
	}
	if executed != 1 {
		t.Fatalf("executed = %d after approval", executed)
	}
}

// Port of TS "should not execute tools when the finish reason is %s".
func TestGenerateText_DoesNotExecuteToolsOnUnsafeFinishReason(t *testing.T) {
	for _, reason := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonError, types.FinishReasonContentFilter, types.FinishReasonOther} {
		t.Run(string(reason), func(t *testing.T) {
			executed := 0
			calls := 0
			model := &testutil.MockLanguageModel{
				ToolSupport: true,
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					calls++
					return &types.GenerateResult{
						FinishReason: reason,
						ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "test"}}},
					}, nil
				},
			}
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: model,
				Tools: []types.Tool{{
					Name: "testTool",
					Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
						executed++
						return "tool-result", nil
					},
				}},
				StopWhen: []StopCondition{StepCountIs(5)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ToolCalls) != 1 || executed != 0 || calls != 1 {
				t.Fatalf("toolCalls=%d executed=%d modelCalls=%d", len(result.ToolCalls), executed, calls)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StreamText resume (port of stream-text.test.ts "tool execution approval")

func collectStream(t *testing.T, result *StreamTextResult) []provider.StreamChunk {
	t.Helper()
	var chunks []provider.StreamChunk
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, *chunk)
	}
	return chunks
}

func TestStreamText_ResumeApprovedToolApproval(t *testing.T) {
	executed := 0
	tool := types.Tool{
		Name:         "tool1",
		Parameters:   valueStringSchema(),
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return "result1", nil
		},
	}
	model := newRecordingModel("Hello, world!")
	var chunks []provider.StreamChunk
	var mu sync.Mutex
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Tools:    []types.Tool{tool},
		Messages: approvalHistory("tool1", map[string]interface{}{"value": "value"}, "", true, ""),
		StopWhen: []StopCondition{StepCountIs(3)},
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			chunks = append(chunks, c)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Steps() // waits for the processing goroutine
	if executed != 1 {
		t.Fatalf("executed = %d", executed)
	}
	mu.Lock()
	first := chunks[0]
	mu.Unlock()
	if first.Type != provider.ChunkTypeToolResult || first.ToolResult == nil || first.ToolResult.Result != "result1" {
		t.Fatalf("first chunk = %+v", first)
	}
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.Output == nil || tr.Output.Value != "result1" {
		t.Fatalf("prompt tool result = %+v", tr)
	}
	msgs := result.ResponseMessages()
	if len(msgs) != 2 || msgs[0].Role != types.RoleTool || msgs[1].Role != types.RoleAssistant {
		t.Fatalf("response messages = %+v", msgs)
	}
	// The resumed tool result is not part of the first step's content.
	for _, part := range result.Steps()[0].Content {
		if _, ok := part.(types.ToolResultContent); ok {
			t.Fatalf("step 0 content contains resumed tool result: %+v", result.Steps()[0].Content)
		}
	}
}

func TestStreamText_ResumeInvalidApprovedInputStreamsToolError(t *testing.T) {
	tool := types.Tool{
		Name:       "deleteFile",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}}},
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("invalid approved input executed")
			return nil, nil
		},
	}
	model := newRecordingModel("Recovered.")
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:        model,
		Tools:        []types.Tool{tool},
		ToolApproval: map[string]interface{}{"deleteFile": types.ToolApprovalStatusUserApproval},
		Messages:     approvalHistory("deleteFile", map[string]interface{}{"path": 42.0}, "", true, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectStream(t, result)
	var toolErr *types.ToolResult
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && c.ToolResult.ToolCallID == "call-1" {
			toolErr = c.ToolResult
		}
	}
	if toolErr == nil || toolErr.Error == nil || !strings.Contains(toolErr.Error.Error(), "Invalid input for tool deleteFile") {
		t.Fatalf("tool error chunk = %+v", toolErr)
	}
	if toolErr.Dynamic {
		t.Fatal("tool error must not be marked dynamic")
	}
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputErrorText {
		t.Fatalf("prompt tool result = %+v", tr)
	}
}

func TestStreamText_ResumeDeniedApprovalStreamsOutputDenied(t *testing.T) {
	tool := types.Tool{
		Name: "tool1",
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			t.Fatal("denied tool executed")
			return nil, nil
		},
	}
	model := newRecordingModel("ok")
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Tools:    []types.Tool{tool},
		Messages: approvalHistory("tool1", map[string]interface{}{"value": "v"}, "", false, "nope"),
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks := collectStream(t, result)
	if len(chunks) == 0 || chunks[0].Type != provider.ChunkTypeToolOutputDenied || chunks[0].ToolResult.ToolCallID != "call-1" {
		t.Fatalf("chunks = %+v", chunks)
	}
	tr := singleToolResult(t, model.lastMessageOfFirstPrompt(t))
	if tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied || tr.Output.Reason != "nope" {
		t.Fatalf("prompt tool result = %+v", tr)
	}
}

func TestStreamText_ResumeRejectsForgedSignature(t *testing.T) {
	_, err := StreamText(context.Background(), StreamTextOptions{
		Model: newRecordingModel("ok"),
		Tools: []types.Tool{{
			Name: "tool1",
			Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				t.Fatal("forged approval executed")
				return nil, nil
			},
		}},
		ExperimentalToolApprovalSecret: []byte("secret"),
		Messages:                       approvalHistory("tool1", map[string]interface{}{"value": "v"}, "forged-signature", true, ""),
	})
	if !IsInvalidToolApprovalSignatureError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamText_DoesNotExecuteToolsOnUnsafeFinishReason(t *testing.T) {
	executed := 0
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "x"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
			}), nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Tools: []types.Tool{{
			Name: "testTool",
			Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				executed++
				return "r", nil
			},
		}},
		StopWhen: []StopCondition{StepCountIs(5)},
		OnChunk:  func(provider.StreamChunk) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Steps()
	if executed != 0 || calls != 1 {
		t.Fatalf("executed=%d calls=%d", executed, calls)
	}
}

// ---------------------------------------------------------------------------
// messagesForModel

func TestMessagesForModelStripsApprovalParts(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "local"},
			types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"},
			types.ToolCallContent{ToolCallID: "c2", ToolName: "mcp", ProviderExecuted: true},
			types.ToolApprovalRequestContent{ApprovalID: "a2", ToolCallID: "c2"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: true},
			types.ToolApprovalResponseContent{ApprovalID: "a2", Approved: true},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c1", ToolName: "local", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r"}},
		}},
	}
	got := messagesForModel(messages)
	if len(got) != 2 {
		t.Fatalf("got %d messages: %+v", len(got), got)
	}
	for _, part := range got[0].Content {
		if _, ok := part.(types.ToolApprovalRequestContent); ok {
			t.Fatal("approval request kept in assistant message")
		}
	}
	if len(got[1].Content) != 2 {
		t.Fatalf("tool message = %+v", got[1])
	}
	resp, ok := got[1].Content[0].(types.ToolApprovalResponseContent)
	if !ok || resp.ApprovalID != "a2" || !resp.ProviderExecuted {
		t.Fatalf("provider-executed approval response = %+v", got[1].Content[0])
	}
	// Input is not mutated.
	if len(messages[1].Content) != 2 || len(messages[0].Content) != 4 {
		t.Fatal("input messages mutated")
	}
}

// Port of TS "should execute transformed approved input after a persisted
// round trip": refined input is approved, its pre-refinement input travels as
// inputSchemaInput, and revalidation re-applies the refinement.
func TestGenerateText_RefinedApprovedInputRoundTrip(t *testing.T) {
	var executedWith map[string]interface{}
	tool := types.Tool{
		Name:         "tool1",
		Parameters:   valueStringSchema(),
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executedWith = in
			return "ok", nil
		},
	}
	refine := map[string]ToolInputRefiner{
		"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
			return map[string]interface{}{"value": strings.TrimSpace(opts.ToolCall.Arguments["value"].(string))}, nil
		},
	}
	first := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				FinishReason: types.FinishReasonToolCalls,
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": " trimmed "}}},
			}, nil
		},
	}
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: first, Tools: []types.Tool{tool}, Messages: initial, ExperimentalRefineToolInput: refine,
	})
	if err != nil {
		t.Fatal(err)
	}
	var approvalID string
	for _, msg := range res.ResponseMessages {
		for _, part := range msg.Content {
			if req, ok := part.(types.ToolApprovalRequestContent); ok {
				approvalID = req.ApprovalID
				in, _ := req.InputSchemaInput.(map[string]interface{})
				if in["value"] != " trimmed " {
					t.Fatalf("inputSchemaInput = %#v", req.InputSchemaInput)
				}
			}
		}
	}
	if approvalID == "" {
		t.Fatal("no approval request")
	}
	history := append(append(initial, res.ResponseMessages...), types.Message{
		Role:    types.RoleTool,
		Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: true}},
	})
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: newRecordingModel("done"), Tools: []types.Tool{tool}, Messages: history, ExperimentalRefineToolInput: refine,
	}); err != nil {
		t.Fatal(err)
	}
	if executedWith["value"] != "trimmed" {
		t.Fatalf("executed with %v", executedWith)
	}
}

// TestGenerateText_RefinedApprovedInputRoundTripWithRawArguments guards
// against F1: every real provider populates ToolCall.RawArguments alongside
// Arguments. RefineToolCalls only updates Arguments, so RawArguments stays
// the pre-refinement JSON. The persisted tool-call part and
// message.ToolCalls must still carry the refined value end to end -- both
// the HMAC signature (when a secret is configured) and the schema
// revalidation on resume are computed from that persisted value, and must
// agree with what was actually approved.
func TestGenerateText_RefinedApprovedInputRoundTripWithRawArguments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secret []byte
	}{
		{name: "without secret"},
		{name: "with secret", secret: []byte("raw-arguments-secret")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var executedWith map[string]interface{}
			tool := types.Tool{
				Name:         "tool1",
				Parameters:   valueStringSchema(),
				ToolApproval: types.ToolApprovalStatusUserApproval,
				Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executedWith = in
					return "ok", nil
				},
			}
			refine := map[string]ToolInputRefiner{
				"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
					return map[string]interface{}{"value": strings.TrimSpace(opts.ToolCall.Arguments["value"].(string))}, nil
				},
			}
			first := &testutil.MockLanguageModel{
				ToolSupport: true,
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					return &types.GenerateResult{
						FinishReason: types.FinishReasonToolCalls,
						ToolCalls: []types.ToolCall{{
							ID:           "call-1",
							ToolName:     "tool1",
							Arguments:    map[string]interface{}{"value": " trimmed "},
							RawArguments: `{"value":" trimmed "}`,
						}},
					}, nil
				},
			}
			initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
			res, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:                          first,
				Tools:                          []types.Tool{tool},
				Messages:                       initial,
				ExperimentalRefineToolInput:    refine,
				ExperimentalToolApprovalSecret: tc.secret,
			})
			if err != nil {
				t.Fatal(err)
			}
			var approvalID string
			for _, msg := range res.ResponseMessages {
				for _, part := range msg.Content {
					if call, ok := part.(types.ToolCallContent); ok {
						if call.Arguments["value"] != "trimmed" {
							t.Fatalf("persisted tool-call part arguments = %#v, want refined value \"trimmed\"", call.Arguments)
						}
					}
					if req, ok := part.(types.ToolApprovalRequestContent); ok {
						approvalID = req.ApprovalID
					}
				}
				for _, call := range msg.ToolCalls {
					if call.Arguments["value"] != "trimmed" {
						t.Fatalf("persisted message.ToolCalls arguments = %#v, want refined value \"trimmed\"", call.Arguments)
					}
				}
			}
			if approvalID == "" {
				t.Fatal("no approval request")
			}
			history := append(append(initial, res.ResponseMessages...), types.Message{
				Role:    types.RoleTool,
				Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: true}},
			})
			if _, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:                          newRecordingModel("done"),
				Tools:                          []types.Tool{tool},
				Messages:                       history,
				ExperimentalRefineToolInput:    refine,
				ExperimentalToolApprovalSecret: tc.secret,
			}); err != nil {
				t.Fatal(err)
			}
			if executedWith["value"] != "trimmed" {
				t.Fatalf("executed with %v", executedWith)
			}
		})
	}
}

// TestStreamText_RefinedApprovedInputRoundTripWithRawArguments is the
// streaming counterpart of
// TestGenerateText_RefinedApprovedInputRoundTripWithRawArguments (see F1).
func TestStreamText_RefinedApprovedInputRoundTripWithRawArguments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secret []byte
	}{
		{name: "without secret"},
		{name: "with secret", secret: []byte("raw-arguments-secret-stream")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var executedWith map[string]interface{}
			tool := types.Tool{
				Name:         "tool1",
				Parameters:   valueStringSchema(),
				ToolApproval: types.ToolApprovalStatusUserApproval,
				Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
					executedWith = in
					return "ok", nil
				},
			}
			refine := map[string]ToolInputRefiner{
				"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
					return map[string]interface{}{"value": strings.TrimSpace(opts.ToolCall.Arguments["value"].(string))}, nil
				},
			}
			first := &testutil.MockLanguageModel{
				ToolSupport: true,
				DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
							ID:           "call-1",
							ToolName:     "tool1",
							Arguments:    map[string]interface{}{"value": " trimmed "},
							RawArguments: `{"value":" trimmed "}`,
						}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				},
			}
			initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
			res, err := StreamText(context.Background(), StreamTextOptions{
				Model:                          first,
				Tools:                          []types.Tool{tool},
				Messages:                       initial,
				ExperimentalRefineToolInput:    refine,
				ExperimentalToolApprovalSecret: tc.secret,
				OnChunk:                        func(c provider.StreamChunk) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Steps() // waits for the processing goroutine
			msgs := res.ResponseMessages()
			var approvalID string
			for _, msg := range msgs {
				for _, part := range msg.Content {
					if call, ok := part.(types.ToolCallContent); ok {
						if call.Arguments["value"] != "trimmed" {
							t.Fatalf("persisted tool-call part arguments = %#v, want refined value \"trimmed\"", call.Arguments)
						}
					}
					if req, ok := part.(types.ToolApprovalRequestContent); ok {
						approvalID = req.ApprovalID
					}
				}
				for _, call := range msg.ToolCalls {
					if call.Arguments["value"] != "trimmed" {
						t.Fatalf("persisted message.ToolCalls arguments = %#v, want refined value \"trimmed\"", call.Arguments)
					}
				}
			}
			if approvalID == "" {
				t.Fatal("no approval request")
			}
			history := append(append(initial, msgs...), types.Message{
				Role:    types.RoleTool,
				Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: true}},
			})
			result2, err := StreamText(context.Background(), StreamTextOptions{
				Model:                          newRecordingModel("done"),
				Tools:                          []types.Tool{tool},
				Messages:                       history,
				ExperimentalRefineToolInput:    refine,
				ExperimentalToolApprovalSecret: tc.secret,
				OnChunk:                        func(c provider.StreamChunk) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			_ = result2.Steps() // waits for the processing goroutine
			if executedWith["value"] != "trimmed" {
				t.Fatalf("executed with %v", executedWith)
			}
		})
	}
}
