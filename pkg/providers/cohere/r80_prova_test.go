package cohere

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertToCohereChatPrompt treats a file part as an image only when
// getTopLevelMediaType(mediaType) is exactly "image"; a media type that only
// starts with "image" (here "imagery/plain") is a document.
func TestR80provaCohereTopLevelImageMediaType(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	for _, tc := range []struct {
		mediaType string
		image     bool
	}{
		{"imagery/plain", false},
		{"image/png", true},
		{"image", true},
	} {
		body, err := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.FileContent{
						FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("\x89PNG\r\n\x1a\nxxxx"), MediaType: tc.mediaType},
					},
				},
			}}},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.mediaType, err)
		}
		_, hasDocs := body["documents"]
		_, isParts := body["messages"].([]map[string]interface{})[0]["content"].([]map[string]interface{})
		if hasDocs == tc.image || isParts != tc.image {
			t.Fatalf("%s: documents=%v image parts=%v, want image=%v", tc.mediaType, hasDocs, isParts, tc.image)
		}
	}
}
