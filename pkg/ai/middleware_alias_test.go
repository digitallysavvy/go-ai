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
	wrapped := WrapProvider(p, []*LanguageModelMiddleware{}, []*EmbeddingModelMiddleware{})
	if wrapped == nil {
		t.Fatal("WrapProvider() = nil")
	}
}
