// Package provider's serialization helpers (this file) are the Go
// equivalent of TS's [WORKFLOW_SERIALIZE]/[WORKFLOW_DESERIALIZE] symbols on
// each LanguageModelV3/ImageModelV4/VideoModelV3/SpeechModelV4/
// TranscriptionModelV4/EmbeddingModelV4/Experimental_EvaluationModelV4
// implementation: every serializable model in this SDK exposes a
// Serialize() (or SerializeStrict()) method, and every provider package
// registers a matching deserializer factory in its own init() via
// RegisterModelDeserializer / RegisterImageModelDeserializer / etc.
//
// Unlike TS, where @workflow/serde wires these symbols into
// packages/workflow's durable step/state persistence automatically (any
// model value that crosses a step boundary is transparently
// serialized/deserialized by the workflow runtime), Go's pkg/workflow and
// pkg/ai packages do not call SerializeModel/DeserializeModel (or their
// typed counterparts) on your behalf anywhere in this SDK. There is no
// equivalent "serde" layer watching for model values. An application that
// wants to persist a model across a workflow step, queue message, or any
// other durability boundary must call provider.SerializeModel(model) (or
// SerializeImageModel/SerializeVideoModel/SerializeSpeechModel/
// SerializeSpeechTranslationModel/SerializeTranscriptionModel/
// SerializeEmbeddingModel/SerializeEvaluationModel, matching the model's
// kind) itself before persisting the result, and the corresponding
// DeserializeModel/Deserialize<Kind>Model function itself after loading it
// back -- both are ordinary, explicitly-called functions, not framework
// plumbing.

package provider

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// SerializableModelStrict is implemented by models whose serialization can
// fail -- e.g. an Open Responses model with registered extension codecs
// cannot be serialized across workflow boundaries, since the codecs
// themselves (functions) cannot be reconstructed from JSON. This mirrors
// the TS SDK's `static [WORKFLOW_SERIALIZE]` throwing a SerializationError.
// SerializeModel prefers this interface over SerializableModel when a model
// implements both.
type SerializableModelStrict interface {
	SerializeStrict() (SerializedModel, error)
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

// RegisteredModelDeserializerKeys returns every provider name currently
// registered with RegisterModelDeserializer. Exposed so tests (and callers
// building an introspection UI) can enumerate what DeserializeModel accepts
// without reaching into package-private state; used by
// pkg/providers/serialization_registry_test.go's permanent
// registry-wide round-trip check.
func RegisteredModelDeserializerKeys() []string {
	modelDeserializersMu.RLock()
	defer modelDeserializersMu.RUnlock()
	keys := make([]string, 0, len(modelDeserializers))
	for k := range modelDeserializers {
		keys = append(keys, k)
	}
	return keys
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
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	if strict, ok := model.(SerializableModelStrict); ok {
		serialized, err := strict.SerializeStrict()
		if err != nil {
			return SerializedModel{}, err
		}
		if serialized.Config == nil {
			serialized.Config = map[string]interface{}{}
		}
		return serialized, nil
	}
	serializable, ok := model.(SerializableModel)
	if !ok {
		return SerializedModel{}, providererrors.NewSerializationError(
			fmt.Sprintf("provider: model %q from provider %q is not serializable", model.ModelID(), model.Provider()),
			nil,
		)
	}
	serialized := serializable.Serialize()
	if serialized.Config == nil {
		serialized.Config = map[string]interface{}{}
	}
	return serialized, nil
}

// -----------------------------------------------------------------------
// Non-language model kinds (image, video, speech, transcription,
// embedding, evaluation).
//
// Language models use the LanguageModel-specific SerializeModel /
// DeserializeModel / RegisterModelDeserializer above. Every other model
// kind shares the generic plumbing below, since the SerializableModel /
// SerializableModelStrict contract (a bare "Serialize() SerializedModel"
// method) has no model-kind-specific shape -- it only needs Provider()
// and ModelID() to produce a useful error. Mirrors the TypeScript SDK,
// where ImageModelV4, VideoModelV3/V4, SpeechModelV4, TranscriptionModelV4,
// EmbeddingModelV4, and Experimental_EvaluationModelV4 implementations all
// carry the same [WORKFLOW_SERIALIZE]/[WORKFLOW_DESERIALIZE] contract as
// LanguageModelV3.
type typedModelDeserializer[M any] func(SerializedModel) (M, error)

type typedModelRegistry[M any] struct {
	mu  sync.RWMutex
	fns map[string]typedModelDeserializer[M]
}

func newTypedModelRegistry[M any]() *typedModelRegistry[M] {
	return &typedModelRegistry[M]{fns: map[string]typedModelDeserializer[M]{}}
}

func (r *typedModelRegistry[M]) register(providerName string, fn typedModelDeserializer[M]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fns[providerName] = fn
}

func (r *typedModelRegistry[M]) deserialize(kind string, serialized SerializedModel) (M, error) {
	r.mu.RLock()
	fn := r.fns[serialized.Provider]
	r.mu.RUnlock()
	var zero M
	if fn == nil {
		return zero, fmt.Errorf("provider: no %s model deserializer registered for %q", kind, serialized.Provider)
	}
	return fn(serialized)
}

// keys returns every provider name currently registered on this registry.
// See RegisteredModelDeserializerKeys's doc comment for why this exists.
func (r *typedModelRegistry[M]) keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.fns))
	for k := range r.fns {
		keys = append(keys, k)
	}
	return keys
}

