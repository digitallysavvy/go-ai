// Command hume demonstrates the Hume speech synthesis provider
// (POST /v0/tts/file). Hume has no selectable model ID.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
)

func main() {
	h := hume.New(hume.Config{APIKey: os.Getenv("HUME_API_KEY")})

	model, err := h.SpeechModel("")
	if err != nil {
		log.Fatal(err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello from the Go AI SDK.",
		OutputFormat: "mp3",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated %d bytes of audio\n", len(result.Audio))
}
