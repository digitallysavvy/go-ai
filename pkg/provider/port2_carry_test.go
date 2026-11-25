package provider

import (
	"context"
	"testing"
)

type port2carryExtraction struct{ id string }

func (port2carryExtraction) SpecificationVersion() string  { return "v4" }
func (port2carryExtraction) Provider() string              { return "port2carry.extraction" }
func (m port2carryExtraction) ModelID() string             { return m.id }
func (port2carryExtraction) SupportedFieldTypes() []string { return nil }
func (port2carryExtraction) Serialize() SerializedModel {
	return SerializedModel{Provider: "port2carry.extraction", ModelID: "m1", Config: map[string]interface{}{"k": "v"}}
}
func (port2carryExtraction) DoExtract(context.Context, ExtractionCallOptions) (*ExtractionResult, error) {
	return nil, nil
}

// Extraction twins of the decision-model serialization registry.
func TestPort2carryExtractionModelSerializationRoundTrip(t *testing.T) {
	RegisterExtractionModelDeserializer("port2carry.extraction", func(s SerializedModel) (ExtractionModel, error) {
		return port2carryExtraction{id: s.ModelID}, nil
	})
	found := false
	for _, k := range RegisteredExtractionModelDeserializerKeys() {
		found = found || k == "port2carry.extraction"
	}
	if !found {
		t.Fatal("key not registered")
	}
	ser, err := SerializeExtractionModel(port2carryExtraction{id: "m1"})
	if err != nil || ser.Provider != "port2carry.extraction" || ser.Config["k"] != "v" {
		t.Fatalf("serialized = %+v, %v", ser, err)
	}
	m, err := DeserializeExtractionModel(ser)
	if err != nil || m.ModelID() != "m1" {
		t.Fatalf("deserialized = %v, %v", m, err)
	}
	if _, err := DeserializeExtractionModel(SerializedModel{Provider: "no-such-extraction"}); err == nil {
		t.Fatal("unknown provider must fail")
	}
	if _, err := SerializeExtractionModel(nil); err == nil {
		t.Fatal("nil model must fail")
	}
}
