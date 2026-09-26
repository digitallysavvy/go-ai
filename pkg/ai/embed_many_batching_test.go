package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Tests ported from packages/ai/src/embed/embed-many.test.ts,
// embed-many-google.test.ts and embed.test.ts (ai@7.0.113).

type batchEmbeddingModel struct {
	maxPerCall  int
	maxBytes    int
	parallel    bool
	doEmbedMany func(ctx context.Context, values []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error)
	transform   func(input provider.EmbeddingProviderOptionsTransformInput) (map[string]interface{}, error)

	mu       sync.Mutex
	calls    [][]string
	callOpts []map[string]interface{}
}

func (m *batchEmbeddingModel) SpecificationVersion() string { return "v4" }
func (m *batchEmbeddingModel) Provider() string             { return "mock-provider" }
func (m *batchEmbeddingModel) ModelID() string              { return "mock-model" }
func (m *batchEmbeddingModel) MaxEmbeddingsPerCall() int    { return m.maxPerCall }
func (m *batchEmbeddingModel) SupportsParallelCalls() bool  { return m.parallel }
func (m *batchEmbeddingModel) MaxInputBytesPerCall() int    { return m.maxBytes }

func (m *batchEmbeddingModel) TransformEmbeddingProviderOptions(_ context.Context, input provider.EmbeddingProviderOptionsTransformInput) (map[string]interface{}, error) {
	if m.transform == nil {
		return input.ProviderOptions, nil
	}
	return m.transform(input)
}

func (m *batchEmbeddingModel) DoEmbed(ctx context.Context, input string, opts *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
	res, err := m.DoEmbedMany(ctx, []string{input}, opts)
	if err != nil {
		return nil, err
	}
	out := &types.EmbeddingResult{Usage: res.Usage, Warnings: res.Warnings}
	if len(res.Embeddings) > 0 {
		out.Embedding = res.Embeddings[0]
	}
	return out, nil
}

func (m *batchEmbeddingModel) DoEmbedMany(ctx context.Context, values []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	m.mu.Lock()
	m.calls = append(m.calls, append([]string(nil), values...))
	var po map[string]interface{}
	if opts != nil {
		po = opts.ProviderOptions
	}
	m.callOpts = append(m.callOpts, po)
	m.mu.Unlock()
	if m.doEmbedMany != nil {
		return m.doEmbedMany(ctx, values, opts)
	}
	embeddings := make([][]float64, len(values))
	for i, v := range values {
		embeddings[i] = []float64{float64(len([]rune(v)))}
	}
	return &types.EmbeddingsResult{Embeddings: embeddings}, nil
}

func (m *batchEmbeddingModel) recordedCalls() [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]string(nil), m.calls...)
}

var testEmbedValues = []string{
	"sunny day at the beach",
	"rainy afternoon in the city",
	"snowy night in the mountains",
}

var dummyEmbeddings = [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}, {0.7, 0.8, 0.9}}

func TestEmbedMany_EmbeddingCountMismatch(t *testing.T) {
	t.Parallel()
	// TS: "should reject an embedding count mismatch in a single call" / "... in each chunk"
	tests := []struct {
		name       string
		maxPerCall int
		fn         func(values []string) [][]float64
		wantMsg    string
	}{
		{"single call", 0, func([]string) [][]float64 { return dummyEmbeddings[:2] }, "Expected 3 embeddings, but received 2."},
		{"each chunk", 2, func(values []string) [][]float64 {
			out := make([][]float64, 0, len(values))
			for range values {
				out = append(out, dummyEmbeddings[0])
			}
			return out[:len(out)-1]
		}, "Expected 2 embeddings, but received 1."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &batchEmbeddingModel{maxPerCall: tt.maxPerCall, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
				return &types.EmbeddingsResult{Embeddings: tt.fn(values)}, nil
			}}
			_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues})
			var invalid *providererrors.InvalidResponseDataError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected InvalidResponseDataError, got %v", err)
			}
			if invalid.Error() != tt.wantMsg {
				t.Fatalf("message = %q, want %q", invalid.Error(), tt.wantMsg)
			}
		})
	}
}

// concurrencyModel tracks the maximum number of concurrent DoEmbedMany calls.
func concurrencyModel(parallel bool, hold time.Duration) (*batchEmbeddingModel, func() int) {
	var mu sync.Mutex
	active, maxActive := 0, 0
	model := &batchEmbeddingModel{maxPerCall: 1, parallel: parallel}
	model.doEmbedMany = func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(hold)
		mu.Lock()
		active--
		mu.Unlock()
		idx := 0
		for i, v := range testEmbedValues {
			if v == values[0] {
				idx = i
			}
		}
		return &types.EmbeddingsResult{Embeddings: [][]float64{dummyEmbeddings[idx]}}, nil
	}
	return model, func() int { mu.Lock(); defer mu.Unlock(); return maxActive }
}

