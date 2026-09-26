package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// Ports of ai/packages/ai/src/ui/convert-to-model-messages.test.ts
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

// ports convert-to-model-messages.test.ts > describe('system message')
func TestConvertToModelMessages_SystemMessage(t *testing.T) {
	t.Run("should convert a simple system message", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleSystem, Parts: []UIMessagePart{
				TextUIPart{Text: "System message"},
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "System message"}}},
		}, result)
	})

	t.Run("should convert a system message with provider metadata", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleSystem, Parts: []UIMessagePart{
				TextUIPart{Text: "System message with metadata", ProviderMetadata: map[string]interface{}{
					"testProvider": map[string]interface{}{"systemSignature": "abc123"},
				}},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, types.RoleSystem, result[0].Role)
		assert.Equal(t, map[string]interface{}{"testProvider": map[string]interface{}{"systemSignature": "abc123"}}, result[0].ProviderOptions)
	})

	t.Run("should merge provider metadata from multiple text parts in system message", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleSystem, Parts: []UIMessagePart{
				TextUIPart{Text: "Part 1", ProviderMetadata: map[string]interface{}{"provider1": map[string]interface{}{"key1": "value1"}}},
				TextUIPart{Text: " Part 2", ProviderMetadata: map[string]interface{}{"provider2": map[string]interface{}{"key2": "value2"}}},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, []types.ContentPart{types.TextContent{Text: "Part 1 Part 2"}}, result[0].Content)
		assert.Equal(t, map[string]interface{}{
			"provider1": map[string]interface{}{"key1": "value1"},
			"provider2": map[string]interface{}{"key2": "value2"},
		}, result[0].ProviderOptions)
	})
}

// ports convert-to-model-messages.test.ts > describe('user message')
func TestConvertToModelMessages_UserMessage(t *testing.T) {
	t.Run("should convert a simple user message", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Hello"}}},
		})
		require.NoError(t, err)
		assert.Equal(t, []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		}, result)
	})

	t.Run("should handle user message file parts", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{
				FileUIPart{MediaType: "image/png", URL: "https://example.com/image.png"},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Len(t, result[0].Content, 1)
		fc, ok := result[0].Content[0].(types.FileContent)
		require.True(t, ok)
		assert.Equal(t, "image/png", fc.MediaType)
		assert.Equal(t, types.FileDataTypeURL, fc.FileData.Type)
		assert.Equal(t, "https://example.com/image.png", fc.FileData.URL)
	})

	t.Run("should include filename for user file parts when provided", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{
				FileUIPart{MediaType: "image/png", URL: "https://example.com/image.png", Filename: "image.png"},
			}},
		})
		require.NoError(t, err)
		fc := result[0].Content[0].(types.FileContent)
		assert.Equal(t, "image.png", fc.Filename)
	})

	t.Run("should use providerReference as data for user file parts", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{
				FileUIPart{MediaType: "image/png", ProviderReference: map[string]string{"openai": "file-abc"}},
			}},
		})
		require.NoError(t, err)
		fc := result[0].Content[0].(types.FileContent)
		assert.Equal(t, types.FileDataTypeReference, fc.FileData.Type)
		assert.Equal(t, types.ProviderReference(map[string]string{"openai": "file-abc"}), fc.FileData.Reference)
	})

	t.Run("invalid file url produces MessageConversionError", func(t *testing.T) {
		_, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{
				FileUIPart{MediaType: "image/png", URL: "not-a-url"},
			}},
		})
		require.Error(t, err)
		assert.True(t, IsMessageConversionError(err))
	})
}

