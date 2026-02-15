package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

type TelemetryMiddleware struct {
	metrics map[string]interface{}
}

func NewTelemetryMiddleware() *TelemetryMiddleware {
	return &TelemetryMiddleware{
		metrics: make(map[string]interface{}),
	}
}

func (m *TelemetryMiddleware) GenerateText(ctx context.Context, opts ai.GenerateTextOptions) (*ai.GenerateTextResult, error) {
	start := time.Now()

	result, err := ai.GenerateText(ctx, opts)

	duration := time.Since(start)

	// Record metrics
	m.recordMetric("request_count", 1)
	m.recordMetric("request_duration_ms", duration.Milliseconds())

	if err == nil {
		m.recordMetric("tokens_used", result.Usage.GetTotalTokens())
		m.recordMetric("success_count", 1)
	} else {
		m.recordMetric("error_count", 1)
	}

	return result, err
}

func (m *TelemetryMiddleware) recordMetric(name string, value interface{}) {
	fmt.Printf("[Metric] %s: %v\n", name, value)
	m.metrics[name] = value
}

func (m *TelemetryMiddleware) GetMetrics() map[string]interface{} {
	return m.metrics
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	telemetry := NewTelemetryMiddleware()

	fmt.Println("=== Telemetry Middleware ===")

	_, _ = telemetry.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say hello",
	})

	fmt.Println("\n=== Collected Metrics ===")
	for key, value := range telemetry.GetMetrics() {
		fmt.Printf("%s: %v\n", key, value)
	}
}
