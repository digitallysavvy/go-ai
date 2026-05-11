//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	p := xai.New(xai.Config{APIKey: os.Getenv("XAI_API_KEY")})
	model, err := p.LanguageModel("grok-4")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Summarize the attached report."},
				types.FileContent{FileData: types.FileData{
					Type:      types.FileDataTypeURL,
					URL:       "https://example.com/report.pdf",
					MediaType: "application/pdf",
				}},
			},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
