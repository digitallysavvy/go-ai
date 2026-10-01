package ai

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestGenerateText_DescriptionFuncResolvedAfterPrepareStep(t *testing.T) {
	t.Parallel()

	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			if len(opts.Tools) != 1 {
				t.Fatalf("tools count = %d, want 1", len(opts.Tools))
			}
			if got, want := opts.Tools[0].Description, "ctx=prepared-tool,sbx=prepared-sbx"; got != want {
				t.Fatalf("tool description = %q, want %q", got, want)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}

	tool := types.Tool{
		Name:       "weather",
		Parameters: map[string]interface{}{"type": "object"},
		DescriptionFunc: func(_ context.Context, options types.ToolDescriptionOptions) string {
			return fmt.Sprintf("ctx=%v,sbx=%v", options.Context, options.ExperimentalSandbox)
		},
	}

	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:               model,
		Prompt:              "hi",
		Tools:               []types.Tool{tool},
		RuntimeContext:      "initial",
		ToolsContext:        map[string]interface{}{"weather": "initial-tool"},
		ExperimentalSandbox: "initial-sbx",
		PrepareStep: func(_ context.Context, _ PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{
				RuntimeContext:      "prepared",
				ToolsContext:        map[string]interface{}{"weather": "prepared-tool"},
				ExperimentalSandbox: "prepared-sbx",
			}
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
}

func TestGenerateText_ToolOrderAfterFilteringAndPrepareStep(t *testing.T) {
	t.Parallel()

	var got []string
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			for _, tool := range opts.Tools {
				got = append(got, tool.Name)
			}
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}

	tools := []types.Tool{
		{Name: "zebra", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "alpha", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "middle", Parameters: map[string]interface{}{"type": "object"}},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:       model,
		Prompt:      "hi",
		Tools:       tools,
		ActiveTools: []string{"zebra", "alpha", "middle"},
		ToolOrder:   []string{"missing", "middle", "middle"},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{
				Tools:     step.Tools,
				ToolOrder: []string{"zebra"},
			}
		},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	want := []string{"zebra", "alpha", "middle"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tool order = %v, want %v", got, want)
	}
}
