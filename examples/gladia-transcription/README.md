# Gladia Transcription Example

This example demonstrates how to use the Gladia provider for audio transcription.

## Setup

1. Get your API key from [Gladia](https://www.gladia.io/)

2. Set the environment variable:
```bash
export GLADIA_API_KEY=your-api-key
```

3. Place an audio file named `audio.mp3` in this directory

## Run

```bash
go run main.go
```

## Expected Output

```
Transcribing audio...

=== Transcription Result ===
Text: Your transcribed text will appear here
Duration: 36.74 seconds
```

## Features Demonstrated

- Creating a Gladia provider
- Getting a transcription model
- Transcribing audio from a file
- Reading transcription results and usage metadata
