package prompt

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestMergeConsecutiveToolMessagesCombinesTwoToolMessages ports the TS
// convert-to-language-model-prompt.test.ts "should combine 2 consecutive
// tool messages into a single tool message" case (hash 33647d7).
func TestMergeConsecutiveToolMessagesCombinesTwoToolMessages(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "toolCallId", ToolName: "toolName", Input: "{}"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolApprovalResponseContent{ApprovalID: "approvalId", Approved: true},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "toolCallId",
					ToolName:   "toolName",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"some": "result"}},
				},
			},
		},
	}

	got := MergeConsecutiveToolMessages(messages)

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (assistant + one combined tool message)", len(got))
	}
	if got[0].Role != types.RoleAssistant {
		t.Fatalf("got[0].Role = %q, want assistant", got[0].Role)
	}
	if got[1].Role != types.RoleTool {
		t.Fatalf("got[1].Role = %q, want tool", got[1].Role)
	}
	if len(got[1].Content) != 2 {
		t.Fatalf("len(got[1].Content) = %d, want 2 (approval response + tool result)", len(got[1].Content))
	}
	if _, ok := got[1].Content[0].(types.ToolApprovalResponseContent); !ok {
		t.Fatalf("got[1].Content[0] = %#v, want ToolApprovalResponseContent", got[1].Content[0])
	}
	result, ok := got[1].Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("got[1].Content[1] = %#v, want ToolResultContent", got[1].Content[1])
	}
	if result.ToolCallID != "toolCallId" {
		t.Fatalf("result.ToolCallID = %q, want toolCallId", result.ToolCallID)
	}
	if got[1].ProviderOptions != nil {
		t.Fatalf("got[1].ProviderOptions = %#v, want nil", got[1].ProviderOptions)
	}
}

// TestMergeConsecutiveToolMessagesPreservesProviderOptions ports the TS
// convert-to-language-model-prompt.test.ts "should preserve provider options
// at tool message boundaries when combining consecutive tool messages" case
// (hash 33647d7). The first message's message-level ProviderOptions must be
// deep-merged (part-level wins) onto its own last part before being folded
// into the combined message, and the combined message keeps the LAST
// message's ProviderOptions.
func TestMergeConsecutiveToolMessagesPreservesProviderOptions(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "toolCallId1", ToolName: "toolName", Input: "{}"},
				types.ToolCallContent{ToolCallID: "toolCallId2", ToolName: "toolName", Input: "{}"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "toolCallId1",
					ToolName:   "toolName",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result1"},
					ProviderOptions: map[string]interface{}{
						"test": map[string]interface{}{
							"cacheControl": "part",
							"partOnly":     true,
						},
					},
				},
			},
			ProviderOptions: map[string]interface{}{
				"test": map[string]interface{}{
					"cacheControl": "first-message",
					"messageOnly":  true,
				},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "toolCallId2",
					ToolName:   "toolName",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result2"},
				},
			},
			ProviderOptions: map[string]interface{}{
				"test": map[string]interface{}{
					"cacheControl": "second-message",
				},
			},
		},
	}

	got := MergeConsecutiveToolMessages(messages)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	toolMsg := got[1]
	if len(toolMsg.Content) != 2 {
		t.Fatalf("len(toolMsg.Content) = %d, want 2", len(toolMsg.Content))
	}

	part1, ok := toolMsg.Content[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("toolMsg.Content[0] = %#v, want ToolResultContent", toolMsg.Content[0])
	}
	wantPart1Options := map[string]interface{}{
		"test": map[string]interface{}{
			"cacheControl": "part", // part-level wins over message-level
			"messageOnly":  true,   // survives from the first message's level
			"partOnly":     true,   // survives from the part's own level
		},
	}
	if !reflect.DeepEqual(part1.ProviderOptions, wantPart1Options) {
		t.Fatalf("part1.ProviderOptions = %#v, want %#v", part1.ProviderOptions, wantPart1Options)
	}

	part2, ok := toolMsg.Content[1].(types.ToolResultContent)
	if !ok {
		t.Fatalf("toolMsg.Content[1] = %#v, want ToolResultContent", toolMsg.Content[1])
	}
	if part2.ProviderOptions != nil {
		t.Fatalf("part2.ProviderOptions = %#v, want nil (never had part-level options)", part2.ProviderOptions)
	}

	wantMsgOptions := map[string]interface{}{
		"test": map[string]interface{}{
			"cacheControl": "second-message",
		},
	}
	if !reflect.DeepEqual(toolMsg.ProviderOptions, wantMsgOptions) {
		t.Fatalf("toolMsg.ProviderOptions = %#v, want %#v (last message wins)", toolMsg.ProviderOptions, wantMsgOptions)
	}
}

