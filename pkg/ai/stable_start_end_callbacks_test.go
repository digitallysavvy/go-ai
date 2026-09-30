package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ported concern (not a single TS test): TS's Embed/EmbedMany/Rerank/
// GenerateObject/StreamObject options carry stable OnStart/OnEnd (and, for
// object generation, OnStepStart) callbacks alongside deprecated
// Experimental* aliases, with the stable name taking precedence when both
// are set (HANDOFF.md item 4 / G3). These tests confirm: (a) the stable
// field fires when set, in preference to the deprecated alias, and (b) the
// deprecated alias still fires on its own for backward compatibility.

func TestEmbed_StableOnStartOnEnd_TakePrecedence(t *testing.T) {
	t.Parallel()
	model := &testutil.MockEmbeddingModel{ProviderName: "p", ModelName: "m"}

	var stableStart, deprecatedStart, stableEnd, deprecatedEnd bool
	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hi",
		OnStart: func(EmbedOnStartEvent) {
			stableStart = true
		},
		ExperimentalOnStart: func(EmbedOnStartEvent) {
			deprecatedStart = true
		},
		OnEnd: func(EmbedOnFinishEvent) {
			stableEnd = true
		},
		ExperimentalOnEnd: func(EmbedOnFinishEvent) {
			deprecatedEnd = true
		},
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if !stableStart || !stableEnd {
		t.Fatalf("expected stable OnStart/OnEnd to fire, got start=%v end=%v", stableStart, stableEnd)
	}
	if deprecatedStart || deprecatedEnd {
		t.Fatalf("expected deprecated aliases NOT to fire when the stable field is set, got start=%v end=%v", deprecatedStart, deprecatedEnd)
	}
}

func TestEmbed_DeprecatedAliasesStillFireAlone(t *testing.T) {
	t.Parallel()
	model := &testutil.MockEmbeddingModel{ProviderName: "p", ModelName: "m"}

	var start, end bool
	_, err := Embed(context.Background(), EmbedOptions{
		Model:                model,
		Input:                "hi",
		ExperimentalOnStart:  func(EmbedOnStartEvent) { start = true },
		ExperimentalOnEnd:    func(EmbedOnFinishEvent) { end = true },
		ExperimentalOnFinish: func(EmbedOnFinishEvent) {},
	})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if !start || !end {
		t.Fatalf("expected deprecated aliases to still fire, got start=%v end=%v", start, end)
	}
}

func TestRerank_StableOnStartOnEnd_TakePrecedence(t *testing.T) {
	t.Parallel()
	model := &testutil.MockRerankingModel{ProviderName: "p", ModelName: "m"}

	var stableStart, deprecatedStart, stableEnd, deprecatedEnd bool
	_, err := Rerank(context.Background(), RerankOptions{
		Model:     model,
		Query:     "q",
		Documents: []string{"a", "b"},
		OnStart: func(RerankOnStartEvent) {
			stableStart = true
		},
		ExperimentalOnStart: func(RerankOnStartEvent) {
			deprecatedStart = true
		},
		OnEnd: func(RerankOnFinishEvent) {
			stableEnd = true
		},
		ExperimentalOnEnd: func(RerankOnFinishEvent) {
			deprecatedEnd = true
		},
	})
	if err != nil {
		t.Fatalf("Rerank() error = %v", err)
	}
	if !stableStart || !stableEnd {
		t.Fatalf("expected stable OnStart/OnEnd to fire, got start=%v end=%v", stableStart, stableEnd)
	}
	if deprecatedStart || deprecatedEnd {
		t.Fatalf("expected deprecated aliases NOT to fire when the stable field is set, got start=%v end=%v", deprecatedStart, deprecatedEnd)
	}
}

func TestGenerateObject_StableCallbacks_TakePrecedence(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"value":"a"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
	})

	var stableStart, deprecatedStart, stableStepStart, deprecatedStepStart, stableEnd, deprecatedEnd bool
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "hi",
		Schema: sch,
		OnStart: func(context.Context, ObjectOnStartEvent) {
			stableStart = true
		},
		ExperimentalOnStart: func(context.Context, ObjectOnStartEvent) {
			deprecatedStart = true
		},
		OnStepStart: func(context.Context, ObjectOnStepStartEvent) {
			stableStepStart = true
		},
		ExperimentalOnStepStart: func(context.Context, ObjectOnStepStartEvent) {
			deprecatedStepStart = true
		},
		OnEnd: func(context.Context, ObjectOnFinishEvent) {
			stableEnd = true
		},
		OnFinishEvent: func(context.Context, ObjectOnFinishEvent) {
			deprecatedEnd = true
		},
	})
	if err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}
	if !stableStart || !stableStepStart || !stableEnd {
		t.Fatalf("expected stable callbacks to fire, got start=%v stepStart=%v end=%v", stableStart, stableStepStart, stableEnd)
	}
	if deprecatedStart || deprecatedStepStart || deprecatedEnd {
		t.Fatalf("expected deprecated aliases NOT to fire when the stable field is set, got start=%v stepStart=%v end=%v", deprecatedStart, deprecatedStepStart, deprecatedEnd)
	}
}

// TestCanonicalEventAliases_Embed_Rerank_Object is a compile-time-shaped
// check that the 29d8cf4 canonical event aliases are usable as their
// underlying deprecated types, both directions.
func TestCanonicalEventAliases_Embed_Rerank_Object(t *testing.T) {
	var _ EmbedStartEvent = EmbedOnStartEvent{}
	var _ EmbedEndEvent = EmbedOnFinishEvent{}
	var _ RerankStartEvent = RerankOnStartEvent{}
	var _ RerankEndEvent = RerankOnFinishEvent{}
	var _ GenerateObjectStartEvent = ObjectOnStartEvent{}
	var _ GenerateObjectStepStartEvent = ObjectOnStepStartEvent{}
	var _ GenerateObjectStepEndEvent = ObjectOnStepFinishEvent{}
	var _ GenerateObjectEndEvent = ObjectOnFinishEvent{}
}
