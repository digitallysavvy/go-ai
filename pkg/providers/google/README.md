# Google Provider

The Google provider mirrors the TypeScript `@ai-sdk/google` surface for Google AI Studio models, including language models, embeddings, image generation, Gemini TTS, and Gemini Live realtime models.

## Setup

```go
import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    providerapi "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/google"
)

p := google.New(google.Config{
    APIKey: os.Getenv("GOOGLE_GENERATIVE_AI_API_KEY"),
})
```

## Gemini TTS Speech

Gemini TTS is exposed through `Provider.SpeechModel` and `Provider.Speech`.

| Constant | Model ID |
|---|---|
| `google.ModelGemini25FlashTTS` | `gemini-2.5-flash-preview-tts` |
| `google.ModelGemini25ProTTS` | `gemini-2.5-pro-preview-tts` |
| `google.ModelGemini31FlashTTSPreview` | `gemini-3.1-flash-tts-preview` |

```go
speechModel, err := p.SpeechModel(google.ModelGemini25FlashTTS)
if err != nil {
    log.Fatal(err)
}

result, err := ai.GenerateSpeech(ctx, ai.GenerateSpeechOptions{
    Model: speechModel,
    Text:  "Welcome to Go-AI.",
    Voice: "Kore",
})
```

Gemini TTS defaults to the `Kore` voice when no voice is supplied. Pass Gemini-specific speech options under `ProviderOptions["google"]` with `google.GoogleSpeechModelOptions`.

## Gemini Live Realtime

Gemini Live is exposed through `Provider.RealtimeModel`, `ExperimentalRealtimeModel`, and `GetRealtimeToken`. Model IDs are strings, matching the TypeScript `GoogleRealtimeModelId` surface.

```go
model, err := p.RealtimeModel("gemini-3.1-flash-live-preview")
if err != nil {
    log.Fatal(err)
}

token, err := model.DoCreateClientSecret(ctx, providerapi.ClientSecretOptions{})
if err != nil {
    log.Fatal(err)
}

ws := model.GetWebSocketConfig(token.Token, token.URL)
fmt.Println(ws.URL)
```

Use `ai.ConnectRealtime` for provider-neutral server-side sessions. Browser-only TypeScript realtime pieces such as `BrowserRealtimeTransport`, `BrowserRealtimeAudio`, and `useRealtime` are not part of the Go runtime.

## Embeddings

Google embeddings use specification version `v4` and support batch calls through Google's `:batchEmbedContents` endpoint.

| Constant | Model ID |
|---|---|
| `google.EmbeddingModelGeminiEmbedding001` | `gemini-embedding-001` |
| `google.EmbeddingModelGeminiEmbedding2` | `gemini-embedding-2` |
| `google.EmbeddingModelGeminiEmbedding2Preview` | `gemini-embedding-2-preview` |

Provider options are passed under `ProviderOptions["google"]` with `google.GoogleEmbeddingProviderOptions`, including task type, output dimensionality, and multimodal content parts.
