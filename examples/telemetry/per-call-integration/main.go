//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type printTelemetry struct {
	telemetry.NoopTelemetryIntegration
}

func (printTelemetry) OnFinish(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	fmt.Printf("finish reason: %s\n", e.FinishReason)
}

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Write one observability tip.",
		Telemetry: &telemetry.Options{
			Integrations: []telemetry.TelemetryIntegration{printTelemetry{}},
			FunctionID:   "per-call-integration-example",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