// serializableModelMetadata is the minimal shape serializeTypedModel needs
// to produce a useful error when a model doesn't implement
// SerializableModel/SerializableModelStrict. Every model kind interface
// (ImageModel, VideoModelV3, SpeechModel, TranscriptionModel,
// EmbeddingModel, EvaluationModel) already satisfies this structurally.
type serializableModelMetadata interface {
	Provider() string
	ModelID() string
}

// serializeTypedModel implements the shared Serialize() dispatch (prefer
// SerializableModelStrict, fall back to SerializableModel) for any model
// kind.
func serializeTypedModel(model serializableModelMetadata) (SerializedModel, error) {
	if strict, ok := model.(SerializableModelStrict); ok {
		serialized, err := strict.SerializeStrict()
		if err != nil {
			return SerializedModel{}, err
		}
		if serialized.Config == nil {
			serialized.Config = map[string]interface{}{}
		}
		return serialized, nil
	}
	if serializable, ok := model.(SerializableModel); ok {
		serialized := serializable.Serialize()
		if serialized.Config == nil {
			serialized.Config = map[string]interface{}{}
		}
		return serialized, nil
	}
	return SerializedModel{}, providererrors.NewSerializationError(
		fmt.Sprintf("provider: model %q from provider %q is not serializable", model.ModelID(), model.Provider()),
		nil,
	)
}

var imageModelDeserializers = newTypedModelRegistry[ImageModel]()

// RegisterImageModelDeserializer registers a model factory for
// DeserializeImageModel.
func RegisterImageModelDeserializer(providerName string, fn func(SerializedModel) (ImageModel, error)) {
	imageModelDeserializers.register(providerName, fn)
}

// DeserializeImageModel reconstructs a serialized image model using a
// registered provider factory.
func DeserializeImageModel(serialized SerializedModel) (ImageModel, error) {
	return imageModelDeserializers.deserialize("image", serialized)
}

// RegisteredImageModelDeserializerKeys returns every provider name
// currently registered with RegisterImageModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredImageModelDeserializerKeys() []string {
	return imageModelDeserializers.keys()
}

// SerializeImageModel returns a JSON-friendly serialized image model
// representation.
func SerializeImageModel(model ImageModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var videoModelDeserializers = newTypedModelRegistry[VideoModelV3]()

// RegisterVideoModelDeserializer registers a model factory for
// DeserializeVideoModel.
func RegisterVideoModelDeserializer(providerName string, fn func(SerializedModel) (VideoModelV3, error)) {
	videoModelDeserializers.register(providerName, fn)
}

// DeserializeVideoModel reconstructs a serialized video model using a
// registered provider factory.
func DeserializeVideoModel(serialized SerializedModel) (VideoModelV3, error) {
	return videoModelDeserializers.deserialize("video", serialized)
}

// RegisteredVideoModelDeserializerKeys returns every provider name
// currently registered with RegisterVideoModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredVideoModelDeserializerKeys() []string {
	return videoModelDeserializers.keys()
}