// ports convert-to-model-messages.test.ts > describe('assistant message')
func TestConvertToModelMessages_AssistantMessage(t *testing.T) {
	t.Run("should convert custom assistant parts", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				CustomContentUIPart{Kind: "provider.custom"},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, []types.ContentPart{types.CustomContent{Kind: "provider.custom"}}, result[0].Content)
	})

	t.Run("should convert a simple assistant text message", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{TextUIPart{Text: "Hello!"}}},
		})
		require.NoError(t, err)
		assert.Equal(t, []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hello!"}}},
		}, result)
	})

	t.Run("should convert an assistant message with reasoning", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				ReasoningUIPart{Text: "thinking..."},
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, []types.ContentPart{types.ReasoningContent{Text: "thinking..."}}, result[0].Content)
	})

	t.Run("should not emit empty assistant message for persistent data", func(t *testing.T) {
		// ports "should not emit empty assistant message for persistent data" (~L3633):
		// a data part with no ConvertDataPart option produces no content and the
		// (would-be-empty) assistant message must be suppressed.
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				DataUIPart{Type: "data-weather", ID: "1", Data: map[string]interface{}{"city": "Tokyo"}},
			}},
		})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("empty tool invocations produce no messages", func(t *testing.T) {
		// ports 'should handle conversation with an assistant message that has empty tool invocations'
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{}},
		})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("should handle assistant message with tool output available", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				StepStartUIPart{},
				TextUIPart{Text: "Let me calculate that for you."},
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateOutputAvailable, ToolCallID: "call1",
					Input:  map[string]interface{}{"operation": "add", "numbers": []interface{}{1, 2}},
					Output: "3",
					CallProviderMetadata: map[string]interface{}{
						"testProvider": map[string]interface{}{"signature": "1234567890"},
					},
				},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 2)

		assistant := result[0]
		require.Len(t, assistant.Content, 2)
		assert.Equal(t, types.TextContent{Text: "Let me calculate that for you."}, assistant.Content[0])
		call := assistant.Content[1].(types.ToolCallContent)
		assert.Equal(t, "call1", call.ToolCallID)
		assert.Equal(t, "calculator", call.ToolName)
		assert.False(t, call.ProviderExecuted)
		assert.Equal(t, map[string]interface{}{"testProvider": map[string]interface{}{"signature": "1234567890"}}, call.ProviderOptions)

		toolMsg := result[1]
		require.Len(t, toolMsg.Content, 1)
		res := toolMsg.Content[0].(types.ToolResultContent)
		assert.Equal(t, "call1", res.ToolCallID)
		assert.Equal(t, "calculator", res.ToolName)
		assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "3"}, res.Output)
		assert.Equal(t, map[string]interface{}{"testProvider": map[string]interface{}{"signature": "1234567890"}}, res.ProviderOptions)
	})

	t.Run("tool output error", func(t *testing.T) {
		// ports describe('tool output error')
		t.Run("preserve result provider metadata when call metadata is unavailable, and input falls back to rawInput", func(t *testing.T) {
			result, err := ConvertToModelMessages(context.Background(), []UIMessage{
				{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
					&ToolUIPart{
						Type: "tool-createWidget", State: ToolStateOutputError, ToolCallID: "call1",
						Input: nil, RawInput: map[string]interface{}{}, ErrorText: "Invalid input",
						ResultProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"namespace": "widget_tools"}},
					},
				}},
			})
			require.NoError(t, err)
			require.Len(t, result, 2)
			call := result[0].Content[0].(types.ToolCallContent)
			assert.Equal(t, map[string]interface{}{}, call.Arguments)
			assert.Equal(t, map[string]interface{}{"openai": map[string]interface{}{"namespace": "widget_tools"}}, call.ProviderOptions)
			res := result[1].Content[0].(types.ToolResultContent)
			assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "Invalid input"}, res.Output)
		})

		t.Run("preserve the deprecated rawInput fallback when input is null", func(t *testing.T) {
			result, err := ConvertToModelMessages(context.Background(), []UIMessage{
				{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
					&ToolUIPart{
						Type: "tool-calculator", State: ToolStateOutputError, ToolCallID: "call1",
						ErrorText: "Error: Invalid input", Input: nil, RawInput: "legacy input",
					},
				}},
			})
			require.NoError(t, err)
			call := result[0].Content[0].(types.ToolCallContent)
			assert.Equal(t, "legacy input", call.Input)
		})

		t.Run("no raw input", func(t *testing.T) {
			result, err := ConvertToModelMessages(context.Background(), []UIMessage{
				{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
					&ToolUIPart{
						Type: "tool-calculator", State: ToolStateOutputError, ToolCallID: "call1",
						Input:     map[string]interface{}{"operation": "add", "numbers": []interface{}{1, 2}},
						ErrorText: "Error: Invalid input",
					},
				}},
			})
			require.NoError(t, err)
			res := result[1].Content[0].(types.ToolResultContent)
			assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "Error: Invalid input"}, res.Output)
		})
	})

	t.Run("should handle assistant message with provider-executed tool output available", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateOutputAvailable, ToolCallID: "call1",
					Input:  map[string]interface{}{"operation": "add", "numbers": []interface{}{1, 2}},
					Output: "3", ProviderExecuted: boolPtr(true),
				},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1) // no separate tool message: result already in assistant content
		require.Len(t, result[0].Content, 2)
		call := result[0].Content[0].(types.ToolCallContent)
		assert.True(t, call.ProviderExecuted)
		res := result[0].Content[1].(types.ToolResultContent)
		assert.True(t, res.ProviderExecuted)
		assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "3"}, res.Output)
	})

	t.Run("should handle assistant message with provider-executed tool output error", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateOutputError, ToolCallID: "call1",
					Input: map[string]interface{}{"operation": "add"}, ErrorText: "boom",
					ProviderExecuted: boolPtr(true),
				},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		res := result[0].Content[1].(types.ToolResultContent)
		assert.Equal(t, types.ToolResultOutputErrorJSON, res.Output.Type)
		assert.Equal(t, "boom", res.Output.Value)
	})

	t.Run("should prefer result provider metadata over call provider metadata for provider-executed tool-result", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateOutputAvailable, ToolCallID: "call1",
					Input: map[string]interface{}{}, Output: "3", ProviderExecuted: boolPtr(true),
					CallProviderMetadata:   map[string]interface{}{"p": map[string]interface{}{"a": "call"}},
					ResultProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"a": "result"}},
				},
			}},
		})
		require.NoError(t, err)
		call := result[0].Content[0].(types.ToolCallContent)
		res := result[0].Content[1].(types.ToolResultContent)
		assert.Equal(t, map[string]interface{}{"p": map[string]interface{}{"a": "call"}}, call.ProviderOptions)
		assert.Equal(t, map[string]interface{}{"p": map[string]interface{}{"a": "result"}}, res.ProviderOptions)
	})
}

