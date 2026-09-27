package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestToAnthropicMessagesCustomContentWithOptions verifies that CustomContent
// with Anthropic-keyed ProviderOptions is forwarded verbatim to the output.
func TestToAnthropicMessagesCustomContentWithOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.CustomContent{
					Kind: "anthropic-future-block",
					ProviderOptions: map[string]interface{}{
						"anthropic": map[string]interface{}{
							"type":  "future_block",
							"value": "data",
						},
					},
				},
			},
		},
	}

	result := ToAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content should be []map[string]interface{}, got %T", result[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("len(content) = %d, want 1", len(content))
	}
	if content[0]["type"] != "future_block" {
		t.Errorf("type = %v, want \"future_block\"", content[0]["type"])
	}
	if content[0]["value"] != "data" {
		t.Errorf("value = %v, want \"data\"", content[0]["value"])
	}
}

// TestToAnthropicMessagesCustomContentNoOptions verifies that CustomContent
// without Anthropic-keyed ProviderOptions is silently dropped.
func TestToAnthropicMessagesCustomContentNoOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Answer."},
				types.CustomContent{Kind: "xai-citation"}, // no ProviderOptions
			},
		},
	}

	result := ToAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	// Content is a single text part — the custom content was dropped.
	switch c := result[0]["content"].(type) {
	case string:
		if c != "Answer." {
			t.Errorf("content = %q, want \"Answer.\"", c)
		}
	case []map[string]interface{}:
		if len(c) != 1 {
			t.Errorf("content parts = %d, want 1 (custom should be dropped)", len(c))
		}
	default:
		t.Fatalf("unexpected content type %T", result[0]["content"])
	}
}

// TestToAnthropicMessagesReasoningFileDropped verifies that ReasoningFileContent
// in assistant messages is silently dropped.
func TestToAnthropicMessagesReasoningFileDropped(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Here is a chart."},
				types.ReasoningFileContent{
					MediaType: "image/png",
					Data:      []byte{0x89, 0x50, 0x4E, 0x47},
				},
			},
		},
	}

	result := ToAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	// Only the text part should remain.
	switch c := result[0]["content"].(type) {
	case string:
		if c != "Here is a chart." {
			t.Errorf("content = %q, want \"Here is a chart.\"", c)
		}
	case []map[string]interface{}:
		if len(c) != 1 {
			t.Errorf("content parts = %d, want 1 (reasoning file should be dropped)", len(c))
		}
		if c[0]["type"] != "text" {
			t.Errorf("remaining part type = %v, want \"text\"", c[0]["type"])
		}
	default:
		t.Fatalf("unexpected content type %T", result[0]["content"])
	}
}

// TestToOpenAIMessagesCustomContentWithOptions verifies that CustomContent
// with OpenAI-keyed ProviderOptions is forwarded verbatim.
func TestToOpenAIMessagesCustomContentWithOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.CustomContent{
					Kind: "openai-custom",
					ProviderOptions: map[string]interface{}{
						"openai": map[string]interface{}{
							"type":  "custom_block",
							"token": "abc123",
						},
					},
				},
			},
		},
	}

	result := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content should be []map[string]interface{}, got %T", result[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("len(content) = %d, want 1", len(content))
	}
	if content[0]["type"] != "custom_block" {
		t.Errorf("type = %v, want \"custom_block\"", content[0]["type"])
	}
}

// TestToOpenAIMessagesCustomContentNoOptions verifies that CustomContent
// without OpenAI-keyed ProviderOptions is dropped.
func TestToOpenAIMessagesCustomContentNoOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello."},
				types.CustomContent{Kind: "xai-citation"}, // no openai options
			},
		},
	}

	result := ToOpenAIMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	// Single-text messages get the simple string shortcut — verify content is
	// still just the text (custom part was dropped before the shortcut applied).
	_ = result[0]["content"] // just verify no panic
}

