package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS generateText (which ToolLoopAgent runs) records a step's request
// messages as the step's messages, without the instructions. The legacy loop
// recorded the converted prompt, with instruction messages first.
func TestR61GenerateAgentRequestMessagesOmitInstructions(t *testing.T) {
	instructions := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "inst"}}}}
	include := &ai.IncludeOptions{RequestMessages: true}
	a := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{}, InstructionMessages: instructions, Include: include})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: &mockLanguageModel{}, Prompt: "go", InstructionMessages: instructions, Include: include})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(res.Steps[0].Request.Messages)
	want, _ := json.Marshal(gen.Steps[0].Request.Messages)
	if string(got) != string(want) {
		t.Fatalf("request messages:\nagent    %s\ngenerate %s", got, want)
	}
	if len(res.Steps[0].Request.Messages) != 1 || res.Steps[0].Request.Messages[0].Role != types.RoleUser {
		t.Fatalf("request messages = %+v, want the user message only", res.Steps[0].Request.Messages)
	}
}
