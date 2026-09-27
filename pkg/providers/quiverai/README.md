# QuiverAI Provider

The QuiverAI provider supports SVG image generation and raster-to-SVG vectorization.

## Setup

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/quiverai"

qprovider := quiverai.New(quiverai.Config{
    APIKey: "your-api-key",
})
```

`APIKey` defaults to `QUIVERAI_API_KEY`. `BaseURL` defaults to `https://api.quiver.ai/v1` and can be overridden with `QUIVERAI_BASE_URL`.

## Generate SVGs

```go
model, _ := qprovider.ImageModel(quiverai.ModelArrow11)

result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Prompt: "minimal rocket logo",
})
```

## Vectorize Images

```go
result, err := model.DoGenerate(ctx, &provider.ImageGenerateOptions{
    Files: []provider.ImageFile{{
        Type: "url",
        URL:  "https://example.com/logo.png",
    }},
    ProviderOptions: map[string]interface{}{
        "quiverai": map[string]interface{}{
            "operation":  quiverai.OperationVectorize,
            "autoCrop":   true,
            "targetSize": 512,
        },
    },
})
```

## Provider Options

- `operation`: `generate` or `vectorize`; defaults to `generate`.
- `instructions`: extra style guidance for generation.
- `temperature`, `topP`, `presencePenalty`, `maxOutputTokens`: sampling controls.
- `autoCrop`, `targetSize`: vectorization options.

The provider returns SVG bytes with `MimeType` set to `image/svg+xml`, QuiverAI image metadata under `ProviderMetadata["quiverai"]`, and token usage when reported by the API.

## Language Models (Arrow 2 / Arrow 2 Telos)

`ModelArrow2` and `ModelArrow2Telos` are available as standard language models, built on the [Open Responses](../openresponses/README.md) transport with QuiverAI's own request policy layered on top (reasoning effort/summary validation, opaque reasoning replay, endpoint-specific error handling, caller-executed "custom" tools via `ProviderID: "quiverai.custom"`).

```go
model, err := qprovider.LanguageModel(quiverai.ModelArrow2)

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Suggest a minimal color palette for a weather app icon set.",
    ProviderOptions: map[string]interface{}{
        "quiverai": map[string]interface{}{
            "reasoningEffort":  "medium", // low | medium | high | xhigh
            "reasoningSummary": "auto",
        },
    },
})
```

See `examples/providers/quiverai` for runnable examples, including custom-tool usage.
