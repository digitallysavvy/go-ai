package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Integrator survivor test (r70): the up-front system-message check uses the
// AllowSystemInMessages the PrepareCall hook returns, as the run's loop does,
// so a hook that opts in lets the run start (and one that opts out rejects
// it before the run).
func TestR70Integ_PrepareCallAllowSystemInMessagesDecidesTheCheck(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "trusted"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	for _, allow := range []bool{true, false} {
		for _, stream := range []bool{false, true} {
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:                 &r70SystemModel{},
				AllowSystemInMessages: !allow,
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					o.AllowSystemInMessages = allow
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				var st *WorkflowStreamResult
				st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
				if err == nil {
					_ = st.ConsumeStream()
					err = st.Err()
				}
			} else {
				_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: msgs})
			}
			if allow && err != nil {
				t.Errorf("hook allows, stream=%v: err = %v", stream, err)
			}
			if !allow && err == nil {
				t.Errorf("hook disallows, stream=%v: want an InvalidPromptError", stream)
			}
		}
	}
}
