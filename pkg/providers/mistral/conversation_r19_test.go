package mistral

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestConversationInputsUnsupportedAssistantContentIsTypedError(t *testing.T) {
	_, _, err := convertToMistralConversationInputs([]types.Message{{
		Role:    types.RoleAssistant,
		Content: []types.ContentPart{types.FileContent{MediaType: "image/png", Data: []byte{1}}},
	}}, newConversationToolNames(nil))
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "assistant content type: file" {
		t.Fatalf("err = %v", err)
	}
}
