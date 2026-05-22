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
