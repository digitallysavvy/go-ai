package xai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func init() {
	provider.RegisterModelDeserializer("googleVertex.xai", deserializeModel)
}

// Serialize implements provider.SerializableModel for workflow boundaries.
// Mirrors TS google-vertex-xai-provider.ts, which builds this model as an
// @ai-sdk/openai-compatible OpenAICompatibleChatLanguageModel (its
// [WORKFLOW_SERIALIZE] serializes {modelId, config}, where config includes
// name/baseURL/includeUsage/supportsStructuredOutputs plus several
// function-valued fields -- fetch, headers, supportedUrls,
// transformRequestBody, convertUsage -- that TS's serializeModelOptions
// drops because functions aren't JSON-serializable).
//
// Go mirrors that gap rather than papering over it: Config.AuthToken and
// Config.TokenSource are excluded from JSON entirely (`json:"-"`, like TS's
// dropped `fetch`), and Config.HTTPClient is stripped by
// provider.SerializableConfig's isSensitiveConfigField the same way
// Config.AccessToken is -- both are treated as credentials, alongside
// APIKey elsewhere in this SDK, and never round-trip through Serialize().
//
// The practical result: deserializing a serialized googleVertex.xai model
// only restores Project/Location/BaseURL/Headers. The caller must re-supply
// auth (AccessToken/AuthToken/TokenSource) after DeserializeModel returns --
// there is no configuration for which this model's auth survives a
// serialize/deserialize round-trip, since Vertex's Google Cloud OAuth token
// is always either a static credential (deliberately stripped, matching
// every other provider's APIKey) or a per-request closure (never
// serializable). This is the "only a static AccessToken is serializable"
// case called out in SER2: even that one case is intentionally excluded
// here for credential hygiene, consistent with the rest of this SDK.
func (m *LanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return p.LanguageModel(serialized.ModelID)
}
