package responses

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CompactionEventToChunk converts a Responses API compaction event into a
// provider.StreamChunk with Kind "openai-compaction".
//
// The ProviderMetadata JSON carries three fields:
//   - "type"             — the raw event type string ("compaction")
//   - "itemId"           — the item ID from the compaction event
//   - "encryptedContent" — the opaque encrypted context blob
//
// Callers must forward the encryptedContent verbatim in subsequent
// Responses API requests to maintain conversation continuity.
func CompactionEventToChunk(event CompactionEvent) *provider.StreamChunk {
	metadata, _ := json.Marshal(map[string]interface{}{
		"type":             event.Type,
		"itemId":           event.ItemID,
		"encryptedContent": event.EncryptedContent,
	})
	return &provider.StreamChunk{
		Type: provider.ChunkTypeCustom,
		CustomContent: &types.CustomContent{
			Kind:             "openai-compaction",
			ProviderMetadata: metadata,
		},
	}
}
