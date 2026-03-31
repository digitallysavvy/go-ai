# Go AI SDK v0.4.0 Release Notes

## Overview

Version 0.4.0 achieves full parity with TS AI SDK v6.0.137 (commit range
`ed17fe86d..429b88a79` plus v6.0.137 backports). 13 PRDs and 244 tasks across
3 priority waves. Highlights: top-level reasoning parameter, SSRF protection,
streaming architecture refactor, deferred provider tool results, new Anthropic
tools, OpenAI Responses API features, XAI Responses API migration, and a new
Prodia provider.

## Installation

```bash
go get github.com/digitallysavvy/go-ai@v0.4.0
```

---

## Breaking Changes

### Streaming Tool Execution Timing

Tools are now executed **after** the stream is fully consumed, not mid-stream.
If you relied on tool results arriving interleaved with text chunks, update your
consumer to expect all text chunks first, then tool-result chunks.

### XAI Default API Changed to Responses API

`xai.Provider.LanguageModel()` now returns a Responses API model. Use
`xai.Provider.ChatCompletionsLanguageModel()` to opt back into the legacy Chat
Completions API.

### Removed Model IDs

- `grok-2` and `grok-2-vision-1212` removed from XAI (use `grok-3` family)

### MCP Default Redirect Mode

MCP HTTP transport now defaults to `MCPRedirectError` (reject redirects).
Set `Redirect: mcp.MCPRedirectFollow` in `TransportConfig` to restore the
previous behavior.

---

## Security

### SSRF Redirect Protection

`validateDownloadURL()` now rejects redirects to private IP ranges, localhost,
link-local, and CGNAT addresses. Wired into HTTP client via `CheckRedirect` for
both pre-fetch and post-redirect validation. Covers IPv4, IPv6, and
IPv4-mapped-IPv6 addresses.

### Streaming Tool Call Fix

All streaming providers (OpenAI, Groq, DeepSeek, Alibaba, OpenAI Responses API)
now accumulate tool call arguments and finalize only at `finish_reason`. Prevents
early finalization from partially-valid JSON fragments mid-stream.

### MCP OAuth State Validation

OAuth state parameter comparison now uses `crypto/subtle.ConstantTimeCompare` to
prevent timing-based CSRF attacks.

---

## New Features

### Top-Level Reasoning Parameter

A unified `Reasoning *types.ReasoningLevel` field on `GenerateTextOptions` and
`StreamTextOptions` maps to each provider's native reasoning control:

| Level | Anthropic | OpenAI | Bedrock | Google |
|-------|-----------|--------|---------|--------|
| `none` | `disabled` | `disabled` | `disabled` / `disabled` | `thinkingBudget: 0` |
| `minimal` | 2% of max output | `low` | 2% / `low` | 2% of max |
| `low` | 10% | `low` | 10% / `low` | 10% |
| `medium` | 30% | `medium` | 30% / `medium` | 30% |
| `high` | 60% | `high` | 60% / `high` | 60% |
| `xhigh` | 90% | `high` | 90% / `high` | 90% |
| `provider-default` | omitted | omitted | omitted | omitted |

Anthropic budgets are dynamic per model (e.g. claude-sonnet-4-6 max 128k).
Bedrock uses Anthropic-style for Claude models, OpenAI-style for Nova models.
Google uses `thinkingLevel` string for Gemini 3 models.

```go
reasoning := types.ReasoningMedium
result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:     model,
    Prompt:    "Explain quantum computing",
    Reasoning: &reasoning,
})
```

### Deferred Provider Tool Results

Provider-executed tools (web search, web fetch, code execution) that return
results asynchronously are now tracked via `pendingDeferredToolCalls`. The step
loop continues when deferred results are pending, even if `FinishReason` is not
`ToolCalls`. Replaces the hardcoded `isProviderExecutedTool()` name map with the
`tool.ProviderExecuted` flag.

### Custom Content & Reasoning File Types

Two new content types for `GenerateResult.Content` and stream chunks:

- **`CustomContent`** — wraps unknown provider response parts with
  `Kind` (e.g. `"xai-citation"`), `ProviderOptions` (input), and
  `ProviderMetadata` (output as `json.RawMessage`)
