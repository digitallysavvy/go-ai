package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockEvaluationModel struct {
	providerName   string
	modelID        string
	supportedTypes []string
	doEvaluate     func(context.Context, provider.EvaluationCallOptions) (*provider.EvaluationResult, error)
	calls          []provider.EvaluationCallOptions
}

func (m *mockEvaluationModel) SpecificationVersion() string { return "v4" }
func (m *mockEvaluationModel) Provider() string             { return m.providerName }
func (m *mockEvaluationModel) ModelID() string              { return m.modelID }
func (m *mockEvaluationModel) SupportedQuestionTypes() []string {
	if m.supportedTypes != nil {
		return m.supportedTypes
	}
	return []string{"choice", "score", "boolean"}
}
func (m *mockEvaluationModel) DoEvaluate(ctx context.Context, opts provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
	m.calls = append(m.calls, opts)
	return m.doEvaluate(ctx, opts)
}

func evaluationQuestionsFixture() map[string]provider.EvaluationQuestion {
	return map[string]provider.EvaluationQuestion{
		"topic": {
			Type:         "choice",
			Instructions: "Team?",
			Criteria:     map[string]interface{}{"billing": nil, "support": map[string]interface{}{"includes": []interface{}{"help"}}},
		},
		"severity": {
			Type:         "score",
			Instructions: []interface{}{"Severity?"},
			Criteria:     []interface{}{"Low", "Medium", "High"},
		},
		"refund": {
			Type:         "boolean",
			Instructions: "Refund?",
			Criteria:     map[string]interface{}{"true": "Money back", "false": nil},
		},
	}
}

func evaluationAnswersFixture() map[string]provider.EvaluationAnswer {
	score := 1.6
	prob := 0.92
	return map[string]provider.EvaluationAnswer{
		"refund": {Type: "boolean", Probability: &prob},
		"severity": {
			Type:          "score",
			Score:         &score,
			Probabilities: map[string]float64{"0": 0, "1": 0.4, "2": 0.6},
		},
		"topic": {
			Type:          "choice",
			Choice:        "billing",
			Probabilities: map[string]float64{"billing": 0.9, "support": 0.1},
		},
	}
}

// TS: evaluate.test.ts "evaluates mixed questions in one call and preserves distributions and metadata"
func TestExperimentalEvaluate_MixedQuestions(t *testing.T) {
	inputTokens, outputTokens := 30, 4
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "test-model",
		doEvaluate: func(_ context.Context, opts provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers:          evaluationAnswersFixture(),
				Warnings:         []types.Warning{{Type: "other", Message: "Provider note"}},
				Usage:            &provider.EvaluationUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
				ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"confidence": 0.8}},
				Response: &provider.EvaluationResponseInfo{
					ID:      "response",
					ModelID: "actual-model",
					Headers: map[string]string{"x-request-id": "request"},
					Body:    map[string]interface{}{"raw": true},
				},
			}, nil
		},
	}

	state := map[string]interface{}{"message": "refund", "history": []interface{}{"hello"}}
	result, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:           mock,
		State:           state,
		Questions:       evaluationQuestionsFixture(),
		Headers:         map[string]string{"custom": "value"},
		ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"option": true}},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("DoEvaluate called %d times, want 1", len(mock.calls))
	}
	call := mock.calls[0]
	if call.Headers["custom"] != "value" {
		t.Fatalf("headers not forwarded: %+v", call.Headers)
	}
	if call.ProviderOptions["test"] == nil {
		t.Fatalf("providerOptions not forwarded: %+v", call.ProviderOptions)
	}

	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 30 {
		t.Fatalf("InputTokens = %v", result.Usage.InputTokens)
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 34 {
		t.Fatalf("TotalTokens = %v, want 34", result.Usage.TotalTokens)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "Provider note" {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
	if result.Response.ID != "response" || result.Response.ModelID != "actual-model" {
		t.Fatalf("Response = %+v", result.Response)
	}
	if result.Answers["topic"].Choice != "billing" {
		t.Fatalf("Answers[topic].Choice = %q, want billing", result.Answers["topic"].Choice)
	}
}

func TestExperimentalEvaluate_RejectsEmptyQuestions(t *testing.T) {
	mock := &mockEvaluationModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:     mock,
		State:     "hello",
		Questions: map[string]provider.EvaluationQuestion{},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestExperimentalEvaluate_RejectsNonJSONState(t *testing.T) {
	mock := &mockEvaluationModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:     mock,
		State:     42, // not string/map/slice
		Questions: evaluationQuestionsFixture(),
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestExperimentalEvaluate_RejectsInvalidChoiceCriteria(t *testing.T) {
	mock := &mockEvaluationModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{}},
		},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (empty choice criteria)", err)
	}
}

func TestExperimentalEvaluate_RejectsInvalidScoreCriteria(t *testing.T) {
	mock := &mockEvaluationModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{"only-one"}},
		},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (score criteria too short)", err)
	}
}

