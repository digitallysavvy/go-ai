package anthropic

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// newTestModel creates a model for unit tests.
func newTestModel(options *ModelOptions) *LanguageModel {
	cfg := Config{APIKey: "test-key", BaseURL: DefaultBaseURL}
	return NewLanguageModel(New(cfg), "claude-opus-4-6", options)
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// --- SR-T11: ReasoningContent{Signature} → {type:"thinking"} in Anthropic message body ---

func TestToAnthropicMessages_ThinkingBlockWithSignature(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{
					Text:      "Let me think...",
					Signature: "sig-abc",
				},
				types.TextContent{Text: "The answer is 42."},
			},
		},
	}

	result := prompt.ToAnthropicMessages(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	parts, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content is not []map[string]interface{}")
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(parts))
	}

	// First part should be the thinking block
	thinkingBlock := parts[0]
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("expected type=thinking, got %v", thinkingBlock["type"])
	}
	if thinkingBlock["thinking"] != "Let me think..." {
		t.Errorf("expected thinking text, got %v", thinkingBlock["thinking"])
	}
	if thinkingBlock["signature"] != "sig-abc" {
		t.Errorf("expected signature=sig-abc, got %v", thinkingBlock["signature"])
	}

	// Second part should be the text block
	if parts[1]["type"] != "text" {
		t.Errorf("expected second part type=text, got %v", parts[1]["type"])
	}
}

// --- SR-T12: ReasoningContent{RedactedData} → {type:"redacted_thinking"} in Anthropic message body ---

func TestToAnthropicMessages_RedactedThinkingBlock(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{
					RedactedData: "opaque-blob-xyz",
				},
				types.TextContent{Text: "Response."},
			},
		},
	}

	result := prompt.ToAnthropicMessages(messages)
	parts, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content is not []map[string]interface{}")
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(parts))
	}

	redacted := parts[0]
	if redacted["type"] != "redacted_thinking" {
		t.Errorf("expected type=redacted_thinking, got %v", redacted["type"])
	}
	if redacted["data"] != "opaque-blob-xyz" {
		t.Errorf("expected data=opaque-blob-xyz, got %v", redacted["data"])
	}
}

// --- SR-T13: ReasoningContent with no Signature or RedactedData → silently omitted ---

func TestToAnthropicMessages_ReasoningWithNoMetadataOmitted(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				// No Signature, no RedactedData — cannot be safely re-sent
				types.ReasoningContent{Text: "Internal thought"},
				types.TextContent{Text: "Reply."},
			},
		},
	}

	result := prompt.ToAnthropicMessages(messages)
	parts, ok := result[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content is not []map[string]interface{}")
	}
	// Only the text part should remain; reasoning block is silently dropped
	if len(parts) != 1 {
		t.Errorf("expected 1 content part (reasoning omitted), got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("expected remaining part to be text, got %v", parts[0]["type"])
	}
}

// --- SR-T14: SendReasoning=false → no thinking or redacted_thinking blocks in outgoing body ---

func TestBuildRequestBody_SendReasoningFalse_StripsThinkingBlocks(t *testing.T) {
	model := newTestModel(&ModelOptions{
		SendReasoning: boolPtr(false),
	})

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Hello"},
					},
				},
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{Text: "Thinking...", Signature: "sig-1"},
						types.TextContent{Text: "Hi there!"},
					},
				},
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Follow-up question"},
					},
				},
			},
		},
	}

	body := model.buildRequestBody(opts, false)

	rawMessages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages is not []map[string]interface{}")
	}

	// Verify no thinking or redacted_thinking blocks appear in any message
	for _, msg := range rawMessages {
		content := msg["content"]
		switch c := content.(type) {
		case string:
			// Simple string content — fine
		case []map[string]interface{}:
			for _, part := range c {
				typ, _ := part["type"].(string)
				if typ == "thinking" || typ == "redacted_thinking" {
					t.Errorf("found unexpected %q block in outgoing body with SendReasoning=false", typ)
				}
			}
		}
	}
}

// --- SR-T15: SendReasoning=nil/true → thinking blocks with signature are included ---

func TestBuildRequestBody_SendReasoningNil_IncludesThinkingBlocks(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sendReasoning *bool
	}{
		{"nil (default)", nil},
		{"explicit true", boolPtr(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := newTestModel(&ModelOptions{
				SendReasoning: tc.sendReasoning,
			})

			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{
							Role: types.RoleAssistant,
							Content: []types.ContentPart{
								types.ReasoningContent{Text: "Thinking...", Signature: "sig-xyz"},
								types.TextContent{Text: "Answer"},
							},
						},
					},
				},
			}

			body := model.buildRequestBody(opts, false)
			rawMessages, ok := body["messages"].([]map[string]interface{})
			if !ok {
				t.Fatalf("messages is not []map[string]interface{}")
			}

			foundThinking := false
			for _, msg := range rawMessages {
				if parts, ok := msg["content"].([]map[string]interface{}); ok {
					for _, part := range parts {
						if part["type"] == "thinking" {
							foundThinking = true
						}
					}
				}
			}
			if !foundThinking {
				t.Errorf("expected thinking block in outgoing body with SendReasoning=%v, but not found", tc.sendReasoning)
			}
		})
	}
}

// --- SR-T16: convertResponse() on a thinking block → ReasoningContent.Signature is set ---