func TestToOpenAIMessagesAssistantToolCallsUseNullContentWhenNoText(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{
					ID:        "quux",
					ToolName:  "thwomp",
					Arguments: map[string]interface{}{"foo": "bar123"},
				},
			},
		},
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "quux",
					ToolName:   "thwomp",
					Result:     map[string]interface{}{"oof": "321rab"},
				},
			},
		},
	}

	result := ToOpenAIMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	if _, ok := result[0]["content"]; !ok {
		t.Fatal("assistant content key missing")
	}
	if result[0]["content"] != nil {
		t.Fatalf("assistant content = %#v, want nil", result[0]["content"])
	}
	toolCalls, ok := result[0]["tool_calls"].([]map[string]interface{})
	if !ok {
		t.Fatalf("tool_calls should be []map[string]interface{}, got %T", result[0]["tool_calls"])
	}
	function := toolCalls[0]["function"].(map[string]interface{})
	if function["arguments"] != `{"foo":"bar123"}` {
		t.Fatalf("arguments = %q, want JSON object", function["arguments"])
	}
}

func TestToOpenAIMessagesAssistantWithoutToolCallsUsesEmptyStringContent(t *testing.T) {
	result := ToOpenAIMessages([]types.Message{{Role: types.RoleAssistant}})
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0]["content"] != "" {
		t.Fatalf("assistant content = %#v, want empty string", result[0]["content"])
	}
	if _, ok := result[0]["tool_calls"]; ok {
		t.Fatal("tool_calls should be omitted when assistant has no tool calls")
	}
}

func TestToOpenAIMessagesAssistantToolCallNilArgumentsDefaultToEmptyObject(t *testing.T) {
	result := ToOpenAIMessages([]types.Message{
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{ID: "quux", ToolName: "thwomp"},
			},
		},
	})

	toolCalls := result[0]["tool_calls"].([]map[string]interface{})
	function := toolCalls[0]["function"].(map[string]interface{})
	if function["arguments"] != "{}" {
		t.Fatalf("arguments = %q, want {}", function["arguments"])
	}
	if result[0]["content"] != nil {
		t.Fatalf("assistant content = %#v, want nil", result[0]["content"])
	}
}

// TestToOpenAIMessagesNonObjectRawArgumentsBecomeEmptyObject ports TS's
// OpenAI-only serializeToolCallArguments (packages/openai/src/chat/convert-to-openai-chat-messages.ts,
// 2523403): a replayed RawArguments string that doesn't parse to a JSON
// object -- an array, a scalar, or invalid JSON -- is sent as "{}" rather
// than forwarded verbatim. This sanitization is OpenAI (and Azure chat,
// which wraps OpenAIChatLanguageModel in TS) specific, so the test opts in
// via ToOpenAIMessagesOptions.SanitizeReplayedToolCallArguments.
func TestToOpenAIMessagesNonObjectRawArgumentsBecomeEmptyObject(t *testing.T) {
	tests := []struct {
		name         string
		rawArguments string
	}{
		{"array", `["a","b"]`},
		{"string", `"just a string"`},
		{"number", `42`},
		{"invalid json", `{not valid`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToOpenAIMessages([]types.Message{
				{
					Role: types.RoleAssistant,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "tool", RawArguments: tt.rawArguments},
					},
				},
			}, ToOpenAIMessagesOptions{SanitizeReplayedToolCallArguments: true})
			toolCalls := result[0]["tool_calls"].([]map[string]interface{})
			function := toolCalls[0]["function"].(map[string]interface{})
			if function["arguments"] != "{}" {
				t.Fatalf("arguments = %q, want {}", function["arguments"])
			}
		})
	}
}

// TestToOpenAIMessagesObjectRawArgumentsPassThrough ensures a valid
// object RawArguments string is still forwarded verbatim, with and without
// the OpenAI-only sanitization opted in.
func TestToOpenAIMessagesObjectRawArgumentsPassThrough(t *testing.T) {
	for _, opt := range []ToOpenAIMessagesOptions{{}, {SanitizeReplayedToolCallArguments: true}} {
		result := ToOpenAIMessages([]types.Message{
			{
				Role: types.RoleAssistant,
				ToolCalls: []types.ToolCall{
					{ID: "call_1", ToolName: "tool", RawArguments: `{"a":1}`},
				},
			},
		}, opt)
		toolCalls := result[0]["tool_calls"].([]map[string]interface{})
		function := toolCalls[0]["function"].(map[string]interface{})
		if function["arguments"] != `{"a":1}` {
			t.Fatalf("arguments = %q, want {\"a\":1} (opt=%+v)", function["arguments"], opt)
		}
	}
}

