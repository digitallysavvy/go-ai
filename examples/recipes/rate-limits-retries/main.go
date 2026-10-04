// Recipe: handle rate limits, retries and timeouts.
//
// Run: OPENAI_API_KEY=... go run ./examples/recipes/rate-limits-retries
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	model, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).
		LanguageModel(openai.ModelGPT6Astra)
	if err != nil {
		log.Fatal(err)
	}

	// The SDK retries 408, 409, 429 and 5xx responses with exponential
	// backoff and honors Retry-After. The default is 2 retries.
	maxRetries := 4
	total := 60 * time.Second

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:      model,
		Prompt:     "Name three Go proverbs.",
		MaxRetries: &maxRetries,
		Timeout:    &ai.TimeoutConfig{Total: &total},
	})
	if err != nil {
		// RetryError means the SDK gave up. Its last error is the cause.
		var retryErr *providererrors.RetryError
		if errors.As(err, &retryErr) {
			fmt.Printf("gave up after %d attempts (%s)\n", len(retryErr.Errors), retryErr.Reason)
		}
		var perr *providererrors.ProviderError
		if errors.As(err, &perr) && perr.StatusCode == 429 {
			fmt.Println("rate limited; retry-after:", perr.ResponseHeaders["retry-after"])
		}
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
