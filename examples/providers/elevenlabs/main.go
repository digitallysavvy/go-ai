// Streams speech from ElevenLabs while the text is still being produced.
//
// Run with ELEVENLABS_API_KEY set. The audio is written to stream.mp3.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
)

func main() {
	ctx := context.Background()

	p := elevenlabs.New(elevenlabs.Config{APIKey: os.Getenv("ELEVENLABS_API_KEY")})
	model, err := p.SpeechModel(elevenlabs.SpeechModelFlashV25)
	if err != nil {
		log.Fatal(err)
	}

	// Text arrives incrementally, like the output of a language model.
	deltas := make(chan string)
	go func() {
		defer close(deltas)
		for _, delta := range []string{"Hello, ", "this speech is ", "being streamed ", "as the text arrives."} {
			deltas <- delta
			time.Sleep(200 * time.Millisecond)
		}
	}()

	result, err := ai.ExperimentalStreamSpeech(ctx, ai.StreamSpeechOptions{
		Model:      model,
		TextStream: ai.NewChannelStringSpeechTextStream(deltas, nil),
	})
	if err != nil {
		log.Fatal(err)
	}

	audio, err := result.AudioStream()
	if err != nil {
		log.Fatal(err)
	}
	defer audio.Close() //nolint:errcheck

	out, err := os.Create("stream.mp3")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close() //nolint:errcheck

	total := 0
	for {
		chunk, err := audio.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		total += len(chunk)
		if _, err := out.Write(chunk); err != nil {
			log.Fatal(err)
		}
	}

	// Result accessors resolve once the stream has finished.
	usage, err := result.Usage()
	if err != nil {
		log.Fatal(err)
	}
	if usage.InputCharacters != nil {
		fmt.Printf("Wrote %d bytes of audio for %d characters\n", total, *usage.InputCharacters)
	}
}
