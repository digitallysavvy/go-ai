# QuiverAI Provider Examples

Examples demonstrating the QuiverAI Arrow 2 / Arrow 2 Telos language models,
which reuse the Open Responses transport with QuiverAI's own request policy
(reasoning effort/summary validation, caller-executed custom tools) layered
on top.

For QuiverAI's SVG generation/vectorization image model, see
[`pkg/providers/quiverai/README.md`](../../../pkg/providers/quiverai/README.md).

## Prerequisites

A QuiverAI API key, set via the `QUIVERAI_API_KEY` environment variable or
`quiverai.Config{APIKey: "..."}`.

## Examples

### 1. basic-chat.go

Text generation with `quiverai.ModelArrow2`, including
`providerOptions.quiverai.reasoningEffort`/`reasoningSummary`.

```bash
go run basic-chat.go
```

### 2. custom-tool.go

QuiverAI's caller-executed "custom" tool (`quiverai.custom`), whose input the
model streams as raw text (e.g. SVG markup) rather than JSON arguments.

```bash
go run custom-tool.go
```

## Configuration

```go
provider := quiverai.New(quiverai.Config{
    APIKey:  "your-api-key", // defaults to QUIVERAI_API_KEY
    BaseURL: "https://api.quiver.ai/v1", // defaults to QUIVERAI_BASE_URL or the QuiverAI API
})

model, err := provider.LanguageModel(quiverai.ModelArrow2) // or quiverai.ModelArrow2Telos
```
