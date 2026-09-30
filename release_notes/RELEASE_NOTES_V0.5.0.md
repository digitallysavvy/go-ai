# Go AI SDK v0.5.0 Release Notes

## Overview

v0.5.0 tracks TypeScript AI SDK parity target **`ai@7.0.118`** (up from
`ai@6.0.137` in v0.4.0). It ships everything merged since the v0.4.0 tag —
about 1,000 commits across three work efforts: the May and June 2026 parity
cycles (runtime/tool context split, call-level tool approval, the provider
v4 tagged-union file data shape, MCP Apps, new Voyage AI provider,
Claude-on-AWS provider) and the September 2026 parity cycle (everything
else: the xAI Chat Completions removal, Bedrock/Bedrock-Anthropic rebuilt on
shared Converse and Anthropic code paths, `StreamText`'s new async and
full-stream chunk lifecycle, a GenAI-semantic-convention telemetry
integration, `pkg/harness` and nine harness adapters, `pkg/codemode`,
async video/batch/evaluation/Files v4 experimental surfaces, and a long list
of provider-specific fixes).

Headline features: `pkg/harness` (a Go port of `@ai-sdk/harness` with
adapters for Claude Code, Codex, OpenCode, Deep Agents, ACP, Cursor, fx,
GitHub Copilot, Grok Build, and a Vercel Sandbox provider); `pkg/codemode`
(model-written JavaScript in a QuickJS-on-WebAssembly sandbox); a rebuilt
Bedrock provider on the Converse API; `StreamText` returning asynchronously
with a redesigned full-stream chunk lifecycle (`ChunkTypeStart` /
`ChunkTypeStartStep` / `ChunkTypeFinishStep` / `ChunkTypeFinish`); call-level
tool approval with signed resume; a GenAI-semantic-convention OpenTelemetry
integration; and thirteen new providers (Voyage AI, Claude on AWS, Fish
Audio, Cartesia, Rev.ai, Hume, Luma, GMI Cloud, Z.AI, MiniMax, TypeSafe AI,
QuiverAI, and the Go-only You.com).

The step-by-step upgrade guide, with before/after code for every breaking
change below, is `docs/08-migration-guides/from-v0.4-to-v0.5.mdx`.

## Installation

```bash
go get github.com/digitallysavvy/go-ai@v0.5.0
```

---

## Breaking Changes

Each item links to its section in the migration guide
(`docs/08-migration-guides/from-v0.4-to-v0.5.mdx`) for full before/after
code.

### Runtime context, tools, and control flow (May/June cycle)

- **Runtime and tool context split**: `ExperimentalContext` is replaced by
  `RuntimeContext`; per-tool state moves to `ToolsContext`, keyed by tool
  name. `ToolExecutionOptions` carries `RuntimeContext` and `ToolContext`.
  `ExperimentalContext` remains as a deprecated alias.
  → migration guide: "Runtime and tool context are split."
- **Tool approval is call-level**: `ToolNeedsApproval` is renamed
  `ToolApproval`; per-tool `NeedsApproval` is deprecated and only applies
  when no call-level approval is configured.
  → migration guide: "Tool approval is call-level."
- **Tool calls: invalid calls are marked, not dropped or executed**: unknown
  tools or schema-invalid input now produce a `NoSuchToolError` /
  `InvalidToolInputError` result (unless `RepairToolCall` fixes it), instead
  of being silently skipped or run with bad input. Applies to
  `WorkflowAgent`'s default path too.
  → migration guide: "Tool calls: invalid calls are marked, not silently
  dropped or executed."
- **Experimental generation options removed**: use `Output`,
  `FilterActiveTools`, and `PrepareStep` in place of `experimental_output`-
  style names carried over from TS.
  → migration guide: "Experimental generation options were removed."
- **`Tool.Strict` is now `*bool`** (tri-state, TS parity): `nil` leaves
  strict mode unset; an explicit `false` is now forwarded to providers.
  Applies to Open Responses `FunctionTool.Strict` too.
  → migration guide: "Tools: Strict is now *bool."
- **`MaxSteps` is deprecated** in favor of `StopWhen`; both still work, and
  `StopWhen` wins when both are set.
  → migration guide: "MaxSteps is deprecated."

### Messages, files, and prompts (May/June cycle)

- **File data is a tagged union**: provider v4 file parts use
  `types.FileData` with a `Type` discriminator
  (`FileDataTypeData`/`URL`/`Reference`/`Text`). `types.ImageContent` still
  works as a compatibility input.
  → migration guide: "File data is a tagged union."
- **System messages in `Messages` are rejected by default**: put system
  instructions in `GenerateTextOptions.System` / `StreamTextOptions.System`;
  opt into provider-native system-role messages with `AllowSystemMessages`
  (alias `AllowSystemInMessages`).
  → migration guide: "System messages in Messages are rejected by default."
- **Upload APIs use provider capabilities**: `ai.UploadFile` /
  `ai.UploadSkill` return `ProviderReference`, usable as
  `types.FileDataTypeReference`.
  → migration guide: "Upload APIs use provider capabilities."

### Workflow (May/June + September)

- **Workflow transport renamed, approval-resume context precedence
  changed**: `workflow.WorkflowChatTransport` is now the client-side
  `ai.ChatTransport`; the old server-side multiplexer is renamed
  `workflow.WorkflowRunMultiplexer`. On approval resume, per-call
  runtime/tools context and sandbox now override agent defaults (previously
  agent defaults always won).
  → migration guide: "Workflow: transport renames and per-call context
  precedence."
- **Models can be serialized for workflows**: `provider.SerializableModel`
  plus `Serialize*`/`Deserialize*`/`Register*Deserializer` now exists for
  every model kind a provider marks workflow-serializable (language, image,
  video, speech, transcription, embedding, evaluation).
  → migration guide: "Models can be serialized for workflows."

### StreamText and the full stream (September cycle)

