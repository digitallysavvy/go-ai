package groq

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
			FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"groq": "file-1"}},
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
