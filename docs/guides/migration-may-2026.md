# May 2026 Migration Guide

This guide covers the agent, registry, middleware, and operational API changes from the May 2026 parity cycle.

## Agent Interface

The `agent.Agent` interface now follows the TypeScript SDK agent contract. Custom agent implementations must expose identity, tool, generate, and stream methods in addition to the legacy Go convenience execution methods.

Before:

```go
type MyAgent struct{}

func (a *MyAgent) Execute(ctx context.Context, prompt string) (*agent.AgentResult, error) { /* ... */ }
func (a *MyAgent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*agent.AgentResult, error) {
    /* ... */
}
```

After:

```go
type MyAgent struct{}

func (a *MyAgent) Version() string { return "agent-v1" }
func (a *MyAgent) ID() string { return "support-agent" }
func (a *MyAgent) Tools() []types.Tool { return nil }
func (a *MyAgent) Generate(ctx context.Context, opts agent.AgentGenerateOptions) (*agent.AgentResult, error) {
    /* ... */
}
func (a *MyAgent) Stream(ctx context.Context, opts agent.AgentStreamOptions) (*ai.StreamTextResult, error) {
    /* ... */
}
func (a *MyAgent) Execute(ctx context.Context, prompt string) (*agent.AgentResult, error) { /* ... */ }
func (a *MyAgent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*agent.AgentResult, error) {
    /* ... */
}
```

## RuntimeContext and ToolsContext

`RuntimeContext` is now the primary call-level context. `ExperimentalContext` remains as a deprecated alias and is used only when `RuntimeContext` is not set. Per-tool context belongs in `ToolsContext` and is validated against each tool's `ContextSchema` before approval callbacks or tool execution.

Before:

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    ExperimentalContext: map[string]any{"requestID": "req_123"},
})
```

After:

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    RuntimeContext: map[string]any{"requestID": "req_123"},
    ToolsContext: map[string]any{
        "lookup": map[string]any{"tenant": "acme"},
    },
})
```

Dynamic tool descriptions receive the same per-tool context used by the TypeScript SDK:

```go
types.Tool{
    Name: "lookup",
    DescriptionFunc: func(ctx context.Context, opts types.ToolDescriptionOptions) string {
        return fmt.Sprintf("Lookup data for %v", opts.Context)
    },
}
```

## Tool Approval

Tool approval callbacks can now return a zero-value `types.ToolApprovalResult{}` to mean `not-applicable`. Denials preserve the approval status and reason in `types.ToolResult` and use a default message that includes the tool name. Per-tool approval callbacks run only after the matching `ContextSchema` validates successfully; validated JSON-schema defaults are applied before `ToolContext` is passed to approval or execution callbacks.

Tool-defined approval callbacks can use the options-based `types.ToolNeedsApprovalFunc` to receive the same data as the TypeScript SDK: tool call ID, pre-call messages, and validated tool context. The older `types.NeedsApprovalFunc` remains available as a deprecated input-only compatibility path.

Before:

```go
ToolApprovalRequired: true,
ToolApprover: func(call types.ToolCall) bool { return false },
```

After:

```go
ToolApproval: types.ToolApprovalFunc(func(
    call types.ToolCall,
    tools []types.Tool,
    messages []types.Message,
    runtimeCtx any,
    toolsCtx map[string]any,
) types.ToolApprovalResult {
    return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
}),
```

## MaxSteps and StopWhen

`MaxSteps` is deprecated for agents. Existing callers still work: `MaxSteps: n` is routed to `StopWhen: []ai.StopCondition{ai.IsStepCount(n)}` and emits a deprecation warning. New code should use `StopWhen` directly.

Before:

```go
agent.NewToolLoopAgent(agent.AgentConfig{Model: model, MaxSteps: 5})
```

After:

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    StopWhen: []ai.StopCondition{ai.IsStepCount(5)},
})
```

## Prompt Field

`Prompt` is available as a TypeScript-compatible alias for the agent system prompt. If `System` is empty, `Prompt` is used as the system prompt sent to the model.

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    Prompt: "You are a careful support agent.",
})
```

## FilterActiveTools