func TestEmbedMany_SupportsParallelCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		parallel         bool
		maxParallelCalls int
		wantMaxActive    int
	}{
		{"should not parallelize when false", false, 0, 1},
		{"should parallelize when true", true, 0, 3},
		{"should support maxParallelCalls", true, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, maxActive := concurrencyModel(tt.parallel, 30*time.Millisecond)
			res, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, MaxParallelCalls: tt.maxParallelCalls})
			if err != nil {
				t.Fatal(err)
			}
			if got := maxActive(); got != tt.wantMaxActive {
				t.Fatalf("max concurrent calls = %d, want %d", got, tt.wantMaxActive)
			}
			if !reflect.DeepEqual(res.Embeddings, dummyEmbeddings) {
				t.Fatalf("embeddings out of order: %v", res.Embeddings)
			}
			if len(res.Responses) != 3 {
				t.Fatalf("responses = %d, want 3", len(res.Responses))
			}
		})
	}
}

func TestEmbedMany_InvalidMaxParallelCalls(t *testing.T) {
	t.Parallel()
	// TS: "should throw InvalidArgumentError when maxParallelCalls is -1"
	// (Go: 0 is the unset default and means unlimited.)
	model := &batchEmbeddingModel{maxPerCall: 1, parallel: true}
	_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, MaxParallelCalls: -1})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "chunkSize" || invalid.Message != "chunkSize must be greater than 0" {
		t.Fatalf("expected chunkSize InvalidArgumentError, got %v", err)
	}
	if len(model.recordedCalls()) != 0 {
		t.Fatal("no model call expected")
	}
}

func TestEmbedMany_Splitting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		maxPerCall int
		maxBytes   int
		values     []string
		wantCalls  [][]string
	}{
		{"should generate embeddings when several calls are required", 2, 0, testEmbedValues,
			[][]string{testEmbedValues[:2], testEmbedValues[2:]}},
		{"should split calls when the UTF-8 input byte budget is exceeded", 5, 7, []string{"éé", "éé", "abc"},
			[][]string{{"éé"}, {"éé", "abc"}}},
		{"should split by input bytes without an embedding count limit", 0, 4, []string{"ab", "cd", "é"},
			[][]string{{"ab", "cd"}, {"é"}}},
		{"should combine embedding count and input byte limits in one pass", 3, 10, []string{"12345678", "12", "12", "12345678"},
			[][]string{{"12345678", "12"}, {"12", "12345678"}}},
		{"should treat an unlimited input byte budget as unlimited", 0, 0, []string{"a", "b"},
			[][]string{{"a", "b"}}},
		{"should send a value larger than the input byte budget by itself", 0, 3, []string{"abcd", "e"},
			[][]string{{"abcd"}, {"e"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := &batchEmbeddingModel{maxPerCall: tt.maxPerCall, maxBytes: tt.maxBytes}
			res, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: tt.values})
			if err != nil {
				t.Fatal(err)
			}
			if got := model.recordedCalls(); !reflect.DeepEqual(got, tt.wantCalls) {
				t.Fatalf("calls = %q, want %q", got, tt.wantCalls)
			}
			if len(res.Embeddings) != len(tt.values) {
				t.Fatalf("embeddings = %d, want %d", len(res.Embeddings), len(tt.values))
			}
			for i, v := range tt.values {
				if res.Embeddings[i][0] != float64(len([]rune(v))) {
					t.Fatalf("embedding %d misaligned: %v", i, res.Embeddings[i])
				}
			}
		})
	}
}

