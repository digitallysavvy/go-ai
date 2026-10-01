// Package main demonstrates uploading a file to Anthropic's Files API via the
// provider-agnostic ai.UploadFile helper.
//
// Run with:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{APIKey: apiKey})

	res, err := ai.UploadFile(context.Background(), ai.UploadFileOptions{
		API:       p,
		Data:      []byte("hello"),
		Filename:  "hello.txt",
		MediaType: "text/plain",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("uploaded reference:", res.ProviderReference)
}