// SerializeVideoModel returns a JSON-friendly serialized video model
// representation.
func SerializeVideoModel(model VideoModelV3) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var speechModelDeserializers = newTypedModelRegistry[SpeechModel]()

// RegisterSpeechModelDeserializer registers a model factory for
// DeserializeSpeechModel.
func RegisterSpeechModelDeserializer(providerName string, fn func(SerializedModel) (SpeechModel, error)) {
	speechModelDeserializers.register(providerName, fn)
}

// DeserializeSpeechModel reconstructs a serialized speech model using a
// registered provider factory.
func DeserializeSpeechModel(serialized SerializedModel) (SpeechModel, error) {
	return speechModelDeserializers.deserialize("speech", serialized)
}

// RegisteredSpeechModelDeserializerKeys returns every provider name
// currently registered with RegisterSpeechModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredSpeechModelDeserializerKeys() []string {
	return speechModelDeserializers.keys()
}

// SerializeSpeechModel returns a JSON-friendly serialized speech model
// representation.
func SerializeSpeechModel(model SpeechModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var transcriptionModelDeserializers = newTypedModelRegistry[TranscriptionModel]()

// RegisterTranscriptionModelDeserializer registers a model factory for
// DeserializeTranscriptionModel.
func RegisterTranscriptionModelDeserializer(providerName string, fn func(SerializedModel) (TranscriptionModel, error)) {
	transcriptionModelDeserializers.register(providerName, fn)
}

// DeserializeTranscriptionModel reconstructs a serialized transcription
// model using a registered provider factory.
func DeserializeTranscriptionModel(serialized SerializedModel) (TranscriptionModel, error) {
	return transcriptionModelDeserializers.deserialize("transcription", serialized)
}

// RegisteredTranscriptionModelDeserializerKeys returns every provider name
// currently registered with RegisterTranscriptionModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredTranscriptionModelDeserializerKeys() []string {
	return transcriptionModelDeserializers.keys()
}

// SerializeTranscriptionModel returns a JSON-friendly serialized
// transcription model representation.
func SerializeTranscriptionModel(model TranscriptionModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var embeddingModelDeserializers = newTypedModelRegistry[EmbeddingModel]()

// RegisterEmbeddingModelDeserializer registers a model factory for
// DeserializeEmbeddingModel.
func RegisterEmbeddingModelDeserializer(providerName string, fn func(SerializedModel) (EmbeddingModel, error)) {
	embeddingModelDeserializers.register(providerName, fn)
}

// DeserializeEmbeddingModel reconstructs a serialized embedding model using
// a registered provider factory.
func DeserializeEmbeddingModel(serialized SerializedModel) (EmbeddingModel, error) {
	return embeddingModelDeserializers.deserialize("embedding", serialized)
}

// RegisteredEmbeddingModelDeserializerKeys returns every provider name
// currently registered with RegisterEmbeddingModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredEmbeddingModelDeserializerKeys() []string {
	return embeddingModelDeserializers.keys()
}

