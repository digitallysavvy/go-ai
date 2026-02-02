# Image-to-Video Generation Example

This example demonstrates how to animate static images into videos using the Google Generative AI provider.

## Prerequisites

- Go 1.21 or later
- Google API key with access to Gemini video generation
- An input image file (`input.jpg`)

## Setup

1. Set your Google API key:
   ```bash
   export GOOGLE_API_KEY="your-api-key"
   ```

2. Place your input image in the same directory as `main.go` and name it `input.jpg`

3. Install dependencies:
   ```bash
   go mod download
   ```

## Run

```bash
go run main.go
```

This will animate your input image and save the result to `output.mp4`.

## How It Works

Image-to-video generation takes a static image and animates it with natural motion. The text prompt guides the type of animation applied to the image.

## Customization

- **Image**: Provide your own image (JPEG or PNG)
- **Text Prompt**: Describe how you want the image animated
- **Duration**: Control video length (typically 3-10 seconds)
- **AspectRatio**: Match your image's aspect ratio for best results

## Example Prompts

- "Animate this image with gentle movement and natural motion"
- "Make the clouds move slowly across the sky"
- "Add a zoom-in effect while keeping the subject centered"
- "Create a parallax effect with depth"

## Tips

- Use high-quality input images (at least 1280x720)
- Keep prompts simple and descriptive
- Match the aspect ratio to your image dimensions
- Shorter videos (3-5 seconds) generally work better
