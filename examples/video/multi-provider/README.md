# Multi-Provider Video Generation Example

This example demonstrates how to use multiple video generation providers (Google, FAL, Replicate) with the same prompt to compare results.

## Prerequisites

- Go 1.21 or later
- API keys for any/all of the following providers:
  - Google Generative AI (`GOOGLE_API_KEY`)
  - FAL (`FAL_KEY`)
  - Replicate (`REPLICATE_API_TOKEN`)

## Setup

Set your API keys (only set the ones you have):

```bash
export GOOGLE_API_KEY="your-google-key"
export FAL_KEY="your-fal-key"
export REPLICATE_API_TOKEN="your-replicate-token"
```

## Run

```bash
go run main.go
```

The example will attempt to generate videos with each configured provider and save them with provider-specific filenames:
- `google_output.mp4`
- `fal_output.mp4`
- `replicate_output.mp4`

## Provider Comparison

| Provider | Strengths | Typical Generation Time |
|----------|-----------|-------------------------|
| **Google Generative AI** | High quality, good prompt understanding | 30-60s |
| **FAL** | Fast generation, good for prototyping | 15-30s |
| **Replicate** | Wide model selection, open-source models | 30-90s |

## Notes

- Providers skipped if API key is not set
- Each provider may interpret the prompt differently
- Video quality and style vary between providers
- Generation times are approximate and depend on queue length
- All providers use async generation with polling

## Example Output

```
=== Google Generative AI ===
✓ Saved to google_output.mp4 (2458624 bytes, video/mp4)

=== FAL ===
✓ Saved to fal_output.mp4 (1824567 bytes, video/mp4)

=== Replicate ===
✓ Saved to replicate_output.mp4 (3145728 bytes, video/mp4)
```

## Choosing a Provider

- **Google**: Best for production use with high quality requirements
- **FAL**: Best for rapid prototyping and iteration
- **Replicate**: Best for experimenting with different open-source models

All providers are production-ready and support the same VideoModelV3 interface, making it easy to switch between them.
