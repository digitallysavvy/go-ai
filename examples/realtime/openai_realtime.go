//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	ctx := context.Background()
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and OPENAI_API_KEY to create a live OpenAI realtime token.")
		return
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set OPENAI_API_KEY to create a live OpenAI realtime token.")
		return
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.RealtimeModel(openai.ModelGPT4oRealtimePreview)
	if err != nil {
		log.Fatalf("create realtime model: %v", err)
	}

	secret, err := model.DoCreateClientSecret(ctx, provider.ClientSecretOptions{})
	if err != nil {
		log.Fatalf("create client secret: %v", err)
	}
	ws := model.GetWebSocketConfig(secret.Token, secret.URL)

	fmt.Printf("WebSocket URL: %s\n", ws.URL)
	fmt.Printf("Protocols: %v\n", ws.Protocols)
	if secret.ExpiresAt != nil {
		fmt.Printf("Expires at unix seconds: %d\n", *secret.ExpiresAt)
	}
}
