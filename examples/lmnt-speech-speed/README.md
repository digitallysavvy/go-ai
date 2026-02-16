# LMNT Speech Synthesis with Speed Control

This example demonstrates how to control speech synthesis speed with the LMNT provider.

## Setup

1. Get your API key from [LMNT](https://www.lmnt.com/)

2. Set the environment variable:
```bash
export LMNT_API_KEY=your-api-key
```

## Run

```bash
go run main.go
```

## Expected Output

```
Generating speech at 1.5x speed...

=== Speech Generation Result ===
Audio saved to: output_fast.mp3
Audio size: 45678 bytes
MIME type: audio/mpeg
Character count: 89
Speed: 1.5x
```

The generated audio will be saved as `output_fast.mp3` in the current directory, playing 50% faster than normal speed.

## Speed Control

The `Speed` parameter accepts a float value:
- `1.0` = Normal speed
- `1.5` = 50% faster
- `0.75` = 25% slower
- Any value between `0.5` and `2.0` is typically supported

## Features Demonstrated

- Creating an LMNT provider
- Getting a speech model
- Generating speech with custom speed
- Saving audio output to a file
- Reading usage metadata
