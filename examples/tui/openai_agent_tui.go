//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/tui"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and OPENAI_API_KEY to run the OpenAI agent TUI.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set OPENAI_API_KEY to run the OpenAI agent TUI.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatalf("create model: %v", err)
	}

	instructions := "You are concise and helpful."
	assistant := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:        model,
		Instructions: &instructions,
	})

	if err := tui.RunAgentTUI(context.Background(), tui.RunAgentTUIOptions{
		Agent: assistant,
		Title: "OpenAI Agent",
	}); err != nil {
		log.Fatal(err)
	}
}
