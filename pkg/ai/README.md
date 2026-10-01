# AI Core

`pkg/ai` contains the provider-neutral generation, streaming, embedding, speech, realtime, sandbox, and tool-loop APIs that mirror the TypeScript `ai` package using idiomatic Go types.

## ToolOrder

`GenerateTextOptions.ToolOrder` and `StreamTextOptions.ToolOrder` control the order tools are sent to providers after active-tool filtering, middleware transforms, and `PrepareStep` updates. Listed names are sent first in the provided order; unlisted tools follow alphabetically.

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:     model,
    Prompt:    "Inspect the account.",
    Tools:     tools,
    ToolOrder: []string{"readProfile", "listOrders"},
})
```

This matches the TypeScript `toolOrder` behavior in `prepare-tools.ts`, including partial order lists and duplicate tool-order entries.

## OnStepEnd

`OnStepEnd` is the canonical callback name for completed generation steps. `OnStepFinish` remains as a deprecated compatibility alias; when both are configured, `OnStepEnd` takes precedence.

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Tools: tools,
    OnStepEnd: func(ctx context.Context, step types.StepResult, userContext interface{}) {
        fmt.Println(step.StepNumber, step.FinishReason, step.Usage)
    },
})
```

The same rename applies to typed event callbacks: use `OnStepEndEvent` instead of `OnStepFinishEvent`. The rename is also reflected in object generation, UI message streams, workflow agents, and telemetry integrations.

## Realtime Sessions

`ConnectRealtime` is the provider-neutral server-side helper for `provider.Experimental_RealtimeModelV4`.

```go
instructions := "You are concise."

session, err := ai.ConnectRealtime(ctx, model, ai.RealtimeSessionOptions{
    SessionConfig: &provider.RealtimeSessionConfig{
        Instructions: &instructions,
    },
})
```

Browser-only TypeScript realtime APIs are intentionally not exposed in Go.
