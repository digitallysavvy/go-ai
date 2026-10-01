//go:build ignore

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
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	uploaded, err := ai.UploadFile(context.Background(), ai.UploadFileOptions{
		API:       p,
		Data:      []byte("Go AI SDK upload example"),
		Filename:  "note.txt",
		MediaType: "text/plain",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"purpose": "assistants"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	file := types.FileContent{
		FileData: types.FileData{
			Type:      types.FileDataTypeReference,
			Reference: uploaded.ProviderReference,
		},
		MediaType: uploaded.MediaType,
		Filename:  uploaded.Filename,
	}

	fmt.Printf("uploaded reference: %#v\n", file.FileData.Reference)
}