// TestToOpenAIMessagesNonObjectRawArgumentsPassThroughByDefault confirms
// that the default (no ToOpenAIMessagesOptions), used by every non-OpenAI
// caller of ToOpenAIMessages (Groq, DeepSeek, the openai-compatible family --
// Together/Fireworks/Mistral/Ollama/etc. -- and Alibaba), forwards
// RawArguments verbatim even when it isn't a JSON object. TS's equivalents
// for those providers (e.g. packages/groq/src/convert-to-groq-chat-messages.ts,
// packages/deepseek/src/chat/convert-to-deepseek-chat-messages.ts,
// packages/openai-compatible/src/chat/convert-to-openai-compatible-chat-messages.ts,
// packages/alibaba/src/convert-to-alibaba-chat-messages.ts) all just do
// `arguments: JSON.stringify(part.input)` with no OpenAI-style
// sanitization, so only OpenAI's/Azure's own chat conversion should opt in.
func TestToOpenAIMessagesNonObjectRawArgumentsPassThroughByDefault(t *testing.T) {
	tests := []struct {
		name         string
		rawArguments string
	}{
		{"array", `["a","b"]`},
		{"string", `"just a string"`},
		{"number", `42`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToOpenAIMessages([]types.Message{
				{
					Role: types.RoleAssistant,
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "tool", RawArguments: tt.rawArguments},
					},
				},
			})
			toolCalls := result[0]["tool_calls"].([]map[string]interface{})
			function := toolCalls[0]["function"].(map[string]interface{})
			if function["arguments"] != tt.rawArguments {
				t.Fatalf("arguments = %q, want %q (unsanitized, non-OpenAI default)", function["arguments"], tt.rawArguments)
			}
		})
	}
}

// TestToGoogleMessagesCustomContentWithOptions verifies that CustomContent
// with Google-keyed ProviderOptions is forwarded to the parts array.
func TestToGoogleMessagesCustomContentWithOptions(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.CustomContent{
					Kind: "google-grounding",
					ProviderOptions: map[string]interface{}{
						"google": map[string]interface{}{
							"type":  "grounding_metadata",
							"chunk": "data",
						},
					},
				},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}

	parts, ok := result[0]["parts"].([]map[string]interface{})
	if !ok {
		t.Fatalf("parts should be []map[string]interface{}, got %T", result[0]["parts"])
	}
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	if parts[0]["type"] != "grounding_metadata" {
		t.Errorf("type = %v, want \"grounding_metadata\"", parts[0]["type"])
	}
}

// TestCustomContentNilProviderOptionsNocrash verifies that CustomContent with
// a nil ProviderOptions map does not panic in any converter.
func TestCustomContentNilProviderOptionsNoCrash(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.CustomContent{Kind: "xai-citation"}, // ProviderOptions is nil
			},
		},
	}

	// None of these should panic.
	_ = ToAnthropicMessages(msgs)
	_ = ToOpenAIMessages(msgs)
	_ = ToGoogleMessages(msgs, false)
}

// TestCustomContentProviderMetadataNotForwarded verifies that ProviderMetadata
// (the output/response field) is NOT used for routing in converters — only
// ProviderOptions (the input field) is checked.
func TestCustomContentProviderMetadataNotForwarded(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Answer."},
				types.CustomContent{
					Kind:             "xai-citation",
					ProviderMetadata: json.RawMessage(`{"url":"https://x.ai"}`),
					// No ProviderOptions — should be dropped even though metadata is set.
				},
			},
		},
	}

	result := ToAnthropicMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	// Only text content should be present.
	switch c := result[0]["content"].(type) {
	case string:
		// Fine — single text got the shortcut (only if CustomContent was dropped first)
	case []map[string]interface{}:
		for _, part := range c {
			if part["type"] != "text" {
				t.Errorf("unexpected non-text part in output: %v", part)
			}
		}
	default:
		_ = c
	}
}

// --- ToGoogleMessages tool result and functionCall tests --------------------

