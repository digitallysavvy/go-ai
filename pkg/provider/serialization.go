package provider

import (
	"fmt"
	"reflect"
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

// SerializeModel returns a JSON-friendly serialized model representation.
func SerializeModel(model LanguageModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, fmt.Errorf("provider: model is nil")
	}
	serializable, ok := model.(SerializableModel)
	if !ok {
		return SerializedModel{}, fmt.Errorf("provider: model %q from provider %q is not serializable", model.ModelID(), model.Provider())
	}
	serialized := serializable.Serialize()
	if serialized.Config == nil {
		serialized.Config = map[string]interface{}{}
	}
	return serialized, nil
}

// SerializableConfig returns a JSON-compatible copy of config with auth-bearing
// fields removed. Workflow deserialization supplies auth separately.
func SerializableConfig(config interface{}) map[string]interface{} {
	sanitized, ok := sanitizeSerializableValue(reflect.ValueOf(config), "", true)
	if !ok || sanitized == nil {
		return nil
	}
	out, ok := sanitized.(map[string]interface{})
	if !ok {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeSerializableValue(value reflect.Value, fieldName string, topLevel bool) (interface{}, bool) {
	if !value.IsValid() {
		return nil, false
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if isSensitiveConfigField(fieldName) {
		return nil, false
	}
	switch value.Kind() {
	case reflect.Bool:
		return value.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), true
	case reflect.Float32, reflect.Float64:
		return value.Float(), true
	case reflect.String:
		return value.String(), true
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value.Bytes(), true
		}
		items := make([]interface{}, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			item, ok := sanitizeSerializableValue(value.Index(i), "", false)
			if !ok {
				return nil, false
			}
			items = append(items, item)
		}
		return items, true
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return nil, false
		}
		out := make(map[string]interface{}, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if isSensitiveConfigField(key) {
				continue
			}
			item, ok := sanitizeSerializableValue(iter.Value(), key, false)
			if !ok {
				if topLevel {
					continue
				}
				return nil, false
			}
			out[key] = item
		}
		return out, true
	case reflect.Struct:
		if !topLevel {
			return nil, false
		}
		out := make(map[string]interface{}, value.NumField())
		valueType := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := valueType.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := jsonFieldName(field)
			if name == "-" || isSensitiveConfigField(name) || isSensitiveConfigField(field.Name) {
				continue
			}
			item, ok := sanitizeSerializableValue(value.Field(i), name, false)
			if ok {
				out[name] = item
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func jsonFieldName(field reflect.StructField) string {
	if tag := field.Tag.Get("json"); tag != "" {
		for i, r := range tag {
			if r == ',' {
				if i == 0 {
					return field.Name
				}
				return tag[:i]
			}
		}
		return tag
	}
	return field.Name
}

func isSensitiveConfigField(key string) bool {
	switch key {
	case "APIKey", "apiKey", "AccessToken", "accessToken", "AWSAccessKeyID", "awsAccessKeyID",
		"AWSSecretAccessKey", "awsSecretAccessKey", "SessionToken", "sessionToken",
		"Headers", "headers", "HTTPClient", "httpClient":
		return true
	default:
		return false
	}
}
