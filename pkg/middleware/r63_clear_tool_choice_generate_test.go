package middleware_test

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The v0.6 migration guide's workaround for servers that reject an explicit
// auto tool choice: a TransformParams middleware that clears ToolChoice
// restores the v0.5 request (no tool choice), while GenerateText still
// passes auto (TS prepareToolChoice) to the wrapped model.
func TestR63IntegratorClearToolChoiceMiddlewareRestoresTheV05Request(t *testing.T) {
	var got []types.ToolChoice
	inner := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		got = append(got, o.ToolChoice)
		return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
	}}
	clearToolChoice := &middleware.LanguageModelMiddleware{
		TransformParams: func(_ context.Context, _ string, params *provider.GenerateOptions, _ provider.LanguageModel) (*provider.GenerateOptions, error) {
			p := *params
			p.ToolChoice = types.ToolChoice{}
			return &p, nil
		},
	}
	model := middleware.WrapLanguageModel(inner, []*middleware.LanguageModelMiddleware{clearToolChoice}, nil, nil)
	_, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "a",
		Tools:  []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != "" {
		t.Fatalf("tool choice at the wrapped model = %+v, want unset", got)
	}
}
