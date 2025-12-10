# Retry Middleware

Automatic retry with exponential backoff for failed AI requests.

## Features

- Exponential backoff
- Configurable max retries
- Automatic error recovery

## Quick Start

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Usage

```go
retry := NewRetryMiddleware(3, 1*time.Second)
result, err := retry.GenerateText(ctx, opts)
```