// ports convert-to-model-messages.test.ts > describe('multiple messages')
func TestConvertToModelMessages_MultipleMessages(t *testing.T) {
	result, err := ConvertToModelMessages(context.Background(), []UIMessage{
		{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Hi"}}},
		{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{TextUIPart{Text: "Hello!"}}},
		{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "How are you?"}}},
	})
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, types.RoleUser, result[0].Role)
	assert.Equal(t, types.RoleAssistant, result[1].Role)
	assert.Equal(t, types.RoleUser, result[2].Role)
}

// ports convert-to-model-messages.test.ts > describe('error handling')
func TestConvertToModelMessages_UnsupportedRole(t *testing.T) {
	_, err := ConvertToModelMessages(context.Background(), []UIMessage{
		{Role: UIMessageRole("unknown"), Parts: nil},
	})
	require.Error(t, err)
	assert.True(t, IsMessageConversionError(err))
}

// ports convert-to-model-messages.test.ts > describe('when ignoring incomplete tool calls')
func TestConvertToModelMessages_IgnoreIncompleteToolCalls(t *testing.T) {
	t.Run("should ignore preliminary tool outputs", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateOutputAvailable, ToolCallID: "call1",
					Input: map[string]interface{}{}, Output: "partial", Preliminary: boolPtr(true),
				},
			}},
		}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("should ignore tool calls that are awaiting approval or have no state", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{Type: "tool-calculator", State: ToolStateInputStreaming, ToolCallID: "call1"},
				&ToolUIPart{Type: "tool-calculator", State: ToolStateInputAvailable, ToolCallID: "call2", Input: map[string]interface{}{}},
				&ToolUIPart{Type: "tool-calculator", State: ToolStateApprovalRequested, ToolCallID: "call3", Input: map[string]interface{}{}},
			}},
		}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("should preserve tool calls with approval responses", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-calculator", State: ToolStateApprovalResponded, ToolCallID: "call1",
					Input:    map[string]interface{}{},
					Approval: &ToolUIPartApproval{ID: "a1", Approved: boolPtr(true)},
				},
			}},
		}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
		require.NoError(t, err)
		require.Len(t, result, 2)
	})
}

