# Middleware Example

This example demonstrates how to use middleware to wrap and enhance model behavior in the Go AI SDK.

## Features Demonstrated

1. **Default Settings Middleware** - Apply default parameters to all calls
2. **Custom Middleware** - Create custom behavior wrapping
3. **Provider-Level Middleware** - Apply middleware to all models from a provider

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

1. Set your OpenAI API key:

```bash
export OPENAI_API_KEY=sk-...
```

2. Run the example:

```bash
go run main.go
```

## What You'll Learn

### Default Settings
- Using `DefaultSettingsMiddleware` to set default temperature
- How settings cascade and can be overridden

### Custom Middleware
- Creating custom middleware with `TransformParams`
- Adding system messages automatically
- Modifying request parameters before they reach the model

### Provider Wrapping
- Applying middleware at the provider level
- How middleware affects all models from a wrapped provider
- The middleware lifecycle and call order

## Middleware Use Cases

Middleware is useful for:
- **Logging** - Track all model calls and responses
- **Caching** - Cache repeated requests
- **Rate Limiting** - Implement backoff and retry logic
- **Default Settings** - Enforce temperature, maxTokens, etc.
- **System Messages** - Add consistent instructions
- **Token Tracking** - Monitor usage across requests
- **Error Recovery** - Implement fallback logic
- **A/B Testing** - Route requests to different models

## Expected Output

The example will demonstrate:
1. Text generation with default temperature settings
2. Automatic system message injection
3. Provider-level middleware affecting all models

## Notes

- Middleware can be stacked - multiple middleware are applied in order
- Middleware can transform both requests and responses
- Use `WrapLanguageModel` for single models, `WrapProvider` for all models
