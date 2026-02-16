# LMNT Speech Synthesis Example

This example demonstrates how to use the LMNT provider for text-to-speech synthesis.

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
Generating speech...

=== Speech Generation Result ===
Audio saved to: output.mp3
Audio size: 45678 bytes
MIME type: audio/mpeg
Character count: 26
```

The generated audio will be saved as `output.mp3` in the current directory.

## Features Demonstrated

- Creating an LMNT provider
- Getting a speech model
- Generating speech from text
- Saving audio output to a file
- Reading usage metadata
