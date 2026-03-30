package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{APIKey: apiKey})

	model, err := p.LanguageModel("claude-sonnet-4-6-20251231")
	if err != nil {
		log.Fatalf("failed to get model: %v", err)
	}

	fmt.Println("=== web_fetch_20260209 — Fetch Plain Text Page ===")
	runTextFetch(model)

	fmt.Println("\n=== web_fetch_20260209 — Fetch with Citations ===")
	runFetchWithCitations(model)
}

// runTextFetch demonstrates fetching a plain text web page.
func runTextFetch(model provider.LanguageModel) {
	maxTokens := 4096
	fetchTool := tools.WebFetch20260209(tools.WebFetch20260209Config{
		MaxContentTokens: &maxTokens,
	})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		System: "You are a helpful assistant. Use web fetch to read the content of URLs.",
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Please fetch and summarize the content at https://docs.anthropic.com/en/docs/about-claude/models/overview"},
				},
			},
		},
		Tools:     []types.Tool{fetchTool},
		MaxTokens: intPtr(1024),
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
}

// runFetchWithCitations demonstrates fetching with citation support enabled.
func runFetchWithCitations(model provider.LanguageModel) {
	maxTokens := 8192
	fetchTool := tools.WebFetch20260209(tools.WebFetch20260209Config{
		MaxContentTokens: &maxTokens,
		Citations:        &tools.WebFetchCitations{Enabled: true},
		AllowedDomains:   []string{"docs.anthropic.com"},
	})

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model: model,
		Messages: []types.Message{
			{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "Fetch the Anthropic model overview page and cite the specific model capabilities mentioned."},
				},
			},
		},
		Tools:     []types.Tool{fetchTool},
		MaxTokens: intPtr(1024),
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result.Text)
}

func intPtr(v int) *int { return &v }