- **`StreamText` is asynchronous**: it now returns before the first model
  request, like TS `streamText`. Only option-validation errors return from
  the call itself; prompt errors, approval-signature failures, and
  first-request failures surface through `Err()` / `ReadAll()` / `Stream()`.
  → migration guide: "StreamText is asynchronous."
- **Full-stream chunk lifecycle changed**: `ChunkTypeFinish` now fires once
  per call (total usage), not once per step. Each step is bracketed by new
  `ChunkTypeStartStep` / `ChunkTypeFinishStep` chunks, and `ChunkTypeStart`
  opens the stream once.
  → migration guide: "StreamText full stream: chunk lifecycle changed."

### Telemetry (May/June + September)

- **Tracers belong to integrations**: `telemetry.Options.Tracer` and
  `WithTracer` are removed; construct an integration with its own tracer
  (`telemetry.NewLegacyOpenTelemetry` / `telemetry.NewOpenTelemetry`) and
  register it with `telemetry.RegisterTelemetryIntegration`.
  `TelemetryIntegration.OnLanguageModelCallStart` now returns
  `context.Context`. `OTelTelemetryIntegration` is renamed
  `LegacyOpenTelemetry` (old name kept as a deprecated alias). Lifecycle
  method names are stable as `OnToolExecutionStart` / `OnToolExecutionEnd`.
  → migration guide: "Telemetry: tracers belong to integrations."
- **Callback event fields excluded from JSON**: tool execution event fields
  `StepNumber`, `ModelProvider`, `ModelID`, `Args`, `Result`, `Error`,
  `DurationMs` are deprecated and no longer serialized.
  → migration guide: "Callbacks / events: deprecated tool-execution fields
  excluded from JSON."
- **LegacyOpenTelemetry span shape changed** (affects dashboards/alerts):
  span names drop the `.<functionId>` suffix; tool-call spans are named
  `ai.toolCall` with the name in `ai.toolCall.name`; root/evaluate spans use
  `ai.model.provider` / `ai.model.id` instead of `gen_ai.system` /
  `gen_ai.request.model`; nested step spans are `ai.<op>.doGenerate` /
  `ai.<op>.doStream`; embed/rerank spans use `ai.value`/`ai.values`/
  `ai.documents`; the nested `"chat <model>"` span is removed. The GenAI
  `OpenTelemetry` integration's root span is now `<operation> <modelId>`
  with 1-indexed step spans.
  → migration guide: "LegacyOpenTelemetry span shape."

### Anthropic and Bedrock (September cycle)

- **Anthropic**: request `system`/user content are now arrays;
  `BaseURL` now includes `/v1` (custom base URLs must include it
  themselves); `DisableParallelToolUse` is `*bool`; model-level `Thinking`
  now takes precedence over call-level `Reasoning`; `ModelOptions` JSON tags
  are camelCase (`budgetTokens`, `contextManagement`, `automaticCaching`);
  `ContainerSkill{Type:"custom"}` now requires `ProviderReference`;
  `ToAnthropicFormatWithCache` is removed; a spliced stream now surfaces as
  a `ChunkTypeError` chunk instead of a Go error from `Next()`.
  → migration guide: "Anthropic."
- **Bedrock-Anthropic rebuilt on `anthropic.LanguageModel`**: the Go-only
  `CacheConfig` API (`WithSystemCache`, `WithToolCache`,
  `WithMessageCacheIndices`, `CacheTTL1Hour`, `CacheTTL5Minutes`,
  `NewCacheConfig`) and `PrepareTools`/`UpgradeToolVersion`/`MapToolName`/
  `GetBetaHeaders`/`IsComputerUseTool` are removed; use
  `anthropic.ModelOptions{AutomaticCaching}` / `CacheControl`. Explicit AWS
  credentials no longer pick up `AWS_SESSION_TOKEN` from the environment.
  `anthropic-aws`'s default base URL now includes `/v1`.
  → migration guide: "Bedrock-Anthropic: rebuilt on anthropic.LanguageModel."
- **Bedrock (Converse)**: `bedrock.LanguageModel` now calls the Converse API
  (`/converse`, `/converse-stream`) instead of `/invoke`; `RawRequest` /
  `RawResponse` carry Converse-shaped JSON. Cohere embeddings'
  `MaxEmbeddingsPerCall` is now 96 (was 1). `AWS_ENDPOINT_URL_BEDROCK_RUNTIME`
  / `AWS_ENDPOINT_URL` are now honored for endpoint override when `BaseURL`
  is unset. The unused `bedrock/anthropic.BaseURLFormat` constant is removed.
  → migration guide: "Bedrock (Converse): invoke API replaced with
  Converse."

### xAI and Gateway (September cycle)

- **xAI Chat Completions API removed**: `ChatCompletionsLanguageModel()`,
  `NewLanguageModel`, and `SearchParameters` are gone.
  `LanguageModel()` (Responses API) is the only xAI language model; use
  `xai.WebSearch` / `xai.XSearch` for live search.
  → migration guide: "xAI Chat Completions API removed."
- **Gateway `xai/*` models renamed `spacexai/*`**:
  `GatewayLanguageModelXai*` constants become `...Spacexai...`; 44 language,
  4 image, and 4 video model IDs were removed upstream. `hipaaCompliant` is
  removed from `gateway.Config` / `GatewayProviderOptions`.
  → migration guide: "Gateway: xai/* models renamed to spacexai/*."

### OpenAI, Azure, Google/Vertex (September cycle)

- **OpenAI reasoning models**: `Temperature`, `TopP`, penalties,
  `LogitBias`, `Logprobs` are dropped with warnings; output limit is sent as
  `max_completion_tokens`. `ReasoningNone` sends `reasoning_effort: "none"`
  (was `"disabled"`). `whisper-1` always requests `verbose_json`. Replayed
  non-object `RawArguments` are sent as `{}` (OpenAI/Azure chat only).
  → migration guide: "OpenAI and Azure chat: reasoning-model parameter
  handling."
- **Azure**: unrecognized base URLs are now treated as custom gateways (no
  `/v1` or `api-version` appended).
  → migration guide: "Azure: base URL classification."
