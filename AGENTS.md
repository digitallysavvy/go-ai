# AGENTS.md

Guidance for AI coding agents working in this repository. People should start
with [CONTRIBUTING.md](./CONTRIBUTING.md); the rules below apply to both.

## Using the SDK

If you are writing an application with the SDK rather than changing it, do not
use the rest of this file. Instead:

- Read <https://goaisdk.com/agents.md>: module path, the six most common calls
  and the usual mistakes.
- Find pages in <https://goaisdk.com/llms.txt> (start with "Start here") or
  <https://goaisdk.com/sitemap.md>. Any docs URL with `.md` appended returns
  that page as markdown.
- Install the agent skill in [`skills/go-ai/SKILL.md`](./skills/go-ai/SKILL.md),
  or run the docs MCP server in [`cmd/goai-docs-mcp`](./cmd/goai-docs-mcp)
  (`go install github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp@latest`).
  The guide at `docs/02-getting-started/04-using-go-ai-with-coding-agents.mdx`
  has a snippet to paste into your own AGENTS.md or CLAUDE.md.
- Reference app (useChat frontend, Go backend, approval-gated tool, coding-agent
  harness): <https://github.com/digitallysavvy/go-ai-shipyard>.

## What this is

`github.com/digitallysavvy/go-ai` is the Go AI SDK: a Go port of Vercel's
TypeScript AI SDK (`ai` on npm), tracking it 1:1 for server-side features.
The current parity target is `ai@7.0.127`. Intentional differences are listed in
`docs/08-migration-guides/known-differences.mdx`; anything else that behaves
differently from TypeScript is a bug.

## Layout

| Path | What lives there |
|---|---|
| `pkg/ai` | The public API: `GenerateText`, `StreamText`, `GenerateObject`, `Embed`, tools, UI message streams |
| `pkg/provider` | Model and provider interfaces, shared types (`pkg/provider/types`) and errors |
| `pkg/providers/<name>` | One package per model provider |
| `pkg/providerutils` | Shared provider code; OpenAI-compatible providers embed `streaming.OpenAICompatStream` |
| `pkg/agent`, `pkg/workflow` | Tool-loop agents and durable workflows |
| `pkg/mcp` | Model Context Protocol client |
| `pkg/middleware`, `pkg/registry` | Model middleware and the provider registry |
| `pkg/harness`, `pkg/codemode` | Coding-agent harnesses and the sandboxed code-mode runtime |
| `examples/` | Runnable examples (some are separate Go modules) |
| `docs/` | Documentation source, published by `website/` (Docusaurus) |
| `cmd/goai-docs-mcp` | MCP server that searches and reads the docs for coding agents |
| `skills/go-ai` | Agent skill for people using the SDK |

## Commands

Run these before every commit; CI runs the same checks.

```bash
go build ./...
go vet ./...
go test -race ./...
.github/scripts/build-examples.sh            # every example compiles and vets
go run docs/scripts/validate-links.go -docs=docs/   # docs links + frontmatter
golangci-lint run ./...                      # v2.14.0 (pinned in CI), config in .golangci.yml
```

For docs-site changes, also run `npm ci && npm run build` in `website/`.

## Conventions

- **Match the TypeScript SDK.** Before changing behavior, check the TypeScript
  source for the same feature: names, defaults, error text and wire formats
  should match. Error messages intentionally copy TypeScript's exact wording,
  which is why some carry `//nolint:staticcheck` (ST1005).
- **JSON tags:** user-facing option and metadata keys are camelCase (TypeScript
  parity); provider wire-format keys built in request bodies stay snake_case
  where the provider API uses it.
- **Streams** implement `provider.TextStream` (`Next`, `Err`, `Close`), not
  `io.Reader`.
- **Tests:** guard variables shared with callback goroutines with a
  `sync.Mutex`, and wait for the callback before asserting (finish callbacks
  can fire after `Text()` returns). Integration tests must skip when their
  credentials are not set.
- **Examples** that hold several `main` programs in one directory need a
  `//go:build ignore` tag on each file.
- **Minimum Go version** is 1.26; CI tests 1.26 and 1.27.
- Keep `pkg/internal/third_party/qjs` on wazero v1.9.x; newer versions break the
  code-mode sandbox (see `.github/dependabot.yml`).
