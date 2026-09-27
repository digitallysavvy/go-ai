package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports packages/anthropic/src/anthropic-language-model.test.ts "on-demand
// compaction" describe block (ai@7.0.113). The `compaction` provider option
// landed after the Sep-23 audit rows were written; see
// prds/p1-high/P1-3-Anthropic-Bedrock-Sep23-2026.md "Anthropic retro review".

// TestCompactionOption_RequestBodyAndBeta ports "should send compaction in
// the request body and add the beta header".
func TestCompactionOption_RequestBodyAndBeta(t *testing.T) {
	var gotBody map[string]interface{}
	var gotBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "hi"}],
			"model": "claude-sonnet-4-5", "stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &ModelOptions{
		Compaction: &CompactionOption{
			Type:         "summarize",
			Instructions: "Preserve decisions and unresolved questions.",
		},
	})
	if err != nil {
		t.Fatalf("LanguageModelWithOptions() error: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Test prompt"},
		MaxTokens: intPtr(100),
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	compaction, ok := gotBody["compaction"].(map[string]interface{})
	if !ok {
		t.Fatalf("request body compaction = %#v, want object", gotBody["compaction"])
	}
	if compaction["type"] != "summarize" {
		t.Errorf("compaction.type = %v, want summarize", compaction["type"])
	}
	if compaction["instructions"] != "Preserve decisions and unresolved questions." {
		t.Errorf("compaction.instructions = %v, want the preserve message", compaction["instructions"])
	}
	if !strings.Contains(gotBeta, "compact-2026-09-04") {
		t.Errorf("anthropic-beta = %q, want it to contain compact-2026-09-04", gotBeta)
	}
}

// TestCompactionOption_MutualExclusivityError ports "should reject compaction
// together with context management".
func TestCompactionOption_MutualExclusivityError(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &ModelOptions{
		Compaction: &CompactionOption{Type: "summarize"},
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{NewClearToolUsesEdit()},
		},
	})
	if err != nil {
		t.Fatalf("LanguageModelWithOptions() error: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Test prompt"},
		MaxTokens: intPtr(100),
	})
	if err == nil {
		t.Fatal("DoGenerate() error = nil, want mutual exclusivity error")
	}
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) {
		t.Fatalf("DoGenerate() error = %T, want *InvalidArgumentError", err)
	}
	if invalidArg.Field != "providerOptions" {
		t.Errorf("Field = %q, want providerOptions", invalidArg.Field)
	}
	wantMsg := "Anthropic provider options `compaction` and `contextManagement` cannot be used together."
	if invalidArg.Message != wantMsg {
		t.Errorf("Message = %q, want %q", invalidArg.Message, wantMsg)
	}
	if called {
		t.Error("HTTP request was made; want validation to fail before any call")
	}
}

// TestCompactionResponse_ParseWithIterationsAndContent ports "should parse
// compaction response with iterations and compaction content" (fixture
// anthropic-compaction.1.json, trimmed to the fields the test checks).
func TestCompactionResponse_ParseWithIterationsAndContent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-4-6", nil)

	response := anthropicResponse{
		ID:         "msg_01D55QDk6AZP2o6n9ko7TkDJ",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-opus-4-6",
		StopReason: "end_turn",
		Content: []anthropicContent{
			{Type: "compaction", Content: json.RawMessage(`"## Summary of Conversation\n\nReact vs Vue.js state management."`)},
			{Type: "text", Text: "Based on our conversation history, you had asked me about algorithms."},
		},
		Usage: anthropicUsage{
			InputTokens:  682,
			OutputTokens: 1320,
			Iterations: []UsageIteration{
				{Type: "compaction", InputTokens: 60385, OutputTokens: 592},
				{Type: "message", InputTokens: 682, OutputTokens: 1320},
			},
		},
	}

	result := model.convertResponseWithOptions(response, convertOptions{})

	if len(result.Content) != 2 {
		t.Fatalf("len(result.Content) = %d, want 2", len(result.Content))
	}
	first, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("result.Content[0] = %T, want types.TextContent", result.Content[0])
	}
	if !strings.Contains(first.Text, "## Summary of Conversation") {
		t.Errorf("first text = %q, want it to contain the summary heading", first.Text)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(first.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode providerMetadata: %v", err)
	}
	if meta["anthropic"]["type"] != "compaction" {
		t.Errorf("providerMetadata.anthropic.type = %v, want compaction", meta["anthropic"]["type"])
	}

	second, ok := result.Content[1].(types.TextContent)
	if !ok {
		t.Fatalf("result.Content[1] = %T, want types.TextContent", result.Content[1])
	}
	if !strings.Contains(second.Text, "Based on our conversation history") {
		t.Errorf("second text = %q, want it to contain the answer", second.Text)
	}
	if second.ProviderMetadata != nil {
		t.Errorf("second.ProviderMetadata = %s, want nil (plain text block)", second.ProviderMetadata)
	}

	// result.Text concatenates all text-type blocks in order (matches TS
	// GenerateTextResult.text, which joins content parts of type 'text').
	if !strings.Contains(result.Text, "## Summary of Conversation") || !strings.Contains(result.Text, "Based on our conversation history") {
		t.Errorf("result.Text = %q, want it to contain both blocks", result.Text)
	}

	metaRaw, err := json.Marshal(result.ProviderMetadata)
	if err != nil {
		t.Fatalf("marshal ProviderMetadata: %v", err)
	}
	var pm map[string]map[string]interface{}
	if err := json.Unmarshal(metaRaw, &pm); err != nil {
		t.Fatalf("decode ProviderMetadata: %v", err)
	}
	iterations, ok := pm["anthropic"]["iterations"].([]interface{})
	if !ok || len(iterations) != 2 {
		t.Fatalf("providerMetadata.anthropic.iterations = %#v, want 2 entries", pm["anthropic"]["iterations"])
	}

	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 60385+682 {
		t.Errorf("Usage.InputTokens = %v, want %d", result.Usage.InputTokens, 60385+682)
	}
	if result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 592+1320 {
		t.Errorf("Usage.OutputTokens = %v, want %d", result.Usage.OutputTokens, 592+1320)
	}
}

