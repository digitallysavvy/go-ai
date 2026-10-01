package anthropicaws

import (
	"encoding/json"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func init() {
	provider.RegisterModelDeserializer("anthropic-aws.messages", deserializeModel)
}

// deserializeModel restores a serialized AnthropicAWS model.
//
// Provider.LanguageModel returns the underlying *anthropic.LanguageModel
// directly (this package has no dedicated model type), so Serialize()
// already comes from anthropic.LanguageModel.Serialize() and already tags
// the result with the correct provider name -- "anthropic-aws.messages",
// since anthropic.LanguageModel.Provider() returns m.provider.Name() and
// anthropicaws.New sets anthropic.Config.Name to "anthropic-aws.messages".
// The only missing piece is a deserializer registered under that name.
//
// Like TS's createAnthropicAws (anthropic-aws-provider.ts), the SigV4/API-key
// signing fetch function that authenticates requests is a Go function value
// and cannot cross a workflow boundary -- TS's own WORKFLOW_SERIALIZE for
// AnthropicMessagesLanguageModel drops it from `config` the same way Go's
// SerializableConfig excludes HTTPClient, so this is parity behavior, not a
// regression. This reconstructs a plain Anthropic model from the surviving
// BaseURL/Headers. Note that Headers no longer carries a baked-in
// "x-api-key" value even when API-key auth was used: SerializableConfig
// redacts credential-bearing header names (R1-2, pkg/provider/serialization.go's
// isSensitiveHeaderName) the same way it already redacted the APIKey struct
// field, since both equally cross a durability boundary. A workflow step
// resuming API-key auth (or needing freshly-signed SigV4 requests) should
// build a new anthropicaws.Provider with a fresh credential instead of
// relying on the deserialized model directly. As a best effort, API-key auth
// specifically recovers automatically the same way anthropicaws.New() itself
// resolves a missing Config.APIKey: from the ANTHROPIC_AWS_API_KEY
// environment variable. Without this, a model that originally authenticated
// via API-key mode (its credential living ONLY in the redacted
// Headers["x-api-key"], never in a struct field) would silently deserialize
// into an unauthenticated model that only fails once a real request hits the
// Anthropic API, instead of recovering the same way every other
// Headers-or-APIKey provider's deserializeModel already does via its own
// New().
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg anthropic.Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.APIKey == "" && cfg.Headers["x-api-key"] == "" {
		cfg.APIKey = os.Getenv("ANTHROPIC_AWS_API_KEY")
	}
	return anthropic.New(cfg).LanguageModel(serialized.ModelID)
}
