package provider

import (
	"encoding/json"
	"fmt"
	"sync"
)

// SerializedModel is the JSON-friendly representation of a provider model used
// across workflow boundaries.
type SerializedModel struct {
	Provider string                 `json:"provider"`
	ModelID  string                 `json:"modelId"`
	Config   map[string]interface{} `json:"config,omitempty"`
}

// SerializableModel is implemented by models that can be restored across
// workflow boundaries.
type SerializableModel interface {
	Serialize() SerializedModel
}

// ModelDeserializer reconstructs a language model from its serialized form.
type ModelDeserializer func(SerializedModel) (LanguageModel, error)

var (
	modelDeserializersMu sync.RWMutex
	modelDeserializers   = map[string]ModelDeserializer{}
)

// RegisterModelDeserializer registers a model factory for DeserializeModel.
func RegisterModelDeserializer(providerName string, fn ModelDeserializer) {
	modelDeserializersMu.Lock()
	defer modelDeserializersMu.Unlock()
	modelDeserializers[providerName] = fn
}

// DeserializeModel reconstructs a serialized language model using a registered
// provider factory.
func DeserializeModel(serialized SerializedModel) (LanguageModel, error) {
	modelDeserializersMu.RLock()
	fn := modelDeserializers[serialized.Provider]
	modelDeserializersMu.RUnlock()
	if fn == nil {
		return nil, fmt.Errorf("provider: no model deserializer registered for %q", serialized.Provider)
	}
	return fn(serialized)
}

// SerializableConfig returns a JSON-compatible copy of config with auth-bearing
// fields removed. Workflow deserialization supplies auth separately.
func SerializableConfig(config interface{}) map[string]interface{} {
	if config == nil {
		return nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	for _, key := range []string{
		"APIKey", "apiKey", "AccessToken", "accessToken", "AWSAccessKeyID", "awsAccessKeyID",
		"AWSSecretAccessKey", "awsSecretAccessKey", "SessionToken", "sessionToken",
		"Headers", "headers", "HTTPClient", "httpClient",
	} {
		delete(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
