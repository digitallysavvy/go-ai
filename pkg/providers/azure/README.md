# Azure OpenAI Provider

The Azure provider mirrors the TypeScript `@ai-sdk/azure` provider for Azure OpenAI Responses, Chat Completions, Completions, embeddings, image, speech, and transcription request surfaces.

## Setup

```go
provider, err := azure.New(azure.Config{
    ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
    APIKey:       os.Getenv("AZURE_API_KEY"),
})
if err != nil {
    log.Fatal(err)
}
```

`LanguageModel` uses the Responses API by default at `/openai/v1/responses?api-version=v1`, matching the TypeScript Azure provider default. Use `ChatModel` for Chat Completions or `CompletionModel` for legacy Completions.

## Microsoft Entra ID

Use `ADTokenProvider` when Azure OpenAI should authenticate with Microsoft Entra ID instead of an API key. The token provider is called with the request context and sends `Authorization: Bearer <token>`.

```go
provider, err := azure.New(azure.Config{
    ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
    ADTokenProvider: func(ctx context.Context) (string, error) {
        return acquireToken(ctx)
    },
})
if err != nil {
    log.Fatal(err)
}
```

`ADTokenProvider` is used across Responses, Chat Completions, Completions, embeddings, image, speech, and transcription request surfaces. Passing both `APIKey` and `ADTokenProvider` returns an invalid-argument error, matching the TypeScript `createAzure` validation.

