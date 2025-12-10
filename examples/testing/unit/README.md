# Unit Testing Example

Unit tests for AI SDK operations with mocks and benchmarks.

## Features

- Mock language models
- Test fixtures
- Benchmarks

## Run Tests

```bash
go test -v
go test -bench=.
go test -cover
```

## Example

```go
func TestGenerateText(t *testing.T) {
    mock := &MockModel{response: "Hello"}
    result, _ := mock.DoGenerate(ctx, nil)
    assert.Equal(t, "Hello", result.Text)
}
```
