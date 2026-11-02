package ai

import (
	"context"
	"errors"
	"math"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r80carryUsageAttrs runs fn with both telemetry integrations and returns the
// usage attribute of every ended span, keyed by span name.
func r80carryUsageAttrs(t *testing.T, fn func(settings *TelemetrySettings)) map[string]interface{} {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	legacy := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("r80carry-legacy")})
	genai := telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("r80carry-genai")})
	fn(&TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{legacy, genai}})
	got := map[string]interface{}{}
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.usage.tokens" || kv.Key == "gen_ai.usage.input_tokens" {
				got[s.Name()+" "+string(kv.Key)] = kv.Value.AsInterface()
			}
		}
	}
	return got
}

func r80carryIsZero(v interface{}) bool {
	switch x := v.(type) {
	case int64:
		return x == 0
	case float64:
		return x == 0
	}
	return false
}

// Owner ruling 2026-11-02 (option b): a provider-reported 0 is recorded on
// the legacy root, the legacy doEmbed span and the GenAI embeddings span
// (TS `usage: { tokens: 0 }`); an unreported 0 stays unset (TS NaN dropped).
func TestR80carryEmbedRecordsReportedZeroUsage(t *testing.T) {
	keys := []string{"ai.embed ai.usage.tokens", "ai.embed.doEmbed ai.usage.tokens", "embeddings mock-embedding gen_ai.usage.input_tokens"}
	for _, tc := range []struct {
		name  string
		usage types.EmbeddingUsage
		want  bool
	}{
		{"reported 0", types.EmbeddingUsage{Reported: true}, true},
		{"unreported 0", types.EmbeddingUsage{}, false},
		{"reported NaN", types.EmbeddingUsage{Tokens: math.NaN(), Reported: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &testutil.MockEmbeddingModel{
				DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
					return &types.EmbeddingResult{Embedding: []float64{0.1}, Usage: tc.usage}, nil
				},
			}
			got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
				if _, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "x", Telemetry: settings}); err != nil {
					t.Fatal(err)
				}
			})
			for _, k := range keys {
				v, ok := got[k]
				if ok != tc.want || (ok && !r80carryIsZero(v)) {
					t.Fatalf("%s = %v (set %v), want set=%v with 0; all: %v", k, v, ok, tc.want, got)
				}
			}
		})
	}
}

// EmbedMany batching ANDs Reported across batches (coordinator correction
// 2026-11-02: TS sums usage.tokens with NaN for a batch without usage), and
// the legacy root records the reported 0 only when every batch reported.
func TestR80carryEmbedManyBatchesAndReported(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reported []bool
		want     bool
	}{
		{"none", []bool{false, false}, false},
		{"first", []bool{true, false}, false},
		{"second", []bool{false, true}, false},
		{"both", []bool{true, true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := 0
			model := &testutil.MockEmbeddingModel{
				MaxEmbeddings: 1,
				DoEmbedManyFunc: func(_ context.Context, inputs []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
					r := tc.reported[call]
					call++
					return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}}, Usage: types.EmbeddingUsage{Reported: r}}, nil
				},
			}
			var res *EmbedManyResult
			got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
				var err error
				res, err = EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, Telemetry: settings})
				if err != nil {
					t.Fatal(err)
				}
			})
			if res.Usage.Reported != tc.want {
				t.Fatalf("Usage.Reported = %v, want %v", res.Usage.Reported, tc.want)
			}
			v, ok := got["ai.embedMany ai.usage.tokens"]
			if ok != tc.want || (ok && !r80carryIsZero(v)) {
				t.Fatalf("root ai.usage.tokens = %v (set %v), want set=%v; all: %v", v, ok, tc.want, got)
			}
		})
	}
}

