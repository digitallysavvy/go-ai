package providerutils

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type serializableMockModel struct {
	providerName string
	modelID      string
	config       map[string]interface{}
}

func (m *serializableMockModel) SpecificationVersion() string { return "v3" }
func (m *serializableMockModel) Provider() string             { return m.providerName }
func (m *serializableMockModel) ModelID() string              { return m.modelID }
func (m *serializableMockModel) SupportsTools() bool          { return true }
func (m *serializableMockModel) SupportsStructuredOutput() bool {
	return true
}
func (m *serializableMockModel) SupportsImageInput() bool { return true }
func (m *serializableMockModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, errors.New("unused")
}
func (m *serializableMockModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, errors.New("unused")
}
func (m *serializableMockModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.providerName, ModelID: m.modelID, Config: m.config}
}

type plainMockModel struct {
	providerName string
	modelID      string
}

func (m *plainMockModel) SpecificationVersion() string { return "v3" }
func (m *plainMockModel) Provider() string             { return m.providerName }
func (m *plainMockModel) ModelID() string              { return m.modelID }
func (m *plainMockModel) SupportsTools() bool          { return true }
func (m *plainMockModel) SupportsStructuredOutput() bool {
	return true
}
func (m *plainMockModel) SupportsImageInput() bool { return true }
func (m *plainMockModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return nil, errors.New("unused")
}
func (m *plainMockModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return nil, errors.New("unused")
}

func TestSerializeModel(t *testing.T) {
	model := &serializableMockModel{
		providerName: "test-provider",
		modelID:      "test-model",
		config:       map[string]interface{}{"baseURL": "http://localhost"},
	}
	got, err := SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel() error = %v", err)
	}
	if got["provider"] != "test-provider" || got["modelId"] != "test-model" {
		t.Fatalf("unexpected serialized payload: %+v", got)
	}
	cfg, ok := got["config"].(map[string]interface{})
	if !ok || cfg["baseURL"] != "http://localhost" {
		t.Fatalf("config not preserved: %+v", got)
	}
}

func TestSerializeModelNotSerializable(t *testing.T) {
	model := &plainMockModel{providerName: "p", modelID: "m"}
	_, err := SerializeModel(model)
	if err == nil {
		t.Fatal("expected non-serializable error")
	}
}

func TestDeserializeModel(t *testing.T) {
	provider.RegisterModelDeserializer("test-provider-serialize-model", func(s provider.SerializedModel) (provider.LanguageModel, error) {
		return &serializableMockModel{providerName: s.Provider, modelID: s.ModelID, config: s.Config}, nil
	})

	model, err := DeserializeModel("test-provider-serialize-model", "m2", map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatalf("DeserializeModel() error = %v", err)
	}
	if model.Provider() != "test-provider-serialize-model" || model.ModelID() != "m2" {
		t.Fatalf("unexpected model: provider=%s model=%s", model.Provider(), model.ModelID())
	}
}
