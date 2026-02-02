package anthropic

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestCacheTTL5Minutes tests cache functionality with 5-minute TTL
func TestCacheTTL5Minutes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Create provider with 5-minute cache
	ttl := CacheTTL5Minutes
	prov := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		CacheConfig: NewCacheConfig(
			WithCacheTTL(ttl),
			WithSystemCache(),
		),
	})

	model, err := prov.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Create large context to cache
	largeContext := strings.Repeat("This is a test document. ", 500)

	// First request - should create cache
	result1, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: largeContext,
			Text:   "What is this document about?",
		},
	})
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Verify cache was created
	if result1.Usage.InputDetails == nil {
		t.Fatal("Expected input details in usage")
	}
	if result1.Usage.InputDetails.CacheWriteTokens == nil || *result1.Usage.InputDetails.CacheWriteTokens == 0 {
		t.Error("Expected cache write tokens on first request")
	}

	// Second request - should use cache
	result2, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: largeContext,
			Text:   "Summarize the document.",
		},
	})
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	// Verify cache was used
	if result2.Usage.InputDetails == nil {
		t.Fatal("Expected input details in usage")
	}
	if result2.Usage.InputDetails.CacheReadTokens == nil || *result2.Usage.InputDetails.CacheReadTokens == 0 {
		t.Error("Expected cache read tokens on second request")
	}
}

// TestCacheTTL1Hour tests cache functionality with 1-hour TTL
func TestCacheTTL1Hour(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Create provider with 1-hour cache
	ttl := CacheTTL1Hour
	prov := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		CacheConfig: NewCacheConfig(
			WithCacheTTL(ttl),
			WithSystemCache(),
		),
	})

	model, err := prov.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Create large context to cache
	largeContext := strings.Repeat("This is a test document for 1-hour caching. ", 500)

	// First request - should create cache with 1h TTL
	result1, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: largeContext,
			Text:   "What is this document about?",
		},
	})
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Verify cache was created
	if result1.Usage.InputDetails == nil {
		t.Fatal("Expected input details in usage")
	}
	if result1.Usage.InputDetails.CacheWriteTokens == nil || *result1.Usage.InputDetails.CacheWriteTokens == 0 {
		t.Error("Expected cache write tokens on first request")
	}
}

// TestCacheWithTools tests cache functionality with tool definitions
func TestCacheWithTools(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Create provider with tool caching
	ttl := CacheTTL1Hour
	prov := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		CacheConfig: NewCacheConfig(
			WithCacheTTL(ttl),
			WithSystemCache(),
			WithToolCache(),
		),
	})

	model, err := prov.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Define tools
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"location"},
		},
	}

	// First request - should cache tools
	result1, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: "You are a helpful assistant.",
			Text:   "What tools do you have?",
		},
		Tools: []types.Tool{weatherTool},
	})
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Verify cache was created
	if result1.Usage.InputDetails == nil {
		t.Fatal("Expected input details in usage")
	}
	if result1.Usage.InputDetails.CacheWriteTokens == nil || *result1.Usage.InputDetails.CacheWriteTokens == 0 {
		t.Error("Expected cache write tokens for tools on first request")
	}
}

// TestBackwardCompatibility tests that caching works without TTL specified
func TestBackwardCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Create provider with cache but no TTL (should default to 5m)
	prov := New(Config{
		Region: "us-east-1",
		Credentials: &AWSCredentials{
			AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
		},
		CacheConfig: NewCacheConfig(
			WithSystemCache(),
		),
	})

	model, err := prov.LanguageModel("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatalf("Failed to get model: %v", err)
	}

	// Create large context
	largeContext := strings.Repeat("Test content. ", 500)

	// Request should work with default TTL
	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			System: largeContext,
			Text:   "What is this?",
		},
	})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Verify cache was created with default TTL
	if result.Usage.InputDetails == nil {
		t.Fatal("Expected input details in usage")
	}
	if result.Usage.InputDetails.CacheWriteTokens == nil || *result.Usage.InputDetails.CacheWriteTokens == 0 {
		t.Error("Expected cache write tokens with default TTL")
	}
}
