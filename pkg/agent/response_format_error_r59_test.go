package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r59FailingFormat struct{}

func (r59FailingFormat) ResponseFormat(context.Context) (*provider.ResponseFormat, error) {
	return nil, errors.New("no format")
}

// TS awaits output.responseFormat inside doGenerate, so the language model
// call start precedes a response format failure.
func TestR59GenerateAgentResponseFormatFailureFollowsModelCallStart(t *testing.T) {
	starts := 0
	model := &mockLanguageModel{responses: []types.GenerateResult{{Text: "x", FinishReason: types.FinishReasonStop}}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Output: r59FailingFormat{},
		OnLanguageModelCallStart: func(context.Context, ai.LanguageModelCallStartEvent) { starts++ }})
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err == nil || !strings.HasSuffix(err.Error(), "no format") {
		t.Fatalf("err = %v, want the response format error", err)
	}
	if starts != 1 {
		t.Fatalf("OnLanguageModelCallStart calls = %d, want 1", starts)
	}
}
