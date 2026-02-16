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

	fmt.Println("=== Streaming Agent with Real-time Updates ===")

	examples := []struct {
		name  string
		query string
	}{
		{
			name:  "Research Task",
			query: "Research the history of Go programming language and summarize key milestones",
		},
		{
			name:  "Data Analysis",
			query: "Analyze sales data for Q4 and provide insights",
		},
		{
			name:  "Code Review",
			query: "Review this code for potential issues: func divide(a, b int) int { return a / b }",
		},
	}

	for i, example := range examples {
		fmt.Printf("\n%s Example %d: %s %s\n", strings.Repeat("=", 20), i+1, example.name, strings.Repeat("=", 20))
		fmt.Printf("Query: %s\n\n", example.query)
		runStreamingAgent(ctx, model, example.query)
		fmt.Println()
	}
}

func runStreamingAgent(ctx context.Context, model provider.LanguageModel, query string) {
	// Define research and analysis tools
	tools := []types.Tool{
		createResearchTool(),
		createDataAnalysisTool(),
		createCodeAnalyzerTool(),
	}

	maxSteps := 6
	stepCount := 0

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: query,
		System: `You are a thorough research assistant. Break down complex queries into steps.
Use available tools to gather information. Think step by step and explain your process.`,
		Tools:    tools,
		MaxSteps: &maxSteps,
		OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			stepCount++
			fmt.Printf("\n[Step %d]\n", stepCount)

			// Show tool calls
			if len(step.ToolCalls) > 0 {
				fmt.Println("🔧 Tool Calls:")
				for _, tc := range step.ToolCalls {
					fmt.Printf("   • %s\n", tc.ToolName)
					if args, ok := tc.Arguments["query"].(string); ok {
						fmt.Printf("     Query: %s\n", args)
					}
					if args, ok := tc.Arguments["code"].(string); ok {
						fmt.Printf("     Code: %s\n", args)
					}
				}
			}

			// Show tool results
			if len(step.ToolResults) > 0 {
				fmt.Println("📊 Results:")
				for _, tr := range step.ToolResults {
					fmt.Printf("   %s\n", summarizeToolResult(tr.Result))
				}
			}

			// Show reasoning text (simulate streaming)
			if step.Text != "" {
				fmt.Println("💭 Reasoning:")
				fmt.Print("   ")
				simulateStreaming(step.Text)
				fmt.Println()
			}
		},
	})

	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("\n[Final Response]")
	fmt.Print("📝 ")

	// Simulate streaming the final response
	simulateStreaming(result.Text)
	fmt.Println()

	// Show final statistics
	fmt.Printf("\n[Statistics]\n")
	fmt.Printf("   Steps: %d\n", stepCount)
	fmt.Printf("   Tokens: %d (input: %d, output: %d)\n",
		result.Usage.GetTotalTokens(), result.Usage.GetInputTokens(), result.Usage.GetOutputTokens())
}

func createResearchTool() types.Tool {
	return types.Tool{
		Name:        "research",
		Description: "Research a topic and retrieve relevant information",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Research query or topic",
				},
				"depth": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"quick", "detailed", "comprehensive"},
					"description": "Research depth",
					"default":     "detailed",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := params["query"].(string)
			depth := "detailed"
			if d, ok := params["depth"].(string); ok {
				depth = d
			}

			// Simulate research with delay
			time.Sleep(300 * time.Millisecond)

			// Return simulated research data
			if strings.Contains(strings.ToLower(query), "go") || strings.Contains(strings.ToLower(query), "golang") {
				return map[string]interface{}{
					"topic": "Go Programming Language",
					"findings": []string{
						"Created at Google in 2007 by Robert Griesemer, Rob Pike, and Ken Thompson",
						"First released as open source in November 2009",
						"Version 1.0 released in March 2012",
						"Known for simplicity, concurrency support (goroutines), and fast compilation",
						"Used by companies like Google, Uber, Docker, and Kubernetes",
					},
					"sources":   []string{"golang.org", "Wikipedia", "ACM Digital Library"},
					"depth":     depth,
					"timestamp": time.Now().Format(time.RFC3339),
				}, nil
			}

			return map[string]interface{}{
				"topic": query,
				"findings": []string{
					"Relevant information found",
					"Multiple sources consulted",
					"Cross-referenced data points",
				},
				"sources":   []string{"source1.com", "source2.com"},
				"depth":     depth,
				"timestamp": time.Now().Format(time.RFC3339),
			}, nil
		},
	}
}