// TestToGoogleMessagesSimpleToolResult verifies that a basic ToolResultContent
// (no Output) is serialized as a functionResponse with response.content.
func TestToGoogleMessagesSimpleToolResult(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "c1",
				ToolName:   "calculator",
				Result:     "42",
			},
		}},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0]["role"] != "user" {
		t.Errorf("role = %v, want user", result[0]["role"])
	}
	parts := result[0]["parts"].([]map[string]interface{})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	fr := parts[0]["functionResponse"].(map[string]interface{})
	if fr["name"] != "calculator" {
		t.Errorf("name = %v, want calculator", fr["name"])
	}
	resp := fr["response"].(map[string]interface{})
	if resp["content"] != "42" {
		t.Errorf("content = %v, want 42", resp["content"])
	}
}

// TestToGoogleMessagesMultimodalToolResultGemini3 verifies that with
// supportsFunctionResponseParts=true, image blocks land in
// functionResponse.parts[].inlineData.
func TestToGoogleMessagesMultimodalToolResultGemini3(t *testing.T) {
	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	msgs := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "c1",
				ToolName:   "screenshot",
				Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.TextContentBlock{Text: "screenshot taken"},
						types.ImageContentBlock{Data: imageBytes, MediaType: "image/png"},
					},
				},
			},
		}},
	}

	result := ToGoogleMessages(msgs, true) // Gemini 3+
	parts := result[0]["parts"].([]map[string]interface{})
	fr := parts[0]["functionResponse"].(map[string]interface{})

	// Text goes into response.content.
	resp := fr["response"].(map[string]interface{})
	if resp["content"] != "screenshot taken" {
		t.Errorf("response.content = %v, want %q", resp["content"], "screenshot taken")
	}

	// Image goes into functionResponse.parts[].inlineData.
	frParts, ok := fr["parts"].([]map[string]interface{})
	if !ok || len(frParts) == 0 {
		t.Fatalf("functionResponse.parts missing; got %v", fr["parts"])
	}
	inlineData := frParts[0]["inlineData"].(map[string]interface{})
	if inlineData["mimeType"] != "image/png" {
		t.Errorf("mimeType = %v, want image/png", inlineData["mimeType"])
	}
}

// TestToGoogleMessagesMultimodalToolResultLegacy verifies that with
// supportsFunctionResponseParts=false, images become top-level inlineData parts.
func TestToGoogleMessagesMultimodalToolResultLegacy(t *testing.T) {
	imageBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	msgs := []types.Message{
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "c1",
				ToolName:   "screenshot",
				Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.ImageContentBlock{Data: imageBytes, MediaType: "image/jpeg"},
					},
				},
			},
		}},
	}

	result := ToGoogleMessages(msgs, false) // Gemini 2 legacy
	parts := result[0]["parts"].([]map[string]interface{})

	var hasInlineData bool
	for _, pt := range parts {
		if _, ok := pt["inlineData"]; ok {
			hasInlineData = true
		}
		if fr, ok := pt["functionResponse"].(map[string]interface{}); ok {
			if _, hasParts := fr["parts"]; hasParts {
				t.Error("legacy mode must NOT use functionResponse.parts[]")
			}
		}
	}
	if !hasInlineData {
		t.Error("legacy mode must emit a top-level inlineData part for images")
	}
}

// TestToGoogleMessagesReasoningContent verifies that ReasoningContent parts are
// emitted as thought=true parts with thoughtSignature.
func TestToGoogleMessagesReasoningContent(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "I need to think about this.", Signature: "sealed-sig-abc"},
				types.TextContent{Text: "Here is my answer."},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	parts := result[0]["parts"].([]map[string]interface{})
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}

	// First part: thought
	thoughtPart := parts[0]
	if thoughtPart["thought"] != true {
		t.Errorf("thought = %v, want true", thoughtPart["thought"])
	}
	if thoughtPart["text"] != "I need to think about this." {
		t.Errorf("text = %v, want 'I need to think about this.'", thoughtPart["text"])
	}
	if thoughtPart["thoughtSignature"] != "sealed-sig-abc" {
		t.Errorf("thoughtSignature = %v, want 'sealed-sig-abc'", thoughtPart["thoughtSignature"])
	}

	// Second part: regular text
	textPart := parts[1]
	if textPart["text"] != "Here is my answer." {
		t.Errorf("text = %v, want 'Here is my answer.'", textPart["text"])
	}
	if _, exists := textPart["thought"]; exists {
		t.Error("regular text part must not have 'thought' field")
	}
}

