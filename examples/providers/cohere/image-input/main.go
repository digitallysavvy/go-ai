//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

func main() {
	p := cohere.New(cohere.Config{APIKey: os.Getenv("COHERE_API_KEY")})
	model, err := p.LanguageModel("command-a-vision-07-2025")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Describe this image in one sentence."},
				types.FileContent{
					FileData: types.FileData{
						Type:      types.FileDataTypeURL,
						URL:       "https://example.com/image.png",
						MediaType: "image/png",
					},
					ProviderOptions: map[string]interface{}{
						"cohere": map[string]interface{}{"imageDetail": "auto"},
					},
				},
			},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