// ports convert-to-model-messages.test.ts > describe('when converting dynamic tool invocations')
func TestConvertToModelMessages_DynamicToolInvocation(t *testing.T) {
	result, err := ConvertToModelMessages(context.Background(), []UIMessage{
		{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
			StepStartUIPart{},
			&ToolUIPart{
				Type: "dynamic-tool", ToolName: "screenshot", State: ToolStateOutputAvailable,
				ToolCallID: "call-1", Input: map[string]interface{}{"value": "value-1"}, Output: "result-1",
			},
		}},
		{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Thanks!"}}},
	}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
	require.NoError(t, err)
	require.Len(t, result, 3)

	call := result[0].Content[0].(types.ToolCallContent)
	assert.Equal(t, "screenshot", call.ToolName)
	assert.False(t, call.ProviderExecuted)

	res := result[1].Content[0].(types.ToolResultContent)
	assert.Equal(t, "screenshot", res.ToolName)
	assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result-1"}, res.Output)

	assert.Equal(t, types.RoleUser, result[2].Role)
}

// ports convert-to-model-messages.test.ts > describe('when converting provider-executed dynamic tool invocations')
func TestConvertToModelMessages_ProviderExecutedDynamicTool(t *testing.T) {
	t.Run("should convert a provider-executed dynamic tool invocation", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				StepStartUIPart{},
				&ToolUIPart{
					Type: "dynamic-tool", ToolName: "screenshot", State: ToolStateOutputAvailable,
					ToolCallID: "call-1", Input: map[string]interface{}{"value": "value-1"}, Output: "result-1",
					ProviderExecuted: boolPtr(true),
					CallProviderMetadata: map[string]interface{}{
						"test-provider": map[string]interface{}{"key-a": "test-value-1", "key-b": "test-value-2"},
					},
				},
			}},
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Thanks!"}}},
		}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
		require.NoError(t, err)
		require.Len(t, result, 2) // no separate tool message

		require.Len(t, result[0].Content, 2)
		call := result[0].Content[0].(types.ToolCallContent)
		assert.True(t, call.ProviderExecuted)
		res := result[0].Content[1].(types.ToolResultContent)
		assert.Equal(t, map[string]interface{}{"test-provider": map[string]interface{}{"key-a": "test-value-1", "key-b": "test-value-2"}}, res.ProviderOptions)
	})

	t.Run("should convert a denied provider-executed tool approval request with an execution-denied result", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				StepStartUIPart{},
				&ToolUIPart{
					Type: "dynamic-tool", ToolName: "screenshot", State: ToolStateApprovalResponded,
					ToolCallID: "call-1", Input: map[string]interface{}{"value": "value-1"},
					ProviderExecuted:     boolPtr(true),
					CallProviderMetadata: map[string]interface{}{"test-provider": map[string]interface{}{"key-a": "test-value-1"}},
					Approval:             &ToolUIPartApproval{ID: "approval-1", Approved: boolPtr(false), Reason: "User denied the request"},
				},
			}},
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "Thanks!"}}},
		}, ConvertToModelMessagesOptions{IgnoreIncompleteToolCalls: true})
		require.NoError(t, err)
		require.Len(t, result, 3)

		require.Len(t, result[0].Content, 2)
		approvalReq := result[0].Content[1].(types.ToolApprovalRequestContent)
		assert.Equal(t, "approval-1", approvalReq.ApprovalID)
		assert.Equal(t, "call-1", approvalReq.ToolCallID)

		require.Len(t, result[1].Content, 2)
		approvalResp := result[1].Content[0].(types.ToolApprovalResponseContent)
		assert.False(t, approvalResp.Approved)
		assert.True(t, approvalResp.ProviderExecuted)
		assert.Equal(t, "User denied the request", approvalResp.Reason)

		res := result[1].Content[1].(types.ToolResultContent)
		assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User denied the request"}, res.Output)
		assert.Equal(t, map[string]interface{}{"test-provider": map[string]interface{}{"key-a": "test-value-1"}}, res.ProviderOptions)

		assert.Equal(t, types.RoleUser, result[2].Role)
	})
}

