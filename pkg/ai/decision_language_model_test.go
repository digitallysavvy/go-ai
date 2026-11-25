package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func decisionLMWithText(text string, capture *[]*provider.GenerateOptions) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "test-model",
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if capture != nil {
				*capture = append(*capture, opts)
			}
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

func categoryQuestion(criteria map[string]interface{}) map[string]provider.DecisionQuestion {
	return map[string]provider.DecisionQuestion{
		"category": {Type: "choice", Instructions: "Which?", Criteria: criteria},
	}
}

// TS: "accepts exact choice labels when an internal option code is not returned"
func TestDecisionLanguageModel_AcceptsExactChoiceLabels(t *testing.T) {
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: decisionLMWithText(`{"q0":"Needs review"}`, nil)})
	result, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: categoryQuestion(map[string]interface{}{"Needs review": nil, "Fine": nil}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Answers["category"]; got.Type != "choice" || got.Choice != "Needs review" {
		t.Fatalf("answer = %+v", got)
	}
}

// TS: "prefers internal option codes over identical choice labels"
func TestDecisionLanguageModel_PrefersOptionCodesOverLabels(t *testing.T) {
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: decisionLMWithText(`{"q0":"c1"}`, nil)})
	result, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "s",
		Questions: categoryQuestion(map[string]interface{}{"c1": "first label", "other": "second label"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Answers["category"]; got.Choice != "other" {
		t.Fatalf("choice = %q, want other", got.Choice)
	}
}

func TestDecisionLanguageModel_RejectsUnknownLabelAndCode(t *testing.T) {
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: decisionLMWithText(`{"q0":"nope"}`, nil)})
	_, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: "s", Questions: categoryQuestion(map[string]interface{}{"a": nil}),
	})
	if err == nil {
		t.Fatal("expected an unknown-option error")
	}
}

// TS: image state parts are forwarded after the rubric and "Shared state:" text.
func TestDecisionLanguageModel_SendsStatePartsInOrder(t *testing.T) {
	var calls []*provider.GenerateOptions
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: decisionLMWithText(`{"q0":"c0"}`, &calls)})
	img := types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}}
	_, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("Inspect."),
			provider.DecisionFilePart(img),
			provider.DecisionJSONPart(map[string]interface{}{"product": "vase"}),
		},
		Questions: categoryQuestion(map[string]interface{}{"a": nil}),
	})
	if err != nil {
		t.Fatal(err)
	}
	content := calls[0].Prompt.Messages[0].Content
	if len(content) != 5 {
		t.Fatalf("content parts = %d", len(content))
	}
	if first, ok := content[0].(types.TextContent); !ok || !strings.HasPrefix(first.Text, `{"questions":`) || strings.Contains(first.Text, "Inspect.") {
		t.Fatalf("first part = %#v", content[0])
	}
	if label := content[1].(types.TextContent); label.Text != "Shared state:" {
		t.Fatalf("label = %q", label.Text)
	}
	if txt := content[2].(types.TextContent); txt.Text != "Inspect." {
		t.Fatalf("text = %q", txt.Text)
	}
	if _, ok := content[3].(types.FileContent); !ok {
		t.Fatalf("file part = %#v", content[3])
	}
	if js := content[4].(types.TextContent); js.Text != `{"product":"vase"}` {
		t.Fatalf("json = %q", js.Text)
	}
}

// TS: non-image file media types are rejected before any model call.
func TestDecisionLanguageModel_RejectsNonImageFiles(t *testing.T) {
	var calls []*provider.GenerateOptions
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: decisionLMWithText(`{"q0":"c0"}`, &calls)})
	_, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
			MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		})},
		Questions: categoryQuestion(map[string]interface{}{"a": nil}),
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
	if len(calls) != 0 {
		t.Fatal("model was called")
	}
}

// The rubric text is model-visible: TS writes each question as
// {id, type, instructions, criteria} and each choice option as
// {label, description}, and keeps q0..q10 in question order.
func TestDecisionLanguageModel_RubricKeyOrder(t *testing.T) {
	var prompt string
	mock := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "m",
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			prompt = opts.Prompt.Messages[0].Content[0].(types.TextContent).Text
			values := map[string]interface{}{}
			for i := 0; i < 11; i++ {
				values[fmt.Sprintf("q%d", i)] = 0.5
			}
			text, _ := json.Marshal(values)
			return &types.GenerateResult{Text: string(text), FinishReason: types.FinishReasonStop}, nil
		},
	}
	model, _ := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	questions := map[string]provider.DecisionQuestion{}
	for i := 0; i < 11; i++ {
		questions[fmt.Sprintf("flag%02d", i)] = provider.DecisionQuestion{Type: "boolean", Instructions: "x"}
	}
	questions["flag00"] = provider.DecisionQuestion{Type: "boolean", Instructions: "x"}
	if _, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: questions}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, `{"questions":{"q0":{"id":"flag00","type":"boolean","instructions":"x"},"q1":{"id":"flag01"`) {
		t.Fatalf("rubric key order: %.200s", prompt)
	}
	if i, j := strings.Index(prompt, `"q9"`), strings.Index(prompt, `"q10"`); i < 0 || j < i {
		t.Fatalf("q10 must follow q9: %s", prompt)
	}

	choice := map[string]provider.DecisionQuestion{"t": {Type: "choice", Instructions: "x", Criteria: map[string]interface{}{"a": "A"}}}
	_, _ = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "s", Questions: choice})
	if !strings.Contains(prompt, `"criteria":{"c0":{"label":"a","description":"A"}}`) {
		t.Fatalf("choice option key order: %s", prompt)
	}
}

// TS #23470: criteria-only questions appear in the prompt without an
// instructions key.
func TestDecisionRubric_OmitsNilInstructions(t *testing.T) {
	r := decisionRubric("q", "score", nil, []interface{}{"Low", "High"}, true)
	if got := r.Keys; len(got) != 3 || got[0] != "id" || got[1] != "type" || got[2] != "criteria" {
		t.Fatalf("keys = %v", got)
	}
	withInstructions := decisionRubric("q", "score", "How severe?", []interface{}{"Low"}, true)
	if got := withInstructions.Keys; len(got) != 4 || got[2] != "instructions" {
		t.Fatalf("keys = %v", got)
	}
}

// TS returns `response: result.response`, so the response body reaches the
// decision result.
func TestDecisionLanguageModel_DoDecide_KeepsResponseBody(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "test-model",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:             `{"q0":0.25}`,
				FinishReason:     types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{ID: "r1", ModelID: "m1", Body: map[string]interface{}{"raw": true}},
			}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "x",
		Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Instructions: "Refund?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := result.Response.Body.(map[string]interface{})
	if result.Response.ID != "r1" || body["raw"] != true {
		t.Fatalf("response = %+v", result.Response)
	}
}

// Go language models report the response body as RawResponse (the body
// GenerateText's step response reads); TS decision results carry it as
// response.body.
func TestDecisionLanguageModel_DoDecide_ReportsRawResponseBody(t *testing.T) {
	mock := &testutil.MockLanguageModel{
		ProviderName: "test",
		ModelName:    "test-model",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:             `{"q0":0.75}`,
				FinishReason:     types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{ID: "r1", ModelID: "m1"},
				RawResponse:      map[string]interface{}{"typed": true},
			}, nil
		},
	}
	model, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     "x",
		Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Instructions: "Refund?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := result.Response.Body.(map[string]interface{})
	if result.Response.ID != "r1" || body["typed"] != true {
		t.Fatalf("response = %+v, want the RawResponse body", result.Response)
	}
}