func createDataAnalysisTool() types.Tool {
	return types.Tool{
		Name:        "analyze_data",
		Description: "Analyze data and generate insights",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"dataset": map[string]interface{}{
					"type":        "string",
					"description": "Name or description of dataset to analyze",
				},
				"metrics": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "Metrics to calculate",
				},
			},
			"required": []string{"dataset"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			dataset := params["dataset"].(string)

			// Simulate analysis
			time.Sleep(400 * time.Millisecond)

			return map[string]interface{}{
				"dataset": dataset,
				"summary": map[string]interface{}{
					"totalRecords": 1543,
					"timeRange":    "Oct 1 - Dec 31",
					"completeness": "98.5%",
				},
				"insights": []string{
					"Strong growth trend observed in November (+25%)",
					"December showed seasonal spike (+40%)",
					"Weekend performance exceeded weekday by 15%",
					"Top performing category: Electronics",
				},
				"recommendations": []string{
					"Increase inventory for peak periods",
					"Focus marketing on high-performing categories",
					"Optimize weekend staffing",
				},
				"timestamp": time.Now().Format(time.RFC3339),
			}, nil
		},
	}
}

func createCodeAnalyzerTool() types.Tool {
	return types.Tool{
		Name:        "analyze_code",
		Description: "Analyze code for potential issues, bugs, or improvements",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code": map[string]interface{}{
					"type":        "string",
					"description": "Code to analyze",
				},
				"language": map[string]interface{}{
					"type":        "string",
					"description": "Programming language",
					"default":     "go",
				},
			},
			"required": []string{"code"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			code := params["code"].(string)
			language := "go"
			if lang, ok := params["language"].(string); ok {
				language = lang
			}

			// Simulate code analysis
			time.Sleep(350 * time.Millisecond)

			issues := []map[string]interface{}{}

			// Check for common issues
			if strings.Contains(code, "/ b") && !strings.Contains(code, "if b == 0") {
				issues = append(issues, map[string]interface{}{
					"type":        "critical",
					"category":    "divide-by-zero",
					"description": "Potential division by zero without validation",
					"line":        1,
					"suggestion":  "Add check: if b == 0 { return error }",
				})
			}

			if !strings.Contains(code, "error") && strings.Contains(code, "func") {
				issues = append(issues, map[string]interface{}{
					"type":        "warning",
					"category":    "error-handling",
					"description": "Function should return error for better error handling",
					"suggestion":  "Change signature to return (int, error)",
				})
			}

			return map[string]interface{}{
				"language":    language,
				"linesOfCode": len(strings.Split(code, "\n")),
				"issues":      issues,
				"summary": map[string]interface{}{
					"critical": 1,
					"warning":  1,
					"info":     0,
				},
				"timestamp": time.Now().Format(time.RFC3339),
			}, nil
		},
	}
}

func summarizeToolResult(result interface{}) string {
	data, ok := result.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("%v", result)
	}

	// Research results
	if findings, ok := data["findings"].([]string); ok {
		return fmt.Sprintf("Found %d findings on %s", len(findings), data["topic"])
	}

	// Data analysis results
	if insights, ok := data["insights"].([]string); ok {
		return fmt.Sprintf("Generated %d insights from analysis", len(insights))
	}

	// Code analysis results
	if issues, ok := data["issues"].([]interface{}); ok {
		return fmt.Sprintf("Identified %d code issues", len(issues))
	}

	return fmt.Sprintf("%v", result)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func simulateStreaming(text string) {
	// Simulate streaming by printing character by character with slight delay
	words := strings.Fields(text)
	lineLength := 0

	for i, word := range words {
		fmt.Print(word)
		lineLength += len(word)

		if i < len(words)-1 {
			fmt.Print(" ")
			lineLength++
		}

		// Add newline for readability
		if lineLength > 80 && i < len(words)-1 {
			fmt.Println()
			fmt.Print("   ")
			lineLength = 0
		}

		// Small delay to simulate streaming
		time.Sleep(20 * time.Millisecond)
	}
}
