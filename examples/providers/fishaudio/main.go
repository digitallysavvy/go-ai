// Command fishaudio demonstrates the Fish Audio speech and transcription
// providers: text-to-speech via POST /v1/tts and speech-to-text via
// POST /v1/asr.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
)

func main() {
	fa := fishaudio.New(fishaudio.Config{APIKey: os.Getenv("FISH_AUDIO_API_KEY")})

	speechModel, err := fa.SpeechModel(fishaudio.ModelS21Pro)
	if err != nil {
		log.Fatal(err)
	}

	speechResult, err := speechModel.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:         "Hello from the Go AI SDK.",
		Voice:        "your-fish-audio-reference-id",
		OutputFormat: "mp3",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated %d bytes of audio\n", len(speechResult.Audio))

	transcriptionModel, err := fa.TranscriptionModel("")
	if err != nil {
		log.Fatal(err)
	}

	transcriptionResult, err := transcriptionModel.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    speechResult.Audio,
		MimeType: "audio/mpeg",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Transcribed text: %s\n", transcriptionResult.Text)
}
