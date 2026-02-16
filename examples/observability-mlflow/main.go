package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/observability/mlflow"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// This example demonstrates how to use MLflow observability with the Go AI SDK.
//
// Prerequisites:
// 1. Start MLflow tracking server:
//    mlflow server --backend-store-uri sqlite:///mlruns.db --port 5000
//
// 2. Set environment variables:
//    export OPENAI_API_KEY=your-api-key
//    export MLFLOW_TRACKING_URI=http://localhost:5000 (optional, defaults to this)
//
// 3. Run the example:
//    go run main.go
//
// 4. View traces in MLflow UI at http://localhost:5000

func main() {
	// Get configuration from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	trackingURI := os.Getenv("MLFLOW_TRACKING_URI")
	if trackingURI == "" {
		trackingURI = "http://localhost:5000"
	}

	// Create MLflow tracker
	tracker, err := mlflow.New(mlflow.Config{
		TrackingURI:    trackingURI,
		ExperimentName: "go-ai-sdk-demo",
		ServiceName:    "openai-example",
		Insecure:       true, // For local development
	})
	if err != nil {
		log.Fatalf("Failed to create MLflow tracker: %v", err)
	}
	defer func() {
		if err := tracker.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracker: %v", err)
		}
	}()

	fmt.Println("MLflow tracker initialized")
	fmt.Printf("Tracking URI: %s\n", trackingURI)
	fmt.Println("Experiment: go-ai-sdk-demo")
	fmt.Println()

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := provider.LanguageModel("gpt-4")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	// Create telemetry settings with MLflow tracer
	telemetrySettings := &telemetry.Settings{
		IsEnabled:     true,
		RecordInputs:  true,
		RecordOutputs: true,
		FunctionID:    "example-generation",
		Tracer:        tracker.Tracer(),
	}

	// Example 1: Simple text generation with telemetry
	fmt.Println("=== Example 1: Simple Text Generation ===")
	result1, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:                 model,
		Prompt:                "Explain what MLflow is in one sentence.",
		ExperimentalTelemetry: telemetrySettings,
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result1.Text)
	if result1.Usage.TotalTokens != nil {
		fmt.Printf("Tokens used: %d\n", *result1.Usage.TotalTokens)
	}
	fmt.Println()

	// Example 2: Generation with parameters
	fmt.Println("=== Example 2: Generation with Parameters ===")
	temp := 0.7
	maxTokens := 150
	result2, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:                 model,
		Prompt:                "Write a haiku about observability in AI systems.",
		Temperature:           &temp,
		MaxTokens:             &maxTokens,
		ExperimentalTelemetry: telemetrySettings,
	})
	if err != nil {
		log.Fatalf("GenerateText failed: %v", err)
	}

	fmt.Printf("Response: %s\n", result2.Text)
	if result2.Usage.TotalTokens != nil {
		fmt.Printf("Tokens used: %d\n", *result2.Usage.TotalTokens)
	}
	fmt.Println()

	// Flush any pending traces
	fmt.Println("Flushing traces to MLflow...")
	if err := tracker.ForceFlush(context.Background()); err != nil {
		log.Printf("Error flushing traces: %v", err)
	}

	fmt.Println("\n✅ Complete! View your traces at:", trackingURI)
	fmt.Println("   Navigate to: Experiments > go-ai-sdk-demo > Traces")
}
