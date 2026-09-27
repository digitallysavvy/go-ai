package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports packages/anthropic/src/anthropic-language-model.test.ts "custom
// provider name support" > "doGenerate" / "doStream" describe blocks
// (ai@7.0.113): when a custom providerOptionsName (a provider created with a
// non-"anthropic" `name`) is used AND the caller supplies providerOptions
// under that custom key (not just the canonical "anthropic" key),
// providerMetadata is duplicated under both "anthropic" and the custom key.
// See anthropic-language-model.ts lines ~1779 (doGenerate) and ~3029
// (doStream message_stop).

const customProviderMetadataName = "my-custom-anthropic"

func customProviderMetadataServerJSON() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_custom_test", "type": "message", "role": "assistant",
			"content": [{"type": "text", "text": "Hello, World!"}],
			"model": "claude-3-haiku-20240307", "stop_reason": "end_turn", "stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 20}
		}`))
	}
}

func customProviderMetadataServerStream() http.HandlerFunc {
	events := []struct {
		event string
		data  string
	}{
		{"message_start", `{"type":"message_start","message":{"id":"msg_custom_stream","type":"message","role":"assistant","content":[],"model":"claude-3-haiku-20240307","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" World!"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":20}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			_, _ = io.WriteString(w, "event: "+e.event+"\ndata: "+e.data+"\n\n")
		}
	}
}

func newCustomProviderMetadataModel(t *testing.T, baseURL string) *LanguageModel {
	t.Helper()
	p := New(Config{APIKey: "test-api-key", Name: customProviderMetadataName, BaseURL: baseURL})
	return NewLanguageModel(p, "claude-3-haiku-20240307", nil)
}

func decodeStreamFinishMetadata(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal finish providerMetadata: %v", err)
	}
	return out
}

func drainForFinish(t *testing.T, s provider.TextStream) *provider.StreamChunk {
	t.Helper()
	for {
		chunk, err := s.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			return chunk
		}
	}
}

// TestCustomProviderKey_DoGenerate_CanonicalKeyOnly ports "should only
// include 'anthropic' key in providerMetadata when providerOptions uses
// 'anthropic' key".
func TestCustomProviderKey_DoGenerate_CanonicalKeyOnly(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerJSON())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := result.ProviderMetadata["anthropic"]; !ok {
		t.Fatalf("providerMetadata missing 'anthropic' key: %#v", result.ProviderMetadata)
	}
	if len(result.ProviderMetadata) != 1 {
		t.Fatalf("providerMetadata = %#v, want only 'anthropic' key", result.ProviderMetadata)
	}
}

// TestCustomProviderKey_DoGenerate_CustomKeyDuplicated ports "should include
// both 'anthropic' and custom key in providerMetadata when providerOptions
// uses custom key".
func TestCustomProviderKey_DoGenerate_CustomKeyDuplicated(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerJSON())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			customProviderMetadataName: map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	anthropicMeta, ok := result.ProviderMetadata["anthropic"]
	if !ok {
		t.Fatalf("providerMetadata missing 'anthropic' key: %#v", result.ProviderMetadata)
	}
	customMeta, ok := result.ProviderMetadata[customProviderMetadataName]
	if !ok {
		t.Fatalf("providerMetadata missing custom key %q: %#v", customProviderMetadataName, result.ProviderMetadata)
	}
	customJSON, _ := json.Marshal(customMeta)
	anthropicJSON, _ := json.Marshal(anthropicMeta)
	if string(customJSON) != string(anthropicJSON) {
		t.Fatalf("custom key metadata = %s, want identical to anthropic key metadata %s", customJSON, anthropicJSON)
	}
}

// TestCustomProviderKey_DoGenerate_NoProviderOptions ports "should only
// include 'anthropic' key in providerMetadata when no providerOptions used".
func TestCustomProviderKey_DoGenerate_NoProviderOptions(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerJSON())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if _, ok := result.ProviderMetadata["anthropic"]; !ok {
		t.Fatalf("providerMetadata missing 'anthropic' key: %#v", result.ProviderMetadata)
	}
	if len(result.ProviderMetadata) != 1 {
		t.Fatalf("providerMetadata = %#v, want only 'anthropic' key", result.ProviderMetadata)
	}
}

// TestCustomProviderKey_DoStream_CanonicalKeyOnly ports the doStream
// equivalent of TestCustomProviderKey_DoGenerate_CanonicalKeyOnly: the finish
// chunk's providerMetadata carries only "anthropic" when providerOptions
// uses the canonical key.
func TestCustomProviderKey_DoStream_CanonicalKeyOnly(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerStream())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	finish := drainForFinish(t, stream)
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	meta := decodeStreamFinishMetadata(t, finish.ProviderMetadata)
	if _, ok := meta["anthropic"]; !ok {
		t.Fatalf("finish providerMetadata missing 'anthropic' key: %#v", meta)
	}
	if len(meta) != 1 {
		t.Fatalf("finish providerMetadata = %#v, want only 'anthropic' key", meta)
	}
}

// TestCustomProviderKey_DoStream_CustomKeyDuplicated ports the doStream
// equivalent of TestCustomProviderKey_DoGenerate_CustomKeyDuplicated.
func TestCustomProviderKey_DoStream_CustomKeyDuplicated(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerStream())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			customProviderMetadataName: map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	finish := drainForFinish(t, stream)
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	meta := decodeStreamFinishMetadata(t, finish.ProviderMetadata)
	if _, ok := meta["anthropic"]; !ok {
		t.Fatalf("finish providerMetadata missing 'anthropic' key: %#v", meta)
	}
	if _, ok := meta[customProviderMetadataName]; !ok {
		t.Fatalf("finish providerMetadata missing custom key %q: %#v", customProviderMetadataName, meta)
	}
}

// TestCustomProviderKey_DoStream_NoProviderOptions ports the doStream
// equivalent of TestCustomProviderKey_DoGenerate_NoProviderOptions.
func TestCustomProviderKey_DoStream_NoProviderOptions(t *testing.T) {
	server := httptest.NewServer(customProviderMetadataServerStream())
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	finish := drainForFinish(t, stream)
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	meta := decodeStreamFinishMetadata(t, finish.ProviderMetadata)
	if _, ok := meta["anthropic"]; !ok {
		t.Fatalf("finish providerMetadata missing 'anthropic' key: %#v", meta)
	}
	if len(meta) != 1 {
		t.Fatalf("finish providerMetadata = %#v, want only 'anthropic' key", meta)
	}
}
