# Google AI Provider Example

Integration examples for Google AI (Gemini) using the go-ai SDK.

## Setup

```bash
export GOOGLE_GENERATIVE_AI_API_KEY=your-api-key
go run main.go
```

Set `GOOGLE_FILE_URI=files/...` to run the optional fileData embedding example with a provider-hosted file from the Google Files API.

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
| `google.ModelGemini31ProPreview` | `gemini-3.1-pro-preview` |
| `google.ModelGemini31ProPreviewCustom` | `gemini-3.1-pro-preview-customtools` |

### Gemini 2.0 / 1.5 series

Full list of constants is in `pkg/providers/google/model_ids.go`.

## Interactions API

Use `Provider.Interactions(modelID)` for stateless or stateful Gemini Interactions API calls and `Provider.InteractionsAgent(agent)` for agent presets such as `google.InteractionsAgentDeepResearchProPreview`.

| Constant | ID |
|---|---|
| `google.InteractionsModelGemini25Flash` | `gemini-2.5-flash` |
| `google.InteractionsModelGemini25Pro` | `gemini-2.5-pro` |
| `google.InteractionsModelGemini3ProImage` | `gemini-3-pro-image-preview` |
| `google.InteractionsAgentDeepResearchProPreview` | `deep-research-pro-preview-12-2025` |
| `google.InteractionsAgentDeepResearchPreview042026` | `deep-research-preview-04-2026` |
| `google.InteractionsAgentDeepResearchMax042026` | `deep-research-max-preview-04-2026` |

Google-specific options are passed under `ProviderOptions["google"]` with `google.GoogleInteractionsProviderOptions`. Supported options include `PreviousInteractionID`, `Store`, `ResponseModalities`, `ServiceTier`, `ThinkingLevel`, `ThinkingSummaries`, `ImageConfig`, `AgentConfig`, and `PollingTimeoutMs`.

```go
store := false
model, _ := provider.Interactions(google.InteractionsModelGemini25Flash)
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: types.Prompt{Text: "Give a concise answer."},
    ProviderOptions: map[string]interface{}{
        "google": google.GoogleInteractionsProviderOptions{
            Store:              &store,
            ResponseModalities: []string{"text"},
            ThinkingLevel:      "low",
        },
    },
})
```

## Embeddings

Google embeddings use specification version `v4`, support up to `2048` values per call, and batch multiple values through Google's `:batchEmbedContents` endpoint.

| Constant | Model ID |
|---|---|
| `google.EmbeddingModelGeminiEmbedding001` | `gemini-embedding-001` |
| `google.EmbeddingModelGeminiEmbedding2` | `gemini-embedding-2` |
| `google.EmbeddingModelGeminiEmbedding2Preview` | `gemini-embedding-2-preview` |

Provider options are passed under `ProviderOptions["google"]` with `google.GoogleEmbeddingProviderOptions`:

```go
dimensions := 768
result, err := embeddingModel.DoEmbed(ctx, "semantic search document", &provider.EmbedModelOptions{
    ProviderOptions: map[string]interface{}{
        "google": google.GoogleEmbeddingProviderOptions{
            TaskType:             "RETRIEVAL_DOCUMENT",
            OutputDimensionality: &dimensions,
        },
    },
})
```

Multimodal embedding content supports `google.TextEmbeddingPart`, `google.ImageEmbeddingPart`, and `google.FileDataEmbeddingPart`. Use `Content` for TypeScript-compatible per-value content, including batch calls:

```go
result, err := embeddingModel.DoEmbed(ctx, "caption", &provider.EmbedModelOptions{
    ProviderOptions: map[string]interface{}{
        "google": google.GoogleEmbeddingProviderOptions{
            Content: [][]google.EmbeddingPart{
                {
                    google.FileDataEmbeddingPart{
                        MimeType: "application/pdf",
                        FileURI:  "files/abc123",
                    },
                },
            },
        },
    },
})
```

## Speech

Google Gemini TTS is exposed through `Provider.SpeechModel` and `Provider.Speech`.

| Constant | Model ID |
|---|---|
| `google.ModelGemini25FlashTTS` | `gemini-2.5-flash-preview-tts` |
| `google.ModelGemini25ProTTS` | `gemini-2.5-pro-preview-tts` |
| `google.ModelGemini31FlashTTSPreview` | `gemini-3.1-flash-tts-preview` |

Run `examples/speech/google_tts.go` for a live example.

## Realtime

Gemini Live is exposed through `Provider.RealtimeModel`, `ExperimentalRealtimeModel`, and `GetRealtimeToken`. Use model ID `gemini-3.1-flash-live-preview` for the June 6 example surface:

```bash
GO_AI_RUN_LIVE_EXAMPLES=1 GOOGLE_GENERATIVE_AI_API_KEY=... go run ../../realtime/google_realtime.go
```

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
| `google.ModelGemini31FlashImagePreview` | `gemini-3.1-flash-image-preview` |

## Aspect Ratios

Image generation supports the following aspect ratios via `provider.ImageGenerateOptions.AspectRatio`:

**Standard (Imagen + Gemini):**
- `google.ImageAspectRatio1x1` - `1:1`
- `google.ImageAspectRatio3x4` - `3:4`
- `google.ImageAspectRatio4x3` - `4:3`
- `google.ImageAspectRatio9x16` - `9:16`
- `google.ImageAspectRatio16x9` - `16:9`

**Extended (Gemini image models only):**
- `google.ImageAspectRatio2x3` - `2:3`
- `google.ImageAspectRatio3x2` - `3:2`
- `google.ImageAspectRatio4x5` - `4:5`
- `google.ImageAspectRatio5x4` - `5:4`
- `google.ImageAspectRatio21x9` - `21:9`
- `google.ImageAspectRatio1x8` - `1:8`
- `google.ImageAspectRatio8x1` - `8:1`
- `google.ImageAspectRatio1x4` - `1:4`
- `google.ImageAspectRatio4x1` - `4:1`

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
- Interactions API generation
- Interactions agents, including Deep Research and Antigravity IDs
- Text embeddings with task type and output dimensionality
- Stable `gemini-embedding-2` embeddings
- Optional fileData embeddings with `GOOGLE_FILE_URI`
- Image generation with `gemini-3.1-flash-image-preview`
- Extended aspect ratio and image size usage