Use `FilterActiveTools` to restrict tool availability per step.

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    Tools: tools,
    FilterActiveTools: func(ctx context.Context, step int, tools []types.Tool) []types.Tool {
        if step == 1 { return tools[:1] }
        return tools
    },
})
```

## Telemetry Event Names

Telemetry integrations should use the stable end-event names from the May 2026 TypeScript SDK cycle:

| Previous Go name | New Go name |
| --- | --- |
| `OnToolCallStart` / `FireOnToolCallStart` | `OnToolExecutionStart` / `FireOnToolCallStart` |
| `OnToolCallFinish` / `FireOnToolCallFinish` | `OnToolExecutionEnd` / `FireOnToolCallFinish` |
| `OnFinish` / `FireOnFinish` | `OnEnd` / `FireOnEnd` |
| `OnEmbedFinish` / `FireOnEmbedFinish` | `OnEmbedEnd` / `FireOnEmbedEnd` |
| `OnRerankFinish` / `FireOnRerankFinish` | `OnRerankEnd` / `FireOnRerankEnd` |

`TelemetryIntegration` now includes the TypeScript-aligned `OnToolExecutionStart` and `OnToolExecutionEnd` methods. `OnToolCallStart` and `OnToolCallFinish` remain as deprecated aliases.

Telemetry no longer emits per-chunk `OnChunk` events; stream consumers should continue using `StreamTextOptions.OnChunk` for application-level chunk handling.

`GenerateTextOptions`, `StreamTextOptions`, `agent.AgentGenerateOptions`, and `agent.AgentConfig` also prefer `OnToolExecutionStart` and `OnToolExecutionEnd` for tool execution callbacks. The older `OnToolCallStart` and `OnToolCallFinish` option fields remain deprecated aliases.

## Result Content And Step Performance

`GenerateTextResult.Content` now matches the TypeScript SDK `result.content` getter: it is the ordered aggregate of every step's content parts, including text, reasoning, files, sources, tool calls, tool results, tool errors, and tool approval request/response parts.

`types.StepResult` now includes `Performance` statistics matching the TypeScript SDK: `StepTimeMs`, `ResponseTimeMs`, `ToolExecutionMs`, `TokensPerSecond`, and streaming-only `TimeToFirstTokenMs`. Use `FinalStep.Performance.TokensPerSecond` for final-step throughput and `Steps[i].Performance` for multi-step workflows.

`StepTimeMs` is measured as wall-clock step duration, including client-side tool execution. `ToolExecutionMs` is keyed by `toolCallID`.

## Sandbox Description

`ai.Sandbox` now includes `Description() string`. When a sandbox is active, non-empty descriptions are appended to system instructions before provider calls, matching the TypeScript SDK sandbox instruction behavior.

```go
sandbox := ai.NewShellSandbox(
    ai.WithShellSandboxDescription("Ubuntu 22.04, root: /workspace"),
)
```

## CallOptionsSchema

`CallOptionsSchema` is enforced before any model call. Validation errors include the `options` schema path and are returned as provider validation errors.

```go
agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    CallOptions: map[string]any{"mode": "safe"},
    CallOptionsSchema: schema.NewSimpleJSONSchema(map[string]any{
        "type": "object",
        "required": []any{"mode"},
    }),
})
```

## Registry Files and Skills

Registries now resolve upload APIs directly. Providers that do not expose the requested API return `NoSuchProviderError`-equivalent errors.

```go
files, err := registry.GetGlobalRegistry().Files("anthropic")
skills, err := registry.GetGlobalRegistry().Skills("anthropic")
```

## Provider v4 File Data and Reasoning Files

Middleware wrappers preserve the wrapped model specification version and pass provider file data, custom content, and reasoning-file content through without downgrading model metadata.

## System Message Validation

System instructions should be passed via `System` or agent `Prompt`. Passing system-role messages still requires `AllowSystemMessages` / `AllowSystemInMessages` on lower-level generation calls.

## Upload APIs

Use `core.UploadFile` and `core.UploadSkill` with providers or registry-resolved `Files()` / `Skills()` APIs. Unsupported providers return explicit capability errors rather than nil API panics.