func TestConvertResponse_ThinkingBlock_SetsSignature(t *testing.T) {
	model := newTestModel(nil)

	response := anthropicResponse{
		StopReason: "end_turn",
		Content: []anthropicContent{
			{
				Type:      "thinking",
				Thinking:  "Let me reason about this...",
				Signature: "crypto-sig-abc123",
			},
			{
				Type: "text",
				Text: "The answer.",
			},
		},
	}

	result := model.convertResponse(response, false)

	if len(result.Content) == 0 {
		t.Fatal("expected at least one content part in result")
	}

	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("expected ReasoningContent, got %T", result.Content[0])
	}
	if rc.Text != "Let me reason about this..." {
		t.Errorf("expected Text='Let me reason about this...', got %q", rc.Text)
	}
	if rc.Signature != "crypto-sig-abc123" {
		t.Errorf("expected Signature='crypto-sig-abc123', got %q", rc.Signature)
	}
	if rc.RedactedData != "" {
		t.Errorf("expected empty RedactedData, got %q", rc.RedactedData)
	}
}

// --- SR-T17: convertResponse() on a redacted_thinking block → ReasoningContent.RedactedData is set ---

func TestConvertResponse_RedactedThinkingBlock_SetsRedactedData(t *testing.T) {
	model := newTestModel(nil)

	response := anthropicResponse{
		StopReason: "end_turn",
		Content: []anthropicContent{
			{
				Type: "redacted_thinking",
				Data: "opaque-redacted-blob",
			},
			{
				Type: "text",
				Text: "Final answer.",
			},
		},
	}

	result := model.convertResponse(response, false)

	if len(result.Content) == 0 {
		t.Fatal("expected at least one content part in result")
	}

	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("expected ReasoningContent, got %T", result.Content[0])
	}
	if rc.Text != "" {
		t.Errorf("expected empty Text for redacted block, got %q", rc.Text)
	}
	if rc.RedactedData != "opaque-redacted-blob" {
		t.Errorf("expected RedactedData='opaque-redacted-blob', got %q", rc.RedactedData)
	}
	if rc.Signature != "" {
		t.Errorf("expected empty Signature for redacted block, got %q", rc.Signature)
	}
}

// --- Round-trip test: API response → ReasoningContent → outgoing request body ---

func TestSendReasoning_RoundTrip(t *testing.T) {
	model := newTestModel(nil) // SendReasoning=nil (default: include)

	// Simulate API response with a thinking block
	response := anthropicResponse{
		StopReason: "end_turn",
		Content: []anthropicContent{
			{
				Type:      "thinking",
				Thinking:  "The user wants X.",
				Signature: "round-trip-sig",
			},
			{
				Type: "text",
				Text: "Here is X.",
			},
		},
	}
	result := model.convertResponse(response, false)

	// Build a follow-up conversation including the reasoning block from the response
	if len(result.Content) == 0 {
		t.Fatal("expected reasoning content in result")
	}
	rc := result.Content[0]

	followUpOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Tell me about X"}}},
				{
					Role:    types.RoleAssistant,
					Content: []types.ContentPart{rc, types.TextContent{Text: "Here is X."}},
				},
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "More detail?"}}},
			},
		},
	}

	body := model.buildRequestBody(followUpOpts, false)
	rawMessages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages is not []map[string]interface{}")
	}

	// Verify the thinking block is in the outgoing body
	found := false
	for _, msg := range rawMessages {
		if parts, ok := msg["content"].([]map[string]interface{}); ok {
			for _, part := range parts {
				if part["type"] == "thinking" && part["signature"] == "round-trip-sig" {
					found = true
				}
			}
		}
	}
	if !found {
		// Dump body for debugging
		b, _ := json.MarshalIndent(rawMessages, "", "  ")
		t.Errorf("expected thinking block with round-trip-sig in outgoing body, got:\n%s", string(b))
	}
}

// --- SR-T18: Integration test stub (skips without ANTHROPIC_API_KEY) ---

func TestSendReasoning_Integration(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping: ANTHROPIC_API_KEY not set")
	}
	// Live integration test: extended thinking round-trip.
	// With a real API key this would:
	// 1. Call claude-opus-4-6 with extended thinking enabled.
	// 2. Capture the thinking block (ReasoningContent with Signature) from the result.
	// 3. Build a follow-up request including that block in message history.
	// 4. Confirm the second call succeeds without API errors.
	t.Log("Integration test for send_reasoning is a manual test — run with a real API key")
}

// --- filterReasoningContent helper tests ---

func TestFilterReasoningContent_RemovesReasoningParts(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "thinking", Signature: "s1"},
				types.TextContent{Text: "visible text"},
			},
		},
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "user message"},
			},
		},
	}

	filtered := filterReasoningContent(messages)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(filtered))
	}

	// First message should have only text content
	if len(filtered[0].Content) != 1 {
		t.Errorf("expected 1 content part in filtered message, got %d", len(filtered[0].Content))
	}
	if filtered[0].Content[0].ContentType() != "text" {
		t.Errorf("expected text content part, got %s", filtered[0].Content[0].ContentType())
	}

	// Original is not modified
	if len(messages[0].Content) != 2 {
		t.Error("filterReasoningContent should not modify original slice")
	}
}

func TestFilterReasoningContent_MessageAllReasoningKeepsEmptyContent(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "only thinking", Signature: "sig"},
			},
		},
	}

	filtered := filterReasoningContent(messages)

	// Message is kept but with empty content slice
	if len(filtered) != 1 {
		t.Fatalf("expected 1 message kept, got %d", len(filtered))
	}
	if len(filtered[0].Content) != 0 {
		t.Errorf("expected empty content for all-reasoning message, got %d parts", len(filtered[0].Content))
	}
}

func TestFilterReasoningContent_NoReasoningUnchanged(t *testing.T) {
	messages := []types.Message{
		{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "hello"}},
		},
	}

	filtered := filterReasoningContent(messages)
	if len(filtered) != 1 || len(filtered[0].Content) != 1 {
		t.Error("messages without reasoning should pass through unchanged")
	}
}
