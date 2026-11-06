package mistral

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS rejects file parts with provider references (UnsupportedFunctionalityError).
func TestRejectsProviderReferenceFiles(t *testing.T) {
	model, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{
			MediaType: "image/png",
			FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"mistral": "file-1"}},
		}},
	}}}}
	_, err = model.DoGenerate(context.Background(), opts)
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("DoGenerate err = %v, want UnsupportedFunctionalityError", err)
	}
	_, err = model.DoStream(context.Background(), opts)
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("DoStream err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestMistralReasoningMapWarnings(t *testing.T) {
	low, high, none := types.ReasoningLow, types.ReasoningHigh, types.ReasoningNone
	max := types.ReasoningMax
	if w := mistralReasoningMapWarnings(true, "", &low); len(w) != 1 || w[0].Type != "compatibility" ||
		w[0].Details != `reasoning "low" is not directly supported by this model. mapped to effort "high".` {
		t.Fatalf("low: %#v", w)
	}
	if w := mistralReasoningMapWarnings(true, "", &max); len(w) != 1 || w[0].Type != "compatibility" {
		t.Fatalf("max: %#v", w)
	}
	for _, r := range []*types.ReasoningLevel{&high, &none} {
		if w := mistralReasoningMapWarnings(true, "", r); len(w) != 0 {
			t.Fatalf("%v: %#v", *r, w)
		}
	}
	if w := mistralReasoningMapWarnings(true, "high", &low); len(w) != 0 {
		t.Fatalf("explicit effort: %#v", w)
	}
	if w := mistralReasoningMapWarnings(false, "", &low); len(w) != 1 || w[0].Type != "unsupported" {
		t.Fatalf("unsupported: %#v", w)
	}
}
