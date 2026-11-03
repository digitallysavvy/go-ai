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