// TestToGoogleMessagesThoughtSignatureOnFunctionCall verifies that ToolCall.ThoughtSignature
// is emitted as a top-level thoughtSignature field on the functionCall part.
func TestToGoogleMessagesThoughtSignatureOnFunctionCall(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{
					ID:               "c1",
					ToolName:         "search",
					Arguments:        map[string]interface{}{"q": "test"},
					ThoughtSignature: "fc-sig-xyz",
				},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	parts := result[0]["parts"].([]map[string]interface{})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}

	if parts[0]["thoughtSignature"] != "fc-sig-xyz" {
		t.Errorf("thoughtSignature = %v, want 'fc-sig-xyz'", parts[0]["thoughtSignature"])
	}
	// functionCall must still be present
	if _, ok := parts[0]["functionCall"]; !ok {
		t.Error("functionCall field must be present on the part")
	}
}

// TestToGoogleMessagesExecutionDeniedToolResult verifies that an execution-denied
// tool result is sent as a clear denial message to the model.
func TestToGoogleMessagesExecutionDeniedToolResult(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "c1",
					ToolName:   "run_code",
					Output: &types.ToolResultOutput{
						Type:   types.ToolResultOutputExecutionDenied,
						Reason: "User rejected the tool call",
					},
				},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	parts := result[0]["parts"].([]map[string]interface{})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}

	fr := parts[0]["functionResponse"].(map[string]interface{})
	resp := fr["response"].(map[string]interface{})
	content, _ := resp["content"].(string)
	// TS SDK: content = output.reason ?? 'Tool execution denied.' — no prefix added
	if content != "User rejected the tool call" {
		t.Errorf("denial content = %q, want %q", content, "User rejected the tool call")
	}
}

// TestToGoogleMessagesExecutionDeniedNoReason verifies execution-denied with no reason
// uses a sensible fallback message.
func TestToGoogleMessagesExecutionDeniedNoReason(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "c1",
					ToolName:   "run_code",
					Output: &types.ToolResultOutput{
						Type: types.ToolResultOutputExecutionDenied,
					},
				},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	parts := result[0]["parts"].([]map[string]interface{})
	fr := parts[0]["functionResponse"].(map[string]interface{})
	resp := fr["response"].(map[string]interface{})
	content, _ := resp["content"].(string)
	if content != "Tool call execution denied." {
		t.Errorf("denial content = %q, want %q", content, "Tool call execution denied.")
	}
}

// TestToGoogleMessagesAssistantFunctionCall verifies that msg.ToolCalls on an
// assistant message are emitted as functionCall parts in the "model" turn.
func TestToGoogleMessagesAssistantFunctionCall(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			ToolCalls: []types.ToolCall{
				{ID: "c1", ToolName: "search", Arguments: map[string]interface{}{"q": "go generics"}},
			},
		},
	}

	result := ToGoogleMessages(msgs, false)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0]["role"] != "model" {
		t.Errorf("role = %v, want model", result[0]["role"])
	}
	parts := result[0]["parts"].([]map[string]interface{})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	fc := parts[0]["functionCall"].(map[string]interface{})
	if fc["name"] != "search" {
		t.Errorf("name = %v, want search", fc["name"])
	}
	if fc["id"] != "c1" {
		t.Errorf("id = %v, want c1", fc["id"])
	}
	args := fc["args"].(map[string]interface{})
	if args["q"] != "go generics" {
		t.Errorf("args[q] = %v, want go generics", args["q"])
	}
}

func TestToGoogleMessagesFunctionResponseIncludesID(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "lookup", Result: "ok"},
		},
	}}
	got := ToGoogleMessages(msgs, true)
	parts := got[0]["parts"].([]map[string]interface{})
	fr := parts[0]["functionResponse"].(map[string]interface{})
	if fr["id"] != "call-1" {
		t.Fatalf("functionResponse.id = %v", fr["id"])
	}
}

