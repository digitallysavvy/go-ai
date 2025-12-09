# Integration Testing Example

Integration tests for AI SDK with real API calls.

## Features

- Real API testing
- Tool integration tests
- End-to-end validation

## Run Tests

```bash
export OPENAI_API_KEY=sk-...
go test -v
go test -v -short  # Skip integration tests
```

## Note

These tests make real API calls and may incur costs.
