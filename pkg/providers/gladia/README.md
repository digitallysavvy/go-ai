# Gladia Provider

The **Gladia provider** for the Go AI SDK contains transcription model support for the Gladia transcription API.

Gladia offers advanced speech recognition with async processing and multi-language support.

## Installation

```bash
go get github.com/digitallysavvy/go-ai/pkg/providers/gladia
```

## Setup

To use the Gladia provider, you need an API key from [Gladia](https://www.gladia.io/).

## Provider Instance

You can create a new provider instance with your API key:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/gladia"

provider := gladia.New(gladia.Config{
    APIKey: "your-api-key",
})
```

## Example: Basic Transcription

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/providers/gladia"
    "github.com/digitallysavvy/go-ai/pkg/provider"
)

func main() {
    // Create provider
    p := gladia.New(gladia.Config{
        APIKey: os.Getenv("GLADIA_API_KEY"),
    })

    // Get transcription model
    model, err := p.TranscriptionModel("whisper-v3")
    if err != nil {
        log.Fatal(err)
    }

    // Read audio file
    audioData, err := os.ReadFile("audio.mp3")
    if err != nil {
        log.Fatal(err)
    }

    // Transcribe
    result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
        Audio:    audioData,
        MimeType: "audio/mpeg",
        Language: "en",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Transcription:", result.Text)
    fmt.Printf("Duration: %.2f seconds\n", result.Usage.DurationSeconds)
}
```

## Example: Transcription with Timestamps

```go
result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
    Audio:      audioData,
    MimeType:   "audio/mpeg",
    Language:   "en",
    Timestamps: true, // Request timestamps
})
if err != nil {
    log.Fatal(err)
}

// Print transcription with timestamps
for _, ts := range result.Timestamps {
    fmt.Printf("[%.2f-%.2f] %s\n", ts.Start, ts.End, ts.Text)
}
```

## Configuration

### Config Fields

- `APIKey` (required): Your Gladia API key
- `BaseURL` (optional): Custom API base URL (defaults to "https://api.gladia.io/v2")

### TranscriptionOptions

- `Audio` (required): Audio data as byte slice
- `MimeType` (required): MIME type of the audio (e.g., "audio/mpeg", "audio/wav")
- `Language` (optional): Language code (e.g., "en", "fr", "es")
- `Timestamps` (optional): Whether to include word-level timestamps

## Supported Audio Formats

Gladia supports various audio formats including:
- MP3 (audio/mpeg)
- WAV (audio/wav)
- M4A (audio/m4a)
- FLAC (audio/flac)
- OGG (audio/ogg)

## Documentation

For more information about the Gladia API, visit the [Gladia documentation](https://docs.gladia.io/).
