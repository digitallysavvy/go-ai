package registry

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Covers A2-5: pkg/registry.Registry gaining TypeScript's
// createProviderRegistry(providers, {separator, languageModelMiddleware,
// imageModelMiddleware}) equivalents.

func TestRegistry_DefaultSeparator(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai:gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ModelID() != "gpt-5" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "gpt-5")
	}
}

func TestRegistry_WithSeparator_CustomChar(t *testing.T) {
	t.Parallel()

	r := NewRegistry(WithSeparator("|"))
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai|gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ModelID() != "gpt-5" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "gpt-5")
	}

	// The default separator should no longer be recognized.
	if _, err := r.ResolveLanguageModel("openai:gpt-5"); err == nil {
		t.Error("expected error resolving with default separator after WithSeparator")
	}
}

func TestRegistry_WithSeparator_MultiCharSeparator(t *testing.T) {
	t.Parallel()

	r := NewRegistry(WithSeparator("::"))
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai::gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model.ModelID() != "gpt-5" {
		t.Errorf("ModelID() = %q, want %q", model.ModelID(), "gpt-5")
	}
}

func TestRegistry_WithLanguageModelMiddleware_WrapsResolvedModel(t *testing.T) {
	t.Parallel()

	called := false
	mw := &middleware.LanguageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
			called = true
			return doGenerate()
		},
	}

	r := NewRegistry(WithLanguageModelMiddleware(mw))
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai:gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("expected language model middleware to be applied to registry-resolved model")
	}
}

func TestRegistry_NoLanguageModelMiddleware_ReturnsUnwrapped(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai:gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := model.(*testutil.MockLanguageModel); !ok {
		t.Errorf("expected unwrapped *testutil.MockLanguageModel, got %T", model)
	}
}

func TestRegistry_WithImageModelMiddleware_WrapsResolvedModel(t *testing.T) {
	t.Parallel()

	called := false
	mw := &middleware.ImageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.ImageResult, error), params *provider.ImageGenerateOptions, model provider.ImageModel) (*types.ImageResult, error) {
			called = true
			return doGenerate()
		},
	}

	r := NewRegistry(WithImageModelMiddleware(mw))
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveImageModel("openai:dall-e-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("expected image model middleware to be applied to registry-resolved model")
	}
}

func TestRegistry_NoImageModelMiddleware_ReturnsUnwrapped(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveImageModel("openai:dall-e-3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := model.(*testutil.MockImageModel); !ok {
		t.Errorf("expected unwrapped *testutil.MockImageModel, got %T", model)
	}
}

func TestRegistry_WithSeparatorAndMiddleware_Combined(t *testing.T) {
	t.Parallel()

	called := false
	mw := &middleware.LanguageModelMiddleware{
		WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
			called = true
			return doGenerate()
		},
	}

	r := NewRegistry(WithSeparator("/"), WithLanguageModelMiddleware(mw))
	r.RegisterProvider("openai", &testutil.MockProvider{ProviderName: "openai"})

	model, err := r.ResolveLanguageModel("openai/gpt-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected middleware to be applied when combined with a custom separator")
	}
}