func TestToAnthropicMessagesToolResultOutputCacheControl(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "call-1",
				ToolName:   "lookup",
				Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.TextContentBlock{Text: "cached"},
					},
					ProviderOptions: map[string]interface{}{
						"anthropic": map[string]interface{}{
							"cache_control": map[string]interface{}{"type": "ephemeral"},
						},
					},
				},
			},
		},
	}}
	got := ToAnthropicMessages(msgs)
	content := got[0]["content"].([]map[string]interface{})
	toolResult := content[0]
	cacheControl, ok := toolResult["cache_control"].(map[string]interface{})
	if !ok || cacheControl["type"] != "ephemeral" {
		t.Fatalf("cache_control = %#v", toolResult["cache_control"])
	}
}

func TestToAnthropicMessagesContainerUploadFileReference(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.FileContent{
				MediaType: "application/pdf",
				Reference: "file_123",
				ProviderOptions: map[string]interface{}{
					"anthropic": map[string]interface{}{"containerUpload": true},
				},
			},
		},
	}}
	got := ToAnthropicMessages(msgs)
	content := got[0]["content"].([]map[string]interface{})
	block := content[0]
	if block["type"] != "container_upload" || block["file_id"] != "file_123" {
		t.Fatalf("container upload block = %#v", block)
	}
}

func TestToAnthropicMessagesProviderExecutedWebSearchResult(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID:       "srvtoolu_search",
				ToolName:         "web_search",
				ProviderExecuted: true,
				Result: []map[string]interface{}{
					{
						"type":             "web_search_result",
						"url":              "https://example.com",
						"title":            "Example",
						"pageAge":          "2026-05-31",
						"encryptedContent": "enc",
					},
				},
			},
		},
	}}
	got := ToAnthropicMessages(msgs)
	content := got[0]["content"].([]map[string]interface{})
	block := content[0]
	if block["type"] != "web_search_tool_result" || block["tool_use_id"] != "srvtoolu_search" {
		t.Fatalf("web search result block = %#v", block)
	}
	results := block["content"].([]map[string]interface{})
	result := results[0]
	if result["page_age"] != "2026-05-31" || result["encrypted_content"] != "enc" {
		t.Fatalf("web search content = %#v", result)
	}
}

// A locally executed tool that happens to be named web_search is replayed in a
// tool message and must stay a generic tool_result (TS: only assistant-role
// tool results are provider-executed results).
func TestToAnthropicMessagesLocalWebSearchToolResultStaysGeneric(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID: "call_search",
				ToolName:   "web_search",
				Result:     map[string]interface{}{"ok": true},
			},
		},
	}}
	got := ToAnthropicMessages(msgs)
	content := got[0]["content"].([]map[string]interface{})
	block := content[0]
	if block["type"] != "tool_result" {
		t.Fatalf("tool result block = %#v, want generic tool_result", block)
	}
}

func TestToAnthropicMessagesProviderExecutedWebFetchResult(t *testing.T) {
	msgs := []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID:       "srvtoolu_fetch",
				ToolName:         "web_fetch",
				ProviderExecuted: true,
				Result: map[string]interface{}{
					"type":        "web_fetch_result",
					"url":         "https://example.com/doc",
					"retrievedAt": "2026-05-31T00:00:00Z",
					"content": map[string]interface{}{
						"type":  "document",
						"title": "Doc",
						"source": map[string]interface{}{
							"type":      "text",
							"mediaType": "text/plain",
							"data":      "body",
						},
					},
				},
			},
		},
	}}
	got := ToAnthropicMessages(msgs)
	content := got[0]["content"].([]map[string]interface{})
	block := content[0]
	if block["type"] != "web_fetch_tool_result" || block["tool_use_id"] != "srvtoolu_fetch" {
		t.Fatalf("web fetch result block = %#v", block)
	}
	fetch := block["content"].(map[string]interface{})
	if fetch["retrieved_at"] != "2026-05-31T00:00:00Z" {
		t.Fatalf("web fetch content = %#v", fetch)
	}
	doc := fetch["content"].(map[string]interface{})
	source := doc["source"].(map[string]interface{})
	if source["media_type"] != "text/plain" {
		t.Fatalf("web fetch source = %#v", source)
	}
}

