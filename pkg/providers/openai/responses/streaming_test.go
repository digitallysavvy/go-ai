package responses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestCompactionEventToChunk_EmitsCustomContent verifies that a compaction event
// from the Responses API streaming parser is converted to a CustomContent chunk
// with Kind "openai-compaction" and the correct metadata fields.
func TestCompactionEventToChunk_EmitsCustomContent(t *testing.T) {
	event := CompactionEvent{
		Type:             "compaction",
		ItemID:           "item_abc123",
		EncryptedContent: "enc_xyz_opaque_blob",
	}

	chunk := CompactionEventToChunk(event)

	if chunk.Type != provider.ChunkTypeCustom {
		t.Errorf("chunk.Type = %q, want %q", chunk.Type, provider.ChunkTypeCustom)
	}

	if chunk.CustomContent == nil {
		t.Fatal("chunk.CustomContent is nil")
	}
	if chunk.CustomContent.Kind != "openai-compaction" {
		t.Errorf("Kind = %q, want %q", chunk.CustomContent.Kind, "openai-compaction")
	}
	if chunk.CustomContent.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata is nil")
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(chunk.CustomContent.ProviderMetadata, &meta); err != nil {
		t.Fatalf("unmarshal ProviderMetadata: %v", err)
	}

	if meta["type"] != "compaction" {
		t.Errorf("meta.type = %v, want %q", meta["type"], "compaction")
	}
	if meta["itemId"] != "item_abc123" {
		t.Errorf("meta.itemId = %v, want %q", meta["itemId"], "item_abc123")
	}
	if meta["encryptedContent"] != "enc_xyz_opaque_blob" {
		t.Errorf("meta.encryptedContent = %v, want %q", meta["encryptedContent"], "enc_xyz_opaque_blob")
	}
}

// TestCompactionEventToChunk_EmptyFields verifies that missing optional fields
// are represented as empty strings in ProviderMetadata (not omitted entirely).
func TestCompactionEventToChunk_EmptyFields(t *testing.T) {
	event := CompactionEvent{
		Type: "compaction",
	}

	chunk := CompactionEventToChunk(event)
	if chunk.CustomContent == nil {
		t.Fatal("chunk.CustomContent is nil")
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(chunk.CustomContent.ProviderMetadata, &meta); err != nil {
		t.Fatalf("unmarshal ProviderMetadata: %v", err)
	}

	if meta["type"] != "compaction" {
		t.Errorf("meta.type = %v, want %q", meta["type"], "compaction")
	}
	// itemId and encryptedContent present as empty strings
	if meta["itemId"] != "" {
		t.Errorf("meta.itemId = %v, want empty string", meta["itemId"])
	}
	if meta["encryptedContent"] != "" {
		t.Errorf("meta.encryptedContent = %v, want empty string", meta["encryptedContent"])
	}
}
