// Package main demonstrates the full upload-then-reference round trip: upload
// a file via the provider-agnostic ai.UploadFile helper, then pass the
// returned provider reference into a subsequent ai.GenerateText call as
// FileContent so the model can read it.
//
// Run with:
//
//	export OPENAI_API_KEY=sk-...
//	go run main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	ctx := context.Background()
	p := openai.New(openai.Config{APIKey: apiKey})

	upload, err := ai.UploadFile(ctx, ai.UploadFileOptions{
		API:       p,
		Data:      []byte("Quarterly revenue increased 12% year over year."),
		MediaType: "text/plain",
		Filename:  "report.txt",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"purpose": "assistants"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	model, err := p.ResponsesModel("gpt-4.1")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Summarize this uploaded file in one sentence."},
					types.FileContent{
						MediaType: upload.MediaType,
						Filename:  upload.Filename,
						FileData: types.FileData{
							Type:      types.FileDataTypeReference,
							Reference: upload.ProviderReference,
							MediaType: upload.MediaType,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
