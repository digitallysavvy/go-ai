package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Web Search Agent ===")

	// Define search and retrieval tools
	tools := []types.Tool{
		createWebSearchTool(),
		createPageContentTool(),
		createFactCheckTool(),
	}

	queries := []string{
		"What are the latest developments in AI in 2024?",
		"Find information about Go programming language best practices",
		"What is the current price of Bitcoin?",
	}

	for i, query := range queries {
		fmt.Printf("Query %d: %s\n", i+1, query)
		fmt.Println(strings.Repeat("=", 60))
		searchAndAnswer(ctx, model, query, tools)
		fmt.Println()
	}
}

func searchAndAnswer(ctx context.Context, model provider.LanguageModel, query string, tools []types.Tool) {
	maxSteps := 8

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: query,
		System: `You are a research assistant with web search capabilities.
When answering questions:
1. Search for relevant information
2. Retrieve page content if needed
3. Synthesize information from multiple sources
4. Cite your sources
5. Fact-check important claims`,
		Tools:    tools,
		MaxSteps: &maxSteps,
		OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			if len(step.ToolCalls) > 0 {
				for _, tc := range step.ToolCalls {
					fmt.Printf("  [Tool] %s: %v\n", tc.ToolName, tc.Arguments)
				}
			}

			if len(step.ToolResults) > 0 {
				for _, tr := range step.ToolResults {
					fmt.Printf("  [Result] %s\n", summarizeResult(tr.Result))
				}
			}
		},
	})

	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("\n[Answer]")
	fmt.Println(result.Text)
	fmt.Printf("\nSteps taken: %d | Tokens: %d\n", len(result.Steps), result.Usage.TotalTokens)
}

func createWebSearchTool() types.Tool {
	return types.Tool{
		Name:        "web_search",
		Description: "Search the web for information on a topic",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
				"numResults": map[string]interface{}{
					"type":        "integer",
					"description": "Number of results to return (default 5)",
					"default":     5,
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := params["query"].(string)
			numResults := 5
			if n, ok := params["numResults"].(float64); ok {
				numResults = int(n)
			}

			// Simulate search results
			time.Sleep(500 * time.Millisecond)

			results := []map[string]interface{}{
				{
					"title":   "Latest AI Developments - Tech News",
					"url":     "https://example.com/ai-news",
					"snippet": "Recent breakthroughs in AI include improvements in large language models...",
				},
				{
					"title":   "AI in 2024: What to Expect",
					"url":     "https://example.com/ai-2024",
					"snippet": "Industry experts predict major advances in AI reasoning and multimodal capabilities...",
				},
				{
					"title":   "Open Source AI Projects",
					"url":     "https://example.com/opensource-ai",
					"snippet": "Community-driven AI projects continue to democratize access to AI technology...",
				},
			}

			if numResults < len(results) {
				results = results[:numResults]
			}

			return map[string]interface{}{
				"query":       query,
				"results":     results,
				"resultCount": len(results),
			}, nil
		},
	}
}

func createPageContentTool() types.Tool {
	return types.Tool{
		Name:        "get_page_content",
		Description: "Retrieve the full content of a web page",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "URL of the page to retrieve",
				},
			},
			"required": []string{"url"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			url := params["url"].(string)

			// Simulate page content retrieval
			time.Sleep(300 * time.Millisecond)

			return map[string]interface{}{
				"url": url,
				"content": `Large language models have seen significant improvements in 2024.
Key developments include enhanced reasoning capabilities, better multimodal understanding,
and more efficient training methods. Several companies have released models that can process
longer contexts and provide more accurate responses.`,
				"title":       "AI Developments 2024",
				"publishDate": "2024-01-15",
			}, nil
		},
	}
}

func createFactCheckTool() types.Tool {
	return types.Tool{
		Name:        "fact_check",
		Description: "Verify factual claims by checking multiple sources",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"claim": map[string]interface{}{
					"type":        "string",
					"description": "Claim to fact-check",
				},
			},
			"required": []string{"claim"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			claim := params["claim"].(string)

			// Simulate fact checking
			time.Sleep(400 * time.Millisecond)

			return map[string]interface{}{
				"claim":      claim,
				"verified":   true,
				"confidence": 0.95,
				"sources":    []string{"source1.com", "source2.com", "source3.com"},
				"summary":    "Claim is supported by multiple reliable sources",
			}, nil
		},
	}
}

func summarizeResult(result interface{}) string {
	if data, ok := result.(map[string]interface{}); ok {
		if results, ok := data["results"].([]map[string]interface{}); ok {
			return fmt.Sprintf("Found %d search results", len(results))
		}
		if content, ok := data["content"].(string); ok {
			if len(content) > 100 {
				return content[:100] + "..."
			}
			return content
		}
		if verified, ok := data["verified"].(bool); ok {
			return fmt.Sprintf("Fact check: %v", verified)
		}
	}
	return fmt.Sprintf("%v", result)
}