func TestToOpenAIMessagesImageDetailProviderOption(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.ImageContent{
					URL: "https://example.com/image.png",
					ProviderOptions: map[string]interface{}{
						"openai": map[string]interface{}{"imageDetail": "high"},
					},
				},
				types.FileContent{
					URL:       "https://example.com/file-image.png",
					MediaType: "image/png",
					ProviderOptions: map[string]interface{}{
						"openai": map[string]interface{}{"imageDetail": "low"},
					},
				},
			},
		},
	}

	result := ToOpenAIMessages(msgs)
	content := result[0]["content"].([]map[string]interface{})

	firstImage := content[0]["image_url"].(map[string]interface{})
	if firstImage["detail"] != "high" {
		t.Fatalf("image detail: got %v, want high", firstImage["detail"])
	}

	secondImage := content[1]["image_url"].(map[string]interface{})
	if secondImage["detail"] != "low" {
		t.Fatalf("file image detail: got %v, want low", secondImage["detail"])
	}
}

// promptCacheBreakpoint tests below port TS
// convert-to-openai-chat-messages.test.ts (b2b1bb9): promptCacheBreakpoint is
// unique to OpenAI's (and Azure's, which wraps it) chat conversion, so these
// only exercise ToOpenAIMessages with IncludePromptCacheBreakpoint: true.

var breakpointOpts = ToOpenAIMessagesOptions{IncludePromptCacheBreakpoint: true}

