package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS builds the decision error messages with template literals
// (`Question "${id}" ...`): the question id appears as is, without Go %q
// escaping of quotes, backslashes or non-ASCII characters.
func TestR66DecisionErrorsQuoteIDAsIs(t *testing.T) {
	const id = `a"b\é`
	questions := map[string]provider.DecisionQuestion{id: {Type: "boolean", Instructions: "?"}}
	err := validateDecisionAnswers(questions, nil, map[string]provider.DecisionAnswer{id: {Type: "choice"}}, nil)
	if want := `Question "a"b\é" returned an answer with the wrong type.`; err == nil || err.Error() != want {
		t.Errorf("validate: err = %v, want %q", err, want)
	}

	mock := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Text: `{"q0":2}`, FinishReason: types.FinishReasonStop}, nil
	}}
	model, mErr := NewDecisionLanguageModel(DecisionLanguageModelOptions{Model: mock})
	if mErr != nil {
		t.Fatal(mErr)
	}
	_, err = model.DoDecide(context.Background(), provider.DecisionCallOptions{State: "x", Questions: questions})
	if want := `Question "a"b\é" must return P(true) as a finite probability in [0, 1].`; err == nil || err.Error() != want {
		t.Errorf("language model: err = %v, want %q", err, want)
	}

	unsupported := &providererrors.DecisionUnsupportedQuestionTypeError{QuestionID: id, QuestionType: "box", Provider: `p"`, ModelID: "m"}
	if want := `Question "a"b\é" has type "box", which is not supported by provider "p"" and model "m".`; unsupported.Error() != want {
		t.Errorf("unsupported: %q, want %q", unsupported.Error(), want)
	}
}

// The other TS template-literal messages that quote a name ("${name}") keep
// it as is, too.
func TestR66TemplateLiteralMessagesQuoteNamesAsIs(t *testing.T) {
	const name = `t"\é`
	check := func(label string, err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to contain %q", label, err, want)
		}
	}

	_, err := CreateIDGenerator(CreateIDGeneratorOptions{Prefix: "p", Separator: `"`, Alphabet: `ab"\`})
	check("separator", err, `The separator """ must not be part of the alphabet "ab"\".`)

	tools := []types.Tool{{Name: name}}
	_, err = ResolveToolCallerConfiguration(tools, ExperimentalToolCallers{`x"`: nil})
	check("unknown tool", err, `unknown tool "x"".`)
	_, err = ResolveToolCallerConfiguration(tools, ExperimentalToolCallers{name: {"nope"}})
	check("invalid caller", err, `tool "t"\é" contains an invalid caller.`)

	_, err = NewToolSearchState([]types.Tool{{Name: name, IsToolSearch: true, DeferLoading: true}}, nil)
	check("tool search", err, `tool "t"\é" must be callable directly`)

	_, err = ExperimentalStreamSpeech(context.Background(), StreamSpeechOptions{Model: &testutil.MockSpeechModel{ModelName: name}, Text: "hi"})
	check("stream speech", err, `model "t"\é" does not support streaming speech.`)
	_, err = ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{Model: &testutil.MockTranscriptionModel{ModelName: name}})
	check("stream transcribe", err, `model "t"\é" does not support streaming transcription.`)

	var got []string
	SetLogWarnings(func(o LogWarningsOptions) {
		for _, w := range o.Warnings {
			got = append(got, w.Message)
		}
	})
	t.Cleanup(func() { resetLogWarningsState(nil) })
	emptyProviderResult(types.ToolCall{ID: "c1", ToolName: name}, "p", "m")
	if want := `[WorkflowAgent] Provider-executed tool "t"\é" (c1) did not receive a result from the stream. This may indicate a provider issue.`; len(got) != 1 || got[0] != want {
		t.Errorf("workflow warning = %q, want %q", got, want)
	}
}
