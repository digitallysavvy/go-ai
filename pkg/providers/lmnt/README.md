# LMNT Provider

The **LMNT provider** for the Go AI SDK contains speech synthesis model support for the LMNT text-to-speech API.

LMNT offers high-quality voice synthesis with voice selection and speed control.

## Installation

```bash
go get github.com/digitallysavvy/go-ai/pkg/providers/lmnt
```

## Setup

To use the LMNT provider, you need an API key from [LMNT](https://www.lmnt.com/).

## Provider Instance

You can create a new provider instance with your API key:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/lmnt"

provider := lmnt.New(lmnt.Config{
    APIKey: "your-api-key",
})
```

## Example: Basic Speech Synthesis

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/providers/lmnt"
    "github.com/digitallysavvy/go-ai/pkg/provider"
)

func main() {
    // Create provider
    p := lmnt.New(lmnt.Config{
        APIKey: os.Getenv("LMNT_API_KEY"),
    })

    // Get speech model
    model, err := p.SpeechModel("default")
    if err != nil {
        log.Fatal(err)
    }

    // Generate speech
    result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
        Text:  "Hello, world!",
        Voice: "aurora",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Save audio to file
    if err := os.WriteFile("output.mp3", result.Audio, 0644); err != nil {
        log.Fatal(err)
    }

    fmt.Println("Audio generated successfully!")
    fmt.Printf("Character count: %d\n", result.Usage.CharacterCount)
}
```

## Example: Speech with Speed Control

```go
speed := 1.2 // 20% faster than normal

result, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
    Text:  "This speech will be generated faster than normal.",
    Voice: "aurora",
    Speed: &speed,
})
if err != nil {
    log.Fatal(err)
}

// Save audio
if err := os.WriteFile("output_fast.mp3", result.Audio, 0644); err != nil {
    log.Fatal(err)
}
```

## Configuration

### Config Fields

- `APIKey` (required): Your LMNT API key
- `BaseURL` (optional): Custom API base URL (defaults to "https://api.lmnt.com/v1")

### SpeechGenerateOptions

- `Text` (required): Text to synthesize
- `Voice` (required): Voice ID to use (e.g., "aurora")
- `Speed` (optional): Speech speed multiplier (e.g., 1.0 = normal, 1.5 = 50% faster)

## Available Voices

LMNT supports various voice options. Common voices include:
- `aurora` - Clear, professional voice
- Check the [LMNT documentation](https://docs.lmnt.com/) for the full list of available voices

## Output Format

The provider returns audio in MP3 format (`audio/mpeg`) by default.

## Documentation

For more information about the LMNT API, visit the [LMNT documentation](https://docs.lmnt.com/).
