//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 and GOOGLE_GENERATIVE_AI_API_KEY to generate Gemini TTS audio.")
		return
	}
	apiKey := os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY")
	if apiKey == "" {
		fmt.Println("Set GOOGLE_GENERATIVE_AI_API_KEY to generate Gemini TTS audio.")
		return
	}

	p := google.New(google.Config{APIKey: apiKey})
	model, err := p.SpeechModel(google.ModelGemini25FlashTTS)
	if err != nil {
		log.Fatalf("create speech model: %v", err)
	}

	result, err := ai.GenerateSpeech(context.Background(), ai.GenerateSpeechOptions{
		Model: model,
		Text:  "Welcome to the Go AI SDK.",
		Voice: "Kore",
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("google-tts.wav", result.Audio.Data, 0o644); err != nil {
		log.Fatal(err)
	}
}
