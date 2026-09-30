// Command revai demonstrates the Rev.ai transcription provider: submit an
// async transcription job, poll it to completion, and fetch the transcript.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
)

func main() {
	r := revai.New(revai.Config{APIKey: os.Getenv("REVAI_API_KEY")})

	model, err := r.TranscriptionModel(revai.ModelMachine)
	if err != nil {
		log.Fatal(err)
	}

	audio, err := os.ReadFile("audio.wav")
	if err != nil {
		log.Fatal(err)
	}

	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:    audio,
		MimeType: "audio/wav",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Transcribed text: %s\n", result.Text)
	for _, segment := range result.Segments {
		fmt.Printf("  [%.2fs - %.2fs] %s\n", segment.Start, segment.End, segment.Text)
	}
}
