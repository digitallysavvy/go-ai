// Command cartesia demonstrates the Cartesia speech and batch transcription
// providers: text-to-speech via POST /tts/bytes and batch speech-to-text via
// POST /stt. Ink 2 realtime/streaming transcription is not covered by this
// example.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
)

func main() {
	c := cartesia.New(cartesia.Config{APIKey: os.Getenv("CARTESIA_API_KEY")})

	speechModel, err := c.SpeechModel(cartesia.ModelSonic35)
	if err != nil {
		log.Fatal(err)
	}

	speechResult, err := speechModel.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:  "Hello from the Go AI SDK.",
		Voice: "your-cartesia-voice-id",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated %d bytes of audio\n", len(speechResult.Audio))

	transcriptionModel, err := c.TranscriptionModel(cartesia.ModelInkWhisper)
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
