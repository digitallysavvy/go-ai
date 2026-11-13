package responses

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// CompactionEventToChunk converts a Responses API compaction event into a
// provider.StreamChunk with Kind "openai.compaction" (audit row 1f509d4 /
// G7: TS custom content kinds are namespaced "{provider}.{type}", not
// "{provider}-{type}"). ConvertPromptToInput still accepts the legacy
// "openai-compaction" spelling on input for backward compatibility.
//
// The ProviderMetadata JSON is {"openai": {type, itemId, encryptedContent}}
// like the TS custom part's providerMetadata:
//   - "type"             — always "compaction"
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
// name like the TS custom part's providerMetadata[providerOptionsName]
// (openai-responses-language-model.ts); v0.6 no longer writes the flat v0.5.2
// keys beside it.
func CompactionEventToChunkForProvider(event CompactionEvent, providerName string) *provider.StreamChunk {
	if event.ItemID == "" {
		event.ItemID = event.ID
	}
	metadata, _ := jsonstr.Marshal(map[string]interface{}{
		providerName: map[string]interface{}{
			"type":             "compaction",
			"itemId":           event.ItemID,
			"encryptedContent": event.EncryptedContent,
		},
	})
	return &provider.StreamChunk{
		Type: provider.ChunkTypeCustom,
		CustomContent: &types.CustomContent{
			Kind:             "openai.compaction",
			ProviderMetadata: metadata,
		},
	}
}
