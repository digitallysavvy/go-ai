# Gladia Transcription with Timestamps

This example demonstrates how to get word-level timestamps from Gladia transcription.

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
Transcribing audio with timestamps...

=== Transcription Result ===
Full Text: Your full transcribed text will appear here
Duration: 36.74 seconds

=== Timestamps (10 segments) ===
[  1] [  0.14s -   5.34s] Galileo was an American robotic space program
[  2] [  5.66s -   8.10s] that studied the planet Jupiter and its moons.
[  3] [  9.14s -  12.12s] Named after the Italian astronomer Galileo Galilei,
...
```

## Features Demonstrated

- Creating a Gladia provider
- Getting a transcription model
- Transcribing audio with timestamps enabled
- Reading and displaying timestamped segments
- Usage metadata

## Use Cases

- Video subtitle generation
- Audio navigation
- Meeting transcription with time references
- Content indexing and search