// TS: evaluate.ts throws EvaluationUnsupportedQuestionTypeError when the
// model doesn't support a question's type.
func TestExperimentalEvaluate_RejectsUnsupportedQuestionType(t *testing.T) {
	mock := &mockEvaluationModel{providerName: "test", modelID: "m", supportedTypes: []string{"boolean"}}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
		},
	})
	var target *providererrors.EvaluationUnsupportedQuestionTypeError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want EvaluationUnsupportedQuestionTypeError", err)
	}
}

// TS: evaluate.test.ts "accepts a rounded distribution sum but rejects errors beyond declared precision"
func TestExperimentalEvaluate_RejectsAnswerWithWrongType(t *testing.T) {
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers: map[string]provider.EvaluationAnswer{
					"q": {Type: "boolean"}, // wrong type: question is "choice"
				},
			}, nil
		},
	}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError", err)
	}
}

func TestExperimentalEvaluate_RejectsMissingAnswer(t *testing.T) {
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{Answers: map[string]provider.EvaluationAnswer{}}, nil
		},
	}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (missing answer)", err)
	}
}

func TestExperimentalEvaluate_RejectsDistributionNotSummingToOne(t *testing.T) {
	prob := 0.5
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers: map[string]provider.EvaluationAnswer{
					"q": {Type: "choice", Choice: "a", Probability: &prob, Probabilities: map[string]float64{"a": 0.5, "b": 0.2}},
				},
			}, nil
		},
	}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil, "b": nil}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (distribution sum != 1)", err)
	}
}

func TestExperimentalEvaluate_AcceptsScoreEqualToProbabilityWeightedMean(t *testing.T) {
	score := 1.6
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers: map[string]provider.EvaluationAnswer{
					"q": {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0, "1": 0.4, "2": 0.6}},
				},
			}, nil
		},
	}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{"Low", "Medium", "High"}},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}
}

func TestExperimentalEvaluate_RejectsScoreNotMatchingWeightedMean(t *testing.T) {
	score := 2.0 // does not match weighted mean 1.6
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers: map[string]provider.EvaluationAnswer{
					"q": {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0, "1": 0.4, "2": 0.6}},
				},
			}, nil
		},
	}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{"Low", "Medium", "High"}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (score mismatch)", err)
	}
}

func TestExperimentalEvaluate_OnStartAndOnEndCallbacks(t *testing.T) {
	prob := 0.5
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "m",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{
				Answers: map[string]provider.EvaluationAnswer{"q": {Type: "boolean", Probability: &prob}},
			}, nil
		},
	}

	var startCalled, endCalled bool
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
		OnStart: func(e EvaluateOnStartEvent) {
			startCalled = true
			if e.OperationID != "ai.evaluate" {
				t.Fatalf("OperationID = %q", e.OperationID)
			}
		},
		OnEnd: func(e EvaluateOnEndEvent) {
			endCalled = true
			if e.Answers["q"].Probability == nil || *e.Answers["q"].Probability != 0.5 {
				t.Fatalf("OnEnd answers = %+v", e.Answers)
			}
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}
	if !startCalled || !endCalled {
		t.Fatalf("startCalled=%v endCalled=%v", startCalled, endCalled)
	}
}

func TestExperimentalEvaluate_ResolvesStringModelViaProvider(t *testing.T) {
	prob := 0.5
	mock := &mockEvaluationModel{
		providerName: "test",
		modelID:      "resolved-model",
		doEvaluate: func(_ context.Context, _ provider.EvaluationCallOptions) (*provider.EvaluationResult, error) {
			return &provider.EvaluationResult{Answers: map[string]provider.EvaluationAnswer{"q": {Type: "boolean", Probability: &prob}}}, nil
		},
	}
	resolver := mockEvaluationModelProvider{models: map[string]provider.EvaluationModel{"resolved-model": mock}}

	result, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model:    "resolved-model",
		Provider: resolver,
		State:    "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalEvaluate() error = %v", err)
	}
	if result.Response.ModelID != "resolved-model" {
		t.Fatalf("ModelID = %q, want resolved-model", result.Response.ModelID)
	}
}

type mockEvaluationModelProvider struct {
	models map[string]provider.EvaluationModel
}

func (p mockEvaluationModelProvider) EvaluationModel(modelID string) (provider.EvaluationModel, error) {
	if m, ok := p.models[modelID]; ok {
		return m, nil
	}
	return nil, errors.New("no such evaluationModel: " + modelID)
}
