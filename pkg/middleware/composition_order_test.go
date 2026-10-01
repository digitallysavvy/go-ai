package middleware

import (
	"context"
	"slices"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestWrapLanguageModel_CompositionOrder(t *testing.T) {
	order := []string{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			order = append(order, "model")
			in, out, total := int64(1), int64(1), int64(2)
			return &types.GenerateResult{
				Text:         "ok",
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total},
			}, nil
		},
	}

	mw1 := &LanguageModelMiddleware{
		TransformParams: func(_ context.Context, callType string, params *provider.GenerateOptions, _ provider.LanguageModel) (*provider.GenerateOptions, error) {
			order = append(order, "mw1-transform-"+callType)
			return params, nil
		},
		WrapGenerate: func(_ context.Context, doGenerate func() (*types.GenerateResult, error), _ func() (provider.TextStream, error), _ *provider.GenerateOptions, _ provider.LanguageModel) (*types.GenerateResult, error) {
			order = append(order, "mw1-wrap-before")
			r, err := doGenerate()
			order = append(order, "mw1-wrap-after")
			return r, err
		},
	}
	mw2 := &LanguageModelMiddleware{
		TransformParams: func(_ context.Context, callType string, params *provider.GenerateOptions, _ provider.LanguageModel) (*provider.GenerateOptions, error) {
			order = append(order, "mw2-transform-"+callType)
			return params, nil
		},
		WrapGenerate: func(_ context.Context, doGenerate func() (*types.GenerateResult, error), _ func() (provider.TextStream, error), _ *provider.GenerateOptions, _ provider.LanguageModel) (*types.GenerateResult, error) {
			order = append(order, "mw2-wrap-before")
			r, err := doGenerate()
			order = append(order, "mw2-wrap-after")
			return r, err
		},
	}

	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{mw1, mw2}, nil, nil)
	_, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	want := []string{
		"mw1-transform-generate",
		"mw1-wrap-before",
		"mw2-transform-generate",
		"mw2-wrap-before",
		"model",
		"mw2-wrap-after",
		"mw1-wrap-after",
	}
	if !slices.Equal(order, want) {
		t.Fatalf("composition order mismatch\n got: %v\nwant: %v", order, want)
	}
}

func TestWrapEmbeddingModel_CompositionOrder(t *testing.T) {
	order := []string{}
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(_ context.Context, input string, _ *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			order = append(order, "model:"+input)
			return &types.EmbeddingResult{Embedding: []float64{1}}, nil
		},
	}

	mw1 := &EmbeddingModelMiddleware{
		TransformInput: func(_ context.Context, input string, _ provider.EmbeddingModel) (string, error) {
			order = append(order, "mw1-transform:"+input)
			return input + "-1", nil
		},
		WrapEmbed: func(_ context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, _ provider.EmbeddingModel) (*types.EmbeddingResult, error) {
			order = append(order, "mw1-wrap-before:"+input)
			r, err := doEmbed()
			order = append(order, "mw1-wrap-after")
			return r, err
		},
	}
	mw2 := &EmbeddingModelMiddleware{
		TransformInput: func(_ context.Context, input string, _ provider.EmbeddingModel) (string, error) {
			order = append(order, "mw2-transform:"+input)
			return input + "-2", nil
		},
		WrapEmbed: func(_ context.Context, doEmbed func() (*types.EmbeddingResult, error), input string, _ provider.EmbeddingModel) (*types.EmbeddingResult, error) {
			order = append(order, "mw2-wrap-before:"+input)
			r, err := doEmbed()
			order = append(order, "mw2-wrap-after")
			return r, err
		},
	}

	wrapped := WrapEmbeddingModel(model, []*EmbeddingModelMiddleware{mw1, mw2}, nil, nil)
	_, err := wrapped.DoEmbed(context.Background(), "base", &provider.EmbedModelOptions{})
	if err != nil {
		t.Fatalf("DoEmbed() error = %v", err)
	}

	want := []string{
		"mw1-transform:base",
		"mw1-wrap-before:base-1",
		"mw2-transform:base-1",
		"mw2-wrap-before:base-1-2",
		"model:base-1-2",
		"mw2-wrap-after",
		"mw1-wrap-after",
	}
	if !slices.Equal(order, want) {
		t.Fatalf("composition order mismatch\n got: %v\nwant: %v", order, want)
	}
}
