package responses

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CompactionEventToChunk converts a Responses API compaction event into a
// provider.StreamChunk with Kind "openai.compaction" (audit row 1f509d4 /
// G7: TS custom content kinds are namespaced "{provider}.{type}", not
// "{provider}-{type}"). ConvertPromptToInput still accepts the legacy
// "openai-compaction" spelling on input for backward compatibility.
//
// The ProviderMetadata JSON carries three fields:
//   - "type"             — the raw event type string ("compaction")
//   - "itemId"           — the item ID from the compaction event
//   - "encryptedContent" — the opaque encrypted context blob
//
// Callers must forward the encryptedContent verbatim in subsequent
// Responses API requests to maintain conversation continuity.
func CompactionEventToChunk(event CompactionEvent) *provider.StreamChunk {
	return CompactionEventToChunkForProvider(event, "openai")
}

// CompactionEventToChunkForProvider is CompactionEventToChunk for a specific
// provider-options name (e.g. "azure"). The metadata is namespaced under that
// name like the TS custom part's providerMetadata[providerOptionsName]; the
// flat v0.5.2 keys are kept alongside for existing callers.
func CompactionEventToChunkForProvider(event CompactionEvent, providerName string) *provider.StreamChunk {
	if event.ItemID == "" {
		event.ItemID = event.ID
	}
	fields := map[string]interface{}{
		"type":             event.Type,
		"itemId":           event.ItemID,
		"encryptedContent": event.EncryptedContent,
	}
	metadata, _ := json.Marshal(map[string]interface{}{
		"type":             event.Type,
		"itemId":           event.ItemID,
		"encryptedContent": event.EncryptedContent,
		providerName:       fields,
	})
	return &provider.StreamChunk{
		Type: provider.ChunkTypeCustom,
		CustomContent: &types.CustomContent{
			Kind:             "openai.compaction",
			ProviderMetadata: metadata,
		},
	}
}
