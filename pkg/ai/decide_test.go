package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type mockDecisionModel struct {
	providerName   string
	modelID        string
	supportedTypes []string
	doDecide       func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error)
	calls          []provider.DecisionCallOptions
}

func (m *mockDecisionModel) SpecificationVersion() string { return "v4" }
func (m *mockDecisionModel) Provider() string             { return m.providerName }
func (m *mockDecisionModel) ModelID() string              { return m.modelID }
func (m *mockDecisionModel) SupportedQuestionTypes() []string {
	if m.supportedTypes != nil {
		return m.supportedTypes
	}
	return []string{"choice", "score", "boolean"}
}
func (m *mockDecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	m.calls = append(m.calls, opts)
	return m.doDecide(ctx, opts)
}

func decisionQuestionsFixture() map[string]provider.DecisionQuestion {
	return map[string]provider.DecisionQuestion{
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

func decisionAnswersFixture() map[string]provider.DecisionAnswer {
	score := 1.6
	prob := 0.92
	return map[string]provider.DecisionAnswer{
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

// TS: decide.test.ts "decides mixed questions in one call and preserves distributions and metadata"
func TestExperimentalDecide_MixedQuestions(t *testing.T) {
	inputTokens, outputTokens := 30, 4
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "test-model",
		doDecide: func(_ context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers:          decisionAnswersFixture(),
				Warnings:         []types.Warning{{Type: "other", Message: "Provider note"}},
				Usage:            &provider.DecisionUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens},
				ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"confidence": 0.8}},
				Response: &provider.DecisionResponseInfo{
					ID:      "response",
					ModelID: "actual-model",
					Headers: map[string]string{"x-request-id": "request"},
					Body:    map[string]interface{}{"raw": true},
				},
			}, nil
		},
	}

	state := map[string]interface{}{"message": "refund", "history": []interface{}{"hello"}}
	result, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:           mock,
		State:           state,
		Questions:       decisionQuestionsFixture(),
		Headers:         map[string]string{"custom": "value"},
		ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"option": true}},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("DoDecide called %d times, want 1", len(mock.calls))
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

func TestExperimentalDecide_RejectsEmptyQuestions(t *testing.T) {
	mock := &mockDecisionModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:     mock,
		State:     "hello",
		Questions: map[string]provider.DecisionQuestion{},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestExperimentalDecide_RejectsNonJSONState(t *testing.T) {
	mock := &mockDecisionModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:     mock,
		State:     42, // not string/map/slice
		Questions: decisionQuestionsFixture(),
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestExperimentalDecide_RejectsInvalidChoiceCriteria(t *testing.T) {
	mock := &mockDecisionModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{}},
		},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (empty choice criteria)", err)
	}
}

func TestExperimentalDecide_RejectsInvalidScoreCriteria(t *testing.T) {
	mock := &mockDecisionModel{providerName: "test", modelID: "m"}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{}},
		},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (no score levels)", err)
	}
}

// TS: decide.ts throws DecisionUnsupportedQuestionTypeError when the
// model doesn't support a question's type.
func TestExperimentalDecide_RejectsUnsupportedQuestionType(t *testing.T) {
	mock := &mockDecisionModel{providerName: "test", modelID: "m", supportedTypes: []string{"boolean"}}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
		},
	})
	var target *providererrors.DecisionUnsupportedQuestionTypeError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want DecisionUnsupportedQuestionTypeError", err)
	}
}

// TS: decide.test.ts "accepts a rounded distribution sum but rejects errors beyond declared precision"
func TestExperimentalDecide_RejectsAnswerWithWrongType(t *testing.T) {
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers: map[string]provider.DecisionAnswer{
					"q": {Type: "boolean"}, // wrong type: question is "choice"
				},
			}, nil
		},
	}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError", err)
	}
}

func TestExperimentalDecide_RejectsMissingAnswer(t *testing.T) {
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{}}, nil
		},
	}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (missing answer)", err)
	}
}

func TestExperimentalDecide_RejectsDistributionNotSummingToOne(t *testing.T) {
	prob := 0.5
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers: map[string]provider.DecisionAnswer{
					"q": {Type: "choice", Choice: "a", Probability: &prob, Probabilities: map[string]float64{"a": 0.5, "b": 0.2}},
				},
			}, nil
		},
	}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil, "b": nil}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (distribution sum != 1)", err)
	}
}

func TestExperimentalDecide_AcceptsScoreEqualToProbabilityWeightedMean(t *testing.T) {
	score := 1.6
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers: map[string]provider.DecisionAnswer{
					"q": {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0, "1": 0.4, "2": 0.6}},
				},
			}, nil
		},
	}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{"Low", "Medium", "High"}},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
}

func TestExperimentalDecide_RejectsScoreNotMatchingWeightedMean(t *testing.T) {
	score := 2.0 // does not match weighted mean 1.6
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers: map[string]provider.DecisionAnswer{
					"q": {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0, "1": 0.4, "2": 0.6}},
				},
			}, nil
		},
	}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "score", Instructions: "i", Criteria: []interface{}{"Low", "Medium", "High"}},
		},
	})
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v, want InvalidResponseDataError (score mismatch)", err)
	}
}

