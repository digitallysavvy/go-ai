package provider

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSerializableConfigDropsAuthAndHTTPClientButPreservesSerializableHeaders(t *testing.T) {
	type nested struct {
		Value string
	}
	type cfg struct {
		APIKey     string
		BaseURL    string
		Headers    map[string]string
		HTTPClient interface{}
		GenerateID func() string
		Tags       []string
		Nested     nested
		Plain      map[string]interface{}
		BadPlain   map[string]interface{}
	}

	got := SerializableConfig(cfg{
		APIKey:  "secret",
		BaseURL: "https://example.com",
		Headers: map[string]string{
			"Authorization":       "Bearer secret",
			"X-Api-Key":           "sk-live-secret",
			"x-goog-api-key":      "goog-secret",
			"Proxy-Authorization": "Basic secret",
			"Cookie":              "session=secret",
			"X-Custom-Secret":     "secret-value",
			"X-Session-Token":     "token-value",
			"X-Request-Id":        "non-sensitive",
			"Content-Type":        "application/json",
		},
		HTTPClient: "client",
		GenerateID: func() string { return "id" },
		Tags:       []string{"a", "b"},
		Nested:     nested{Value: "drop"},
		Plain:      map[string]interface{}{"ok": true},
		BadPlain:   map[string]interface{}{"fn": func() {}},
	})

	if got["BaseURL"] != "https://example.com" {
		t.Fatalf("BaseURL not preserved: %#v", got)
	}
	if tags, ok := got["Tags"].([]interface{}); !ok || len(tags) != 2 {
		t.Fatalf("Tags not preserved: %#v", got)
	}
	if plain, ok := got["Plain"].(map[string]interface{}); !ok || plain["ok"] != true {
		t.Fatalf("Plain not preserved: %#v", got)
	}
	headers, ok := got["Headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("Headers not preserved: %#v", got)
	}
	// Non-sensitive headers must still round-trip.
	if headers["X-Request-Id"] != "non-sensitive" {
		t.Fatalf("non-sensitive header X-Request-Id dropped: %#v", headers)
	}
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("non-sensitive header Content-Type dropped: %#v", headers)
	}
	// Credential-bearing headers (R1-2) must be redacted, case-insensitively,
	// including provider-specific auth headers and generic secret/token
	// patterns.
	for _, key := range []string{
		"Authorization", "X-Api-Key", "x-goog-api-key", "Proxy-Authorization",
		"Cookie", "X-Custom-Secret", "X-Session-Token",
	} {
		if v, ok := headers[key]; ok {
			t.Fatalf("sensitive header %q leaked into serialized config: %v", key, v)
		}
	}
	for _, key := range []string{"APIKey", "HTTPClient", "GenerateID", "Nested", "BadPlain"} {
		if _, ok := got[key]; ok {
			t.Fatalf("%s should be removed from serialized config: %#v", key, got)
		}
	}
}

type serializableTestModel struct{}

func (serializableTestModel) SpecificationVersion() string { return "v3" }
func (serializableTestModel) Provider() string             { return "test" }
func (serializableTestModel) ModelID() string              { return "model" }
func (serializableTestModel) SupportsTools() bool          { return false }
func (serializableTestModel) SupportsStructuredOutput() bool {
	return false
}
func (serializableTestModel) SupportsImageInput() bool { return false }
func (serializableTestModel) DoGenerate(context.Context, *GenerateOptions) (*types.GenerateResult, error) {
	return nil, nil
}
func (serializableTestModel) DoStream(context.Context, *GenerateOptions) (TextStream, error) {
	return nil, nil
}
func (serializableTestModel) Serialize() SerializedModel {
	return SerializedModel{Provider: "test", ModelID: "model"}
}

func TestSerializeModelNormalizesNilConfig(t *testing.T) {
	serialized, err := SerializeModel(serializableTestModel{})
	if err != nil {
		t.Fatalf("SerializeModel() error = %v", err)
	}
	if serialized.Config == nil {
		t.Fatal("Config = nil, want empty map")
	}
	if len(serialized.Config) != 0 {
		t.Fatalf("Config = %#v, want empty map", serialized.Config)
	}
}

// serializableTestImageModel is a minimal ImageModel used to exercise the
// generic typed-model-kind plumbing (SerializeImageModel/DeserializeImageModel/
// RegisterImageModelDeserializer), which every non-language-model provider
// serialization.go added for U4 relies on.
type serializableTestImageModel struct{}

func (serializableTestImageModel) SpecificationVersion() string { return "v4" }
func (serializableTestImageModel) Provider() string             { return "typed-test-image" }
func (serializableTestImageModel) ModelID() string              { return "image-model" }
func (serializableTestImageModel) DoGenerate(context.Context, *ImageGenerateOptions) (*types.ImageResult, error) {
	return nil, nil
}
func (serializableTestImageModel) Serialize() SerializedModel {
	return SerializedModel{Provider: "typed-test-image", ModelID: "image-model"}
}

func TestSerializeImageModelAndDeserializeImageModelRoundTrip(t *testing.T) {
	RegisterImageModelDeserializer("typed-test-image", func(s SerializedModel) (ImageModel, error) {
		if s.ModelID != "image-model" {
			t.Fatalf("unexpected ModelID in deserializer: %q", s.ModelID)
		}
		return serializableTestImageModel{}, nil
	})

	serialized, err := SerializeImageModel(serializableTestImageModel{})
	if err != nil {
		t.Fatalf("SerializeImageModel() error = %v", err)
	}
	if serialized.Provider != "typed-test-image" || serialized.ModelID != "image-model" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if serialized.Config == nil {
		t.Fatal("Config = nil, want empty map")
	}

	restored, err := DeserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeImageModel() error = %v", err)
	}
	if restored.Provider() != "typed-test-image" || restored.ModelID() != "image-model" {
		t.Fatalf("unexpected restored model: provider=%q model=%q", restored.Provider(), restored.ModelID())
	}
}

// unserializableTestImageModel implements ImageModel but neither
// SerializableModel nor SerializableModelStrict.
type unserializableTestImageModel struct{}

func (unserializableTestImageModel) SpecificationVersion() string { return "v4" }
func (unserializableTestImageModel) Provider() string             { return "no-serialize" }
func (unserializableTestImageModel) ModelID() string              { return "m" }
func (unserializableTestImageModel) DoGenerate(context.Context, *ImageGenerateOptions) (*types.ImageResult, error) {
	return nil, nil
}

func TestSerializeImageModelRejectsNonSerializableModel(t *testing.T) {
	var m ImageModel = unserializableTestImageModel{}
	if _, err := SerializeImageModel(m); err == nil {
		t.Fatal("expected error for a model that does not implement SerializableModel")
	}
}

func TestDeserializeImageModelErrorsWhenUnregistered(t *testing.T) {
	if _, err := DeserializeImageModel(SerializedModel{Provider: "no-such-image-provider"}); err == nil {
		t.Fatal("expected error for an unregistered provider")
	}
}

func TestSerializeImageModelErrorsOnNilModel(t *testing.T) {
	if _, err := SerializeImageModel(nil); err == nil {
		t.Fatal("expected error for a nil model")
	}
}
