//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
)

func main() {
	if os.Getenv("GO_AI_RUN_LIVE_EXAMPLES") != "1" {
		fmt.Println("Set GO_AI_RUN_LIVE_EXAMPLES=1 with Vertex credentials to generate Vertex TTS audio.")
		return
	}
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")
	if project == "" || token == "" {
		fmt.Println("Set GOOGLE_VERTEX_PROJECT and GOOGLE_VERTEX_ACCESS_TOKEN to generate Vertex TTS audio.")
		return
	}

	p, err := googlevertex.New(googlevertex.Config{
		Project:     project,
		Location:    "us-central1",
		AccessToken: token,
	})
	if err != nil {
		log.Fatal(err)
	}
	model, err := p.SpeechModel(googlevertex.SpeechModelGemini25FlashTTS)
	if err != nil {
		log.Fatalf("create speech model: %v", err)
	}

	result, err := ai.GenerateSpeech(context.Background(), ai.GenerateSpeechOptions{
		Model: model,
		Text:  "Vertex AI Gemini can synthesize speech from Go.",
		Voice: "Kore",
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("vertex-tts.wav", result.Audio.Data, 0o644); err != nil {
		log.Fatal(err)
	}
}
