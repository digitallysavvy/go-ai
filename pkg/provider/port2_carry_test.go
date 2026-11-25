package provider

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

// A text state part keeps its provider options on the wire shape (TS text
// part `{ type, text, providerOptions }`).
func TestPort2carryDecisionTextPartProviderOptionsMarshal(t *testing.T) {
	p := DecisionStatePart{Type: "text", Text: "Invoice", ProviderOptions: map[string]interface{}{"user": map[string]interface{}{"v": "k"}}}
	b, err := p.MarshalJSON()
	if err != nil || string(b) != `{"type":"text","text":"Invoice","providerOptions":{"user":{"v":"k"}}}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	b, _ = DecisionTextPart("x").MarshalJSON()
	if string(b) != `{"type":"text","text":"x"}` {
		t.Fatalf("marshal = %s", b)
	}
}

type port2carryCapture struct {
	telemetry.NoopTelemetryIntegration
	state interface{}
}

func (c *port2carryCapture) OnExtractionModelCallStart(_ context.Context, e telemetry.ExtractionModelCallStartEvent) {
	c.state = e.State
}

// The telemetry copy of a real decision state drops text-part provider
// options (TS 797dc0254e); the caller's state keeps them.
func TestPort2carryTelemetryDropsDecisionTextPartProviderOptions(t *testing.T) {
	opts := map[string]interface{}{"user": map[string]interface{}{"v": "private"}}
	state := []DecisionStatePart{{Type: "text", Text: "Invoice", ProviderOptions: opts}, DecisionJSONPart(map[string]interface{}{"providerOptions": "keep"})}
	cap := &port2carryCapture{}
	telemetry.FireOnExtractionModelCallStart(context.Background(), telemetry.ExtractionModelCallStartEvent{
		Settings: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{cap}}, State: state,
	})
	got, ok := cap.state.([]DecisionStatePart)
	if !ok || len(got) != 2 {
		t.Fatalf("captured state = %#v", cap.state)
	}
	if got[0].ProviderOptions != nil || got[0].Text != "Invoice" {
		t.Fatalf("telemetry text part = %+v", got[0])
	}
	if got[1].Value == nil {
		t.Fatalf("json part changed: %+v", got[1])
	}
	if state[0].ProviderOptions == nil {
		t.Fatal("the caller's state was modified")
	}
}
