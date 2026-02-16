//go:build ignore
// +build ignore

package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
)

// This example demonstrates a multi-turn conversation chatbot using a local LLM
// Prerequisites:
// 1. Install and start LMStudio (or Ollama, LocalAI, etc.)
// 2. Load a conversational model (e.g., Mistral-7B-Instruct, Llama-2-7B-Chat)

func main() {
	// Create Open Responses provider
	provider := openresponses.New(openresponses.Config{
		BaseURL: "http://localhost:1234/v1",
		Name:    "lmstudio",
	})

	// Get language model
	model, err := provider.LanguageModel("local-model")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// System prompt to guide the chatbot's behavior
	systemPrompt := "You are a helpful, friendly assistant. Keep your responses concise and conversational."

	// Message history
	conversation := []types.Message{}

	// Create a reader for user input
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=== Local LLM Chatbot ===")
	fmt.Println("Type your messages and press Enter. Type 'exit' or 'quit' to end the conversation.")
	fmt.Println()

	for {
		// Get user input
		fmt.Print("You: ")
		userInput, err := reader.ReadString('
')
		if err != nil {
			log.Printf("Error reading input: %v", err)
			break
		}

		userInput = strings.TrimSpace(userInput)

		// Check for exit commands
		if strings.ToLower(userInput) == "exit" || strings.ToLower(userInput) == "quit" {
			fmt.Println("Goodbye!")
			break
		}

		// Skip empty input
		if userInput == "" {
			continue
		}

		// Add user message to conversation
		conversation = append(conversation, types.Message{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: userInput},
			},
		})

		// Generate response
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:    model,
			System:   systemPrompt,
			Messages: conversation,
		})
		if err != nil {
			log.Printf("Error generating response: %v", err)
			// Remove the failed user message
			conversation = conversation[:len(conversation)-1]
			continue
		}

		// Print assistant response
		fmt.Printf("
Assistant: %s

", result.Text)

		// Add assistant message to conversation
		conversation = append(conversation, types.Message{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.TextContent{Text: result.Text},
			},
		})

		// Print token usage
		fmt.Printf("(Tokens: %d)
", result.Usage.GetTotalTokens())
	}

	// Print conversation summary
	fmt.Printf("

=== Conversation Summary ===
")
	fmt.Printf("Total turns: %d
", len(conversation)/2)
}
