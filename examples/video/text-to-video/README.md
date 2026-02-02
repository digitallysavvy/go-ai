# Text-to-Video Generation Example

This example demonstrates how to generate videos from text prompts using the Google Generative AI provider.

## Prerequisites

- Go 1.21 or later
- Google API key with access to Gemini video generation

## Setup

1. Set your Google API key:
   ```bash
   export GOOGLE_API_KEY="your-api-key"
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

## Run

```bash
go run main.go
```

This will generate a video based on the text prompt and save it to `output.mp4`.

## Customization

You can customize the video generation by modifying the `GenerateVideoOptions`:

- **Prompt**: Describe what you want in the video
- **AspectRatio**: Set the aspect ratio (e.g., "16:9", "9:16", "1:1")
- **Resolution**: Set the resolution (e.g., "1920x1080", "1280x720")
- **Duration**: Set video length in seconds (optional)
- **Seed**: Set a seed for reproducible generation (optional)

## Example Output

```
Video saved to output.mp4
Media type: video/mp4
Size: 2458624 bytes
```

## Notes

- Video generation can take 30-60 seconds depending on complexity
- The API uses polling to check for completion
- Videos are automatically downloaded and can be saved to disk