- **`ReasoningFileContent`** — reasoning output as binary file with
  `MediaType` and `Data` (`[]byte`, auto base64 in JSON)

Both types flow through all four message converters (OpenAI, Anthropic, Google,
Vertex) and surface in stream chunks (`ChunkTypeCustom`, `ChunkTypeReasoningFile`).

### Telemetry Registry

Global `TelemetryIntegration` interface with `sync.RWMutex`-protected registry.
`RegisterTelemetryIntegration()` / `GetTelemetryIntegration()` replace per-call
telemetry options. Ships with `NoopTelemetryIntegration` (default) and
`OTelTelemetryIntegration`. `Fire*` fan-out functions wired into generate.go and
stream.go — no direct OTel imports in core.

---

## Anthropic Provider

### New Tools: webSearch_20260209 & webFetch_20260209

Updated tool versions with enhanced configuration:

- **`WebSearch20260209(config)`** — `AllowedDomains`, `BlockedDomains`,
  `UserLocation` (approximate geolocation), `MaxUses`. Returns
  `[]WebSearchResult20260209` with `EncryptedContent` for multi-turn citations.
- **`WebFetch20260209(config)`** — `AllowedDomains`, `BlockedDomains`,
  `Citations`, `MaxContentTokens`, `MaxUses`. Returns `WebFetchResult20260209`
  with discriminated `WebFetchSource` (PDF via base64 or plain text).
  `IsPDF()` / `IsPlainText()` helpers.

Both tools set `SupportsDeferredResults: true` and auto-inject the
`code-execution-web-tools-2026-02-09` beta header.

### Fine-Grained Tool Input Streaming

`EagerInputStreaming *bool` per-tool option on custom function tools. When
enabled, emits `tool-input-start`, `tool-input-delta`, and `tool-input-end`
stream chunks as the model streams tool call arguments. Provider tools
(web_search, web_fetch) do not emit these events.

### Error Code Preservation

`AnthropicAPIError.error.type` now surfaces as `ProviderError.Code`.

---

## OpenAI Provider

### GPT-5.4 Model Family

New model IDs: `gpt-5.4`, `gpt-5.4-pro`, `gpt-5.4-2026-03-05`,
`gpt-5.4-pro-2026-03-05`. Plus `gpt-5.3-chat-latest` and `gpt-5.3-codex`.

### Responses API: Server-Side Compaction

Compaction events in Responses API streaming are parsed and emitted as
`CustomContent` chunks with `Kind: "openai-compaction"` and metadata containing
`type`, `itemId`, `encryptedContent`.

### Tool Search

`openai.ToolSearch(args)` factory with server (default) and client execution
modes. Tool ID: `openai.tool_search`. Client mode routes `tool_search_call`
events to a user-provided `Execute` callback with `ToolCallID` round-trip.

### CustomTool Name Field Removed

`CustomTool` no longer has a `Name` field. Name is supplied via `ToTool(name)`
method, matching the TS SDK's key-based naming convention.

### store=false Drops Unencrypted Reasoning

When `store=false` in OpenAI provider options, `ReasoningContent` without
`EncryptedContent` is automatically stripped from assistant history messages
before serialization.

---

## XAI Provider

- **Responses API as default** — `LanguageModel()` returns Responses API model
- **Chat Completions opt-in** — `ChatCompletionsLanguageModel()` for legacy API
- **Image editing** — accepts `[]ImageFile` for multi-image input
- **b64_json** output format support
- **Quality and User params** for image model options
- **CostInUsdTicks** in both image and video provider metadata
- **ReasoningSummary** option (`auto`, `concise`, `detailed`) in Responses API
- **ModerationError** type for video model content filtering
- **Logprobs** — `*bool` and `TopLogprobs *int` options (chat + Responses API)
- **Reasoning extraction fix** — handles both `summary` and `content` arrays

---

## Google Provider

- **VALIDATED function calling mode** — auto-enabled when any tool has `Strict: true`
- **Grounding metadata accumulation** — merged across all stream chunks
- **Multimodal tool-result** — `functionResponse.parts[]` for Gemini 3+ models
- **finishMessage** in Vertex provider metadata
- **7 native Vertex tools** — GoogleSearch, EnterpriseWebSearch, GoogleMaps,
  UrlContext, FileSearch, CodeExecution, VertexRagStore