// Zero chunks (empty values with a limited model): TS sums nothing and
// records usage.tokens 0; Go reports the usage (empty AND is true).
func TestR80carryEmbedManyZeroChunksRecordsZero(t *testing.T) {
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings: 1,
		DoEmbedManyFunc: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			t.Fatal("no model call expected for zero chunks")
			return nil, nil
		},
	}
	var res *EmbedManyResult
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		var err error
		res, err = EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{}, Telemetry: settings})
		if err != nil {
			t.Fatal(err)
		}
	})
	if !res.Usage.Reported || res.Usage.Tokens != 0 {
		t.Fatalf("Usage = %+v, want reported 0", res.Usage)
	}
	if v, ok := got["ai.embedMany ai.usage.tokens"]; !ok || !r80carryIsZero(v) {
		t.Fatalf("root ai.usage.tokens = %v (set %v), want 0; all: %v", v, ok, got)
	}
}

// A positive sum with an unreported batch keeps the v0.5 reading: the sum is
// recorded although Reported is false (TS: NaN, dropped; known difference
// "Embedding usage across EmbedMany batches").
func TestR80carryEmbedManyPositiveSumWithUnreportedBatch(t *testing.T) {
	call := 0
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings: 1,
		DoEmbedManyFunc: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			call++
			if call == 1 {
				return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}}, Usage: types.EmbeddingUsage{Tokens: 3, Reported: true}}, nil
			}
			return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}}}, nil
		},
	}
	var res *EmbedManyResult
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		var err error
		res, err = EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, Telemetry: settings})
		if err != nil {
			t.Fatal(err)
		}
	})
	if res.Usage.Reported || res.Usage.Tokens != 3 {
		t.Fatalf("Usage = %+v, want unreported with 3 tokens", res.Usage)
	}
	v, ok := got["ai.embedMany ai.usage.tokens"]
	if !ok {
		t.Fatalf("root ai.usage.tokens not set; all: %v", got)
	}
	if n, _ := v.(int64); n != 3 {
		t.Fatalf("root ai.usage.tokens = %v, want 3", v)
	}
}

func TestR80carryTelemetryUsageFromEmbeddingUsageReported(t *testing.T) {
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{Reported: true}); u.TotalTokens == nil || *u.TotalTokens != 0 {
		t.Fatalf("reported 0: %+v", u)
	}
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{}); u.TotalTokens != nil {
		t.Fatalf("unreported 0: %+v", u)
	}
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{Tokens: math.Inf(1), Reported: true}); u.TotalTokens != nil {
		t.Fatalf("reported Inf: %+v", u)
	}
}

// TS embed() rejects only `modelResponse.embeddings[0] == null`: an empty
// vector is a result, a missing embedding is InvalidResponseDataError.
func TestR80carryEmbedAcceptsEmptyVector(t *testing.T) {
	for _, tc := range []struct {
		name      string
		embedding []float64
		wantErr   bool
	}{
		{"empty vector", []float64{}, false},
		{"missing", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &testutil.MockEmbeddingModel{
				DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
					return &types.EmbeddingResult{Embedding: tc.embedding}, nil
				},
			}
			capture := &telemetryCapture{}
			res, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "x",
				Telemetry: &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{capture}}})
			capture.mu.Lock()
			ends := capture.embedFinishes
			capture.mu.Unlock()
			// TS onEmbedEnd embeddings: modelResponse.embeddings ([[]] or []).
			if len(ends) != 1 || len(ends[0].Embeddings) != map[bool]int{true: 0, false: 1}[tc.wantErr] {
				t.Fatalf("doEmbed end events = %+v", ends)
			}
			if tc.wantErr {
				var invalid *providererrors.InvalidResponseDataError
				if !errors.As(err, &invalid) || invalid.Error() != "No embedding generated." {
					t.Fatalf("got %v, want InvalidResponseDataError", err)
				}
				return
			}
			if err != nil || res.Embedding == nil || len(res.Embedding) != 0 {
				t.Fatalf("got %+v, %v; want the empty vector", res, err)
			}
		})
	}
}
