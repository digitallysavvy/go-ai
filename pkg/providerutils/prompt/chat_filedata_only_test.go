package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A file part that carries only the tagged FileData must not produce an empty
// image URL.
func TestOpenAIFileContentPartFileDataOnlyImage(t *testing.T) {
	for name, fd := range map[string]types.FileData{
		"url":  {Type: types.FileDataTypeURL, URL: "https://example.com/a.png", MediaType: "image/png"},
		"data": {Type: types.FileDataTypeData, Data: []byte("abc"), MediaType: "image/png"},
	} {
		t.Run(name, func(t *testing.T) {
			part := openAIFileContentPart(types.FileContent{FileData: fd}, false)
			img, _ := part["image_url"].(map[string]interface{})
			url, _ := img["url"].(string)
			if part["type"] != "image_url" || url == "" {
				t.Fatalf("part = %#v", part)
			}
		})
	}
}
