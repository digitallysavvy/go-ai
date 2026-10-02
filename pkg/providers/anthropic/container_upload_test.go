package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDoGenerate_ContainerUploadContentBlock verifies that a "container_upload"
// response content block (a file the model uploaded to the code execution
// container) is surfaced as a CustomContent{Kind: "anthropic.container_upload"}
// part with the file id in ProviderMetadata, mirroring TS
// anthropic-language-model.ts's doGenerate case 'container_upload' (custom-
// content emission, item 8).
func TestDoGenerate_ContainerUploadContentBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_container_upload",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-5",
			"content": [
				{"type": "text", "text": "Here is the file."},
				{"type": "container_upload", "file_id": "file_123"}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModel(ClaudeSonnet4_5)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		MaxTokens: intPtr(100),
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	var custom *types.CustomContent
	for _, c := range result.Content {
		if cc, ok := c.(types.CustomContent); ok {
			custom = &cc
			break
		}
	}
	if custom == nil {
		t.Fatalf("expected a CustomContent part in result.Content, got %+v", result.Content)
	}
	if custom.Kind != "anthropic.container_upload" {
		t.Errorf("Kind = %q, want %q", custom.Kind, "anthropic.container_upload")
	}
	var metadata map[string]map[string]interface{}
	if err := json.Unmarshal(custom.ProviderMetadata, &metadata); err != nil {
		t.Fatalf("failed to unmarshal ProviderMetadata: %v", err)
	}
	anthropicMeta, ok := metadata["anthropic"]
	if !ok {
		t.Fatalf("expected ProviderMetadata namespaced under \"anthropic\", got %+v", metadata)
	}
	if anthropicMeta["fileId"] != "file_123" {
		t.Errorf("ProviderMetadata[\"anthropic\"][\"fileId\"] = %v, want %q", anthropicMeta["fileId"], "file_123")
	}
	if result.Text != "Here is the file." {
		t.Errorf("Text = %q, want %q", result.Text, "Here is the file.")
	}
}

// TestDoStream_FallbackContentBlockBetweenReasoningBlocks ports TS's "should
// preserve a mid-output fallback boundary between reasoning blocks"
// (anthropic-language-model.test.ts, commit a587f554f7, #21736): a
// "fallback" content_block_start/stop pair streamed between two "thinking"
// blocks must survive as an anthropic.fallback custom chunk positioned
// between the two thinking blocks' chunks, so a later turn can tell the
// signed thinking blocks before and after the model hop apart. It also
// covers the signature_delta fix (coordinator follow-up to #21736): each
// thinking block's signature must survive as a reasoning chunk carrying
// ProviderMetadata.anthropic.signature (previously dropped by a bare
// `continue`), not just its text.
func TestDoStream_FallbackContentBlockBetweenReasoningBlocks(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-5-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Opus 5.5 thinking\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"SIG_FROM_OPUS_5_5\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"fallback\",\"from\":{\"model\":\"claude-opus-5-5\"},\"to\":{\"model\":\"claude-opus-4-8\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Opus 4.8 thinking\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"SIG_FROM_OPUS_4_8\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":2}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":3,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"lookup\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":3,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{}\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":3}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":70}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		chunks = append(chunks, chunk)
	}

	firstSignatureIdx, fallbackIdx, secondThinkingTextIdx, secondSignatureIdx := -1, -1, -1, -1
	for i, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning && c.Reasoning == "" && len(c.ProviderMetadata) > 0 && firstSignatureIdx == -1 {
			firstSignatureIdx = i
		}
		if c.Type == provider.ChunkTypeCustom && c.CustomContent != nil && c.CustomContent.Kind == "anthropic.fallback" && fallbackIdx == -1 {
			fallbackIdx = i
		}
		if c.Type == provider.ChunkTypeReasoning && c.Reasoning == "Opus 4.8 thinking" && secondThinkingTextIdx == -1 {
			secondThinkingTextIdx = i
		}
		if c.Type == provider.ChunkTypeReasoning && c.Reasoning == "" && len(c.ProviderMetadata) > 0 && firstSignatureIdx != -1 && i > firstSignatureIdx && secondSignatureIdx == -1 {
			secondSignatureIdx = i
		}
	}

	if firstSignatureIdx == -1 {
		t.Fatalf("expected a reasoning chunk carrying the first thinking block's signature, got %#v", chunks)
	}
	var firstMeta map[string]interface{}
	if err := json.Unmarshal(chunks[firstSignatureIdx].ProviderMetadata, &firstMeta); err != nil {
		t.Fatalf("unmarshal first signature ProviderMetadata: %v", err)
	}
	if anthropicMeta, _ := firstMeta["anthropic"].(map[string]interface{}); anthropicMeta["signature"] != "SIG_FROM_OPUS_5_5" {
		t.Fatalf("first signature metadata = %#v, want SIG_FROM_OPUS_5_5", anthropicMeta)
	}

	if fallbackIdx == -1 || fallbackIdx <= firstSignatureIdx {
		t.Fatalf("expected the fallback chunk after the first thinking block's signature, got index %d (signature at %d)", fallbackIdx, firstSignatureIdx)
	}
	var fallbackMeta map[string]interface{}
	if err := json.Unmarshal(chunks[fallbackIdx].CustomContent.ProviderMetadata, &fallbackMeta); err != nil {
		t.Fatalf("unmarshal fallback ProviderMetadata: %v", err)
	}
	anthropicFallbackMeta, _ := fallbackMeta["anthropic"].(map[string]interface{})
	from, _ := anthropicFallbackMeta["from"].(map[string]interface{})
	to, _ := anthropicFallbackMeta["to"].(map[string]interface{})
	if from["model"] != "claude-opus-5-5" || to["model"] != "claude-opus-4-8" {
		t.Fatalf("fallback metadata = %#v, want from/to models", anthropicFallbackMeta)
	}

	if secondThinkingTextIdx == -1 || secondThinkingTextIdx <= fallbackIdx {
		t.Fatalf("expected the second thinking block's text after the fallback chunk, got index %d (fallback at %d)", secondThinkingTextIdx, fallbackIdx)
	}
	if secondSignatureIdx == -1 || secondSignatureIdx <= secondThinkingTextIdx {
		t.Fatalf("expected the second thinking block's signature after its text, got index %d (text at %d)", secondSignatureIdx, secondThinkingTextIdx)
	}
	var secondMeta map[string]interface{}
	if err := json.Unmarshal(chunks[secondSignatureIdx].ProviderMetadata, &secondMeta); err != nil {
		t.Fatalf("unmarshal second signature ProviderMetadata: %v", err)
	}
	if anthropicMeta, _ := secondMeta["anthropic"].(map[string]interface{}); anthropicMeta["signature"] != "SIG_FROM_OPUS_4_8" {
		t.Fatalf("second signature metadata = %#v, want SIG_FROM_OPUS_4_8", anthropicMeta)
	}
}