// SerializeEmbeddingModel returns a JSON-friendly serialized embedding
// model representation.
func SerializeEmbeddingModel(model EmbeddingModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var speechTranslationModelDeserializers = newTypedModelRegistry[SpeechTranslationModel]()

// RegisterSpeechTranslationModelDeserializer registers a model factory for
// DeserializeSpeechTranslationModel.
func RegisterSpeechTranslationModelDeserializer(providerName string, fn func(SerializedModel) (SpeechTranslationModel, error)) {
	speechTranslationModelDeserializers.register(providerName, fn)
}

// DeserializeSpeechTranslationModel reconstructs a serialized speech
// translation model using a registered provider factory.
func DeserializeSpeechTranslationModel(serialized SerializedModel) (SpeechTranslationModel, error) {
	return speechTranslationModelDeserializers.deserialize("speech translation", serialized)
}

// RegisteredSpeechTranslationModelDeserializerKeys returns every provider
// name currently registered with RegisterSpeechTranslationModelDeserializer.
// See RegisteredModelDeserializerKeys's doc comment.
func RegisteredSpeechTranslationModelDeserializerKeys() []string {
	return speechTranslationModelDeserializers.keys()
}

// SerializeSpeechTranslationModel returns a JSON-friendly serialized speech
// translation model representation. Mirrors TS
// GoogleSpeechTranslationModel/OpenAISpeechTranslationModel
// [WORKFLOW_SERIALIZE].
func SerializeSpeechTranslationModel(model SpeechTranslationModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

var evaluationModelDeserializers = newTypedModelRegistry[EvaluationModel]()

// RegisterEvaluationModelDeserializer registers a model factory for
// DeserializeEvaluationModel.
func RegisterEvaluationModelDeserializer(providerName string, fn func(SerializedModel) (EvaluationModel, error)) {
	evaluationModelDeserializers.register(providerName, fn)
}

// DeserializeEvaluationModel reconstructs a serialized evaluation model
// using a registered provider factory.
func DeserializeEvaluationModel(serialized SerializedModel) (EvaluationModel, error) {
	return evaluationModelDeserializers.deserialize("evaluation", serialized)
}

// RegisteredEvaluationModelDeserializerKeys returns every provider name
// currently registered with RegisterEvaluationModelDeserializer. See
// RegisteredModelDeserializerKeys's doc comment.
func RegisteredEvaluationModelDeserializerKeys() []string {
	return evaluationModelDeserializers.keys()
}

// SerializeEvaluationModel returns a JSON-friendly serialized evaluation
// model representation.
func SerializeEvaluationModel(model EvaluationModel) (SerializedModel, error) {
	if model == nil {
		return SerializedModel{}, providererrors.NewSerializationError("provider: model is nil", nil)
	}
	return serializeTypedModel(model)
}

// SerializableConfig returns a JSON-compatible copy of config for workflow
// boundaries. It mirrors the TypeScript SDK's serializeModelOptions behavior:
// JSON-serializable values, including static headers, are preserved while
// functions and other non-serializable values are omitted.
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
		"HTTPClient", "httpClient":
		return true
	}
	// sanitizeSerializableValue calls isSensitiveConfigField with both
	// struct field names (e.g. "Headers", "APIKey") and, for a
	// map[string]string field such as Config.Headers, each individual map
	// key (e.g. "Authorization", "X-Api-Key") -- see the map case in
	// sanitizeSerializableValue below. Many provider Config structs accept
	// custom auth via a Headers map, so without this check a credential
	// header would cross straight into SerializedModel.Config verbatim
	// (R1-2): isSensitiveHeaderName catches those by name/pattern
	// regardless of which field the map came from.
	return isSensitiveHeaderName(key)
}

// isSensitiveHeaderName reports whether key is a credential-bearing HTTP
// header name, matching (case-insensitively) both the literal header names
// go-ai's own providers actually send -- Anthropic's "x-api-key", Azure's
// "api-key", Google's "x-goog-api-key", and the standard
// "Authorization"/"Proxy-Authorization"/"Cookie"/"Set-Cookie" -- and the
// generic "*-api-key" / "*-token" / "*secret*" patterns a custom or
// provider-specific header might use. A redacted header is simply dropped
// from the serialized config, exactly like the APIKey/AccessToken struct
// fields above; non-sensitive headers are unaffected.
func isSensitiveHeaderName(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "authorization", "proxy-authorization", "x-api-key", "api-key",
		"x-goog-api-key", "cookie", "set-cookie":
		return true
	}
	if strings.Contains(normalized, "secret") {
		return true
	}
	return strings.HasSuffix(normalized, "-api-key") || strings.HasSuffix(normalized, "-token")
}
