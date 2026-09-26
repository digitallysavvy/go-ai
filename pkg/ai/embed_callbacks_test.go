package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestEmbedOnStartCallbackFired verifies that ExperimentalOnStart is called
// with correct fields before the embedding model is invoked.
func TestEmbedOnStartCallbackFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *EmbedOnStartEvent

	result, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello world",
		ExperimentalOnStart: func(e EmbedOnStartEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if e.OperationID != "ai.embed" {
		t.Errorf("OperationID = %q, want %q", e.OperationID, "ai.embed")
	}
	if e.Provider != "test-provider" {
		t.Errorf("Provider = %q, want %q", e.Provider, "test-provider")
	}
	if e.ModelID != "test-model" {
		t.Errorf("ModelID = %q, want %q", e.ModelID, "test-model")
	}
	if len(e.Values) != 1 || e.Values[0] != "hello world" {
		t.Errorf("Values = %v, want [hello world]", e.Values)
	}
	if e.Ctx == nil {
		t.Error("Ctx should not be nil")
	}
}

// TestEmbedOnFinishCallbackFired verifies that ExperimentalOnFinish is called
// with embeddings and usage after the model returns.
func TestEmbedOnFinishCallbackFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *EmbedOnFinishEvent

	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello",
		ExperimentalOnFinish: func(e EmbedOnFinishEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnFinish was not called")
	}
	if e.OperationID != "ai.embed" {
		t.Errorf("OperationID = %q, want %q", e.OperationID, "ai.embed")
	}
	if len(e.Embeddings) != 1 {
		t.Errorf("Embeddings count = %d, want 1", len(e.Embeddings))
	}
	if len(e.Embeddings[0]) == 0 {
		t.Error("expected non-empty embedding vector")
	}
	if e.Usage.TotalTokens == 0 {
		t.Error("expected non-zero usage tokens")
	}
}

// TestEmbedOnStartHeadersAndMaxRetries verifies that MaxRetries and Headers from
// EmbedOptions are forwarded to the EmbedOnStartEvent.
func TestEmbedOnStartHeadersAndMaxRetries(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *EmbedOnStartEvent

	_, err := Embed(context.Background(), EmbedOptions{
		Model:      model,
		Input:      "hello",
		MaxRetries: intPtr(3),
		Headers:    map[string]string{"X-Custom": "value"},
		ExperimentalOnStart: func(e EmbedOnStartEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if e.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", e.MaxRetries)
	}
	// version.WithUserAgentSuffix normalizes header keys to lowercase, mirroring
	// TS withUserAgentSuffix's use of the WHATWG Headers class (which lowercases
	// header names) before adding the "ai/<version>" user-agent suffix.
	if e.Headers["x-custom"] != "value" {
		t.Errorf("Headers[x-custom] = %q, want %q", e.Headers["x-custom"], "value")
	}
	if !strings.Contains(e.Headers["user-agent"], "go-ai/") {
		t.Errorf("Headers[user-agent] = %q, want it to contain go-ai/<version>", e.Headers["user-agent"])
	}
}

// TestEmbedOnFinishWarningsForwarded verifies that provider warnings are
// propagated through to EmbedOnFinishEvent.Warnings.
func TestEmbedOnFinishWarningsForwarded(t *testing.T) {
	t.Parallel()

	warn := types.Warning{Type: "unsupported-setting", Message: "dimensions ignored"}
	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
		DoEmbedFunc: func(_ context.Context, _ string, _ *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{
				Embedding: []float64{0.1, 0.2, 0.3},
				Usage:     types.EmbeddingUsage{TotalTokens: 5},
				Warnings:  []types.Warning{warn},
			}, nil
		},
	}

	var mu sync.Mutex
	var captured *EmbedOnFinishEvent

	_, err := Embed(context.Background(), EmbedOptions{
		Model: model,
		Input: "hello",
		ExperimentalOnFinish: func(e EmbedOnFinishEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnFinish was not called")
	}
	if len(e.Warnings) != 1 {
		t.Fatalf("Warnings count = %d, want 1", len(e.Warnings))
	}
	if e.Warnings[0].Type != warn.Type || e.Warnings[0].Message != warn.Message {
		t.Errorf("Warnings[0] = %+v, want %+v", e.Warnings[0], warn)
	}
}

// TestEmbedManyOnStartCallbackFired verifies that EmbedMany also fires
// ExperimentalOnStart with all input values.
func TestEmbedManyOnStartCallbackFired(t *testing.T) {
	t.Parallel()

	model := &testutil.MockEmbeddingModel{
		ProviderName: "test-provider",
		ModelName:    "test-model",
	}

	var mu sync.Mutex
	var captured *EmbedOnStartEvent

	inputs := []string{"one", "two", "three"}
	_, err := EmbedMany(context.Background(), EmbedManyOptions{
		Model:  model,
		Inputs: inputs,
		ExperimentalOnStart: func(e EmbedOnStartEvent) {
			mu.Lock()
			ev := e
			captured = &ev
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("EmbedMany failed: %v", err)
	}

	mu.Lock()
	e := captured
	mu.Unlock()

	if e == nil {
		t.Fatal("ExperimentalOnStart was not called")
	}
	if e.OperationID != "ai.embedMany" {
		t.Errorf("OperationID = %q, want %q", e.OperationID, "ai.embedMany")
	}
	if len(e.Values) != len(inputs) {
		t.Errorf("Values count = %d, want %d", len(e.Values), len(inputs))
	}
	for i, v := range inputs {
		if e.Values[i] != v {
			t.Errorf("Values[%d] = %q, want %q", i, e.Values[i], v)
		}
	}
}
