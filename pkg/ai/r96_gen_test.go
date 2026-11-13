package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS DecisionLanguageModel parses the model text with safeParseJSON
// (secureJsonParse): a __proto__ key fails the parse, so the decision fails
// with "Decision did not return valid JSON." rather than the value-count check.
func TestR96genDecisionLanguageModelForbiddenPrototype(t *testing.T) {
	for _, text := range []string{
		`{"q0":0.5,"__proto__":{}}`,
		`{"q0":0.5,"constructor":{"prototype":{}}}`,
	} {
		lm := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		}}
		model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: lm})
		if err != nil {
			t.Fatal(err)
		}
		_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
			State:     "s",
			Questions: map[string]provider.DecisionQuestion{"a": {Type: "boolean", Instructions: "?"}},
		})
		var invalid *providererrors.InvalidResponseDataError
		if !errors.As(err, &invalid) || invalid.Message != "Decision did not return valid JSON." {
			t.Fatalf("%s: err = %v, want Decision did not return valid JSON.", text, err)
		}
		if invalid.Data != text {
			t.Fatalf("%s: data = %#v, want the text", text, invalid.Data)
		}
	}
}

// TS parseAndValidateObjectResult: safeParseJSON rejects a __proto__ key, so
// generateObject fails with "could not parse the response" and a
// JSONParseError cause, before any schema validation.
func TestR96genGenerateObjectForbiddenPrototype(t *testing.T) {
	text := `{"name":"x","__proto__":{"admin":true}}`
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		},
	}
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  model,
		Prompt: "p",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		}),
	})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) || noObj.Message != "No object generated: could not parse the response." {
		t.Fatalf("err = %v, want could not parse the response", err)
	}
	var parseErr *providererrors.JSONParseError
	if !errors.As(noObj.Cause, &parseErr) || !errors.Is(noObj.Cause, securejson.ErrForbiddenPrototype) {
		t.Fatalf("cause = %#v, want a JSONParseError wrapping the forbidden prototype error", noObj.Cause)
	}
}
