# Provider Utils

`pkg/providerutils` contains shared helpers and public utility types used by providers.

## Sandbox Session

Sandbox implementations use the TypeScript SDK v7 naming and file-helper surface. Implement `providerutils.SandboxSession` or the exported alias `providerutils.Experimental_SandboxSession`.

```go
type SandboxSession interface {
    Description() string
    Run(ctx context.Context, opts SandboxProcessOptions) (SandboxRunResult, error)
    Spawn(ctx context.Context, opts SandboxProcessOptions) (SandboxProcess, error)
    ReadFile(ctx context.Context, path string) (io.ReadCloser, error)
    ReadBinaryFile(ctx context.Context, path string) ([]byte, error)
    ReadTextFile(ctx context.Context, opts SandboxReadTextFileOptions) (*string, error)
    WriteFile(ctx context.Context, path string, content io.Reader) error
    WriteBinaryFile(ctx context.Context, path string, content []byte) error
    WriteTextFile(ctx context.Context, opts SandboxWriteTextFileOptions) error
}
```

`SandboxProcessOptions.Env` is forwarded to both `Run` and `Spawn`, matching the TypeScript `Experimental_SandboxSession` process options shape.

```go
result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
    Command: "printenv GREETING",
    Env: map[string]string{
        "GREETING": "hello",
    },
})
```

The old Go-only sandbox names are superseded by `Experimental_SandboxSession`, `SandboxProcessOptions`, and `SandboxRunResult`.

