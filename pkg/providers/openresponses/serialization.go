package openresponses

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func init() {
	provider.RegisterModelDeserializer("open-responses.responses", deserializeModel)
}

// SerializeStrict implements provider.SerializableModelStrict (row 9a68261 /
// P1-5c item 5). An Open Responses model with registered extension codecs
// cannot be serialized across workflow boundaries, since the codecs
// themselves (Go functions) cannot be reconstructed from JSON. Mirrors TS's
// `static [WORKFLOW_SERIALIZE]` throwing a SerializationError when
// `extensionRegistry.byExtensionId.size > 0`.
func (m *LanguageModel) SerializeStrict() (provider.SerializedModel, error) {
	if m.provider.extensionRegistry != nil && len(m.provider.extensionRegistry.ByExtensionID) > 0 {
		return provider.SerializedModel{}, providererrors.NewSerializationError(
			"Open Responses models with registered extensions cannot be serialized across workflow boundaries. Recreate the provider with its extension codecs inside the workflow step.",
			nil,
		)
	}
	return provider.SerializedModel{
		Provider: m.Provider(),
		ModelID:  m.ModelID(),
		Config:   provider.SerializableConfig(m.provider.config),
	}, nil
}

// deserializeModel reconstructs an Open Responses provider and model from a
// serialized config. Registered only under the default provider name
// ("open-responses.responses"): a caller who customizes Config.Name must
// recreate the provider directly (the same limitation TS has for any
// Provider() value that depends on non-default config).
func deserializeModel(serialized provider.SerializedModel) (provider.LanguageModel, error) {
	var cfg Config
	data, _ := json.Marshal(serialized.Config)
	_ = json.Unmarshal(data, &cfg)
	p := New(cfg)
	return p.LanguageModel(serialized.ModelID)
}
