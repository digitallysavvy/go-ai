package anthropicaws

import (
	"encoding/json"

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
// BaseURL/Headers (which already carry a baked-in "x-api-key" header if
// API-key auth was used). A workflow step that needs freshly-signed SigV4
// requests after resuming should build a new anthropicaws.Provider instead
// of relying on the deserialized model directly.
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg anthropic.Config
	data, err := json.Marshal(serialized.Config)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return anthropic.New(cfg).LanguageModel(serialized.ModelID)
}
