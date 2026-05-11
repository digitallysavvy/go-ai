package provider

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSerializableConfigDropsAuthHeadersAndHTTPClient(t *testing.T) {
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
		APIKey:     "secret",
		BaseURL:    "https://example.com",
		Headers:    map[string]string{"Authorization": "Bearer secret"},
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
	for _, key := range []string{"APIKey", "Headers", "HTTPClient", "GenerateID", "Nested", "BadPlain"} {
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
