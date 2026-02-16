package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func main() {
	// Create gateway provider
	provider, err := gateway.New(gateway.Config{
		APIKey: os.Getenv("AI_GATEWAY_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create gateway provider: %v", err)
	}

	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("Gateway Timeout Handling Examples")
	fmt.Println(strings.Repeat("=", 70))

	// Example 1: Text generation with short timeout (will likely timeout)
	fmt.Println("\nExample 1: Text Generation with Short Timeout")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("This example demonstrates timeout error handling for text generation.")
	fmt.Println("Using a very short timeout to trigger a timeout error...")

	textModel, err := provider.LanguageModel("openai/gpt-4")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	// Create context with very short timeout (100ms)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel1()

	_, err = ai.GenerateText(ctx1, ai.GenerateTextOptions{
		Model: textModel,
		Prompt: "Write a detailed essay about the history of artificial intelligence.",
		MaxTokens: ptr(1000),
	})

	if err != nil {
		fmt.Println("\n❌ Error occurred (as expected):")
		if gatewayerrors.IsGatewayTimeoutError(err) {
			fmt.Println("   Detected: GatewayTimeoutError")
			fmt.Printf("   Message: %v\n", err)
		} else {
			fmt.Printf("   Error type: %T\n", err)
			fmt.Printf("   Message: %v\n", err)
		}
	}

	// Example 2: Text generation with appropriate timeout
	fmt.Println("\n\nExample 2: Text Generation with Appropriate Timeout")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("Using a reasonable timeout (30 seconds)...")

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	result, err := ai.GenerateText(ctx2, ai.GenerateTextOptions{
		Model: textModel,
		Prompt: "Explain what the AI Gateway is in one sentence.",
		MaxTokens: ptr(100),
	})

	if err != nil {
		if gatewayerrors.IsGatewayTimeoutError(err) {
			fmt.Println("\n❌ Timeout Error:")
			fmt.Printf("   %v\n", err)
		} else {
			log.Printf("Failed to generate text: %v", err)
		}
	} else {
		fmt.Println("\n✓ Text generated successfully!")
		fmt.Printf("  Response: %s\n", result.Text)
	}

	// Example 3: Video generation with short timeout (will likely timeout)
	fmt.Println("\n\nExample 3: Video Generation with Short Timeout")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("Video generation typically takes several minutes.")
	fmt.Println("Using a short timeout (1 second) to demonstrate timeout handling...")

	videoModel, err := provider.VideoModel("google/veo-3.1-generate-001")
	if err != nil {
		log.Fatalf("Failed to create video model: %v", err)
	}

	ctx3, cancel3 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel3()

	_, err = ai.GenerateVideo(ctx3, ai.GenerateVideoOptions{
		Model: videoModel,
		Prompt: ai.VideoPrompt{
			Text: "A cat playing with a ball",
		},
		AspectRatio: "16:9",
		Duration:    ptr(4.0),
	})

	if err != nil {
		fmt.Println("\n❌ Error occurred (as expected):")
		if gatewayerrors.IsGatewayTimeoutError(err) {
			fmt.Println("   Detected: GatewayTimeoutError")
			fmt.Println("   The error message includes troubleshooting guidance:")
			fmt.Printf("\n%v\n", err)
		} else {
			fmt.Printf("   Error type: %T\n", err)
			fmt.Printf("   Message: %v\n", err)
		}
	}

	// Example 4: Video generation with appropriate timeout
	fmt.Println("\n\nExample 4: Video Generation with Appropriate Timeout")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Println("Using an appropriate timeout for video generation (10 minutes)...")
	fmt.Println("Note: This may take several minutes to complete.")

	ctx4, cancel4 := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel4()

	result4, err := ai.GenerateVideo(ctx4, ai.GenerateVideoOptions{
		Model: videoModel,
		Prompt: ai.VideoPrompt{
			Text: "A beautiful sunset over the ocean",
		},
		AspectRatio: "16:9",
		Duration:    ptr(3.0),
	})

	if err != nil {
		if gatewayerrors.IsGatewayTimeoutError(err) {
			fmt.Println("\n❌ Timeout Error:")
			fmt.Printf("   Even with 10 minutes timeout, the request timed out.\n")
			fmt.Printf("   Consider increasing the timeout further or checking your connection.\n")
			fmt.Printf("   Error: %v\n", err)
		} else {
			log.Printf("Failed to generate video: %v", err)
		}
	} else {
		fmt.Println("\n✓ Video generated successfully!")
		fmt.Printf("  Number of videos: %d\n", len(result4.Videos))
		if len(result4.Videos) > 0 {
			fmt.Printf("  Video type: %s\n", result4.Videos[0].MediaType)
			fmt.Printf("  Media type: %s\n", result4.Videos[0].MediaType)
		}
	}

	// Summary
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("Timeout Handling Best Practices")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\n1. Text Generation:")
	fmt.Println("   - Use 30-60 seconds for simple prompts")
	fmt.Println("   - Use 2-5 minutes for complex generations")
	fmt.Println("\n2. Video Generation:")
	fmt.Println("   - Use at least 5-10 minutes")
	fmt.Println("   - Consider 15-30 minutes for complex videos")
	fmt.Println("\n3. Error Handling:")
	fmt.Println("   - Check for GatewayTimeoutError specifically")
	fmt.Println("   - Read error messages for troubleshooting guidance")
	fmt.Println("   - Adjust timeouts based on operation complexity")
	fmt.Println("\n4. Production Recommendations:")
	fmt.Println("   - Use context.WithTimeout() for all operations")
	fmt.Println("   - Implement retry logic for transient failures")
	fmt.Println("   - Log timeout errors for monitoring")
	fmt.Println("   - Consider async processing for long operations")
}

func ptr[T any](v T) *T {
	return &v
}
