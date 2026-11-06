package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A ToolApprovalResponseContent with ProviderExecutedSet marshals an explicit
// `providerExecuted: false`; an unset one omits the key.
func TestToolApprovalResponseContent_MarshalsExplicitProviderExecutedFalse(t *testing.T) {
	explicit, err := json.Marshal(types.ToolApprovalResponseContent{ApprovalID: "a", ProviderExecutedSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(explicit), `"providerExecuted":false`) {
		t.Fatalf("explicit = %s", explicit)
	}
	unset, err := json.Marshal(types.ToolApprovalResponseContent{ApprovalID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unset), "providerExecuted") {
		t.Fatalf("unset = %s", unset)
	}
}

func TestToUIMessageChunk_ApprovalResponseForwardsExplicitProviderExecutedFalse(t *testing.T) {
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{
		Type:                 provider.ChunkTypeToolApprovalResponse,
		ToolApprovalResponse: &types.ToolApprovalResponseContent{ApprovalID: "a", ProviderExecutedSet: true},
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("suppressed")
	}
	if v, present := chunk["providerExecuted"]; !present || v != false {
		t.Fatalf("providerExecuted = %v (present %v), want false", v, present)
	}
	result, ok := ToUIMessageChunk(provider.StreamChunk{
		Type:       provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{ToolCallID: "c", ToolName: "t", Result: "ok", ProviderExecutedSet: true},
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("suppressed")
	}
	if v, present := result["providerExecuted"]; !present || v != false {
		t.Fatalf("tool-output-available providerExecuted = %v (present %v), want false", v, present)
	}
}

func explicitFalseToolCallStream(id string) *testutil.MockLanguageModel {
	calls := 0
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			calls++
			if calls > 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "done"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "noop", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		},
	}
}

// TS automatic approvals send `providerExecuted: toolCall.providerExecuted`,
// so a tool call with an explicit false gives approval responses with an
// explicit false. The locally executed output of an approved call has none.
func TestStreamText_AutomaticApprovalKeepsExplicitProviderExecutedFalse(t *testing.T) {
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusDenied, types.ToolApprovalStatusApproved} {
		t.Run(string(status), func(t *testing.T) {
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model:        explicitFalseToolCallStream("c1"),
				Prompt:       "hi",
				ToolApproval: map[string]types.ToolApprovalValue{"noop": status},
				StopWhen:     []StopCondition{StepCountIs(2)},
				Tools: []types.Tool{{
					Name: "noop",
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						return "ok", nil
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			chunks, errs := result.ToUIMessageStream(context.Background())
			got := map[string]interface{}{}
			for c := range chunks {
				typ, _ := c["type"].(string)
				if typ == "tool-approval-response" || typ == "tool-output-available" {
					v, ok := c["providerExecuted"]
					if !ok {
						v = "omitted"
					}
					got[typ] = v
				}
			}
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
			if got["tool-approval-response"] != false {
				t.Fatalf("tool-approval-response providerExecuted = %#v, want false", got["tool-approval-response"])
			}
			if status == types.ToolApprovalStatusApproved && got["tool-output-available"] != "omitted" {
				t.Fatalf("tool-output-available providerExecuted = %#v, want omitted", got["tool-output-available"])
			}
		})
	}
}

func TestGenerateText_AutomaticDenialResponseMessageKeepsExplicitProviderExecutedFalse(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "noop", Arguments: map[string]interface{}{}, ProviderExecutedSet: true}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:        model,
		Prompt:       "hi",
		ToolApproval: map[string]types.ToolApprovalValue{"noop": types.ToolApprovalStatusDenied},
		StopWhen:     []StopCondition{StepCountIs(1)},
		Tools: []types.Tool{{
			Name: "noop",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range result.ResponseMessages {
		for _, part := range msg.Content {
			if resp, ok := part.(types.ToolApprovalResponseContent); ok {
				found = true
				b, _ := json.Marshal(resp)
				if !resp.ProviderExecutedSet || !strings.Contains(string(b), `"providerExecuted":false`) {
					t.Fatalf("approval response = %s (set %v)", b, resp.ProviderExecutedSet)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no approval response in %#v", result.ResponseMessages)
	}
}