- **Google/Vertex**: provider-option key order is now `googleVertex` →
  `vertex` → `google`; Gemini 2.5 thinking budget drops its 1024-token
  floor; non-Gemini Imagen models are removed; Gemini image models ignore
  `N` (auto-batched); the Interactions API sends a flat step array;
  `ToGoogleMessages` is deprecated.
  → migration guide: "Google / Vertex."

### Core, schema, embeddings (September cycle)

- **`GenerateText`/`StreamText`**: return `ToolChoiceViolationError` when
  the model ignores a required/specific tool choice; structured-output
  parse semantics match TS (`NoObjectGeneratedError` on truncation);
  `CustomContent.Kind` uses the `provider.type` form (e.g. `xai.citation`),
  not `provider-type`; no default denial reason for denied tool approvals.
  → migration guide: "Core: structured output, tool choice violation,
  custom content."
- **Embed/EmbedMany/Rerank**: `MaxRetries` is now `*int` (`nil` → 2, the TS
  default); an empty `EmbedMany` call now returns an empty result instead of
  an error.
  → migration guide: "Embed / EmbedMany / Rerank: MaxRetries is now a
  pointer."
- **Schema**: the validator now enforces `additionalProperties: false`.
  → migration guide: "Schema: additionalProperties: false is enforced."
- **UI message streams**: `isAborted` is no longer set from context
  cancellation alone; a new `Outcome` field
  (`completed`/`failed`/`aborted`/`unknown`) is added; `ValidateUIMessages`
  is stricter about required `input`/`output`; `finish-step` no longer
  closes active text/reasoning parts.
  → migration guide: "UI message streams: isAborted / isCancelled
  semantics."
- **SmoothStream**: an invalid `chunking` value now returns
  `*errors.InvalidArgumentError`.

### Smaller providers (September cycle)

- **Perplexity**: migrated to the Agent API (`/v1/agent`); every Sonar-era
  `providerOptions.perplexity` key is removed and replaced
  (`instructions`, `tools`, `models`, `max_steps`, `max_tool_calls`,
  `previous_response_id`, `store`, `language_preference`,
  `reasoning.effort`, `skills`); `PerplexityMetadata` reshaped
  (`Images` always nil, `Cost` gains fields, new `ToolCalls` map); reasoning
  tokens are now a subset of output tokens; PDF input rejected.
- **MCP stdio**: the child process env is now allowlisted
  (`StdioTransportConfig.Env` + `PATH`/`HOME`/`USER`/`LOGNAME`/`SHELL`/
  `TERM`) instead of inherited wholesale.
- **Harness**: `sandboxConfig.OnBootstrap` now actually runs for a
  caller-owned `SandboxSession`.
- **HuggingFace**: ported to the Responses API (`/responses` on
  `router.huggingface.co/v1`); `EmbeddingModel`/`ImageModel` always error
  now (types removed); `Provider()` is `"huggingface.responses"`.
- **CosineSimilarity**: a zero/empty vector now returns `(0, nil)` instead
  of an error; a length mismatch returns a typed `*InvalidArgumentError`.
- **fal**: default base URL fixed to `https://fal.run` (was doubling the
  path); pass fully-qualified model IDs.
- **Middleware/Registry**: `AddToolInputExamplesOptions.Remove` is now
  `*bool`; a model ID without a registry separator now returns a typed
  `NoSuchModelError`.
- **OpenAI-compatible streams**: a stream ending without `finish_reason` now
  emits an error chunk instead of ending silently.
- **Moonshot**: default base URL is now `https://api.moonshot.ai/v1`;
  provider metadata key is `moonshotai`. **Baseten**: default base URL is
  `https://inference.baseten.co/v1`, chat provider name `baseten.chat`.
  **Cerebras**: retired model constants removed. **DeepSeek**:
  `SupportsImageInput()` is now `true`. **Mistral embeddings**:
  `MaxEmbeddingsPerCall` is 32 (was 2048), `SupportsParallelCalls` is
  false. **Together**: `providerOptions.togetherai` honored alongside
  `together`.

---

## Behavior Changes

- **Outgoing requests now carry a `User-Agent` header**: every provider
  tags requests with `ai-sdk/<provider>/<version> runtime/go/<goVersion>`
  (e.g. `ai-sdk/openai/0.5.0 runtime/go/go1.25.1`), appended to any
  `User-Agent` you set. This matches the TypeScript AI SDK and replaces the
  previous behavior of sending no custom `User-Agent`. The non-streaming
  `pkg/ai` calls (`GenerateText`, `GenerateObject`, `GenerateImage`,
  `Embed` / `EmbedMany`, `Transcribe`, `GenerateSpeech`, `GenerateVideo`,
  `Batch`, `Evaluate`) also add an `ai/<version>` segment; `StreamText`,
  `StreamObject` and `Rerank` do not, matching TS. Wrapper providers can set
  the tagged provider name with the new `anthropic.Config.UserAgentName` /
  `openai.Config.UserAgentName`, or turn the Anthropic tag off with
  `anthropic.Config.NoUserAgentTag`.
- Consecutive tool messages are merged into one before every provider call,
  for all providers.
- `MCPClient.Connect` rejects a negative `MaxRetries` with `MCPClientError`
  instead of silently clamping it to 0.
- `DefaultSettingsMiddleware` no longer rebuilds `GenerateOptions` from a
  fixed field list (which dropped `ProviderOptions`, `Prompt.System`,
  `Reasoning`, runtime/tools context, and `Telemetry` on every call);
  `ProviderOptions` are now deep-merged and the caller's params win.
- `SimulateStreamingMiddleware` now emits provider-executed tool results;
  `Notify` listeners run concurrently.
- MCP HTTP transport: with the 2026-07-28 protocol, `mcp-session-id` is no
  longer sent or captured, matching TS; legacy-protocol behavior is
  unchanged. `Close()` now cancels in-flight requests and waits for them to
  exit. A response whose Content-Type is neither `application/json` nor
  `text/event-stream` (including a missing header) now fails instead of
  being accepted.