// ports convert-to-model-messages.test.ts > describe('when converting tool approval request responses')
func TestConvertToModelMessages_ToolApprovals(t *testing.T) {
	t.Run("should propagate reason from a pending approval request", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateApprovalRequested, ToolCallID: "call-1",
					Input:    map[string]interface{}{"city": "Tokyo"},
					Approval: &ToolUIPartApproval{ID: "a1", RequestReason: "requires operator review"},
				},
			}},
		})
		require.NoError(t, err)
		req := result[0].Content[1].(types.ToolApprovalRequestContent)
		assert.Equal(t, "a1", req.ApprovalID)
		assert.Equal(t, "call-1", req.ToolCallID)
		assert.Equal(t, "requires operator review", req.Reason)
	})

	t.Run("should keep request and response reasons separate after approval", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateApprovalResponded, ToolCallID: "call-1",
					Input: map[string]interface{}{"city": "Tokyo"},
					Approval: &ToolUIPartApproval{
						ID: "a1", Approved: boolPtr(true),
						RequestReason: "requires operator review",
						Reason:        "approved by on-call operator",
					},
				},
			}},
		})
		require.NoError(t, err)
		req := result[0].Content[1].(types.ToolApprovalRequestContent)
		assert.Equal(t, "requires operator review", req.Reason)

		resp := result[1].Content[0].(types.ToolApprovalResponseContent)
		assert.Equal(t, "approved by on-call operator", resp.Reason)
	})

	t.Run("should propagate signature from approval to tool-approval-request part", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{TextUIPart{Text: "What is the weather in Tokyo?"}}},
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				StepStartUIPart{},
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateApprovalResponded, ToolCallID: "call-1",
					Input:    map[string]interface{}{"city": "Tokyo"},
					Approval: &ToolUIPartApproval{ID: "a1", Approved: boolPtr(true), Signature: "test-sig"},
				},
			}},
		})
		require.NoError(t, err)
		req := result[1].Content[1].(types.ToolApprovalRequestContent)
		assert.Equal(t, "test-sig", req.Signature)

		resp := result[2].Content[0].(types.ToolApprovalResponseContent)
		assert.True(t, resp.Approved)
		assert.Equal(t, "", resp.Reason)
	})

	t.Run("should not include signature in tool-approval-request when approval has no signature", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateApprovalResponded, ToolCallID: "call-1",
					Input:    map[string]interface{}{"city": "Tokyo"},
					Approval: &ToolUIPartApproval{ID: "a1", Approved: boolPtr(true)},
				},
			}},
		})
		require.NoError(t, err)
		req := result[0].Content[1].(types.ToolApprovalRequestContent)
		assert.Equal(t, "", req.Signature)
	})

	t.Run("should convert tool output denied (static tool)", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateOutputDenied, ToolCallID: "call-1",
					Input:    map[string]interface{}{"city": "Tokyo"},
					Approval: &ToolUIPartApproval{ID: "a1", Approved: boolPtr(false), Reason: "no thanks"},
				},
			}},
		})
		require.NoError(t, err)
		require.Len(t, result[1].Content, 2)
		approvalResp := result[1].Content[0].(types.ToolApprovalResponseContent)
		assert.False(t, approvalResp.Approved)
		res := result[1].Content[1].(types.ToolResultContent)
		assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "no thanks"}, res.Output)
	})

	t.Run("should convert tool output denied without approval (static tool), default reason", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				&ToolUIPart{
					Type: "tool-weather", State: ToolStateOutputDenied, ToolCallID: "call-1",
					Input: map[string]interface{}{"city": "Tokyo"},
				},
			}},
		})
		require.NoError(t, err)
		res := result[1].Content[0].(types.ToolResultContent)
		assert.Equal(t, &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "Tool call execution denied."}, res.Output)
	})
}

// ports convert-to-model-messages.test.ts > describe('data part conversion')
func TestConvertToModelMessages_DataPartConversion(t *testing.T) {
	converter := func(part DataUIPart) types.ContentPart {
		if m, ok := part.Data.(map[string]interface{}); ok {
			if city, ok := m["city"].(string); ok {
				return types.TextContent{Text: "weather for " + city}
			}
		}
		return nil
	}

	t.Run("user message data part", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleUser, Parts: []UIMessagePart{
				DataUIPart{Type: "data-weather", ID: "1", Data: map[string]interface{}{"city": "Tokyo"}},
			}},
		}, ConvertToModelMessagesOptions{ConvertDataPart: converter})
		require.NoError(t, err)
		assert.Equal(t, []types.ContentPart{types.TextContent{Text: "weather for Tokyo"}}, result[0].Content)
	})

	t.Run("assistant message data part, nil converter result is skipped", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				DataUIPart{Type: "data-other", ID: "1", Data: map[string]interface{}{"foo": "bar"}},
			}},
		}, ConvertToModelMessagesOptions{ConvertDataPart: converter})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("assistant message data part converted", func(t *testing.T) {
		result, err := ConvertToModelMessages(context.Background(), []UIMessage{
			{Role: UIMessageRoleAssistant, Parts: []UIMessagePart{
				DataUIPart{Type: "data-weather", ID: "1", Data: map[string]interface{}{"city": "Kyoto"}},
			}},
		}, ConvertToModelMessagesOptions{ConvertDataPart: converter})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, []types.ContentPart{types.TextContent{Text: "weather for Kyoto"}}, result[0].Content)
	})
}
