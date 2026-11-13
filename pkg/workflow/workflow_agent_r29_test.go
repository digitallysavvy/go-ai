package workflow

import (
	"context"
	"testing"

	agentpkg "github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// The positional prompt of Generate/Stream must survive an options struct that
// carries only callbacks (it used to be replaced by the options' empty Prompt
// and the call failed with "either prompt or messages is required").
func TestPositionalPromptKeptWhenOptionsHaveNone(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatal(err)
	}
	ended := false
	if _, err := a.Generate(context.Background(), "hello", &agentpkg.AgentGenerateOptions{
		OnEnd: func(context.Context, ai.OnFinishEvent) { ended = true },
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !ended {
		t.Fatal("OnEnd not called")
	}
	res, err := a.Stream(context.Background(), "hello", &agentpkg.AgentStreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_ = res.ConsumeStream()
}

func TestUnsupportedPerCallInstructionsFail(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "p", Instructions: 42}); err == nil {
		t.Fatal("generate: want an error for unsupported instructions")
	}
	if _, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "p", Instructions: 42}); err == nil {
		t.Fatal("stream: want an error for unsupported instructions")
	}
}
