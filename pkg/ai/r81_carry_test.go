package ai

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports of otel/src/open-telemetry.test.ts "embed integration" and
// ai/src/embed/embed-many.test.ts "result.providerMetadata" (r81 carry).

type r81carrySpanUsage struct {
	name      string
	tokens    int64
	hasTokens bool
}

func r81carryEmbedSpans(t *testing.T, run func(settings *telemetry.Options)) []r81carrySpanUsage {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	run(&telemetry.Options{
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("t")})},
	})
	var out []r81carrySpanUsage
	for _, s := range rec.Ended() {
		u := r81carrySpanUsage{name: s.Name()}
		for _, kv := range s.Attributes() {
			if kv.Key == attribute.Key("gen_ai.usage.input_tokens") {
				u.tokens, u.hasTokens = kv.Value.AsInt64(), true
			}
			if kv.Key == attribute.Key("gen_ai.operation.name") && kv.Value.AsString() != "embeddings" {
				t.Errorf("span %s: gen_ai.operation.name = %q, want embeddings", s.Name(), kv.Value.AsString())
			}
		}
		out = append(out, u)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].tokens > out[j].tokens })
	return out
}

func TestR81carryOTelEmbedUsageOnlyOnProviderSpan(t *testing.T) {
	model := &batchEmbeddingModel{doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		return &types.EmbeddingsResult{
			Embeddings: [][]float64{{0.1, 0.2, 0.3}},
			Usage:      types.EmbeddingUsage{Tokens: 14, InputTokens: 14, TotalTokens: 14, Reported: true},
		}, nil
	}}
	got := r81carryEmbedSpans(t, func(settings *telemetry.Options) {
		if _, err := Embed(context.Background(), EmbedOptions{Model: model, Input: "sunny day at the beach", Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	want := []r81carrySpanUsage{
		{name: "embeddings mock-model", tokens: 14, hasTokens: true},
		{name: "embeddings mock-model"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %+v, want %+v", got, want)
	}
}

func TestR81carryOTelEmbedManyBatchedUsageOnlyOnProviderSpans(t *testing.T) {
	model := &batchEmbeddingModel{maxPerCall: 2, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		embeddings := make([][]float64, len(values))
		for i := range values {
			embeddings[i] = []float64{0.1, 0.2, 0.3}
		}
		n := len(values) * 14
		return &types.EmbeddingsResult{
			Embeddings: embeddings,
			Usage:      types.EmbeddingUsage{Tokens: float64(n), InputTokens: n, TotalTokens: n, Reported: true},
		}, nil
	}}
	got := r81carryEmbedSpans(t, func(settings *telemetry.Options) {
		if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"sunny", "day", "beach"}, Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	want := []r81carrySpanUsage{
		{name: "embeddings mock-model", tokens: 28, hasTokens: true},
		{name: "embeddings mock-model", tokens: 14, hasTokens: true},
		{name: "embeddings mock-model"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %+v, want %+v", got, want)
	}
}

func TestR81carryOTelEmbedOmitsUsageWhenNotReported(t *testing.T) {
	model := &batchEmbeddingModel{}
	var result *EmbedResult
	got := r81carryEmbedSpans(t, func(settings *telemetry.Options) {
		var err error
		if result, err = Embed(context.Background(), EmbedOptions{Model: model, Input: "sunny day at the beach", Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	if result.Usage.Reported {
		t.Error("usage marked reported for a provider that returned none (TS tokens NaN)")
	}
	if len(got) != 2 {
		t.Fatalf("spans = %+v, want 2", got)
	}
	for _, s := range got {
		if s.hasTokens {
			t.Errorf("span %s has gen_ai.usage.input_tokens", s.name)
		}
	}
}

// embed-many.test.ts "merges metadata for provider %s as an own property":
// an empty provider entry is kept, and later batches merge into an earlier
// entry under the same provider key, in first-seen order.
func TestR81carryEmbedManyMergesProviderMetadataPerKey(t *testing.T) {
	for _, providerName := range []string{"__proto__", "constructor", "toString"} {
		t.Run(providerName, func(t *testing.T) {
			metadata := []map[string]interface{}{
				{"other": map[string]interface{}{}},
				{providerName: map[string]interface{}{"first": true}},
				{providerName: map[string]interface{}{"second": true}},
			}
			call := 0
			model := &batchEmbeddingModel{maxPerCall: 1, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
				call++
				return &types.EmbeddingsResult{Embeddings: [][]float64{{float64(call)}}, ProviderMetadata: metadata[call-1]}, nil
			}}
			result, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b", "c"}})
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{
				"other":      map[string]interface{}{},
				providerName: map[string]interface{}{"first": true, "second": true},
			}
			if !reflect.DeepEqual(result.ProviderMetadata, want) {
				t.Fatalf("providerMetadata = %#v, want %#v", result.ProviderMetadata, want)
			}
			if !reflect.DeepEqual(result.Embeddings, [][]float64{{1}, {2}, {3}}) {
				t.Fatalf("embeddings = %v", result.Embeddings)
			}
		})
	}
}

// r81carryLegacySpans runs fn with a LegacyOpenTelemetry integration and
// returns each ended span as name plus attributes (the user-agent header is
// replaced by "UA", since the Go version differs from the TS test value).
func r81carryLegacySpans(t *testing.T, functionID string, recordIO *bool, run func(settings *telemetry.Options)) []map[string]interface{} {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	run(&telemetry.Options{
		IsEnabled:     telemetry.Bool(true),
		FunctionID:    functionID,
		RecordInputs:  recordIO,
		RecordOutputs: recordIO,
		Integrations:  []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})},
	})
	var out []map[string]interface{}
	for _, s := range rec.Ended() {
		m := map[string]interface{}{"name": s.Name()}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.request.headers.user-agent" {
				m[string(kv.Key)] = "UA"
				continue
			}
			m[string(kv.Key)] = kv.Value.AsInterface()
		}
		out = append(out, m)
	}
	return out
}

// legacy-open-telemetry.test.ts "LegacyOpenTelemetry integration with
// embedMany > should record telemetry data when enabled (multiple calls
// path)" (snapshot), spans in end order (TS lists them in start order).
func TestR81carryLegacyEmbedManyMultipleCallsSnapshot(t *testing.T) {
	call := 0
	model := &batchEmbeddingModel{maxPerCall: 2, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		call++
		if call == 1 {
			return &types.EmbeddingsResult{Embeddings: dummyEmbeddings[:2], Usage: types.EmbeddingUsage{Tokens: 10, Reported: true}}, nil
		}
		return &types.EmbeddingsResult{Embeddings: dummyEmbeddings[2:], Usage: types.EmbeddingUsage{Tokens: 20, Reported: true}}, nil
	}}
	got := r81carryLegacySpans(t, "test-function-id", nil, func(settings *telemetry.Options) {
		if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	child := func(embeddings, values []string, tokens int64) map[string]interface{} {
		return map[string]interface{}{
			"name":                          "ai.embedMany.doEmbed",
			"ai.embeddings":                 embeddings,
			"ai.model.id":                   "mock-model",
			"ai.model.provider":             "mock-provider",
			"ai.operationId":                "ai.embedMany.doEmbed",
			"ai.request.headers.user-agent": "UA",
			"ai.settings.maxRetries":        int64(2),
			"ai.telemetry.functionId":       "test-function-id",
			"ai.usage.tokens":               tokens,
			"ai.values":                     values,
			"operation.name":                "ai.embedMany.doEmbed test-function-id",
			"resource.name":                 "test-function-id",
		}
	}
	want := []map[string]interface{}{
		child([]string{"[0.1,0.2,0.3]", "[0.4,0.5,0.6]"}, []string{`"sunny day at the beach"`, `"rainy afternoon in the city"`}, 10),
		child([]string{"[0.7,0.8,0.9]"}, []string{`"snowy night in the mountains"`}, 20),
		{
			"name":                          "ai.embedMany",
			"ai.embeddings":                 []string{"[0.1,0.2,0.3]", "[0.4,0.5,0.6]", "[0.7,0.8,0.9]"},
			"ai.model.id":                   "mock-model",
			"ai.model.provider":             "mock-provider",
			"ai.operationId":                "ai.embedMany",
			"ai.request.headers.user-agent": "UA",
			"ai.settings.maxRetries":        int64(2),
			"ai.telemetry.functionId":       "test-function-id",
			"ai.usage.tokens":               int64(30),
			"ai.values":                     []string{`"sunny day at the beach"`, `"rainy afternoon in the city"`, `"snowy night in the mountains"`},
			"operation.name":                "ai.embedMany test-function-id",
			"resource.name":                 "test-function-id",
		},
	}
	if !reflect.DeepEqual(got, want) {
		for i := range got {
			for k, v := range got[i] {
				if i < len(want) && !reflect.DeepEqual(v, want[i][k]) {
					t.Errorf("span %d %s: got %#v (%T), want %#v (%T)", i, k, v, v, want[i][k], want[i][k])
				}
			}
		}
		t.Fatalf("spans:\n got %#v\nwant %#v", got, want)
	}
}

func r81carryDiffSpans(t *testing.T, got, want []map[string]interface{}) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("spans:\n got %#v\nwant %#v", got, want)
}

// legacy-open-telemetry.test.ts "LegacyOpenTelemetry integration with
// embed" snapshots (spans in end order).
func TestR81carryLegacyEmbedSnapshots(t *testing.T) {
	const value = "sunny day at the beach"
	base := func(name, functionID string) map[string]interface{} {
		m := map[string]interface{}{
			"name":                          name,
			"ai.model.id":                   "mock-model",
			"ai.model.provider":             "mock-provider",
			"ai.operationId":                name,
			"ai.request.headers.user-agent": "UA",
			"ai.settings.maxRetries":        int64(2),
			"operation.name":                name,
		}
		if functionID != "" {
			m["ai.telemetry.functionId"] = functionID
			m["operation.name"] = name + " " + functionID
			m["resource.name"] = functionID
		}
		return m
	}
	with := func(m map[string]interface{}, kv ...interface{}) map[string]interface{} {
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	model := func(usage types.EmbeddingUsage) *batchEmbeddingModel {
		return &batchEmbeddingModel{doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1, 0.2, 0.3}}, Usage: usage}, nil
		}}
	}
	reported10 := types.EmbeddingUsage{Tokens: 10, Reported: true}
	for _, tc := range []struct {
		name       string
		functionID string
		recordIO   *bool
		usage      types.EmbeddingUsage
		want       []map[string]interface{}
	}{
		{
			name:  "should omit usage attributes when the provider does not return usage",
			usage: types.EmbeddingUsage{},
			want: []map[string]interface{}{
				with(base("ai.embed.doEmbed", ""), "ai.embeddings", []string{"[0.1,0.2,0.3]"}, "ai.values", []string{`"sunny day at the beach"`}),
				with(base("ai.embed", ""), "ai.embedding", "[0.1,0.2,0.3]", "ai.value", `"sunny day at the beach"`),
			},
		},
		{
			name:       "should record telemetry data when enabled",
			functionID: "test-function-id",
			usage:      reported10,
			want: []map[string]interface{}{
				with(base("ai.embed.doEmbed", "test-function-id"), "ai.embeddings", []string{"[0.1,0.2,0.3]"}, "ai.values", []string{`"sunny day at the beach"`}, "ai.usage.tokens", int64(10)),
				with(base("ai.embed", "test-function-id"), "ai.embedding", "[0.1,0.2,0.3]", "ai.value", `"sunny day at the beach"`, "ai.usage.tokens", int64(10)),
			},
		},
		{
			name:     "should not record telemetry inputs / outputs when disabled",
			recordIO: telemetry.Bool(false),
			usage:    reported10,
			want: []map[string]interface{}{
				with(base("ai.embed.doEmbed", ""), "ai.usage.tokens", int64(10)),
				with(base("ai.embed", ""), "ai.usage.tokens", int64(10)),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r81carryLegacySpans(t, tc.functionID, tc.recordIO, func(settings *telemetry.Options) {
				if _, err := Embed(context.Background(), EmbedOptions{Model: model(tc.usage), Input: value, Telemetry: settings}); err != nil {
					t.Fatal(err)
				}
			})
			r81carryDiffSpans(t, got, tc.want)
		})
	}
}