- **gemini-embedding-2-preview** model ID with multimodal embedding support
- **Reasoning files** marked as `ReasoningFileContent` in responses

---

## Multi-Provider Reasoning Migration

Top-level `Reasoning` parameter wired through all OpenAI-compatible providers:

| Provider | Mapping | Notes |
|----------|---------|-------|
| DeepSeek | `thinking` object | budget_tokens from level |
| Alibaba | `enable_thinking` + `thinking_budget` | |
| Fireworks | `reasoning_effort` | low/medium/high |
| Groq | `reasoning_effort` | low/medium/high |
| Mistral | `reasoning_effort` | mistral-small-latest only; warning on others |
| Perplexity | warning | not supported |
| Cohere | warning | not supported |
| Open Responses | `reasoning_effort` | low/medium/high |
| XAI | `reasoning_effort` | via OnReasoningDelta hook |

---

## Core SDK

### Tool-Level Timeouts

`TimeoutConfig` gains `ToolMs` (global tool timeout) and `Tools map[string]int`
(per-tool overrides). `GetToolTimeout(toolName)` helper. Wired into both
generate.go and stream.go tool execution paths.

### Embed & Rerank Callbacks

- `ExperimentalOnStart` / `ExperimentalOnFinish` on `EmbedOptions`, `EmbedManyOptions`, `RerankOptions`
- Typed event structs: `EmbedOnStartEvent`, `EmbedOnFinishEvent`, `RerankOnStartEvent`, `RerankOnFinishEvent`
- Provider options and HTTP response headers threaded through events

### Stream Result Provider Metadata

`StreamTextResult.ProviderMetadata()` returns accumulated provider metadata from
all stream chunks.

### ToolResult.Input Population

`ToolResult.Input` is now always populated with `call.Arguments` for dynamic
tool results.

---

## KlingAI Provider

### v3.0 Motion Control

New video generation mode with endpoint routing to `/v1/videos/motion-control`:

- **Multi-shot** — `MultiShot`, `ShotType`, `MultiPrompt` with per-shot `Duration` (string)
- **Element control** — `ElementList []ElementRef` (up to 3 for I2V, 1 for motion control)
- **Voice control** — `VoiceList []VoiceRef`
- **Motion brush** — `StaticMask`, `DynamicMasks` with trajectory coordinates

Model IDs: `kling-v3.0-motion-control`, `kling-v2.6-motion-control`.

---

## New Provider: Prodia

Full provider with language model (img2img) and video model (T2V/I2V):

- **Language model** — multipart form-data requests with `job` + `input` parts.
  Supports 11 aspect ratios. `DoGenerate` and `DoStream` (synchronous wrapper).
- **Video model** — T2V (`wan2-2.lightning.txt2vid.v0`) and I2V
  (`wan2-2.lightning.img2vid.v0`). Multipart response parsing with MIME
  type detection.
- **Shared infrastructure** — `prodia_api.go` with `buildMultipartJobRequest`,
  `postMultipartToProdia`, `parseMultipartResponse` helpers.

---

## Provider Fixes

- **Alibaba**: single-item content array with `cache_control` preserves array structure
- **Perplexity**: `ProviderMetadata` restructured to `{Images, Usage, Cost}` sub-objects; cost reads from `usage.cost.*` wire format
- **MCP**: protocol version `2025-11-25` added (full list: `2025-11-25`, `2025-06-18`, `2025-03-26`, `2024-11-05`)
- **MCP**: `MCPRedirectMode` typed option — `MCPRedirectError` (default) and `MCPRedirectFollow`
- **HTTP transport**: custom `User-Agent` header removed

---

## Testing

```
go vet ./...
go test -race ./...
```

All 244 tasks verified across 13 PRDs. Integration tests skip gracefully
without provider credentials. Comprehensive test coverage for all new features
including security fixes, streaming behavior, telemetry integration, and
provider-specific functionality.

---

**TS SDK Parity:** v6.0.137 (fully compatible)
**TS SDK Commit Range:** `ed17fe86d..429b88a79` plus v6.0.137 backports
**Total PRDs:** 13
**Total Tasks:** 244
**Branch:** `release/0.4.0`
