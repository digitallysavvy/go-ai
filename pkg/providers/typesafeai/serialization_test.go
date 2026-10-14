package typesafeai

import "testing"

func TestTypeSafeAISerializeAndDeserializeDecisionModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.DecisionModel("jev-latest")
	if err != nil {
		t.Fatalf("DecisionModel error = %v", err)
	}
	serialized := model.(*DecisionModel).Serialize()
	if serialized.Provider != "typesafe.decision" || serialized.ModelID != "jev-latest" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "typesafe.decision" || restored.ModelID() != "jev-latest" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
