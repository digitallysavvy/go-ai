package huggingface

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestProviderOptionsAreValidatedAndEmptyValuesSent(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "m")

	for _, opts := range []map[string]interface{}{
		{"metadata": map[string]interface{}{"a": 1}},
		{"instructions": 5},
		{"strictJsonSchema": "yes"},
		{"reasoningEffort": nil},
	} {
		_, _, err := model.getArgs(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"huggingface": opts}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Fatalf("%v: err = %v, want InvalidArgumentError", opts, err)
		}
	}

	// TS sends metadata, instructions and reasoning.effort whenever they are
	// set, including empty values.
	body, _, err := model.getArgs(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"huggingface": map[string]interface{}{
		"metadata": map[string]interface{}{}, "instructions": "", "reasoningEffort": "",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["metadata"]; !ok {
		t.Error("empty metadata dropped")
	}
	if v, ok := body["instructions"]; !ok || v != "" {
		t.Errorf("empty instructions dropped: %v", body["instructions"])
	}
	if r, ok := body["reasoning"].(map[string]interface{}); !ok || r["effort"] != "" {
		t.Errorf("empty reasoning effort dropped: %v", body["reasoning"])
	}
}
