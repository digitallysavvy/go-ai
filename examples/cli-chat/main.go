package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create provider and model
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})
	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Initialize conversation history
	messages := []types.Message{}

	// Create reader for user input
	reader := bufio.NewReader(os.Stdin)

	// Print welcome message
	fmt.Println("╔════════════════════════════════════════╗")
	fmt.Println("║   Go AI SDK - Interactive Chat CLI     ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /exit    - Exit the chat")
	fmt.Println("  /clear   - Clear conversation history")
	fmt.Println("  /help    - Show this help message")
	fmt.Println()

	// Main chat loop
	for {
		// Get user input
		fmt.Print("\n\033[1;32mYou:\033[0m ")
		userInput, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading input: %v", err)
			continue
		}

		userInput = strings.TrimSpace(userInput)

		// Handle commands
		if strings.HasPrefix(userInput, "/") {
			if handleCommand(userInput, &messages) {
				break // Exit loop if exit command
			}
			continue
		}

		// Skip empty input
		if userInput == "" {
			continue
		}

		// Add user message to history
		messages = append(messages, types.Message{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: userInput},
			},
		})

		// Stream the AI response
		stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
			Model:    model,
			Messages: messages,
		})
		if err != nil {
			log.Printf("Error: %v\n", err)
			// Remove the user message we just added
			messages = messages[:len(messages)-1]
			continue
		}

		// Print assistant response with streaming
		fmt.Print("\n\033[1;34mAssistant:\033[0m ")
		var fullResponse strings.Builder
		for chunk := range stream.Chunks() {
			if chunk.Type == provider.ChunkTypeText {
				fmt.Print(chunk.Text)
				fullResponse.WriteString(chunk.Text)
			}
		}
		fmt.Println()

		// Check for stream errors
		if err := stream.Err(); err != nil {
			log.Printf("Stream error: %v\n", err)
			// Remove the user message we just added
			messages = messages[:len(messages)-1]
			continue
		}

		// Add assistant response to history
		messages = append(messages, types.Message{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: fullResponse.String()},
			},
		})

		// Show token usage
		usage := stream.Usage()
		fmt.Printf("\n\033[2m(Tokens: %d)\033[0m", usage.GetTotalTokens())
	}
}

// handleCommand processes special commands
func handleCommand(cmd string, messages *[]types.Message) bool {
	switch cmd {
	case "/exit":
		fmt.Println("\nGoodbye! 👋")
		return true

	case "/clear":
		*messages = []types.Message{}
		fmt.Println("\n✓ Conversation history cleared")
		return false

	case "/help":
		fmt.Println("\nAvailable commands:")
		fmt.Println("  /exit    - Exit the chat")
		fmt.Println("  /clear   - Clear conversation history")
		fmt.Println("  /help    - Show this help message")
		return false

	default:
		fmt.Printf("\nUnknown command: %s (use /help for available commands)\n", cmd)
		return false
	}
}
