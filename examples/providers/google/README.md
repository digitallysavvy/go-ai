# Google AI Provider Example

Integration examples for Google AI (Gemini) using the go-ai SDK.

## Setup

```bash
export GOOGLE_GENERATIVE_AI_API_KEY=your-api-key
go run main.go
```

## Language Models

### Gemini 2.5 series

| Constant | Model ID |
|---|---|
| `google.ModelGemini25Pro` | `gemini-2.5-pro` |
| `google.ModelGemini25Flash` | `gemini-2.5-flash` |
| `google.ModelGemini25FlashLite` | `gemini-2.5-flash-lite` |

### Gemini 3 series

| Constant | Model ID |
|---|---|
| `google.ModelGemini3ProPreview` | `gemini-3-pro-preview` |
| `google.ModelGemini3FlashPreview` | `gemini-3-flash-preview` |
| `google.ModelGemini31ProPreview` | `gemini-3.1-pro-preview` (**New**) |
| `google.ModelGemini31ProPreviewCustom` | `gemini-3.1-pro-preview-customtools` (**New**) |

### Gemini 2.0 / 1.5 series (see model_ids.go for the full list)

Full list of constants is in `pkg/providers/google/model_ids.go`.

## Image Models

### Imagen models (use `:predict` API)

| Constant | Model ID |
|---|---|
| `google.ModelImagen40Generate001` | `imagen-4.0-generate-001` |
| `google.ModelImagen40UltraGenerate001` | `imagen-4.0-ultra-generate-001` |
| `google.ModelImagen40FastGenerate001` | `imagen-4.0-fast-generate-001` |

### Gemini image models (use `:generateContent` API)

| Constant | Model ID |
|---|---|
| `google.ModelGemini25FlashImage` | `gemini-2.5-flash-image` |
| `google.ModelGemini3ProImagePreview` | `gemini-3-pro-image-preview` |
| `google.ModelGemini31FlashImagePreview` | `gemini-3.1-flash-image-preview` (**New**) |

## Aspect Ratios

Image generation supports the following aspect ratios via `provider.ImageGenerateOptions.AspectRatio`:

**Standard (Imagen + Gemini):**
- `google.ImageAspectRatio1x1` — `1:1`
- `google.ImageAspectRatio3x4` — `3:4`
- `google.ImageAspectRatio4x3` — `4:3`
- `google.ImageAspectRatio9x16` — `9:16`
- `google.ImageAspectRatio16x9` — `16:9`

**Extended (Gemini image models only, added in #12897):**
- `google.ImageAspectRatio2x3` — `2:3`
- `google.ImageAspectRatio3x2` — `3:2`
- `google.ImageAspectRatio4x5` — `4:5`
- `google.ImageAspectRatio5x4` — `5:4`
- `google.ImageAspectRatio21x9` — `21:9`
- `google.ImageAspectRatio1x8` — `1:8`
- `google.ImageAspectRatio8x1` — `8:1`
- `google.ImageAspectRatio1x4` — `1:4`
- `google.ImageAspectRatio4x1` — `4:1`

## Image Sizes (Gemini image models)

The output resolution can be set via `ProviderOptions`:

```go
opts := &provider.ImageGenerateOptions{
    Prompt: "A photo",
    ProviderOptions: map[string]interface{}{
        "google": map[string]interface{}{
            "imageSize": google.ImageSize2K, // "512", "1K", "2K", or "4K"
        },
    },
}
```

## Usage Examples

See `main.go` for working examples including:
- Text generation with Gemini Pro
- Image generation with `gemini-3.1-flash-image-preview`
- Aspect ratio usage
