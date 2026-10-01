# Azure OpenAI Provider Example

Integration pattern for Azure OpenAI Service.

## Note

Structural example showing Azure OpenAI integration.
Full implementation requires Azure SDK.

## Configuration

- Endpoint URL
- API Key
- Deployment name
- Optional Microsoft Entra ID token provider

## Quick Start

```bash
export AZURE_OPENAI_ENDPOINT=https://...
export AZURE_OPENAI_KEY=...
go run main.go
```

## Microsoft Entra ID

Use `azure.Config.ADTokenProvider` when Azure OpenAI should authenticate with Microsoft Entra ID instead of an API key. The provider sends `Authorization: Bearer <token>` across Responses, Chat Completions, Completions, embeddings, image, speech, and transcription request surfaces.

```go
provider, err := azure.New(azure.Config{
    ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
    ADTokenProvider: func(ctx context.Context) (string, error) {
        return acquireToken(ctx)
    },
})
```

Passing both `APIKey` and `ADTokenProvider` returns an invalid-argument error, matching the TypeScript Azure provider validation. See `examples/generate-text/azure_entra_id.go` for the credential-gated example.
