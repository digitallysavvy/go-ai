package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 73 (gen): ToolErrorContent.Err (owner ruling 2026-10-31, option c)
// in the legacy ToolLoopAgent loop.

type r73genAgentToolErr struct{ code int }

func (e *r73genAgentToolErr) Error() string { return "agent tool failed" }

func TestR73GenAgentToolErrorContentKeepsTypedErr(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{
		Model: r72agentToolModel(),
		Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, &r73genAgentToolErr{code: 9}
		}}},
	})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) == 0 {
		t.Fatal("no steps")
	}
	var te *types.ToolErrorContent
	for _, p := range res.Steps[0].Content {
		if v, ok := p.(types.ToolErrorContent); ok {
			te = &v
		}
	}
	if te == nil {
		t.Fatalf("step 0 content = %#v, want a tool-error", res.Steps[0].Content)
	}
	var target *r73genAgentToolErr
	if !errors.As(te.Err, &target) || target.code != 9 {
		t.Fatalf("Err = %#v, want the tool's *r73genAgentToolErr", te.Err)
	}
	if te.Error != "agent tool failed" || te.ErrorText != "Error: agent tool failed" {
		t.Fatalf("Error = %#v, ErrorText = %q", te.Error, te.ErrorText)
	}
}
