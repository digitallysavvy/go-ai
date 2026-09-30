package typesafeai

import "testing"

func TestTypeSafeAISerializeAndDeserializeEvaluationModel(t *testing.T) {
	p := New(Config{APIKey: "k"})
	model, err := p.EvaluationModel("jev-latest")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}
	serialized := model.(*EvaluationModel).Serialize()
	if serialized.Provider != "typesafe.evaluation" || serialized.ModelID != "jev-latest" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "typesafe.evaluation" || restored.ModelID() != "jev-latest" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}
