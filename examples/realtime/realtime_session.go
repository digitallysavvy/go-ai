//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and OPENAI_API_KEY to connect a live OpenAI realtime session.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set OPENAI_API_KEY to connect a live OpenAI realtime session.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.RealtimeModel(openai.ModelGPT4oRealtimePreview)
	if err != nil {
		log.Fatalf("create realtime model: %v", err)
	}

	instructions := "Answer briefly."
	session, err := ai.ConnectRealtime(ctx, model, ai.RealtimeSessionOptions{
		SessionConfig: &provider.RealtimeSessionConfig{
			Instructions: &instructions,
		},
	})
	if err != nil {
		log.Fatalf("connect realtime session: %v", err)
	}
	defer session.Close()

	if err := session.Send(ctx, provider.RealtimeClientEvent{
		Type: "response-create",
		Options: &provider.RealtimeResponseCreateOptions{
			Instructions: &instructions,
		},
	}); err != nil {
		log.Fatalf("send event: %v", err)
	}

	events, err := session.Read(ctx)
	if err != nil {
		log.Fatalf("read event: %v", err)
	}
	for _, event := range events {
		fmt.Printf("%s %s %s\n", event.Type, event.ResponseID, string(event.Raw))
	}
}