// TestMergeConsecutiveToolMessagesThreeInARow exercises the push-down chain
// across three consecutive tool messages: each part must end up carrying its
// OWN originating message's ProviderOptions (as a part-level fallback),
// matching what per-message "last part" cache-control resolution would
// produce without merging.
func TestMergeConsecutiveToolMessagesThreeInARow(t *testing.T) {
	msg := func(id, cacheControl string) types.Message {
		return types.Message{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: id,
					ToolName:   "toolName",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r-" + id},
				},
			},
			ProviderOptions: map[string]interface{}{
				"anthropic": map[string]interface{}{"cacheControl": cacheControl},
			},
		}
	}
	messages := []types.Message{msg("a", "A"), msg("b", "B"), msg("c", "C")}

	got := MergeConsecutiveToolMessages(messages)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].Content) != 3 {
		t.Fatalf("len(got[0].Content) = %d, want 3", len(got[0].Content))
	}

	// Only messages that get superseded by a later one have their
	// ProviderOptions pushed down onto their own last part (a and b, here).
	// The LAST message's ProviderOptions (c) is never pushed down -- it
	// survives only as the combined message's top-level ProviderOptions,
	// exactly like TS.
	wantByIndex := []string{"A", "B", ""}
	for i, want := range wantByIndex {
		part, ok := got[0].Content[i].(types.ToolResultContent)
		if !ok {
			t.Fatalf("Content[%d] = %#v, want ToolResultContent", i, got[0].Content[i])
		}
		if want == "" {
			if part.ProviderOptions != nil {
				t.Fatalf("Content[%d].ProviderOptions = %#v, want nil", i, part.ProviderOptions)
			}
			continue
		}
		anthropicOpts, _ := part.ProviderOptions["anthropic"].(map[string]interface{})
		if anthropicOpts == nil || anthropicOpts["cacheControl"] != want {
			t.Fatalf("Content[%d].ProviderOptions = %#v, want cacheControl=%q", i, part.ProviderOptions, want)
		}
	}

	// The combined message's own ProviderOptions is the LAST message's.
	wantMsgOpts := map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": "C"}}
	if !reflect.DeepEqual(got[0].ProviderOptions, wantMsgOpts) {
		t.Fatalf("got[0].ProviderOptions = %#v, want %#v", got[0].ProviderOptions, wantMsgOpts)
	}
}

// TestMergeConsecutiveToolMessagesNoop verifies that non-tool messages and
// isolated (non-consecutive) tool messages are left untouched.
func TestMergeConsecutiveToolMessagesNoop(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r1"}},
			},
		},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "more"}}},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "2", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r2"}},
			},
		},
	}

	got := MergeConsecutiveToolMessages(messages)
	if len(got) != len(messages) {
		t.Fatalf("len(got) = %d, want %d (no consecutive tool messages to combine)", len(got), len(messages))
	}
}

// TestMergeConsecutiveToolMessagesDoesNotMutateInput guards against a
// regression where combining two tool messages wrote the push-down of the
// first message's ProviderOptions directly into index len(content)-1 of the
// FIRST message's own Content slice before that slice had been copied --
// silently mutating the caller-supplied []types.Message backing array
// in place. Since MergeConsecutiveToolMessages is called on every
// GenerateText/StreamText/agent step, and the same []types.Message value can
// legitimately be reused by a caller across multiple calls (e.g. retries,
// multi-provider fallback), it must never mutate its input.
func TestMergeConsecutiveToolMessagesDoesNotMutateInput(t *testing.T) {
	original := types.ToolResultContent{
		ToolCallID: "toolCallId1",
		ToolName:   "toolName",
		Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result1"},
	}
	messages := []types.Message{
		{
			Role:    types.RoleTool,
			Content: []types.ContentPart{original},
			ProviderOptions: map[string]interface{}{
				"test": map[string]interface{}{"cacheControl": "first-message"},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "toolCallId2", ToolName: "toolName", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result2"}},
			},
			ProviderOptions: map[string]interface{}{
				"test": map[string]interface{}{"cacheControl": "second-message"},
			},
		},
	}

	_ = MergeConsecutiveToolMessages(messages)

	// The original input slice's first message must be byte-for-byte
	// untouched: its Content[0] must still be the original value with no
	// ProviderOptions pushed into it.
	got, ok := messages[0].Content[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("messages[0].Content[0] = %#v, want ToolResultContent", messages[0].Content[0])
	}
	if got.ProviderOptions != nil {
		t.Fatalf("input mutated: messages[0].Content[0].ProviderOptions = %#v, want nil (original untouched)", got.ProviderOptions)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("input mutated: messages[0].Content[0] = %#v, want unchanged %#v", got, original)
	}

	// Calling it a second time on the SAME original input must produce an
	// identical result to the first call (proves the first call didn't leave
	// behind state that would change a subsequent merge).
	firstResult := MergeConsecutiveToolMessages(messages)
	secondResult := MergeConsecutiveToolMessages(messages)
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("merge is not idempotent across repeated calls on the same input:\nfirst:  %#v\nsecond: %#v", firstResult, secondResult)
	}
}
