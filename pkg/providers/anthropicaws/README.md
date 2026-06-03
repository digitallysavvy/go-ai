# Anthropic AWS Provider

`pkg/providers/anthropicaws` calls Claude Platform on AWS with the same language model, Files API, Skills API, and Anthropic tool support as `pkg/providers/anthropic`.

## API Key

```go
provider, err := anthropicaws.New(anthropicaws.Config{
    Region:      os.Getenv("AWS_REGION"),
    WorkspaceID: os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"),
    APIKey:      os.Getenv("ANTHROPIC_AWS_API_KEY"),
})
```

## SigV4

If no API key is provided, requests are signed with SigV4 using `AccessKeyID`/`SecretAccessKey`, environment variables, or `CredentialProvider`.

```go
provider, err := anthropicaws.New(anthropicaws.Config{
    Region:          os.Getenv("AWS_REGION"),
    WorkspaceID:     os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"),
    AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
    SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
    SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
})
```

Use `provider.LanguageModel(anthropic.ClaudeOpus4_8)` and other Anthropic model constants with the returned provider.
