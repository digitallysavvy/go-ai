package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS EmbedResult.value / EmbedManyResult.values echo the embedded text.
func TestEmbedResultsEchoValues(t *testing.T) {
	t.Parallel()
	model := &testutil.MockEmbeddingModel{}
	one, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "hello"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if one.Value != "hello" {
		t.Fatalf("Embed().Value = %q, want hello", one.Value)
	}
	many, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("EmbedMany() error = %v", err)
	}
	if !reflect.DeepEqual(many.Values, []string{"a", "b"}) {
		t.Fatalf("EmbedMany().Values = %v, want [a b]", many.Values)
	}
}