// TestCompactionResponse_OmitEmptyContent ports "should omit compaction
// response with %s content" (empty string and null cases).
func TestCompactionResponse_OmitEmptyContent(t *testing.T) {
	tests := []struct {
		name    string
		content json.RawMessage
	}{
		{name: "empty", content: json.RawMessage(`""`)},
		{name: "null", content: json.RawMessage(`null`)},
		{name: "absent", content: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, "claude-3-haiku-20240307", nil)

			response := anthropicResponse{
				ID:         "msg_123",
				Type:       "message",
				Role:       "assistant",
				Model:      "claude-3-haiku-20240307",
				StopReason: "end_turn",
				Content: []anthropicContent{
					{Type: "compaction", Content: tt.content},
					{Type: "text", Text: "Hello"},
				},
				Usage: anthropicUsage{InputTokens: 100, OutputTokens: 50},
			}

			result := model.convertResponseWithOptions(response, convertOptions{})

			if result.Text != "Hello" {
				t.Errorf("result.Text = %q, want %q", result.Text, "Hello")
			}
			if len(result.Content) != 0 {
				t.Errorf("result.Content = %#v, want empty (compaction dropped, single plain text block)", result.Content)
			}
		})
	}
}

// TestCompactionResponse_StopReasonOther ports "should return compaction stop
// reason as other".
func TestCompactionResponse_StopReasonOther(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-3-haiku-20240307", nil)

	response := anthropicResponse{
		ID:         "msg_123",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-3-haiku-20240307",
		StopReason: "compaction",
		Content: []anthropicContent{
			{Type: "compaction", Content: json.RawMessage(`"Compaction summary..."`)},
		},
		Usage: anthropicUsage{InputTokens: 100, OutputTokens: 50},
	}

	result := model.convertResponseWithOptions(response, convertOptions{})
	if result.FinishReason != types.FinishReasonOther {
		t.Errorf("FinishReason = %v, want %v", result.FinishReason, types.FinishReasonOther)
	}
}

