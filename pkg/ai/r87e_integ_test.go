package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Coordinator ruling (round 87e): a single Embed whose provider sends no
// usage (Google never does) returns the v0.5.2 zero usage, which marshals,
// and the root span drops ai.usage.tokens where TS's `usage ?? {tokens:
// NaN}` is NaN.
func TestR87eintegEmbedWithoutUsageKeepsZeroAndDropsAttribute(t *testing.T) {
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.1}}, nil
		},
	}
	var res *EmbedResult
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		var err error
		res, err = Embed(context.Background(), EmbedOptions{Model: model, Input: "a", Telemetry: settings})
		if err != nil {
			t.Fatal(err)
		}
	})
	if res.Usage.Tokens != 0 || res.Usage.Reported {
		t.Fatalf("Usage = %+v, want the v0.5.2 zero", res.Usage)
	}
	if _, err := json.Marshal(res.Usage); err != nil {
		t.Fatalf("Usage does not marshal: %v", err)
	}
	if v, ok := got["ai.embed ai.usage.tokens"]; ok {
		t.Fatalf("root ai.usage.tokens = %v, want it dropped; all: %v", v, got)
	}
}

// Every EmbedMany batch reported: the sum is recorded on the root span.
func TestR87eintegEmbedManyAllReportedRecordsSum(t *testing.T) {
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings: 1,
		DoEmbedManyFunc: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}}, Usage: types.EmbeddingUsage{Tokens: 2, Reported: true}}, nil
		},
	}
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	if v, ok := got["ai.embedMany ai.usage.tokens"]; !ok || v.(int64) != 4 {
		t.Fatalf("root ai.usage.tokens = %v (set %v), want 4", v, ok)
	}
}

// TS UnsupportedModelVersionError interpolates provider and model between
// literal quotes; %q would escape them.
func TestR87eintegUnsupportedModelVersionTextUnescaped(t *testing.T) {
	err := &UnsupportedModelVersionError{Version: "v3", Provider: `a"b`, ModelID: `m\1`}
	want := `Unsupported model version v3 for provider "a"b" and model "m\1". AI SDK 5 only supports models that implement specification version "v2".`
	if err.Error() != want {
		t.Fatalf("Error() = %s\nwant      %s", err.Error(), want)
	}
}