func TestEmbedMany_AggregatesChunkResults(t *testing.T) {
	t.Parallel()
	// TS: "should include responses in the result", "should include usage in the result",
	// "should aggregate warnings from multiple calls", provider metadata merge.
	call := 0
	var mu sync.Mutex
	model := &batchEmbeddingModel{maxPerCall: 2, doEmbedMany: func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		mu.Lock()
		call++
		n := call
		mu.Unlock()
		embeddings := make([][]float64, len(values))
		for i := range values {
			embeddings[i] = []float64{float64(n)}
		}
		return &types.EmbeddingsResult{
			Embeddings:       embeddings,
			Usage:            types.EmbeddingUsage{Tokens: 10, InputTokens: 10, TotalTokens: 10},
			Warnings:         []types.Warning{{Type: "other", Message: "w" + string(rune('0'+n))}},
			Responses:        []types.EmbeddingResponse{{Headers: map[string]string{"call": string(rune('0' + n))}}},
			ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"call": n, "first" + string(rune('0'+n)): true}},
		}, nil
	}}
	var endEvent EmbedOnFinishEvent
	res, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:             model,
		Inputs:            testEmbedValues,
		ExperimentalOnEnd: func(e EmbedOnFinishEvent) { endEvent = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Tokens != 20 || res.Usage.TotalTokens != 20 || res.Usage.InputTokens != 20 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Warnings) != 2 || res.Warnings[0].Message != "w1" || res.Warnings[1].Message != "w2" {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if len(res.Responses) != 2 || res.Responses[0].Headers["call"] != "1" || res.Responses[1].Headers["call"] != "2" {
		t.Fatalf("responses = %+v", res.Responses)
	}
	want := map[string]interface{}{"p": map[string]interface{}{"call": 2, "first1": true, "first2": true}}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("provider metadata = %#v", res.ProviderMetadata)
	}
	if len(endEvent.Responses) != 2 || endEvent.Usage.TotalTokens != 20 {
		t.Fatalf("end event = %+v", endEvent)
	}
}

func TestEmbedMany_RetriesFailedBatchOnly(t *testing.T) {
	t.Parallel()
	// TS embed-many-google.test.ts: "retries a failed batch with the same content
	// without repeating successful batches".
	var mu sync.Mutex
	failed := false
	model := &batchEmbeddingModel{maxPerCall: 2}
	model.doEmbedMany = func(_ context.Context, values []string, _ *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if values[0] == testEmbedValues[2] && !failed {
			failed = true
			pe := providererrors.NewProviderError("mock", 429, "RESOURCE_EXHAUSTED", "Rate limited", nil)
			pe.ResponseHeaders = map[string]string{"retry-after-ms": "0"}
			return nil, pe
		}
		out := make([][]float64, len(values))
		for i := range values {
			out[i] = []float64{1}
		}
		return &types.EmbeddingsResult{Embeddings: out}, nil
	}
	res, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{testEmbedValues[:2], testEmbedValues[2:], testEmbedValues[2:]}
	if got := model.recordedCalls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if len(res.Responses) != 2 {
		t.Fatalf("responses = %d, want 2", len(res.Responses))
	}
}

func TestEmbedMany_NonRetryableErrorsAreNotRetried(t *testing.T) {
	t.Parallel()
	model := &batchEmbeddingModel{doEmbedMany: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		return nil, providererrors.NewProviderError("mock", 400, "bad", "bad request", nil)
	}}
	_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, MaxRetries: 2})
	if err == nil || len(model.recordedCalls()) != 1 {
		t.Fatalf("expected one call and an error, got %d calls, err=%v", len(model.recordedCalls()), err)
	}
	if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: testEmbedValues, MaxRetries: -1}); err == nil {
		t.Fatal("negative MaxRetries must be rejected")
	}
}

func TestEmbedMany_ProviderOptionsTransformerAlignment(t *testing.T) {
	t.Parallel()
	// TS embed-many-google.test.ts: "preserves content alignment through wrapped
	// models with maxParallelCalls %s" and "rejects mismatched content before
	// making any requests".
	values := make([]string, 5)
	content := []interface{}{"c0", nil, "c2", "c3", nil}
	newModel := func() *batchEmbeddingModel {
		return &batchEmbeddingModel{maxPerCall: 2, parallel: true, transform: func(in provider.EmbeddingProviderOptionsTransformInput) (map[string]interface{}, error) {
			c := in.ProviderOptions["mock"].(map[string]interface{})["content"].([]interface{})
			if len(c) != len(in.Values) {
				return nil, errors.New("content length mismatch")
			}
			return map[string]interface{}{"mock": map[string]interface{}{"content": c[in.StartIndex:in.EndIndex]}}, nil
		}}
	}
	for _, parallel := range []int{1, 2, 0} {
		model := newModel()
		wrapped := middleware.WrapEmbeddingModel(model, []*middleware.EmbeddingModelMiddleware{{SpecificationVersion: "v4"}}, nil, nil)
		_, err := EmbedMany(context.Background(), EmbedManyOptions{
			Model:            wrapped,
			Inputs:           values,
			MaxParallelCalls: parallel,
			ProviderOptions:  map[string]interface{}{"mock": map[string]interface{}{"content": content}},
		})
		if err != nil {
			t.Fatal(err)
		}
		model.mu.Lock()
		var got []interface{}
		for i, call := range model.calls {
			sliced := model.callOpts[i]["mock"].(map[string]interface{})["content"].([]interface{})
			if len(sliced) != len(call) {
				t.Fatalf("batch %d misaligned: %d values, %d content", i, len(call), len(sliced))
			}
			got = append(got, sliced...)
		}
		model.mu.Unlock()
		// Parallel batches may record calls out of order; compare as a multiset
		// via length and per-batch alignment above, and exact order when sequential.
		if parallel == 1 && !reflect.DeepEqual(got, content) {
			t.Fatalf("content = %v, want %v", got, content)
		}
		if len(got) != len(content) {
			t.Fatalf("content entries = %d, want %d", len(got), len(content))
		}
	}

	model := newModel()
	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:           model,
		Inputs:          values,
		ProviderOptions: map[string]interface{}{"mock": map[string]interface{}{"content": content[:4]}},
	})
	if err == nil || len(model.recordedCalls()) != 0 {
		t.Fatalf("expected an error before any request, got err=%v calls=%d", err, len(model.recordedCalls()))
	}
}

