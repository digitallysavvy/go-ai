package deepseek

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PB-20 (provb2b-07): DeepSeek prompt checks are TS InvalidPromptError
// (convert-to-deepseek-chat-messages.ts:131, :300).
func TestR91provbDeepSeekInvalidPrompt(t *testing.T) {
	_, err := deepseekResolveUserImagePart(types.FileContent{MediaType: "image/png", URL: "https://example.com/" + strings.Repeat("a", 8200)}, "deepseek")
	var ip *providererrors.InvalidPromptError
	if !errors.As(err, &ip) || err.Error() != "Invalid prompt: DeepSeek image URLs must not exceed 8192 characters." {
		t.Fatalf("err = %v", err)
	}
}
