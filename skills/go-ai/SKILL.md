---
name: go-ai
description: Write Go code with the Go AI SDK (github.com/digitallysavvy/go-ai). Use when a Go project imports github.com/digitallysavvy/go-ai/pkg/..., or the task is generating text or structured output, streaming, tools, tool approval, agents, MCP, embeddings or a useChat backend in Go.
---

# Go AI SDK

Go toolkit for AI applications and agents, ported from the Vercel AI SDK (TypeScript). Module `github.com/digitallysavvy/go-ai`, Go 1.26 or later, packages under `pkg/`.

## Read the docs first

1. Fetch https://goaisdk.com/agents.md. It has the module path, the six most common calls and the usual mistakes.
2. Find a page in https://goaisdk.com/llms.txt (see "Start here") or https://goaisdk.com/sitemap.md.
3. Fetch any docs page as markdown by appending `.md` to its URL. Use https://goaisdk.com/llms-core.txt only when you need the core docs in one file. Avoid llms-full.txt.

If the `goai-docs-mcp` server is configured, call `search_docs` and `read_doc` instead of fetching.

## Rules that prevent most mistakes

- `provider.LanguageModel(id)` returns `(model, error)`. Check the error.
- Streams are `provider.TextStream` (`Next`, `Err`, `Close`) or `result.Chunks()`. They do not implement `io.Reader`.
- Provider option and metadata keys are camelCase.
- Use `ToolApproval` on a tool, not the deprecated `NeedsApproval`.
- `ai.GenerateText` and `ai.StreamText` run one step unless you set `StopWhen` (for example `ai.StepCountIs(5)`). An `agent.ToolLoopAgent` defaults to 20 steps.
- For a useChat endpoint call `ai.PipeUIMessageStreamToResponse(ctx, result, w)` or `agent.PipeAgentUIStreamFromUIMessagesToResponse`. They set the status and stream headers on an `http.ResponseWriter`; do not set them yourself.
- `OnStepFinish` has different signatures in `ai` and `agent`. Check the godoc with `go doc`.
- Use model ID constants from the provider package (for example `anthropic.ClaudeSonnet5_5`). Do not write model IDs from memory.

## Minimal call

```go
prov := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
model, err := prov.LanguageModel(anthropic.ClaudeSonnet5_5)
if err != nil {
    return err
}
res, err := ai.GenerateText(ctx, ai.GenerateTextOptions{Model: model, Prompt: "Hello"})
if err != nil {
    return err
}
fmt.Println(res.Text)
```

Imports: `github.com/digitallysavvy/go-ai/pkg/ai` and `.../pkg/providers/anthropic`.

## Reference

- Reference app with a useChat frontend, Go backend, approval-gated tool and coding-agent harness: https://github.com/digitallysavvy/go-ai-shipyard
- Setup guide for agents: https://goaisdk.com/docs/getting-started/using-go-ai-with-coding-agents
