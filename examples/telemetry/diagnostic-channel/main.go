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

func main() {
	unsubscribe := telemetry.SubscribeDiagnostic(func(ctx context.Context, msg telemetry.DiagnosticMessage) error {
		fmt.Printf("diagnostic event: %s\n", msg.Type)
		return nil
	})
	defer unsubscribe()

	telemetry.RegisterTelemetryIntegration(telemetry.NoopTelemetryIntegration{})

	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say hello in five words.",
		Telemetry: &telemetry.Options{
			FunctionID: "diagnostic-channel-example",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
}
