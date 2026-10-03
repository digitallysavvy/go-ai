// Recipe: use Claude with reasoning.
//
// Run: ANTHROPIC_API_KEY=... go run ./examples/recipes/claude-reasoning
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	model, err := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}).
		LanguageModel(anthropic.ClaudeSonnet5_5)
	if err != nil {
		log.Fatal(err)
	}

	// One portable setting. The SDK maps it to Claude's thinking budget.
	effort := types.ReasoningMedium
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:     model,
		Prompt:    "A train leaves at 3:40 and the trip takes 2 hours 35 minutes. When does it arrive?",
		Reasoning: &effort,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Reasoning blocks are content parts of the final step.
	if len(result.Steps) > 0 {
		for _, part := range result.Steps[len(result.Steps)-1].Content {
			if rc, ok := part.(types.ReasoningContent); ok {
				fmt.Println("reasoning:", rc.Text)
			}
		}
	}
	fmt.Println("answer:", result.Text)
}