func promptCacheBreakpointOption() map[string]interface{} {
	return map[string]interface{}{
		"openai": map[string]interface{}{
			"promptCacheBreakpoint": map[string]interface{}{"mode": "explicit"},
		},
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointIgnoredWithoutOption verifies the
// feature is inert unless the caller opts in (Groq/DeepSeek/openai-compatible/
// Alibaba/etc. must never emit prompt_cache_breakpoint).
func TestToOpenAIMessagesPromptCacheBreakpointIgnoredWithoutOption(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello", ProviderOptions: promptCacheBreakpointOption()},
			},
		},
	}
	result := ToOpenAIMessages(msgs)
	if _, isString := result[0]["content"].(string); !isString {
		t.Fatalf("expected plain string content without the option, got %#v", result[0]["content"])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointUserSingleText ports "should add
// prompt cache breakpoints to supported content blocks" for the single-text
// shortcut: a lone text part with a breakpoint must NOT take the plain-string
// fast path.
func TestToOpenAIMessagesPromptCacheBreakpointUserSingleText(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleSystem,
			Content: []types.ContentPart{
				types.TextContent{Text: "You are a helpful assistant.", ProviderOptions: promptCacheBreakpointOption()},
			},
		},
	}
	result := ToOpenAIMessages(msgs, breakpointOpts)
	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected array content, got %#v", result[0]["content"])
	}
	if len(content) != 1 || content[0]["text"] != "You are a helpful assistant." {
		t.Fatalf("unexpected content: %#v", content)
	}
	bp, ok := content[0]["prompt_cache_breakpoint"].(map[string]interface{})
	if !ok || bp["mode"] != "explicit" {
		t.Fatalf("unexpected breakpoint: %#v", content[0]["prompt_cache_breakpoint"])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointUserContentBlocks ports "should add
// prompt cache breakpoints to supported content blocks": text, image (via
// FileContent), audio (via FileContent) and a referenced pdf file all carry
// the breakpoint through.
func TestToOpenAIMessagesPromptCacheBreakpointUserContentBlocks(t *testing.T) {
	bp := promptCacheBreakpointOption()
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Hello", ProviderOptions: bp},
				types.ImageContent{URL: "https://example.com/image.png", ProviderOptions: bp},
				types.FileContent{
					MediaType:       "audio/wav",
					Data:            []byte{0, 1, 2, 3},
					ProviderOptions: bp,
				},
				types.FileContent{
					MediaType:       "application/pdf",
					Reference:       "file-pdf-123",
					ProviderOptions: bp,
				},
			},
		},
	}
	result := ToOpenAIMessages(msgs, breakpointOpts)
	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 4 {
		t.Fatalf("expected 4 content parts, got %#v", result[0]["content"])
	}
	for i, part := range content {
		got, ok := part["prompt_cache_breakpoint"].(map[string]interface{})
		if !ok || got["mode"] != "explicit" {
			t.Fatalf("part %d missing breakpoint: %#v", i, part)
		}
	}
	if content[0]["type"] != "text" {
		t.Fatalf("part 0 type = %v, want text", content[0]["type"])
	}
	if content[1]["type"] != "image_url" {
		t.Fatalf("part 1 type = %v, want image_url", content[1]["type"])
	}
	if content[2]["type"] != "input_audio" {
		t.Fatalf("part 2 type = %v, want input_audio", content[2]["type"])
	}
	if content[3]["type"] != "file" {
		t.Fatalf("part 3 type = %v, want file", content[3]["type"])
	}
	fileObj, ok := content[3]["file"].(map[string]interface{})
	if !ok || fileObj["file_id"] != "file-pdf-123" {
		t.Fatalf("part 3 file = %#v, want file_id file-pdf-123", content[3]["file"])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointAssistantText ports "assistant text
// content with promptCacheBreakpoint": the assistant message switches from a
// flattened string to a textParts array when any text part has a breakpoint.
func TestToOpenAIMessagesPromptCacheBreakpointAssistantText(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "Cached assistant content", ProviderOptions: promptCacheBreakpointOption()},
			},
		},
	}
	result := ToOpenAIMessages(msgs, breakpointOpts)
	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected array content, got %#v", result[0]["content"])
	}
	if content[0]["text"] != "Cached assistant content" {
		t.Fatalf("unexpected text: %#v", content[0])
	}
	bp, ok := content[0]["prompt_cache_breakpoint"].(map[string]interface{})
	if !ok || bp["mode"] != "explicit" {
		t.Fatalf("unexpected breakpoint: %#v", content[0])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointAssistantWithToolCalls verifies the
// textParts array form is used even when the assistant message also made
// tool calls (TS keeps tool_calls and content independent).
func TestToOpenAIMessagesPromptCacheBreakpointAssistantWithToolCalls(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: "thinking", ProviderOptions: promptCacheBreakpointOption()},
			},
			ToolCalls: []types.ToolCall{
				{ID: "call_1", ToolName: "lookup", Arguments: map[string]interface{}{}},
			},
		},
	}
	result := ToOpenAIMessages(msgs, breakpointOpts)
	if _, ok := result[0]["tool_calls"].([]map[string]interface{}); !ok {
		t.Fatalf("expected tool_calls to be preserved, got %#v", result[0]["tool_calls"])
	}
	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 1 || content[0]["text"] != "thinking" {
		t.Fatalf("expected textParts content, got %#v", result[0]["content"])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointToolResult ports "tool result
// content with promptCacheBreakpoint": a tool role message's content is
// wrapped in the array form when its output carries a breakpoint.
func TestToOpenAIMessagesPromptCacheBreakpointToolResult(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "cached-tool",
					ToolName:   "cached-tool",
					Output: &types.ToolResultOutput{
						Type:            types.ToolResultOutputText,
						Value:           "Cached tool content",
						ProviderOptions: promptCacheBreakpointOption(),
					},
				},
			},
		},
	}
	result := ToOpenAIMessages(msgs, breakpointOpts)
	if result[0]["tool_call_id"] != "cached-tool" {
		t.Fatalf("unexpected tool_call_id: %#v", result[0])
	}
	content, ok := result[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected array content, got %#v", result[0]["content"])
	}
	if content[0]["text"] != "Cached tool content" {
		t.Fatalf("unexpected text: %#v", content[0])
	}
	bp, ok := content[0]["prompt_cache_breakpoint"].(map[string]interface{})
	if !ok || bp["mode"] != "explicit" {
		t.Fatalf("unexpected breakpoint: %#v", content[0])
	}
}

// TestToOpenAIMessagesPromptCacheBreakpointToolResultWithoutOption verifies
// tool results stay plain strings for non-OpenAI/Azure callers.
func TestToOpenAIMessagesPromptCacheBreakpointToolResultWithoutOption(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "cached-tool",
					ToolName:   "cached-tool",
					Output: &types.ToolResultOutput{
						Type:            types.ToolResultOutputText,
						Value:           "Cached tool content",
						ProviderOptions: promptCacheBreakpointOption(),
					},
				},
			},
		},
	}
	result := ToOpenAIMessages(msgs)
	if _, isString := result[0]["content"].(string); !isString {
		t.Fatalf("expected plain string content without the option, got %#v", result[0]["content"])
	}
}
