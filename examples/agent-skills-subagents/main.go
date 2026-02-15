package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set")
	}

	// Create OpenAI provider and model
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// ========================================================================
	// Create main coordinator agent with skills
	// ========================================================================
	mainConfig := agent.AgentConfig{
		Model:    model,
		System:   "You are a main coordinator agent with text processing skills and specialized subagents.",
		MaxSteps: 5,
	}
	mainAgent := agent.NewToolLoopAgent(mainConfig)

	// Add text processing skills to main agent
	formatSkill := &agent.Skill{
		Name:        "format_text",
		Description: "Formats text by removing extra whitespace and normalizing line breaks",
		Handler: func(ctx context.Context, input string) (string, error) {
			// Remove extra whitespace
			lines := strings.Split(input, "\n")
			formatted := make([]string, 0)
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					formatted = append(formatted, trimmed)
				}
			}
			return strings.Join(formatted, "\n"), nil
		},
	}
	mainAgent.AddSkill(formatSkill)

	// ========================================================================
	// Create content analysis subagent with skills
	// ========================================================================
	contentConfig := agent.AgentConfig{
		Model:    model,
		System:   "You are a content analysis specialist. Analyze text content and provide detailed insights.",
		MaxSteps: 3,
	}
	contentAgent := agent.NewToolLoopAgent(contentConfig)

	// Add analysis skills
	sentimentSkill := &agent.Skill{
		Name:        "analyze_sentiment",
		Description: "Analyzes the sentiment of text",
		Handler: func(ctx context.Context, input string) (string, error) {
			// Simple sentiment analysis (mock)
			positive := strings.Count(strings.ToLower(input), "good") +
				strings.Count(strings.ToLower(input), "great") +
				strings.Count(strings.ToLower(input), "excellent")
			negative := strings.Count(strings.ToLower(input), "bad") +
				strings.Count(strings.ToLower(input), "poor") +
				strings.Count(strings.ToLower(input), "terrible")

			if positive > negative {
				return "Sentiment: Positive", nil
			} else if negative > positive {
				return "Sentiment: Negative", nil
			}
			return "Sentiment: Neutral", nil
		},
	}

	readabilitySkill := &agent.Skill{
		Name:        "analyze_readability",
		Description: "Analyzes text readability",
		Handler: func(ctx context.Context, input string) (string, error) {
			words := strings.Fields(input)
			sentences := strings.Count(input, ".") + strings.Count(input, "!") + strings.Count(input, "?")
			if sentences == 0 {
				sentences = 1
			}
			avgWordsPerSentence := float64(len(words)) / float64(sentences)
			return fmt.Sprintf("Readability: %.1f words per sentence", avgWordsPerSentence), nil
		},
	}

	contentAgent.AddSkill(sentimentSkill)
	contentAgent.AddSkill(readabilitySkill)

	// ========================================================================
	// Create summarization subagent with skills
	// ========================================================================
	summaryConfig := agent.AgentConfig{
		Model:    model,
		System:   "You are a summarization specialist. Create concise summaries of text content.",
		MaxSteps: 3,
	}
	summaryAgent := agent.NewToolLoopAgent(summaryConfig)

	// Add summarization skill
	extractKeywordsSkill := &agent.Skill{
		Name:        "extract_keywords",
		Description: "Extracts key words from text",
		Handler: func(ctx context.Context, input string) (string, error) {
			words := strings.Fields(input)
			// Simple keyword extraction (get longer words)
			keywords := make([]string, 0)
			for _, word := range words {
				cleaned := strings.Trim(strings.ToLower(word), ".,!?;:")
				if len(cleaned) > 5 {
					keywords = append(keywords, cleaned)
				}
			}
			if len(keywords) > 5 {
				keywords = keywords[:5]
			}
			return fmt.Sprintf("Keywords: %s", strings.Join(keywords, ", ")), nil
		},
	}
	summaryAgent.AddSkill(extractKeywordsSkill)

	// ========================================================================
	// Register subagents with main agent
	// ========================================================================
	mainAgent.AddSubagent("content_analyzer", contentAgent)
	mainAgent.AddSubagent("summarizer", summaryAgent)

	// ========================================================================
	// Demo: Process a document using skills and subagents
	// ========================================================================
	sampleText := `
	This is a great example of how Go can be used to build powerful AI applications.
	The Go-AI SDK makes it easy to work with language models.

	We love how clean and efficient the code is. It's excellent for building production systems.
	The performance is outstanding and the developer experience is fantastic.
	`

	fmt.Println("=== Document Processing Pipeline ===\n")
	fmt.Println("Original Text:")
	fmt.Println(sampleText)
	fmt.Println()

	// Step 1: Format the text using main agent's skill
	fmt.Println("Step 1: Formatting text...")
	formattedText, err := mainAgent.ExecuteSkill(context.Background(), "format_text", sampleText)
	if err != nil {
		log.Fatalf("Format failed: %v", err)
	}
	fmt.Println("Formatted Text:")
	fmt.Println(formattedText)
	fmt.Println()

	// Step 2: Analyze content using content analyzer subagent's skills
	fmt.Println("Step 2: Analyzing content...")

	// Get the content analyzer subagent
	contentSubagent, _ := mainAgent.GetSubagent("content_analyzer")
	contentToolLoopAgent := contentSubagent.(*agent.ToolLoopAgent)

	sentimentResult, _ := contentToolLoopAgent.ExecuteSkill(
		context.Background(),
		"analyze_sentiment",
		formattedText,
	)
	fmt.Println(sentimentResult)

	readabilityResult, _ := contentToolLoopAgent.ExecuteSkill(
		context.Background(),
		"analyze_readability",
		formattedText,
	)
	fmt.Println(readabilityResult)
	fmt.Println()

	// Step 3: Extract keywords using summarizer subagent's skill
	fmt.Println("Step 3: Extracting keywords...")

	summarySubagent, _ := mainAgent.GetSubagent("summarizer")
	summaryToolLoopAgent := summarySubagent.(*agent.ToolLoopAgent)

	keywordsResult, _ := summaryToolLoopAgent.ExecuteSkill(
		context.Background(),
		"extract_keywords",
		formattedText,
	)
	fmt.Println(keywordsResult)
	fmt.Println()

	// Step 4: Delegate full analysis to content analyzer subagent
	fmt.Println("Step 4: Full content analysis via delegation...")
	analysisResult, err := mainAgent.DelegateToSubagent(
		context.Background(),
		"content_analyzer",
		"Provide a complete analysis of this text: "+formattedText,
	)
	if err != nil {
		log.Fatalf("Delegation failed: %v", err)
	}
	fmt.Printf("Analysis: %s\n", analysisResult.Text)
	fmt.Println()

	// Step 5: Delegate summarization to summarizer subagent
	fmt.Println("Step 5: Summarization via delegation...")
	summaryResult, err := mainAgent.DelegateToSubagent(
		context.Background(),
		"summarizer",
		"Create a brief summary of this text: "+formattedText,
	)
	if err != nil {
		log.Fatalf("Delegation failed: %v", err)
	}
	fmt.Printf("Summary: %s\n", summaryResult.Text)
	fmt.Println()

	// Step 6: Show capabilities
	fmt.Println("=== System Capabilities ===")
	fmt.Println("\nMain Agent Skills:")
	for _, skill := range mainAgent.ListSkills() {
		fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
	}

	fmt.Println("\nMain Agent Subagents:")
	for _, name := range mainAgent.ListSubagents() {
		fmt.Printf("  - %s\n", name)
	}

	fmt.Println("\nContent Analyzer Skills:")
	for _, skill := range contentToolLoopAgent.ListSkills() {
		fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
	}

	fmt.Println("\nSummarizer Skills:")
	for _, skill := range summaryToolLoopAgent.ListSkills() {
		fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
	}

	fmt.Println("\n✅ Skills and subagents integration example completed successfully!")
}
