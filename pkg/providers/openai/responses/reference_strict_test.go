package responses

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS resolveProviderReference throws NoSuchProviderReferenceError when the
// reference has no ID for the provider; it never borrows another provider's.
func TestConvertPromptToInputRejectsReferenceWithoutProviderKey(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{FileData: types.FileData{
			Type:      types.FileDataTypeReference,
			Reference: types.ProviderReference{"anthropic": "file_a"},
			MediaType: "application/pdf",
		}}},
	}}}
	_, _, err := ConvertPromptToInputWithOptions(prompt, "system", ConvertOptions{ProviderOptionsName: "xai"})
	var noRef *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &noRef) || noRef.Provider != "xai" {
		t.Fatalf("err = %v, want NoSuchProviderReferenceError for xai", err)
	}
}
