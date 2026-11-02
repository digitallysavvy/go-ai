package ai

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The getting-started EmailSummary example
// (docs/02-getting-started/01-navigating-the-library.mdx).
type r80integEmailSummary struct {
	Sender    string   `json:"sender"`
	Subject   string   `json:"subject"`
	KeyPoints []string `json:"key_points"`
	Priority  string   `json:"priority" validate:"required"`
}

func r80integTextModel(text string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: text, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

// The documented ObjectOutput struct-schema example works: v0.5.2 always
// failed with "No object generated: response did not match schema."
func TestR80integObjectOutputStructSchemaDocExample(t *testing.T) {
	ctx := context.Background()
	result, err := GenerateText(ctx, GenerateTextOptions{
		Model:  r80integTextModel(`{"sender":"a@b.c","subject":"Hi","key_points":["x"],"priority":"high"}`),
		Prompt: "Summarize this email: ...",
		Output: ObjectOutput[r80integEmailSummary](ObjectOutputOptions{
			Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := result.Output.(r80integEmailSummary)
	if summary.Priority != "high" || summary.Sender != "a@b.c" || len(summary.KeyPoints) != 1 {
		t.Fatalf("summary = %+v", summary)
	}

	o := ObjectOutput[r80integEmailSummary](ObjectOutputOptions{Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{}))})
	if _, err := o.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{}`}); err == nil {
		t.Fatal("missing required priority: want an error")
	} else {
		var noObj *NoObjectGeneratedError
		if !errors.As(err, &noObj) {
			t.Fatalf("err = %T %v, want *NoObjectGeneratedError", err, err)
		}
	}
}

// GenerateObject with a struct schema validates the decoded object.
func TestR80integGenerateObjectStructSchema(t *testing.T) {
	res, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  r80integTextModel(`{"priority":"low"}`),
		Prompt: "x",
		Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := res.Object.(map[string]interface{}); !ok || m["priority"] != "low" {
		t.Fatalf("object = %#v", res.Object)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model:  r80integTextModel(`{"subject":"s"}`),
		Prompt: "x",
		Schema: schema.NewSimpleStructSchema(reflect.TypeOf(r80integEmailSummary{})),
	}); err == nil {
		t.Fatal("missing required priority: want an error")
	}
}
