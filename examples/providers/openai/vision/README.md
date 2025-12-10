# OpenAI Vision (GPT-4 Vision)

Demonstrates image understanding capabilities with GPT-4 Vision models.

## Features

- **Image analysis** from local files or URLs
- **Multiple image comparison**
- **OCR** (text extraction from images)
- **Scene understanding**
- **Object detection** and description

## Prerequisites

- OpenAI API key
- GPT-4 Vision access (gpt-4o, gpt-4-turbo, gpt-4-vision-preview)

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running

```bash
cd examples/providers/openai/vision
go run main.go
```

## Use Cases

1. **Image Description** - Detailed scene analysis
2. **OCR** - Extract text from documents/screenshots
3. **Visual QA** - Answer questions about images
4. **Image Comparison** - Compare multiple images
5. **Object Detection** - Identify objects in images

## Supported Image Formats

- JPEG, PNG, GIF, WebP
- Base64 encoded or URL
- Max size: 20MB

## Documentation

- [OpenAI Vision API](https://platform.openai.com/docs/guides/vision)
- [Go AI SDK Docs](../../../../docs)
