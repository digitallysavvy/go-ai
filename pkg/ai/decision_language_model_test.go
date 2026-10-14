package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func mixedDecisionQuestions() map[string]provider.DecisionQuestion {
	return map[string]provider.DecisionQuestion{
		"topic": {
			Type:         "choice",
			Instructions: "Team?",
			Criteria:     map[string]interface{}{"billing": nil, "support": "help related"},
		},
		"severity": {
			Type:         "score",
			Instructions: "Severity?",
			Criteria:     []interface{}{"Low", "Medium", "High"},
		},
		"refund": {
			Type:         "boolean",
			Instructions: "Refund?",
		},
	}
}

// TS: decision-language-model.test.ts — verifies reasoning defaults to
// "none" and the JSON schema / prompt are built as expected, then decodes a
// well-formed structured response.
func TestDecisionLanguageModel_DoDecide_MixedQuestions(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if opts.Reasoning == nil || *opts.Reasoning != types.ReasoningNone {
				t.Fatalf("Reasoning = %v, want ReasoningNone", opts.Reasoning)
			}
			if opts.ResponseFormat == nil || opts.ResponseFormat.Type != "json" {
				t.Fatalf("ResponseFormat = %+v", opts.ResponseFormat)
			}
			schema, _ := opts.ResponseFormat.Schema.(map[string]interface{})
			required, _ := schema["required"].([]string)
			if len(required) != 3 {
				t.Fatalf("required = %v, want 3 entries", required)
			}

			// Figure out which internal key maps to which question so the
			// response can be constructed generically regardless of sort order.
			properties, _ := schema["properties"].(map[string]interface{})
			values := map[string]interface{}{}
			for key, propRaw := range properties {
				prop, _ := propRaw.(map[string]interface{})
				switch prop["type"] {
				case "string":
					enum, _ := prop["enum"].([]string)
					if len(enum) == 0 {
						t.Fatalf("choice property missing enum: %+v", prop)
					}
					values[key] = enum[0] // pick the first option deterministically
				case "number":
					values[key] = 0.5
				}
			}
			text, err := json.Marshal(values)
			if err != nil {
				t.Fatalf("marshal response: %v", err)
			}
			inputTokens, outputTokens := int64(10), int64(4)
			return &types.GenerateResult{
				Text:         string(text),
				FinishReason: types.FinishReasonStop,
				Usage:        types.Usage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
			}, nil
		},
	}

	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	if model.Provider() != "test.decision" {
		t.Fatalf("Provider() = %q, want test.decision", model.Provider())
	}
	if model.ModelID() != "test-model" {
		t.Fatalf("ModelID() = %q, want test-model", model.ModelID())
	}

	result, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "some state",
		Questions: mixedDecisionQuestions(),
	})
	if err != nil {
		t.Fatalf("DoDecide() error = %v", err)
	}
	if len(result.Answers) != 3 {
		t.Fatalf("Answers = %+v, want 3 entries", result.Answers)
	}
	if result.Answers["topic"].Type != "choice" || result.Answers["topic"].Choice == "" {
		t.Fatalf("Answers[topic] = %+v", result.Answers["topic"])
	}
	if result.Answers["severity"].Type != "score" || result.Answers["severity"].Score == nil {
		t.Fatalf("Answers[severity] = %+v", result.Answers["severity"])
	}
	if result.Answers["refund"].Type != "boolean" || result.Answers["refund"].Probability == nil {
		t.Fatalf("Answers[refund] = %+v", result.Answers["refund"])
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 10 {
		t.Fatalf("Usage = %+v", result.Usage)
	}
}

func TestDecisionLanguageModel_ProviderOverride(t *testing.T) {
	mock := &testutil.MockLanguageModel{ProviderName: "test", ModelName: "m"}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock, Provider: "custom.eval"})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	if model.Provider() != "custom.eval" {
		t.Fatalf("Provider() = %q, want custom.eval", model.Provider())
	}
}

func TestDecisionLanguageModel_RejectsNilModel(t *testing.T) {
	_, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestDecisionLanguageModel_RejectsEmptyQuestions(t *testing.T) {
	mock := &testutil.MockLanguageModel{}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: map[string]provider.DecisionQuestion{}})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestDecisionLanguageModel_RejectsNonStopFinish(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "{}", FinishReason: types.FinishReasonLength}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}},
	})
	var target *providererrors.InvalidResponseDataError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*providererrors.InvalidResponseDataError); ok {
		target = e
	}
	if target == nil {
		t.Fatalf("err = %v (%T), want InvalidResponseDataError", err, err)
	}
}

func TestDecisionLanguageModel_RejectsInvalidJSON(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "not json", FinishReason: types.FinishReasonStop}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}},
	})
	if err == nil {
		t.Fatalf("expected error for invalid JSON response")
	}
}

func TestDecisionLanguageModel_RejectsUnknownChoice(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"q0":"not-a-real-option"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
		},
	})
	if err == nil {
		t.Fatalf("expected error for unknown choice")
	}
}

// TS: decide.ts / DecisionLanguageModel throws when a question type isn't
// choice/score/boolean.
func TestDecisionLanguageModel_RejectsUnsupportedQuestionType(t *testing.T) {
	mock := &testutil.MockLanguageModel{}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: map[string]provider.DecisionQuestion{"q": {Type: "unknown", Instructions: "i"}},
	})
	var target *providererrors.DecisionUnsupportedQuestionTypeError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*providererrors.DecisionUnsupportedQuestionTypeError); ok {
		target = e
	}
	if target == nil {
		t.Fatalf("err = %v (%T), want DecisionUnsupportedQuestionTypeError", err, err)
	}
}

// End-to-end: NewDecisionLanguageModel satisfies provider.DecisionModel
// and plugs directly into ExperimentalDecide.
func TestDecisionLanguageModel_WorksWithExperimentalDecide(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "m",
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"q0":0.75}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatalf("NewDecisionLanguageModel() error = %v", err)
	}

	result, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
	if result.Answers["q"].Probability == nil || *result.Answers["q"].Probability != 0.75 {
		t.Fatalf("Answers[q] = %+v", result.Answers["q"])
	}
}
