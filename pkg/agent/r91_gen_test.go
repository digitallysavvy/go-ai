package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 91 gen tests (TS 2ebc78b654).

// core1-D01 (agent half): the loop passes the last step's response to
// parseCompleteOutput (TS generate-text.ts:1808), so the
// NoObjectGeneratedError carries the model response.
func TestR91genAgentOutputParseErrorResponse(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:          []types.ContentPart{types.TextContent{Text: "not json"}},
			Text:             "not json",
			FinishReason:     types.FinishReasonStop,
			ResponseMetadata: &types.ResponseMetadata{ID: "resp-1", ModelID: "m-1"},
		}, nil
	}}
	a := NewToolLoopAgent(AgentConfig{
		Model:  model,
		Output: ai.ObjectOutput[map[string]interface{}](ai.ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}),
	})
	_, err := a.Execute(context.Background(), "prompt")
	var noObj *ai.NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v, want NoObjectGeneratedError", err)
	}
	if noObj.Response == nil || noObj.Response.ID != "resp-1" || noObj.Response.ModelID != "m-1" {
		t.Fatalf("Response = %+v, want id resp-1 / model m-1", noObj.Response)
	}
}