func TestExperimentalDecide_OnStartAndOnEndCallbacks(t *testing.T) {
	prob := 0.5
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "m",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{
				Answers: map[string]provider.DecisionAnswer{"q": {Type: "boolean", Probability: &prob}},
			}, nil
		},
	}

	var startCalled, endCalled bool
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: mock,
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
		OnStart: func(e DecideOnStartEvent) {
			startCalled = true
			if e.OperationID != "ai.decide" {
				t.Fatalf("OperationID = %q", e.OperationID)
			}
		},
		OnEnd: func(e DecideOnEndEvent) {
			endCalled = true
			if e.Answers["q"].Probability == nil || *e.Answers["q"].Probability != 0.5 {
				t.Fatalf("OnEnd answers = %+v", e.Answers)
			}
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
	if !startCalled || !endCalled {
		t.Fatalf("startCalled=%v endCalled=%v", startCalled, endCalled)
	}
}

func TestExperimentalDecide_ResolvesStringModelViaProvider(t *testing.T) {
	prob := 0.5
	mock := &mockDecisionModel{
		providerName: "test",
		modelID:      "resolved-model",
		doDecide: func(_ context.Context, _ provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"q": {Type: "boolean", Probability: &prob}}}, nil
		},
	}
	resolver := mockDecisionModelProvider{models: map[string]provider.DecisionModel{"resolved-model": mock}}

	result, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model:    "resolved-model",
		Provider: resolver,
		State:    "s",
		Questions: map[string]provider.DecisionQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalDecide() error = %v", err)
	}
	if result.Response.ModelID != "resolved-model" {
		t.Fatalf("ModelID = %q, want resolved-model", result.Response.ModelID)
	}
}

type mockDecisionModelProvider struct {
	models map[string]provider.DecisionModel
}

func (p mockDecisionModelProvider) DecisionModel(modelID string) (provider.DecisionModel, error) {
	if m, ok := p.models[modelID]; ok {
		return m, nil
	}
	return nil, errors.New("no such decisionModel: " + modelID)
}

// TS: decide.test.ts "rejects refused questions with a refusal error without retrying"
func TestExperimentalDecide_RejectsRefusedQuestionsWithoutRetrying(t *testing.T) {
	for _, refused := range [][]string{{"topic"}, {"severity"}, {"refund"}, {"refund", "topic"}} {
		answers := decisionAnswersFixture()
		for _, id := range refused {
			answers[id] = provider.DecisionAnswer{Type: "refusal"}
		}
		mock := &mockDecisionModel{
			providerName: "test", modelID: "m",
			doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
				return &provider.DecisionResult{Answers: answers}, nil
			},
		}
		var endCalled bool
		_, err := ExperimentalDecide(context.Background(), DecideOptions{
			Model: mock, State: "", Questions: decisionQuestionsFixture(),
			OnEnd: func(DecideOnEndEvent) { endCalled = true },
		})
		var target *providererrors.DecisionRefusalError
		if !errors.As(err, &target) {
			t.Fatalf("%v: err = %v, want DecisionRefusalError", refused, err)
		}
		if target.Provider != "test" || target.ModelID != "m" || len(target.QuestionIDs) != len(refused) {
			t.Fatalf("%v: error = %+v", refused, target)
		}
		if len(mock.calls) != 1 || endCalled {
			t.Fatalf("%v: calls = %d, endCalled = %v", refused, len(mock.calls), endCalled)
		}
	}
}

// TS: decide.test.ts "accepts a selected choice within the declared probability rounding"
func TestExperimentalDecide_AcceptsRoundedChoiceTies(t *testing.T) {
	questions := map[string]provider.DecisionQuestion{
		"topic": {Type: "choice", Instructions: "Team?", Criteria: map[string]interface{}{"billing": nil, "support": nil, "other": nil}},
	}
	run := func(rounding *provider.DecisionRounding) error {
		mock := &mockDecisionModel{
			providerName: "test", modelID: "m",
			doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
				return &provider.DecisionResult{
					Rounding: rounding,
					Answers: map[string]provider.DecisionAnswer{
						"topic": {Type: "choice", Choice: "support", Probabilities: map[string]float64{"billing": 0.44, "support": 0.43, "other": 0.13}},
					},
				}, nil
			},
		}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: mock, State: "text", Questions: questions})
		return err
	}
	decimals := 2
	if err := run(&provider.DecisionRounding{ProbabilityDecimals: &decimals}); err != nil {
		t.Fatalf("rounded tie rejected: %v", err)
	}
	// Without declared rounding the same answer is not a highest-probability pick.
	if err := run(nil); err == nil {
		t.Fatal("expected an error without declared rounding")
	}
}

// TS abortSignal.throwIfAborted() after the model call: an aborted caller
// gets the abort error, not the model's answers.
func TestExperimentalDecide_AbortAfterModelCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mock := &mockDecisionModel{
		providerName: "test", modelID: "test-model",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			cancel()
			return &provider.DecisionResult{Answers: decisionAnswersFixture()}, nil
		},
	}
	_, err := ExperimentalDecide(ctx, DecideOptions{Model: mock, State: map[string]interface{}{"m": "x"}, Questions: decisionQuestionsFixture()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