// TestCompactionResponse_PreserveSignature ports "should preserve the
// signature on compaction responses".
func TestCompactionResponse_PreserveSignature(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, "claude-opus-5", nil)

	response := anthropicResponse{
		ID:         "msg_compaction",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-opus-5",
		StopReason: "compaction",
		Content: []anthropicContent{
			{Type: "compaction", Content: json.RawMessage(`"Summary of the conversation."`), Signature: "compaction-signature"},
		},
		Usage: anthropicUsage{
			InputTokens:  0,
			OutputTokens: 0,
			Iterations: []UsageIteration{
				{Type: "compaction", InputTokens: 120, OutputTokens: 30},
			},
		},
	}

	result := model.convertResponseWithOptions(response, convertOptions{})

	if len(result.Content) != 1 {
		t.Fatalf("len(result.Content) = %d, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("result.Content[0] = %T, want types.TextContent", result.Content[0])
	}
	if text.Text != "Summary of the conversation." {
		t.Errorf("text.Text = %q, want %q", text.Text, "Summary of the conversation.")
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(text.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode providerMetadata: %v", err)
	}
	if meta["anthropic"]["type"] != "compaction" {
		t.Errorf("providerMetadata.anthropic.type = %v, want compaction", meta["anthropic"]["type"])
	}
	if meta["anthropic"]["signature"] != "compaction-signature" {
		t.Errorf("providerMetadata.anthropic.signature = %v, want compaction-signature", meta["anthropic"]["signature"])
	}
	if result.FinishReason != types.FinishReasonOther {
		t.Errorf("FinishReason = %v, want %v", result.FinishReason, types.FinishReasonOther)
	}
}

// TestCompactionReplay_SignedBlocksAddBetaHeader ports "should replay signed
// compaction blocks and add the beta header": an assistant history message
// carrying a text part with providerOptions.anthropic={type:"compaction",
// signature} must be replayed as a "compaction" wire block, and the request
// must carry the compact-2026-09-04 beta even though no Compaction option was
// set on this call.
func TestCompactionReplay_SignedBlocksAddBetaHeader(t *testing.T) {
	var gotBody map[string]interface{}
	var gotBeta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "hi"}],
			"model": "claude-sonnet-4-5", "stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model, err := p.LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.TextContent{
							Text: "Summary of the conversation.",
							ProviderOptions: map[string]interface{}{
								"anthropic": map[string]interface{}{
									"type":      "compaction",
									"signature": "compaction-signature",
								},
							},
						},
					},
				},
				{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: "Continue the conversation."}},
				},
			},
		},
		MaxTokens: intPtr(100),
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}

	messages, ok := gotBody["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want 2 messages", gotBody["messages"])
	}
	assistantMsg, ok := messages[0].(map[string]interface{})
	if !ok {
		t.Fatalf("messages[0] = %#v, want object", messages[0])
	}
	assistantContent, ok := assistantMsg["content"].([]interface{})
	if !ok || len(assistantContent) != 1 {
		t.Fatalf("messages[0].content = %#v, want 1 block", assistantMsg["content"])
	}
	block, ok := assistantContent[0].(map[string]interface{})
	if !ok {
		t.Fatalf("content block = %#v, want object", assistantContent[0])
	}
	if block["type"] != "compaction" {
		t.Errorf("block.type = %v, want compaction", block["type"])
	}
	if block["content"] != "Summary of the conversation." {
		t.Errorf("block.content = %v, want the summary text", block["content"])
	}
	if block["signature"] != "compaction-signature" {
		t.Errorf("block.signature = %v, want compaction-signature", block["signature"])
	}
	if !strings.Contains(gotBeta, "compact-2026-09-04") {
		t.Errorf("anthropic-beta = %q, want it to contain compact-2026-09-04", gotBeta)
	}
}

// TestStream_CompleteSignedOnDemandCompaction ports "should stream complete
// signed on-demand compaction blocks": a compaction block that arrives fully
// formed in content_block_start (no compaction_delta events follow) because
// both signature and content are present.
func TestStream_CompleteSignedOnDemandCompaction(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_compaction\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"compaction\",\"content\":\"Summary of the conversation.\",\"signature\":\"compaction-signature\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"compaction\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":0,\"iterations\":[{\"type\":\"compaction\",\"input_tokens\":120,\"output_tokens\":30}]}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	textChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() text chunk error: %v", err)
	}
	if textChunk.Type != provider.ChunkTypeText {
		t.Fatalf("chunk type = %v, want text", textChunk.Type)
	}
	if textChunk.Text != "Summary of the conversation." {
		t.Errorf("text = %q, want %q", textChunk.Text, "Summary of the conversation.")
	}

	finishChunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() finish chunk error: %v", err)
	}
	if finishChunk.Type != provider.ChunkTypeFinish {
		t.Fatalf("chunk type = %v, want finish", finishChunk.Type)
	}
	if finishChunk.FinishReason != types.FinishReasonOther {
		t.Errorf("FinishReason = %v, want %v", finishChunk.FinishReason, types.FinishReasonOther)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finishChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode provider metadata: %v", err)
	}
	iterations, ok := meta["anthropic"]["iterations"].([]interface{})
	if !ok || len(iterations) != 1 {
		t.Fatalf("iterations = %#v, want 1 entry", meta["anthropic"]["iterations"])
	}
}

// TestStream_CompactionDeltaAfterStart ports "should stream compaction
// content blocks with provider metadata" (adapted for this SDK's flat text
// chunk model, which has no text-start/text-end boundary chunks): a
// compaction block opened via content_block_start with no inline content
// streams its text purely through compaction_delta events, followed by a
// plain text block.
func TestStream_CompactionDeltaAfterStart(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-4-6\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"compaction\",\"content\":null}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":\"## Summary of Conversation\\n\\n\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"compaction_delta\",\"content\":\"React vs Vue.js state management.\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Here is the answer.\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":40}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	var texts []string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeText {
			texts = append(texts, chunk.Text)
		}
	}

	all := strings.Join(texts, "")
	if !strings.Contains(all, "## Summary of Conversation") {
		t.Errorf("streamed text = %q, want it to contain the compaction summary", all)
	}
	if !strings.Contains(all, "React vs Vue.js") {
		t.Errorf("streamed text = %q, want it to contain the compaction body", all)
	}
	if !strings.Contains(all, "Here is the answer.") {
		t.Errorf("streamed text = %q, want it to contain the regular text block", all)
	}
}
