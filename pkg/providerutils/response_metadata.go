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
// created == 0 is treated as "absent", leaving Timestamp at its zero value.
// TS checks `created != null`, so a present 0 is the epoch there: an int64
// cannot tell the two apart (nor hold a fractional TS z.number() value), so
// callers whose wire value may be 0 or fractional pass 0 here and set
// Timestamp themselves from the decoded `*float64` or raw body as `new
// Date(created * 1000)` (groq, mistral, deepseek, alibaba, moonshot, the
// OpenAI/xAI Responses models and openresponses do). Headers is set by the
// caller (it isn't part of the shared TS helper's input).
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
