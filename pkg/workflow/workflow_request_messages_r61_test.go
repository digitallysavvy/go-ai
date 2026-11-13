package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent records a generate step's request messages
// (include.requestMessages) as its language model prompt, the system
// message first (model-call-iterator conversationPrompt).
func TestR61WorkflowGenerateRequestMessagesAreThePrompt(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model:        &wfMockModel{calls: 1},
		Instructions: "be brief",
		Include:      &ai.IncludeOptions{RequestMessages: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %d", len(res.Steps))
	}
	msgs := res.Steps[0].Request.Messages
	if len(msgs) != 2 || msgs[0].Role != types.RoleSystem || msgs[1].Role != types.RoleUser {
		t.Fatalf("request messages = %+v, want system then user", msgs)
	}
	if text, ok := msgs[0].Content[0].(types.TextContent); !ok || text.Text != "be brief" {
		t.Fatalf("system message = %+v", msgs[0])
	}
}
