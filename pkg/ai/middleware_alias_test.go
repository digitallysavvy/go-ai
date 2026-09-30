package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestMiddlewareAliases_WrapLanguageModel(t *testing.T) {
	base := &testutil.MockLanguageModel{}
	temp := 0.7
	wrapped := WrapLanguageModel(
		base,
		[]*LanguageModelMiddleware{
			DefaultSettingsMiddleware(&provider.GenerateOptions{Temperature: &temp}),
		},
		nil,
		nil,
	)
	if wrapped == nil {
		t.Fatal("WrapLanguageModel() = nil")
	}
}

func TestMiddlewareAliases_WrapProvider(t *testing.T) {
	p := &testutil.MockProvider{}
	// The 3-argument call shape must keep compiling unchanged (no breaking
	// change from adding image model middleware support).
	wrapped := WrapProvider(p, []*LanguageModelMiddleware{}, []*EmbeddingModelMiddleware{})
	if wrapped == nil {
		t.Fatal("WrapProvider() = nil")
	}
}

func TestMiddlewareAliases_WrapProvider_WithImageModelMiddleware(t *testing.T) {
	p := &testutil.MockProvider{}
	called := false
	imgMiddleware := &ImageModelMiddleware{
		OverrideModelID: func(model provider.ImageModel) string {
			called = true
			return "overridden"
		},
	}
	wrapped := WrapProvider(p, nil, nil, WithImageModelMiddleware([]*ImageModelMiddleware{imgMiddleware}))
	if wrapped == nil {
		t.Fatal("WrapProvider() = nil")
	}
	model, err := wrapped.ImageModel("test-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ModelID() != "overridden" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "overridden")
	}
	if !called {
		t.Error("expected OverrideModelID to be called")
	}
}
