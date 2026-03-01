## Feb 28, 2026 — TypeScript SDK Parity Update

Closes the gap to the TypeScript AI SDK commit range `c123363c0..ed17fe86d`
(124 commits). 18 PRDs across 3 priority waves.

---

### ⚠️ Breaking Change

**Anthropic structured output request format** (`P0-5`)
`output_format` has been renamed to `output_config.format` in the Anthropic and
Amazon Bedrock request builders, matching the live API. Any client constructing
raw Anthropic request bodies must update accordingly.

---

### 🆕 New Provider

**ByteDance / Volcengine** (`P0-1`)
Full video generation provider at `pkg/providers/bytedance/`. Async-polling model:
submit → poll on `request_id` → return on completion. Supports all ByteDance video
model IDs. Integration test skips without `BYTEDANCE_API_KEY`.

---

### 🏗️ Architecture

**Output[T] system** (`P0-2`)
Unifies `GenerateText` and `GenerateObject` under a single `WithOutput` parameter.
Five output factories: `TextOutput`, `ObjectOutput`, `ArrayOutput`, `ChoiceOutput`,
`JSONOutput`. `GenerateObject`/`StreamObject` are deprecated but remain functional.

**Structured callback events** (`P0-3`)
Formal event system with six typed events: `OnStartEvent`, `OnStepStartEvent`,
`OnToolCallStartEvent`, `OnToolCallFinishEvent`, `OnStepFinishEvent`,
`OnFinishEvent`. Panic-safe `Notify[E]` dispatch utility. Agent callback merging.

---

### ✨ Anthropic Provider

- **Code execution tool** (`P0-4`) — `code-execution-20260120` tool with three
  input types: `programmatic-tool-call`, `bash_code_execution`,
  `text_editor_code_execution`. Beta header auto-injected.
- **Provider updates** (`P0-5`) — Automatic caching, Claude Sonnet 4.6 model ID,
  `compaction_delta` null content fix, `SupportsStructuredOutput()` routing fix,
  `DisableParallelToolUse`, thinking strips `temperature`/`topP`/`topK`,
  `fine-grained-tool-streaming` / `cache-control` beta headers.
- **Streaming tool calls** (`P1-4`) — `DoStream` now emits `ChunkTypeToolCall`
  chunks. Buffers `input_json_delta` fragments across `content_block_start` →
  `content_block_stop`; emits assembled arguments at stop. Thinking block
  streaming via `thinking_delta` → `ChunkTypeReasoning`.
- **Native MCP client** (`P1-6`) — `MCPServerConfig` / `MCPToolConfiguration`
  types. `mcp-client-2025-04-04` beta header auto-injected when `MCPServers`
  non-empty. `mcp_servers` request body field. `mcp_tool_use` /
  `mcp_tool_result` response block handling.
- **Agent container & skills** (`P1-7`) — `ContainerConfig` / `ContainerSkill`
  types. `Container` / `ContainerID` model options. Three beta headers injected
  when skills present: `code-execution-2025-08-25`, `skills-2025-10-02`,
  `files-api-2025-04-14`. Container body field as string or object.
- **Structured output mode** (`P2-5`) — `StructuredOutputMode` option:
  `auto` (default), `outputFormat`, `jsonTool`. `auto` routes to
  `output_config.format` on 4.6/4.5 models; falls back to a synthetic `json`
  tool on older models.
- **Send reasoning** (`P2-6`) — `ReasoningContent` gains `Signature` and
  `RedactedData` fields for Anthropic thinking block round-trips.
  `ToAnthropicMessages` now emits `thinking` / `redacted_thinking` blocks.
  `SendReasoning *bool` option strips thinking from history when `false`.

---

### ✨ OpenAI Provider (`P1-1`)

- `CustomTool` with grammar / text format options
- `LocalShell`, `Shell`, `ApplyPatch` container tool types
- MCP approval response type
- `Phase` field on Responses API message items

---

### ✨ Google Provider (`P1-3`)

- `gemini-3.1-pro-preview` and `gemini-3.1-flash-image-preview` model constants
- New image aspect ratios and sizes for Google AI and Vertex Imagen

---

### ✨ KlingAI (`P1-2`)

- `kling-v3.0-t2v` and `kling-v3.0-i2v` model ID constants
- `IsImageToVideo()` updated to recognize v3.0-i2v

---

### ✨ Fireworks (`P1-5`)

- Async image generation for `flux-kontext-*` models: submit → poll every 2s
  → return on `succeeded`

---

### ✨ Gateway (`P2-3`)

- SSE video generation with heartbeat keep-alive — handles `heartbeat`,
  `progress`, `complete`, `error` event types; context cancellation supported
- `ProjectID` field + `WithProjectID()` option — injects project ID header on
  all requests for observability

---

### 🐛 Bug Fixes (`P2-4`)

- Unknown tool name in model response produces error `ToolResult`, not duplicate
  tool part
- Tool choice (`required` / `auto` / specific) now always forwarded to provider
- Stream resumption only sets status to `submitted` when an active stream exists
- `StreamingToolCallDelta.Type` changed to `*string` (nullable) for OpenAI
- `WebSearchToolCall.Action` is now `*string` (optional) for OpenAI
- OpenAI reasoning parts with `EncryptedContent` included even without `ItemID`
- Bedrock and Groq both pass `strict: true` in tool definitions when strict mode
  is requested
- `chatgpt-image` recognized in default response format prefix set

---

### 🔧 Model IDs & Minor Provider Fixes (`P2-2`)

- OpenAI: `gpt-5.3-codex` constant + missing model IDs
- XAI: resolution option + image model IDs
- Bedrock: complete Anthropic model ID set
- Cerebras: deprecated model IDs removed
- TogetherAI: `TOGETHER_API_KEY` env var support
- Alibaba: cache control applied to all messages
- Prodia: price parameter support

---

### 📚 Docs & Examples (`P2-1`)

- `WithOutput` shown as primary pattern for structured data
- `ToolLoopAgent` consistently named across agent docs
- Provider architecture guide added
- `gpt-5.3-codex`, Gemini flash-image, Anthropic context editing examples
- Memory management and coding agents guides

---

### Testing

```
go test -race ./...
go build ./...
golangci-lint run ./...
```

All integration tests skip gracefully without provider credentials.