// legacy-open-telemetry.test.ts "LegacyOpenTelemetry integration with
// rerank" inline snapshots (spans in end order). TS rerank adds no
// user-agent header.
func TestR81carryLegacyRerankSnapshots(t *testing.T) {
	docs := []string{"sunny day at the beach", "rainy day in the city", "cloudy day in the mountains"}
	jsonDocs := []string{`"sunny day at the beach"`, `"rainy day in the city"`, `"cloudy day in the mountains"`}
	ranking := []string{`{"index":2,"relevanceScore":0.9}`, `{"index":0,"relevanceScore":0.8}`, `{"index":1,"relevanceScore":0.7}`}
	model := &testutil.MockRerankingModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoRerankFunc: func(context.Context, *provider.RerankOptions) (*types.RerankResult, error) {
		return &types.RerankResult{Ranking: []types.RerankItem{{Index: 2, RelevanceScore: 0.9}, {Index: 0, RelevanceScore: 0.8}, {Index: 1, RelevanceScore: 0.7}}}, nil
	}}
	span := func(name, functionID string, kv ...interface{}) map[string]interface{} {
		m := map[string]interface{}{
			"name":                   name,
			"ai.model.id":            "mock-model-id",
			"ai.model.provider":      "mock-provider",
			"ai.operationId":         name,
			"ai.settings.maxRetries": int64(2),
			"operation.name":         name,
		}
		if functionID != "" {
			m["ai.telemetry.functionId"] = functionID
			m["operation.name"] = name + " " + functionID
			m["resource.name"] = functionID
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	topN := 3
	for _, tc := range []struct {
		name       string
		functionID string
		recordIO   *bool
		want       []map[string]interface{}
	}{
		{
			name: "should record telemetry data when isEnabled is not explicitly set",
			want: []map[string]interface{}{
				span("ai.rerank.doRerank", "", "ai.documents", jsonDocs, "ai.ranking", ranking, "ai.ranking.type", "text"),
				span("ai.rerank", "", "ai.documents", jsonDocs),
			},
		},
		{
			name:       "should record telemetry data when enabled (single call path)",
			functionID: "test-function-id",
			want: []map[string]interface{}{
				span("ai.rerank.doRerank", "test-function-id", "ai.documents", jsonDocs, "ai.ranking", ranking, "ai.ranking.type", "text"),
				span("ai.rerank", "test-function-id", "ai.documents", jsonDocs),
			},
		},
		{
			name:     "should not record telemetry inputs / outputs when disabled",
			recordIO: telemetry.Bool(false),
			want: []map[string]interface{}{
				span("ai.rerank.doRerank", "", "ai.ranking.type", "text"),
				span("ai.rerank", ""),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r81carryLegacySpans(t, tc.functionID, tc.recordIO, func(settings *telemetry.Options) {
				if _, err := Rerank(context.Background(), RerankOptions{Model: model, Documents: docs, Query: "rainy day", TopN: &topN, Telemetry: settings}); err != nil {
					t.Fatal(err)
				}
			})
			r81carryDiffSpans(t, got, tc.want)
		})
	}
}

// open-telemetry.ts onEmbedOperationStart/End and onEmbedStart/End with every
// embedding-related supplemental option on, through EmbedMany: the root
// carries the base supplemental attributes (runtime context, headers) and
// ai.values / ai.embeddings; each provider span repeats the base
// supplemental attributes and carries its batch's values, embeddings and
// usage.
func TestR81carryOTelEmbedManySupplementalAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	it := telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("t"), Embedding: true, Headers: true, RuntimeContext: true, Usage: true})
	model := &batchEmbeddingModel{maxPerCall: 2, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		embeddings := make([][]float64, len(values))
		for i := range values {
			embeddings[i] = []float64{float64(i)}
		}
		return &types.EmbeddingsResult{Embeddings: embeddings, Usage: types.EmbeddingUsage{Tokens: float64(len(values)), Reported: true}}, nil
	}}
	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:          model,
		Inputs:         []string{"a", "b", "c"},
		Headers:        map[string]string{"x-request-id": "request-123"},
		RuntimeContext: map[string]interface{}{"userId": "user-123"},
		Telemetry: &telemetry.Options{
			IncludeRuntimeContext: map[string]bool{"userId": true},
			Integrations:          []telemetry.TelemetryIntegration{it},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]interface{}
	for _, s := range rec.Ended() {
		m := map[string]interface{}{"name": s.Name(), "kind": s.SpanKind().String()}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.request.headers.user-agent" {
				m[string(kv.Key)] = "UA"
				continue
			}
			m[string(kv.Key)] = kv.Value.AsInterface()
		}
		got = append(got, m)
	}
	child := func(values, embeddings []string, tokens int64) map[string]interface{} {
		return map[string]interface{}{
			"name": "embeddings mock-model", "kind": "client",
			"gen_ai.operation.name": "embeddings", "gen_ai.provider.name": "mock-provider", "gen_ai.request.model": "mock-model",
			"ai.request.headers.x-request-id": "request-123", "ai.request.headers.user-agent": "UA",
			"ai.settings.context.userId": "user-123",
			"ai.values":                  values, "ai.embeddings": embeddings, "gen_ai.usage.input_tokens": tokens,
		}
	}
	want := []map[string]interface{}{
		child([]string{`"a"`, `"b"`}, []string{"[0]", "[1]"}, 2),
		child([]string{`"c"`}, []string{"[0]"}, 1),
		{
			"name": "embeddings mock-model", "kind": "client",
			"gen_ai.operation.name": "embeddings", "gen_ai.provider.name": "mock-provider", "gen_ai.request.model": "mock-model",
			"ai.request.headers.x-request-id": "request-123", "ai.request.headers.user-agent": "UA",
			"ai.settings.context.userId": "user-123",
			"ai.values":                  []string{`"a"`, `"b"`, `"c"`}, "ai.embeddings": []string{"[0]", "[1]", "[0]"},
		},
	}
	r81carryDiffSpans(t, got, want)
}
