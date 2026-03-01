// Package main demonstrates using GenerateText with ChoiceOutput to classify
// text into one of a fixed set of options.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Sentiment is a string enum type for sentiment classification.
type Sentiment string

const (
	Positive Sentiment = "positive"
	Negative Sentiment = "negative"
	Neutral  Sentiment = "neutral"
)

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("failed to create model: %v", err)
	}

	reviews := []string{
		"This product is absolutely amazing! Best purchase I've ever made.",
		"Terrible quality. Broke after two days. Complete waste of money.",
		"It's okay. Does what it says, nothing more.",
	}

	for _, review := range reviews {
		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: fmt.Sprintf("Classify the sentiment of this review: %q", review),
			Output: ai.ChoiceOutput[Sentiment](ai.ChoiceOutputOptions[Sentiment]{
				Options:     []Sentiment{Positive, Negative, Neutral},
				Name:        "sentiment",
				Description: "The overall sentiment of the review",
			}),
		})
		if err != nil {
			log.Fatalf("generation failed: %v", err)
		}

		// result.Output is typed as 'any'; type-assert to the concrete type.
		sentiment, ok := result.Output.(Sentiment)
		if !ok {
			log.Fatalf("unexpected output type: %T", result.Output)
		}

		fmt.Printf("Review: %.60s...\nSentiment: %s\n\n", review, sentiment)
	}
}