- Image/video polling (BFL, Fireworks, Google, Vertex, Replicate, fal) now
  enforces a wall-clock deadline instead of an attempt count; async video
  polling defaults are 5s interval / 10min timeout.
- KlingAI resolves credentials on each request instead of once in `New()`,
  so environment variable changes take effect without rebuilding the
  provider.
- Embed/EmbedMany/Rerank retry-exhaustion error text no longer starts with
  "embedding failed: " / "batch embedding failed: " / "reranking failed: ";
  use `errors.As`/`errors.Is` against the wrapped `RetryError` instead of
  matching text.
- Model `SupportedUrls` matching no longer treats non-wildcard media-type
  keys as prefixes (`image/png` no longer matches `image/png-foo`), which
  changes which file URLs get downloaded versus passed through.

---

## Security Fixes

- Tool approvals are verified on resume (HMAC v1, byte-compatible with TS).
- MCP OAuth state parameter comparison uses
  `crypto/subtle.ConstantTimeCompare` to prevent timing-based CSRF.
- Downloads: DNS pinning at dial time, a synced blocklist, bounded reads,
  and credential stripping for the rest of a redirect chain after any
  cross-origin hop.
- MCP OAuth discovery is now SSRF-guarded; policy-opa fails closed.
- Dependency bumps: `github.com/labstack/echo/v4` → v4.15.4 (fixes
  GHSA-vfp3-v2gw-7wfq / CVE-2026-55677 — encoded `%2F` bypassed
  route-level middleware and could disclose static files); `go-chi/chi` →
  v5.3.0; OpenTelemetry → v1.44.0; `google.golang.org/grpc` → v1.83.2
  (GO-2026-6348, GO-2026-6061, GO-2026-6443); `golang.org/x/text` → v0.41.0
  (GO-2026-5970); `github.com/quic-go/quic-go` → v0.59.1 (GO-2026-5676).
  `govulncheck` reports no reachable vulnerabilities.
- The Black Forest Labs poll URL, the OpenAI image-edit `url` file input,
  and the Anthropic batch `results_url` are now fetched through the
  SSRF-safe download path (DNS-pinned per redirect hop, credentials only
  sent to trusted origins, bounded reads).
- Alibaba video status polling now path-encodes the provider-returned task
  ID.

---

## New Features

### Core

- Stable lifecycle callbacks: `OnStart`, `OnStepStart`,
  `OnLanguageModelCallStart`/`End`, `OnToolExecutionStart`/`End`,
  `OnStepEnd`, `OnEnd`, `OnAbort`, `OnError` (deprecated experimental/finish
  aliases remain; stable names win).
- `RepairToolCall` (stable, available on `GenerateText`/`StreamText`/
  `ToolLoopAgent`/`WorkflowAgent`); `FingerprintTools` / `DetectToolDrift`;
  `LogWarnings` (now emitted by `GenerateText`/`StreamText`/
  `GenerateObject`/`StreamObject`); `InstructionMessages` plus
  `middleware.DefaultInstructionsMiddleware`.
- `PrepareStep` for per-step setting overrides — on agent settings, per
  call, and in `PrepareCall`.
- Stream retries: `StreamRetries`, `OnErrorRetry`,
  `provider.StreamProviderError`; `TimeoutConfig.FirstChunk`;
  `TimeoutConfig.PerChunk` now resets only on output.
- `ai.ToolSearch` and `types.Tool.DeferLoading` for native tool search with
  tools loaded on demand; `ExperimentalToolCallers` for tools called by
  local or provider callers.
