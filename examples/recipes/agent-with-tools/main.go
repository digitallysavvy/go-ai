// Recipe: run a tool loop with an agent.
//
// Run: ANTHROPIC_API_KEY=... go run ./examples/recipes/agent-with-tools
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func main() {
	model, err := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}).
		LanguageModel(anthropic.ClaudeSonnet5_5)
	if err != nil {
		log.Fatal(err)
	}

	clock := types.Tool{
		Name:        "current_time",
		Description: "Returns the current time in RFC 3339 format.",
		Parameters:  schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return time.Now().Format(time.RFC3339), nil
		},
	}

	assistant := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:    model,
		System:   "You are a helpful assistant. Use tools when they help.",
		Tools:    []types.Tool{clock},
		StopWhen: []ai.StopCondition{ai.IsStepCount(5)},
	})

	result, err := assistant.Generate(context.Background(), agent.AgentGenerateOptions{
		Prompt: "What time is it right now? Answer in one sentence.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
