package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

type RetryMiddleware struct {
	maxRetries int
	baseDelay  time.Duration
}

func NewRetryMiddleware(maxRetries int, baseDelay time.Duration) *RetryMiddleware {
	return &RetryMiddleware{maxRetries: maxRetries, baseDelay: baseDelay}
}

func (m *RetryMiddleware) GenerateText(ctx context.Context, opts ai.GenerateTextOptions) (*ai.GenerateTextResult, error) {
	var lastErr error

	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		if attempt > 0 {
			delay := m.baseDelay * time.Duration(math.Pow(2, float64(attempt-1)))
			fmt.Printf("Retry attempt %d after %v\n", attempt, delay)
			time.Sleep(delay)
		}

		result, err := ai.GenerateText(ctx, opts)
		if err == nil {
			if attempt > 0 {
				fmt.Printf("✓ Succeeded on attempt %d\n", attempt+1)
			}
			return result, nil
		}

		lastErr = err
		fmt.Printf("✗ Attempt %d failed: %v\n", attempt+1, err)
	}

	return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()
	retry := NewRetryMiddleware(3, 1*time.Second)

	fmt.Println("=== Retry Middleware Demo ===")

	result, err := retry.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say hello in 3 words",
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nResponse: %s\n", result.Text)
	fmt.Printf("Tokens: %d\n", result.Usage.GetTotalTokens())
}