func TestEmbeddingMiddlewareForwardsCapabilities(t *testing.T) {
	t.Parallel()
	model := &batchEmbeddingModel{maxBytes: 300_000}
	wrapped := middleware.WrapEmbeddingModel(model, []*middleware.EmbeddingModelMiddleware{{}}, nil, nil)
	if m, ok := wrapped.(provider.EmbeddingModelMaxInputBytesPerCall); !ok || m.MaxInputBytesPerCall() != 300_000 {
		t.Fatal("wrapped model must forward MaxInputBytesPerCall")
	}
	if _, ok := wrapped.(provider.EmbeddingModelProviderOptionsTransformer); !ok {
		t.Fatal("wrapped model must forward the provider options transformer")
	}
}

func TestEmbed_RejectsEmptyEmbedding(t *testing.T) {
	t.Parallel()
	// TS embed.test.ts: "should reject when the model returns no embeddings"
	capture := &telemetryCapture{}
	endCalled := false
	model := &batchEmbeddingModel{doEmbedMany: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
		return &types.EmbeddingsResult{Embeddings: [][]float64{}, Usage: types.EmbeddingUsage{Tokens: 5}}, nil
	}}
	_, err := Embed(context.Background(), EmbedOptions{
		Model:             model,
		Input:             "sunny day at the beach",
		Telemetry:         &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{capture}},
		ExperimentalOnEnd: func(EmbedOnFinishEvent) { endCalled = true },
	})
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) || invalid.Error() != "No embedding generated." {
		t.Fatalf("expected InvalidResponseDataError, got %v", err)
	}
	if endCalled || len(model.recordedCalls()) != 1 {
		t.Fatalf("onEnd called=%v calls=%d", endCalled, len(model.recordedCalls()))
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.finishes) != 0 {
		t.Fatal("telemetry finish must not fire on error")
	}
}

func TestEmbedAndEmbedMany_RuntimeContext(t *testing.T) {
	t.Parallel()
	// TS db59d78: runtime context attribution for embed/embedMany. Callbacks get
	// the full context; telemetry only the included keys.
	runtimeCtx := map[string]interface{}{"tenant": "acme", "secret": "x"}
	for _, many := range []bool{false, true} {
		capture := &telemetryCapture{}
		settings := &TelemetrySettings{
			IsEnabled:             telemetry.Bool(true),
			Integrations:          []telemetry.TelemetryIntegration{capture},
			IncludeRuntimeContext: map[string]bool{"tenant": true},
		}
		var start EmbedOnStartEvent
		var end EmbedOnFinishEvent
		model := &batchEmbeddingModel{}
		var err error
		if many {
			_, err = EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a"}, RuntimeContext: runtimeCtx, Telemetry: settings,
				ExperimentalOnStart: func(e EmbedOnStartEvent) { start = e }, ExperimentalOnEnd: func(e EmbedOnFinishEvent) { end = e }})
		} else {
			_, err = Embed(context.Background(), EmbedOptions{Model: model, Input: "a", RuntimeContext: runtimeCtx, Telemetry: settings,
				ExperimentalOnStart: func(e EmbedOnStartEvent) { start = e }, ExperimentalOnEnd: func(e EmbedOnFinishEvent) { end = e }})
		}
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(start.RuntimeContext, runtimeCtx) || !reflect.DeepEqual(end.RuntimeContext, runtimeCtx) {
			t.Fatalf("callbacks must receive the full runtime context: %v / %v", start.RuntimeContext, end.RuntimeContext)
		}
		capture.mu.Lock()
		if len(capture.starts) != 1 || !reflect.DeepEqual(capture.starts[0].RuntimeContext, map[string]interface{}{"tenant": "acme"}) {
			t.Fatalf("telemetry runtime context must be filtered: %+v", capture.starts)
		}
		capture.mu.Unlock()
	}
}
