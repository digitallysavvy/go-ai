package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestSystemPromptAndInPromptSystemMessageBothSent is the proof for review
// finding F6 (state/parity/sep_23_2026/review-p0-1-p0-2-round1.md): the
// previous standalone Bedrock-Anthropic implementation dropped any in-prompt
// system message whenever Prompt.System was also set — its buildRequestBody
// only consulted the converter's System block when opts.Prompt.System was
// empty, then unconditionally overwrote body["system"] with the raw
// Prompt.System string otherwise (pkg/providers/bedrock/anthropic/language_model.go,
// pre-WG-B2). Now that Bedrock-Anthropic wraps anthropic.LanguageModel (the
// same converter googlevertex/anthropic uses), both the leading system prompt
// and a mid-conversation system-role message must survive, exactly as they
// would through the direct Anthropic provider.
func TestSystemPromptAndInPromptSystemMessageBothSent(t *testing.T) {
	var reqBody map[string]interface{}
	var betaHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		betaHeader = r.Header.Get("anthropic-beta")
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	// Prompt.System (the leading/top-level system prompt) AND a mid-conversation
	// system-role message in the same call — the exact combination the review
	// finding says was broken.
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "You are a helpful assistant.",
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi there!"}}},
				{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Remember: be concise."}}},
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What's 2+2?"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Text != "ok" {
		t.Fatalf("Text = %q", result.Text)
	}

	// The leading system prompt must appear in the top-level "system" field.
	systemBlocks, ok := reqBody["system"].([]interface{})
	if !ok || len(systemBlocks) == 0 {
		t.Fatalf("system = %#v, want the leading system prompt to be present", reqBody["system"])
	}
	firstSystem, ok := systemBlocks[0].(map[string]interface{})
	if !ok || firstSystem["text"] != "You are a helpful assistant." {
		t.Fatalf("system[0] = %#v, want the leading system prompt text", systemBlocks[0])
	}

	// The mid-conversation system message must survive as its own message
	// with role "system", NOT be dropped (the F6 bug) and NOT be merged into
	// the top-level system field.
	messages, ok := reqBody["messages"].([]interface{})
	if !ok {
		t.Fatalf("messages = %#v, want an array", reqBody["messages"])
	}
	foundMidConversationSystem := false
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok || msg["role"] != "system" {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok || len(content) == 0 {
			continue
		}
		block, ok := content[0].(map[string]interface{})
		if ok && block["text"] == "Remember: be concise." {
			foundMidConversationSystem = true
		}
	}
	if !foundMidConversationSystem {
		t.Fatalf("mid-conversation system message was dropped; messages = %#v", messages)
	}

	// Converter betas must propagate: a mid-conversation system message
	// requires the mid-conversation-system beta from the shared Anthropic
	// prompt converter (prompt.AnthropicBetaMidConversationSystem). The
	// standalone implementation never read betas from the converter at all.
	if !strings.Contains(betaHeader, "mid-conversation-system-2026-04-07") {
		t.Fatalf("anthropic-beta = %q, want it to contain the mid-conversation-system beta", betaHeader)
	}
}

// TestConverterWarningsPropagate proves that warnings produced by the shared
// Anthropic prompt converter (not just its betas) reach the caller through
// Bedrock-Anthropic's GenerateResult, using a case the converter is known to
// warn on: a JSON response format with no schema.
func TestConverterWarningsPropagate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, BearerToken: "token", HTTPClient: srv.Client()})
	model, err := p.LanguageModel("test-model-id")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"}, // no Schema: triggers a warning
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected a warning for a schema-less JSON response format to propagate")
	}
}
