//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Example: Streaming text generation with gpt-5.3-codex
// Demonstrates streaming using the gpt-5.3-codex model, printing tokens as they arrive.
//
// Run with: OPENAI_API_KEY=... go run gpt-5.3-codex-stream.go

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	prov := openai.New(openai.Config{APIKey: apiKey})

	model, err := prov.LanguageModel("gpt-5.3-codex")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	ctx := context.Background()

	// Streaming code generation
	fmt.Println("=== gpt-5.3-codex: Streaming Code Generation ===")
	fmt.Println()

	stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleSystem,
				Content: []types.ContentPart{
					types.TextContent{Text: "You are an expert Go programmer. Write clean, idiomatic Go code with comments."},
				},
			},
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Implement a thread-safe LRU cache in Go with Get and Put operations."},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}

	// Print tokens as they arrive
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatalf("Stream error: %v", err)
	}
	fmt.Println()
	fmt.Println()

	// Streaming code review
	fmt.Println("=== gpt-5.3-codex: Streaming Code Review ===")
	fmt.Println()

	code := `
func processItems(items []string) []string {
    var result []string
    for i := 0; i < len(items); i++ {
        if items[i] != "" {
            result = append(result, items[i])
        }
    }
    return result
}`

	reviewStream, err := ai.StreamText(ctx, ai.StreamTextOptions{
		Model:  model,
		Prompt: fmt.Sprintf("Review this Go code and suggest idiomatic improvements:\n```go%s\n```", code),
	})
	if err != nil {
		log.Fatalf("Failed to start review stream: %v", err)
	}

	for chunk := range reviewStream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
	if err := reviewStream.Err(); err != nil {
		log.Fatalf("Review stream error: %v", err)
	}
	fmt.Println()
}
