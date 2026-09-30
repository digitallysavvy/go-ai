package providerutils

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BuildResponseMetadata converts common provider response fields (id, model,
// created-at unix seconds) into language model response metadata. Matches TS
// createLanguageModelResponseMetadata (provider-utils/src/create-language-model-response-metadata.ts),
// which deepseek/groq/mistral (93b2acd) all re-export as their own
// get-response-metadata module.
//
// created == 0 is treated as "absent" (TS: created != null), leaving
// Timestamp at its zero value. Headers is set by the caller (it isn't part
// of the shared TS helper's input).
func BuildResponseMetadata(id, modelID string, created int64) *types.ResponseMetadata {
	meta := &types.ResponseMetadata{
		ID:      id,
		ModelID: modelID,
	}
	if created != 0 {
		meta.Timestamp = time.Unix(created, 0)
	}
	return meta
}
