package xai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseProviderOptions(xaiLanguageModelResponsesOptions).
func TestResponsesProviderOptionsValidation(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	m, _ := p.LanguageModel("grok-4.20-reasoning")
	bad := []map[string]interface{}{
		{"reasoningEffort": "minimal"}, {"reasoningSummary": "short"}, {"logprobs": "yes"}, {"topLogprobs": 9},
		{"topLogprobs": 1.5}, {"minP": 2}, {"maxTurns": 1.5}, {"serviceTier": "flex"}, {"store": nil},
		{"include": []interface{}{"bogus"}}, {"user": 1}, {"previousResponseId": 2},
	}
	for _, o := range bad {
		for _, stream := range []bool{false, true} {
			var err error
			gen := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{"xai": o}}
			if stream {
				_, err = m.DoStream(context.Background(), gen)
			} else {
				_, err = m.DoGenerate(context.Background(), gen)
			}
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) {
				t.Errorf("%v (stream=%v): error = %v, want InvalidArgumentError", o, stream, err)
			}
		}
	}
	for _, o := range []map[string]interface{}{
		{}, {"reasoningEffort": "xhigh", "topLogprobs": 8, "minP": 0.5, "include": nil, "serviceTier": "priority"},
		{"include": []interface{}{"reasoning.encrypted_content"}, "store": false},
	} {
		if err := validateResponsesOptions(map[string]interface{}{"xai": o}); err != nil {
			t.Errorf("%v rejected: %v", o, err)
		}
	}
	if err := validateResponsesOptions(map[string]interface{}{"xai": XAIResponsesProviderOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}