- `ai.UIMessageStreamError` / `IsUIMessageStreamError`;
  `ai.LastAssistantMessageIsCompleteWithToolCalls` /
  `LastAssistantMessageIsCompleteWithApprovalResponses`;
  `ai.CreateIDGenerator` / `ai.GenerateID`;
  `ai.NewStreamTextResultFromParts`; `StreamTextResult.Files()`,
  `.Reasoning()`/`.ReasoningText()` (deprecated aliases for the final
  step's reasoning).
- `UIMessageStreamWriter.SetOutcome` / `UIMessageStreamOutcome`;
  `UIMessageStreamResponseInit.Header` (`http.Header`, for repeated
  headers like multiple `Set-Cookie`); SSE responses now flush after every
  event.
- `ai.UploadFile` / `ai.UploadSkill` for provider file/skill uploads
  returning `ProviderReference`.
- Files API v4: `ai.GetFileMetadata`, `ai.DownloadFile`, `ai.DeleteFile`,
  streamed uploads (OpenAI, xAI); Files capabilities survive
  `WrapProvider`.
- Batch API (experimental): `ai.ExperimentalStartBatch`,
  `ExperimentalGetBatchStatus`, `ExperimentalGetBatchResults`,
  `ExperimentalCancelBatch`, `ExperimentalListBatches` over
  `provider.BatchV4`/`BatchProvider` (Anthropic, OpenAI, Google, Gateway).
- Evaluation (experimental): `ai.ExperimentalEvaluate`,
  `provider.EvaluationModel`, `ai.NewEvaluationLanguageModel` (Anthropic,
  OpenAI, Google, Gateway, TypeSafe AI).
- Async video (experimental): `ai.ExperimentalStartVideo` /
  `ExperimentalGetVideoStatus` with polling or webhooks, over
  `provider.VideoModelStarter`/`VideoModelStatusChecker`/
  `VideoModelWebhookHandler` (fal, Google, Vertex, Replicate, xAI, Alibaba,
  ByteDance, KlingAI, BFL, MiniMax); new call options `FrameImages`,
  `InputReferences`, `GenerateAudio`.
- Streaming transcription/translation (experimental):
  `ai.ExperimentalStreamTranscribe` / `ExperimentalStreamTranslate` over
  `provider.TranscriptionStreamer`/`AudioStream` (Gateway, OpenAI, Google,
  ElevenLabs, Cartesia, xAI).
- Speech translation (experimental): Google `SpeechTranslationModel` (Live
  Translation over BidiGenerateContent WebSocket) and OpenAI
  `SpeechTranslationModel` (`/realtime/translations` WebSocket), via
  `Provider.SpeechTranslationModel()` / `Translation()`.
- Workflow serialization for every model kind a provider marks
  workflow-serializable (image, video, speech, transcription, embedding,
  evaluation), via `provider.Serialize*Model`/`Deserialize*Model`/
  `Register*ModelDeserializer`; `provider.Registered*DeserializerKeys()`.
- `errors.NoSuchModelError`; image model middleware (`WrapImageModel`,
  `ImageModelMiddleware`, `WithImageModelMiddleware`);
  `LanguageModelMiddleware.OverrideSupportedURLs`; registry options
  `WithSeparator`, `WithLanguageModelMiddleware`, `WithImageModelMiddleware`.
- `pkg/schema`: `$ref`-cycle guard (a cycle that never consumes input now
  errors instead of overflowing the stack); schema defaults applied before
  validation (a missing field with a `default` no longer fails), across
  MCP structured output, `ValidateUIMessages`, tool-approval revalidation,
  agent call options, harness tool `ContextSchema`, and
  `workflow.ValidateSerializableToolInput`.
- Streaming request bodies: `StreamText` steps now populate
  `Request.Body` for streaming calls on nearly every provider (the stream
  implements the new optional `provider.StreamRequestBody`), as they
  already did for `GenerateText`.
- Realtime capability interfaces: `DoCreateClientSecret` and
  `GetWebSocketConfig` moved off `Experimental_RealtimeModelV4` into the
  optional `provider.RealtimeClientSecretCreator` and
  `provider.RealtimeWebSocketConfigProvider`, as in TS, where both are
  optional. `ai.ConnectRealtime` and the providers' `GetRealtimeToken`
  check for them at runtime.

### Agents and workflow

- `AgentSession.SuspendTurn` detaches the local handle (TS parity);
  `WorkflowAgent` gains `MaxRetries`, `Timeout`, `ExperimentalDownload`, and
  TS `prepareCall` parity.
- `WorkflowAgent` and `ToolLoopAgent.GenerateAgent` honor deferred tool
  discovery (`ToolSearch`/`DeferLoading`) and `ExperimentalToolCallers`,
  applied before `OnStepStartEvent`.
- `pkg/workflow` harness helpers: `RunHarnessAgent` / `RunHarnessAgentStep`
  / `RunHarnessAgentTimeSlice`, `HarnessWorkflowState`, output persistence,
  `sandboxSession` forwarding.

### Harness

- `pkg/harness` (Go port of `@ai-sdk/harness`): `Agent`/`AgentSession` with
  sessions, host/builtin tool approvals, client-side tool pauses,
  continuations, `StopWhen` (suspending the turn so it can be resumed),
  active/inactive tools, caller cancellation as abort, structured output
  (`AgentSettings.Output`), `ExperimentalSteer`, and turn/step/tool
  telemetry through registered integrations.
- Adapters: `pkg/harness/claudecode`, `pkg/harness/codex`,
  `pkg/harness/opencode`, `pkg/harness/deepagents`, `pkg/harness/acp` (with
  process-loss recovery), and ACP-based Cursor, fx, GitHub Copilot, and
  Grok Build adapters, each with native subscription credential handling.
- `pkg/harness/sandbox/vercel` (port of `@ai-sdk/sandbox-vercel`) runs
  harness sandboxes on Vercel Sandbox microVMs, with network policies and
  template snapshots (`VERCEL_OIDC_TOKEN` or explicit
  `Token`/`TeamID`/`ProjectID`).
- `CreateHarnessSandboxTemplate`, `Agent.GetSandboxTemplate`, an
  OnBootstrap marker so `onBootstrap` doesn't re-run.
- `pkg/providerutils/websocket`, a shared helper used by every realtime and
  streaming-transcription connection.

### Code-mode (experimental)

- `pkg/codemode`: `RunCodeMode`, `CreateCodeModeTool`, `CodeModeTool` (a
  local tool caller) run model-written JavaScript in a QuickJS-on-
  WebAssembly sandbox (via `tetratelabs/wazero`, pure Go) under the TS
  execution-policy limits.
- Signed continuations (HMAC-SHA256, TS-compatible canonical envelope);
  approval mode `"interrupt"`; host interrupts
  (`RequestCodeModeInterrupt`, `ContinueCodeModeInterrupt`,
  `GetCodeModeInterrupt`, `IsCodeModeInterrupt`, `UnwrapCodeModeResult`);
  approval continuations (`ContinueCodeModeApproval`,
  `GetCodeModeApprovalResponse`, `IsCodeModeApprovalInterrupt`,
  `ToCodeModeApprovalMessages`); `SetCodeModeContinuationSigningKey`.
- It vendors a patched copy of fastschema/qjs v0.0.6
  (`pkg/internal/third_party/qjs`, MIT license) to fix two memory-read
  bugs.

### MCP

- Full OAuth `Auth()` flow (dynamic client registration, PKCE, token
  exchange/refresh, SSE 401 recovery, issuer pinning); 2026 protocol
  support (`server/discover`, `completion/complete`, `mcp-session-id`
  session lifecycle, `x-mcp-header` tool parameter headers); tool
  annotations; paginated list calls; `MaxRetries`; `MCPClient.
  InitializeResult()`; `MCPTransportSendOptions`; MCP Apps `_meta.ui`
  validation and fingerprinting.
- `MCPClient.OnElicitationRequest` handles server `elicitation/create`
  requests; `MCPClient.ListResourceTemplates` lists resource templates
  (`resources/templates/list`).
- A standing inbound SSE listener for legacy-protocol servers (Last-Event-Id
  resume, backoff reconnects, OAuth refresh on 401,
  `HTTPTransportConfig.OnError` diagnostics); every MCP HTTP transport
  request now sends a `User-Agent` header.

### Telemetry

- `telemetry.NewOpenTelemetry`: a GenAI-semantic-convention integration
  (`gen_ai.*` spans/attributes, opt-in AI SDK supplemental attributes).
  Running it alongside `LegacyOpenTelemetry` closes every span of both
  integrations correctly (sibling roots).
- `GenerateObject`/`StreamObject` now emit telemetry spans; step spans
  carry `ai.prompt.messages`/`ai.prompt.tools`/`ai.prompt.toolChoice`;
  streaming spans record `ai.stream.firstChunk`/`ai.stream.finish` events.
  `LegacyOpenTelemetry` root spans carry `ai.model.provider`,
  `ai.model.id`, `ai.settings.*`, `ai.request.headers.*`,
  `operation.name`, `resource.name`; step spans carry `gen_ai.request.*`
  settings.
  `ai.ExperimentalEvaluate` telemetry now goes through registered
  integrations like every other call.

### Schema, embeddings, media

- Signature-based media sniffing (AVIF/HEIC/ADTS AAC, no more false
  `image/bmp`); validated-redirect polling for async results;
  `SerializationError`.
- `Together AI`: `RerankingModel` (`POST /v1/rerank`).

### Providers

- `openai.Config.TransformRequestBody`: rewrite the Chat Completions
  request body before it is sent (TS `transformRequestBody`), for
  OpenAI-compatible wrapper providers. Cerebras uses it, so the reported
  request body is the transformed one. Groq model ID constants
  (`pkg/providers/groq`, chat and transcription).
- **New providers**: Voyage AI (embedding/rerank), Fish Audio
  (speech/transcription), Cartesia (speech/transcription, plus Ink 2
  realtime transcription), Rev.ai (transcription), Hume (speech), Luma
  (async image), GMI Cloud, Z.AI, MiniMax (chat + async video), TypeSafe AI
  (evaluation), QuiverAI (Arrow 2 / Arrow 2 Telos, built on OpenResponses).
- **anthropicaws** ("Claude Platform on AWS" — a distinct provider from
  Bedrock-Anthropic).
- **Anthropic**: request-level `Compaction` model option (on-demand
  conversation summarization, mutually exclusive with `ContextManagement`);
  `ForwardContainerIDFromLastStep`; citations surfaced as source parts
  (including web_fetch-sourced documents); `container_upload` custom
  content; `mcp_tool_result` carries paired tool name, dynamic flag, and
  metadata; every `providerOptions.anthropic` field is honored per call and
  merged over construction-time `ModelOptions`; Anthropic-based providers
  also honor their own provider-name key (`providerOptions.minimax`,
  `providerOptions.bedrock`, ...).
- **OpenAI Responses**: computer tool (`computer_call`), async and
  programmatic tool calling, GPT-6 reasoning config, GPT-5.6
  `reasoningMode`/`reasoningContext`, prompt cache options, JSON schema
  normalization, `serviceTier` on finish, citations/annotations as source
  parts, per-part reasoning summary boundaries, progressive tool input for
  custom/apply_patch tools, MCP approvals answered in earlier turns,
  allowed-tools canonical aliases. `provider.StreamChunk.RawFinishReason`
  now carries the provider's raw finish reason on every provider.
- **xAI**: Responses API parity across video/image/speech; experimental
  batch (text + images); Files v4; streaming speech-to-text;
  `referenceVoiceIds` (max 3 entries); keyframes, pinned last frame,
  `generateAudio`, image/audio input references (R2V) for video;
  `minP`/`maxTurns`/`parallelToolCalls`/`promptCacheKey`/`safetyIdentifier`
  on Responses.
- **Google/Vertex**: `googlevertex.Provider.Interactions` /
  `InteractionsAgent` / `InteractionsManagedAgent` (including managed
  agents); Chirp 3 HD text-to-speech on Vertex; Gemini 3.5 Transcribe
  (unary `google.Provider.TranscriptionModel`, `-live` models over the
  Vertex Live WebSocket); `googlevertex/xai` sub-provider for Grok models
  on Vertex (Model-as-a-Service); Google Vertex video (Veo); realtime
  lifecycle events, `thinkingConfig`, `functionResponse`; server-executed
  built-in tool call/result parts; per-chunk function-call argument
  streaming.
- **Bedrock**: portable reasoning sent only to models with known support
  (Anthropic, OpenAI, Nova 2 Lite; others get a warning); Mistral
  tool-call IDs hashed (FNV-1a → base62) to avoid collisions; forced
  Anthropic tool choice matches provider-defined tools by wire name;
  `ProviderError` carries response headers/body; self-serializing
  Anthropic tools (computer, computer_toolset_20260801, text_editor_
  20250728) forwarded as Converse toolSpecs.
- **Gateway**: typed conditional evaluation fallbacks
  (`GatewayProviderOptions.Models []GatewayModelFallback`, built with
  `GatewayModel(id)` / `GatewayConditionalModelFallback(id, when)`);
  Browserbase search/fetch tools; `structured-output` `has` capability
  (`GatewayHasStructuredOutput`); async video and webhook passthrough;
  streaming transcription (`ai-gateway-transcription.v1` WebSocket);
  realtime model support for `ai.ConnectRealtime`.
- **Cohere**: tool calling now works end-to-end (`tools`/`tool_choice`/
  `TopP`/`TopK`/penalties/`Seed`/`StopSequences`/JSON `response_format`);
  citations become document source parts; `STOP_SEQUENCE` maps to `stop`;
  `providerOptions.cohere.thinking` takes precedence over `Reasoning`.
- **Smaller providers**: Alibaba (JSON-schema gating, preserved thinking,
  wan2.7/wan3 video protocols), Cerebras/Baseten/DeepInfra/Together
  (OpenAI-compatible wrapper parity), DeepSeek (V4 vision, Files API
  references, reasoning, logprobs), Moonshot (own chat implementation with
  reasoning/tool-schema parity), Mistral (reasoning-preserving messages,
  Voxtral), Fireworks/Groq (reasoning gating, Groq `TranscriptionModel`),
  batch speech/transcription parity for AssemblyAI/Deepgram/ElevenLabs
  (`TranscriptionModel`)/Gladia, BFL (FLUX 3 video model), OpenAI-compatible
  video (`video/*` parts as `video_url` for Fireworks, gmicloud, Z.ai,
  Together, Baseten, Cerebras, DeepInfra via `openai.Config.AllowVideo`).
- **fal**: `SpeechModel` and `TranscriptionModel`, `Config.Headers`, API key
  fallback `FAL_API_KEY` then `FAL_KEY`.
- **LlamaIndex**: `pkg/llamaindex.ToUIMessageStream` adapts a LlamaIndex
  chat-engine delta stream to UI message stream chunks
  (`OnStart`/`OnToken`/`OnText`/`OnFinal` callbacks).
- **LangChain**: lifecycle callback options and stream-adapter API parity
  fixes; `langchain.LangSmithDeploymentTransport` implements `ai.
  ChatTransport`.
- **TUI**: `RunAgentTUI` accepts an `ai.ChatTransport`; backspace deletes
  whole grapheme clusters.

---

## Bug Fixes

### Core and streaming

- `schemaToMap` handles an empty item schema; `ShellSandbox` output race
  fixed; duplicate text/reasoning IDs are remapped across steps;
  `PipeTextStreamToWriter` flushes per chunk and returns write errors; the
  provider `TextStream` is closed on a mid-stream error;
  `ExperimentalTransform` chaining now applies every transform to every
  chunk; the internal HTTP client keeps the real status on body-read
  errors.
- `StreamText` without callbacks now executes tools and later steps;
  `Timeout.Total` no longer cancels its own first request right after
  starting; step-start telemetry now fires before each step's model call so
  spans nest correctly (the OpenTelemetry root-span fallback is removed).
- A failed embed/embedMany/rerank retry attempt now ends its model-call
  span with error status instead of leaking it
  (`EmbeddingModelCallEndEvent`/`RerankingModelCallEndEvent` gained an
  `Error` field; custom integrations receive `OnEmbedEnd`/`OnRerankEnd` for
  failed attempts too).
- Telemetry: step and model-call spans now always end on a provider error
  (with error status) or an abort (without) in `GenerateText`,
  `StreamText`, and `GenerateObject`/`StreamObject` — they used to leak.
- Text parts keep their `providerMetadata`; each `text-start` begins a new
  text part.

### Anthropic

- Multi-step tool use (tool_use/tool_result); `result.Text` now joins all
  text blocks (previously kept only the first); live streaming emits
  web_search/web_fetch tool results and sources (previously dropped);
  mid-stream `error` events are surfaced instead of silently ignored;
  finish-reason mapping corrected (`pause_turn`→`stop`, `refusal`→
  `content-filter`, `model_context_window_exceeded`→`length`); initial
  effort-only system messages are kept inline instead of dropped.

### OpenAI / Responses / Azure

- Structured-output `response_format` now sends `json_schema`/`json_object`
  correctly (OpenAI chat, Groq, Mistral, Fireworks, Together, and
  inheritors).
- Previously-dropped Responses output items — `image_generation_call`,
  `mcp_call`/`mcp_list_tools`/`mcp_approval_request`, `file_search_call`,
  `code_interpreter_call`, `tool_search_call` — are now decoded in generate
  and stream (including batch results).
- `response.failed` emits an error chunk before finish; function tools
  always send `strict` (defaulting to false); non-streaming tool results no
  longer incorrectly set `providerExecuted`; realtime WebSocket bearer-token
  parsing is now case-insensitive with flexible whitespace (affects realtime
  transcription too).

### Bedrock

- Rerank request key fixed; forced tool choice matches provider-defined
  Anthropic tools by wire name; `modelStreamErrorException` (424) marked
  retryable.

### Providers (general)

- Cerebras and Vercel call Chat Completions, not Responses. Moonshot
  accepts any model ID. DeepSeek resolves provider file references and
  validates response fields, and drops temperature/topP with a warning
  when thinking is enabled. Groq parses its error envelope and keeps the
  HTTP status. Fireworks fixes error-envelope union types, status code, and
  `reasoning: none`. Replicate returns `InvalidResponseDataError` for
  failed/canceled predictions. Mistral fixes usage shape, streaming
  tool-input chunks, and speech telemetry redaction. Deepgram array query
  options are comma-joined. Gladia poll errors keep the response body and
  headers.
- Cohere: `Usage.Raw` is now the complete raw usage object.
- ByteDance/Alibaba video: default poll interval/timeout corrected to
  match TS (5s/600s); Alibaba `VideoModel()` no longer rejects model IDs
  outside a fixed allowlist.
- OpenAI-compatible providers: tool results of type `content` are now sent
  as the JSON-stringified content-block array (previously only the first
  text block); streamed responses now report token usage (the trailing
  `stream_options.include_usage` chunk was being dropped).
- Google realtime and Gemini 3.5 Transcribe: model IDs that already
  contain `/` (e.g. `tunedModels/x`) are used as-is instead of being
  prefixed with `models/`. Google/Vertex: unsupported tool-result file URL
  schemes now fail with `DownloadError` instead of passing through; `gs://`
  URLs are still forwarded as `fileData` on Vertex Gemini 3+.
  `SupportedUrls` matching no longer treats non-wildcard media types as
  prefixes.
- ElevenLabs and Deepgram read `ELEVENLABS_API_KEY`/`DEEPGRAM_API_KEY` when
  no API key is passed.
- Async video: Google video now calls `:predictLongRunning` (was a
  nonexistent `:generateVideo` endpoint); Replicate model IDs without a
  version now go to `/models/{id}/predictions`.
- `RawFinishReason` is now set by every provider in both generate and
  stream (previously effectively always empty for non-streaming calls); xAI
  Responses streaming reports `tool-calls` when a function call completes,
  and maps `response.failed` to `error`.

### MCP

- HTTP transport: response bodies are closed on 202 and notification
  sends (fixed a connection leak); `Close()` cancels in-flight requests and
  streamed responses and waits for them to exit; a response with an
  unexpected Content-Type now errors instead of being silently accepted; a
  `text/event-stream` response (or a legacy GET SSE 2xx) with no body now
  errors instead of hanging; `Connect` rejects a negative `MaxRetries`.
- `StdioTransportConfig.Env` and `WorkingDir` are now actually applied to
  the child process (previously ignored).

### Realtime / WebSocket

- A dropped connection or a failed audio send now fails the stream instead
  of finishing silently, across the Gateway, OpenAI, Google, ElevenLabs,
  Cartesia, and xAI realtime/transcription paths. The Gateway realtime
  model now implements the realtime interface for `ai.ConnectRealtime`; the
  Gateway team subprotocol is taken from per-call headers; the WebSocket
  `Origin` header is derived from the target URL instead of
  `http://localhost/`.

### Harness

- Codex app-server protocol migration; host-tool input validation before
  approval and execution (invalid input settles as a tool error without
  running the tool), and revalidation when an approved call resumes;
  host tools execute with unstripped (absolute-path) input; a turn is
  released before its stream reports finish (fixes "already has a turn in
  progress" on a follow-up turn); GitHub Copilot's `gh auth token` lookup
  now times out after 10s; Grok Build picks the first matching OAuth
  record in file order (deterministic); Claude Code and Codex adapters
  report bridge diagnostics through `Observability.Report`; the ACP
  adapter forwards MCP-routed tool calls with `Dynamic: true` instead of
  dropping them; `StopWhen` looks ahead one event before suspending.

### Other

- JSON Schema: a `$ref` cycle that never consumes input now returns a
  validation error instead of overflowing the stack; defaults are applied
  before validation.
- Error messages no longer start with a stray "L" (a lint-cleanup
  regression from v0.4.0, ~88 strings); transport failures now read
  `Cannot connect to API: <cause>` like TS.

---

## Deprecations

- `ExperimentalContext` → `RuntimeContext`/`ToolsContext`.
- `NeedsApproval` (per-tool) → call-level `ToolApproval`.
- `ExperimentalFilterActiveTools` → `FilterActiveTools`.
- `MaxSteps` → `StopWhen` (still works as a compatibility shorthand).
- `ExperimentalTelemetry` → `Telemetry`; `telemetry.Settings` remains as an
  alias for `telemetry.Options`.
- `OTelTelemetryIntegration` → `LegacyOpenTelemetry`.
- `ToGoogleMessages` → the new Google/Vertex message converter.
- `StreamTextResult.FullStream()` remains as a deprecated alias for
  `Stream()`.
- Tool-execution event fields `StepNumber`, `ModelProvider`, `ModelID`,
  `Args`, `Result`, `Error`, `DurationMs` are deprecated (excluded from
  JSON; use the documented replacement fields).

---

## Not Ported (TS-only)

- OpenAI Live over WebRTC (browser transport) — the Go SDK uses the server
  WebSocket instead.
- `@ai-sdk/harness-cline` and `@ai-sdk/harness-pi` — these run vendor Node
  SDKs in-process; use the TS SDK for them.
- DeepSeek batch API and extra Files methods — TS `@ai-sdk/deepseek` has
  neither; existing Go file support already matches TS.
- Durable webhook suspension for async video in workflows — it depends on
  the Node-only Vercel Workflow DevKit runtime. Go polls until the job is
  done.

---

## Known Differences

- `StreamObject` (deprecated in both SDKs) returns after the stream is
  fully consumed and reports progress through `OnChunk`, while TS returns
  lazy streams immediately. Use `StreamText` with `Output`
  (`PartialOutput`/`ElementStream`) for incremental partial objects — the
  replacement TS also recommends.
- Vercel Sandbox harness provider (`pkg/harness/sandbox/vercel`) has no
  `@vercel/oidc` token-refresh loop; `VERCEL_OIDC_TOKEN` is re-read on each
  call (explicit `Token`/`TeamID`/`ProjectID` also work).
- Code-mode: approvals requested concurrently inside one `Promise.all`-style
  batch come back as a chain of single interrupts rather than one batch,
  because the embedded QuickJS build exposes no job-queue quiescence hook.
- See `docs/08-migration-guides/known-differences.mdx` for the full,
  maintained list, including Go-specific notes on Bedrock's lazy `fetch`
  resolution having no direct runtime equivalent.

---

## Dependency Floors

From `go.mod`:

| Module | Version |
|---|---|
| `github.com/labstack/echo/v4` | v4.15.4 |
| `github.com/go-chi/chi/v5` | v5.3.0 |
| `github.com/gofiber/fiber/v2` | v2.52.13 |
| `go.opentelemetry.io/otel` (+ `sdk`, `trace`, `otlptrace`, `otlptracehttp`) | v1.44.0 |
| `google.golang.org/grpc` | v1.83.2 |
| `github.com/quic-go/quic-go` | v0.59.1 |
| `github.com/tetratelabs/wazero` | v1.9.0 |
| `golang.org/x/net` | v0.58.0 |
| `golang.org/x/sys` | v0.47.0 |
| `golang.org/x/text` | v0.41.0 |
| `golang.org/x/crypto` | v0.55.0 |
| `golang.org/x/oauth2` | v0.36.0 |
| `golang.org/x/time` | v0.15.0 |

`pkg/internal/third_party/qjs` vendors a patched copy of fastschema/qjs
v0.0.6 (MIT license), fixing two memory-read bugs, compiled to
`qjs.wasm` and run under `wazero`. It is not a Go module dependency; it
ships as source + WASM inside the repository.

## Requirements

- **Go 1.25 or later** (`go.mod` declares `go 1.25.0`).

---

**TS SDK parity target:** `ai@7.0.118`
**Base tag:** `v0.4.0` (2026-03-29)
**Scope:** `v0.4.0..HEAD`, ~1,000 commits across the May, June, and
September 2026 parity cycles.
