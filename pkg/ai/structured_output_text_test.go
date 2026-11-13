package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func structuredTextModel(structured *string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:                 `{"name":"draft"}{"name":"final"}`,
				StructuredOutputText: structured,
				FinishReason:         types.FinishReasonStop,
			}, nil
		},
	}
}

// TS #23895: generateText parses JSON output from structuredOutputText while
// result.text keeps the complete text.
func TestGenerateText_ParsesStructuredOutputText(t *testing.T) {
	t.Parallel()
	final := `{"name":"final"}`
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: structuredTextModel(&final), Prompt: "hi",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r31ObjectSchema}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Output.(map[string]interface{})["name"]; got != "final" {
		t.Fatalf("output name = %v", got)
	}
	if res.Text != `{"name":"draft"}{"name":"final"}` {
		t.Fatalf("text changed: %q", res.Text)
	}
}

// Without structuredOutputText the concatenated text is parsed (and fails).
func TestGenerateText_NoStructuredOutputTextParsesFullText(t *testing.T) {
	t.Parallel()
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: structuredTextModel(nil), Prompt: "hi",
		Output: ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: r31ObjectSchema}),
	})
	if err == nil {
		t.Fatal("expected a parse error for concatenated JSON")
	}
}

// A non-JSON output (plain text) ignores structuredOutputText.
func TestGenerateText_TextOutputIgnoresStructuredOutputText(t *testing.T) {
	t.Parallel()
	final := `{"name":"final"}`
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: structuredTextModel(&final), Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"name":"draft"}{"name":"final"}` {
		t.Fatalf("text = %q", res.Text)
	}
}

// generateObject parses structuredOutputText and hands the same text to repair.
func TestGenerateObject_ParsesStructuredOutputText(t *testing.T) {
	t.Parallel()
	final := `{"name":"final"}`
	res, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: structuredTextModel(&final), Prompt: "hi", Schema: r31ObjectSchema,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Object.(map[string]interface{})["name"]; got != "final" {
		t.Fatalf("object name = %v", got)
	}

	bad := `{"name":`
	var repairInput string
	res, err = GenerateObject(context.Background(), GenerateObjectOptions{
		Model: structuredTextModel(&bad), Prompt: "hi", Schema: r31ObjectSchema,
		RepairText: func(ctx context.Context, text string, parseErr error) (*string, error) {
			repairInput = text
			fixed := `{"name":"fixed"}`
			return &fixed, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repairInput != bad || res.Object.(map[string]interface{})["name"] != "fixed" {
		t.Fatalf("repair input = %q, object = %v", repairInput, res.Object)
	}
}
