# Changelog

All notable changes to the Go AI SDK will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - Unreleased

TS parity catch-up from `ai@7.0.127` to `ai@7.0.153`, built on v0.5.3 (all
v0.5.3 fixes are included). See
[`release_notes/RELEASE_NOTES_V0.6.0.md`](./release_notes/RELEASE_NOTES_V0.6.0.md)
and the [v0.5 to v0.6 migration guide](./docs/08-migration-guides/from-v0.5-to-v0.6.mdx).

### Added

- Experimental steering: `ai.NewSteering()` and `ExperimentalSteering` on
  `GenerateText`, `StreamText` and the agent inject messages between steps
  (`StepResult.ExperimentalSteeringMessages`).
- `OnStepError` on `GenerateText`, `StreamText` and the agent; agent
  `ErrorHandler` and `AbortHandler`.
- `ai.ExperimentalStreamSpeech` with the optional `provider.SpeechStreamer`
  interface, implemented by the AI Gateway and ElevenLabs over WebSocket.
- `pkg/toolbash` (bash tool over a sandbox session) and `pkg/toolwebfetch`
  (web fetch with SSRF checks on every redirect, DNS pinning and
  HTML-to-Markdown).
- MCP events: `MCPClientConfig.ExperimentalEvents`,
  `(*MCPClient).ExperimentalEvents()` / `ExperimentalManagedEvents()` and
  `ExperimentalCreateMCPEventWebhook` (Standard Webhooks v1 signatures). MCP
  per-request timeouts (`mcp.WithRequestOptions`) and
  `InitializationOptions`.
- fx: opt-in libfx runtime, `fx.Settings{Runtime: fx.RuntimeLibfx}`, with AI
  Gateway or Codex/Grok subscription auth and HTTP/SSE MCP servers. The
  sandbox `fx acp` runtime stays the default.
- Decide: `box` questions, `MaxOutputTokens` and `Reasoning`,
  `QuestionOrder` / `CriteriaOrder`, and a Perplexity decision model.
- HeyGen video provider (`pkg/providers/heygen`).
- Models: Claude Haiku 5.5, `gemini-4-argon` (long-decode continuation),
  `gemini-nano-banana-2.1`, MiniMax M3.1, `kimi-k3.1-pro`, Mistral Voxtral 3
  and GLM 5.3 reasoning, `grok-imagine-image-2.0`, BFL flux-3, Azure
  MAI-Image; regenerated AI Gateway catalog.
- `ai.StreamTextLowMemory`, `ToolCallConcurrency`,
  `types.FinishReasonInterrupted`, `ToolResultContent.IsError`,
  `StreamTextResult.NewStream()`, `codemode.ResetMaxWorkers()`.
- Image prompt input for the Claude Code, Codex, OpenCode, Deep Agents, ACP
  and GitHub Copilot harness adapters.
- 32-bit builds (linux/386, linux/arm), checked in CI.

### Changed

- Tools in a step run concurrently by default, as TS `Promise.all`. Set
  `ToolCallConcurrency: 1` for sequential execution.
- Errors are returned as is, without Go-added prefixes; match them with
  `errors.As` / `errors.Is`.
- `StreamTextResult.Text()` is the final step's text; `OnFinishEvent`
  aggregates usage, tool calls and results across steps.
- `Usage.TotalTokens` is input + output tokens for every provider.
- Provider model IDs follow TS `config.provider` (`groq.chat`,
  `openai.image`, ...). Option and metadata keys are unchanged.
- Responses, stream chunks and provider options are validated against the
  TS schemas; mismatches fail with "Invalid JSON response" or
  `InvalidArgumentError`.
- Experimental evaluate is renamed to decide (old names are deprecated
  aliases); wire and telemetry names changed to `ai.decide*` /
  `ai.decision.*`.
- `WorkflowAgent.Stream` has no default step limit; MCP clients have no
  default request timeout.
- An OPA HTTP policy response without `result` is not-applicable instead of
  denied.
- Generated response, approval and UI message IDs use the TS formats
  (`aitxt-`/`aiobj-` plus 24 characters, or 16 for UI messages) instead of
  `call-` plus 24; approval IDs come from `InternalOptions.GenerateID`.
- Code mode runs on wazero v1.12.0 and compiles QuickJS once per process
  (faster startup).

### Breaking

- `GenerateObjectOptions.MaxRetries` and `StreamObjectOptions.MaxRetries`
  are `*int`; an explicit 0 disables retries.
- `telemetry.Options.RecordInputs` / `RecordOutputs` are `*bool`, and unset
  now records prompts and outputs. Set both to `telemetry.Bool(false)` to
  keep them out of spans.
- Evaluate end-event struct literals use the Decide embedded field names;
  unkeyed `acp.TextContentBlock` literals no longer compile.
- Prompts with neither or both of `Prompt` and `Messages`, or an empty
  `Messages`, fail with `*errors.InvalidPromptError`.
- `GenerateObject` / `StreamObject` reject system messages in `Messages`
  unless `AllowSystemMessages` is set; use `Instructions`.
- ACP lifecycle state persisted by v0.5.x does not resume.
- KlingAI reports an unknown model ID from `DoStart`, not `VideoModel()`.
- Code mode: default 10 s CPU limit (`CPUTimeoutMs`) and a memory-based
  concurrent-script cap (at most 32, `*codemode.ConcurrencyError`).
- OpenAI Responses `promptCacheOptions` is validated.
- Failed generated-media downloads in BFL, Luma and fal return
  `*providererrors.ProviderError`.
- Wrapper providers (Baseten, Cerebras, DeepInfra, Fireworks, Together,
  Vercel, Vertex MaaS, Azure delegates) read only their own API key.
- A failed harness `DoSuspendTurn` after a `StopWhen` condition fails the
  turn.
- `StreamText` exposes the provider request body only with
  `Include.RequestBody`; the `GenerateText` step-end event bodies follow
  `Include`.
- MCP OAuth rejects pre-registered confidential clients without an
  `Issuer`; `ToolChoice: none` is enforced with
  `*ai.ToolChoiceViolationError`; truncated streams finish with
  `interrupted`.

### Fixed

- AI Gateway language model speaks the V4 wire protocol.
- Tool parameters built with `schema.NewSimpleJSONSchema` reached providers
  as `{}`.
- Bedrock Anthropic (InvokeModel) streaming returned no content.
- Anthropic `contextManagement` edits are sent with snake_case names, and
  function tool schemas are sent untouched.
- `ToUIMessageStream` no longer blocks after a failed callback, and a
  panicking callback fails the stream instead of crashing the process.
- MCP `HTTPTransport` works after `Close` then `Connect`.
- Code mode counts real sandbox thread CPU time on Linux, macOS and Windows.
- fx on libfx rejects `MemoryLimitBytes` above 4 GiB instead of crashing.
- Stability no longer panics on nil options; Bedrock picks up rotated AWS
  credentials without a restart.
- The SDK reports version 0.6.0 in `User-Agent` and telemetry.

### Security

- Downloads: `.internal` hostnames are blocked, and hostnames are mapped
  through IDNA before the SSRF blocklist, so fullwidth forms cannot bypass
  it.
- Fireworks, Together, the Azure delegates and Azure DeepSeek no longer fall
  back to `OPENAI_API_KEY` / `DEEPSEEK_API_KEY` (the other wrappers were
  fixed in v0.5.3).
- Code mode: sandboxed scripts no longer see Go stack traces or host source
  paths; a panicking host tool is a tool failure.
- Harness JSON parsing rejects `__proto__` and `constructor.prototype` keys;
  the local sandbox gains a `HostEnv` allowlist and owner-only session
  directories.
- Response-derived IDs (ByteDance, KlingAI, fal, Luma, Rev.ai) are encoded
  as single path segments; BFL and OpenAI-compatible image downloads send
  credentials only to same-origin URLs.
- Also includes every v0.5.3 security fix.

## [0.5.3] - 2026-10-27

Patch release: bug and security fixes backported from the v0.6.0 line.

### Security

- Replicate: the video status poll no longer sends the API token to the
  response-derived `urls.get` unless it is same-origin with the configured base
  URL. fal: the video status poll sends the provider `Authorization` header
  only to the same-origin queue URL.
- Harness: ACP (and Fx) authentication files are rejected unless the path is
  relative without traversal, and are restricted to the owner (`chmod 600`)
  after writing. They were written world-readable.
- Agents: the legacy `ToolLoopAgent` loop (`Execute`, `GenerateAgent`) fails
  closed on an unknown approval status or an unsupported `ToolApproval` value
  (a tool error) instead of running the tool unapproved.
- Code mode: the TypeScript stripper, which runs before the sandbox and outside
  its timeout, is bounded on deep nesting, long `a<b,` lists and unterminated
  regex candidates (a 256 KiB snippet took up to 22 s). Sources nested deeper
  than 256 brackets run unstripped.
- Gladia: the `result_url` poll sends the API key and caller headers only when
  the URL is same-origin with the configured base URL.
- Downloads: the SSRF hostname blocklist strips trailing dots, so `localhost.`
  and `x.local.` are blocked.
- MCP OAuth: `javascript:`, `data:` and `vbscript:` URLs are rejected in
  authorization server and protected resource metadata, token responses and
  client registration responses.
- MCP OAuth: the protected resource check compares normalized paths, so
  `https://host/api/../admin` is no longer accepted under `https://host/api`.
- Cerebras, DeepInfra, Baseten, Vercel and Vertex MaaS no longer fall back to
  `OPENAI_API_KEY`, which sent the OpenAI key to another service. They read
  `CEREBRAS_API_KEY`, `DEEPINFRA_API_KEY`, `BASETEN_API_KEY` and
  `VERCEL_API_KEY` (new `openai.Config.APIKeyEnvVar` and `NoAPIKey`).
- Multipart uploads (OpenAI/xAI files, OpenAI batch): CR/LF in a part name,
  file name or media type no longer injects part header lines (low).
- Vertex Gemini Live transcription dials the host derived from the location,
  never `BaseURL`, and rejects a location that is not a single DNS label before
  sending the bearer token (low).
- Bedrock event stream: a declared frame length over 16 MiB is rejected instead
  of being buffered.

### Fixed

- The SDK version reported in `User-Agent` headers (`ai/<version>`,
  `ai-sdk/<version>`, `ai-sdk-<provider>/<version>`) said `0.5.0` in
  v0.5.1 and v0.5.2. It now reports `0.5.3`.
- `GenerateObject` and `StreamObject` sent responseFormat type `json_schema` /
  `json_object` instead of `json` (as the TS SDK does). Providers that key JSON
  mode off `json` (Gemini, Cohere, Alibaba, Bedrock, Perplexity, Hugging Face,
  xAI and OpenAI Responses, Open Responses) lost JSON mode or the schema. The
  response schema is now the JSON Schema map. Azure and Ollama map `json` to
  `json_schema` / `json_object` on the wire.
- Tools whose `Parameters` is a `schema.Schema` (such as
  `schema.NewSimpleJSONSchema(...)`) reached providers as `{}`. The schema is
  now resolved to its JSON Schema map in `GenerateText`, `StreamText`, batch
  and agents (new `toolutil.ResolveToolSchemas`).
- Bedrock Anthropic (InvokeModel) streaming returned no content: events are
  now dispatched on the payload `type`, the `[DONE]` sentinel is skipped, and
  modeled exceptions carry their status code and retryability.
- Streams were cut off by the default HTTP client's 60s whole-request timeout,
  and so were slow non-streaming calls. The default client has no whole-request
  timeout (bound a call with its context) and a 5 minute response header
  timeout.
- Retries: `GenerateText`, `StreamText`, agents and `GenerateObject` retried
  only AI Gateway models. They now retry every provider (default 2 retries),
  honour `ProviderError.Retryable` and retry connection failures. 408, 409,
  429 and 5xx are retried; 400 and 401 are not. Set `MaxRetries` to 0 to
  disable. `NewProviderError` now takes the status code, headers and message
  from an HTTP error in the cause chain when none is given.
- OpenAI chat streaming never returned usage. It now requests
  `stream_options.include_usage` (as TS does for OpenAI, Azure, Baseten and
  Bedrock Mantle), holds the finish chunk until usage arrives and returns it.
  The OpenAI-compatible wrappers (Cerebras, DeepInfra, Vercel, Vertex MaaS)
  do not send it, matching TS. A text delta carrying `finish_reason` no longer
  drops the finish. New `openai.Config.OmitStreamUsage` controls this.
- A call to an unknown tool no longer stops `GenerateText` / `StreamText`; it is
  answered with a tool error and the loop continues.
- Bedrock: SigV4 signed the decoded path, so model IDs containing `:` (for
  example embedding and image models) were rejected. The path is signed as sent.
- ACP: mapping and authentication types (`ModelMapping`, `InstructionMapping`,
  `OutputSchemaMapping`, `PermissionModeMapping`, `Authentication`) were
  serialized with Go field names, so they never took effect in the bridge, and
  empty lists were encoded as `null`.
- Anthropic: `Generate` put text before thinking in `Content`, so the next turn
  was rejected with thinking enabled. Content keeps the order the model emitted.
- Gemini: a `functionCall` without an id used the tool name as the id, so
  parallel calls to one function collided. A generated id is used.
- OpenAI-compatible chat: file parts carrying only `FileData` produced empty
  image URLs.
- Alibaba and Moonshot: non-streaming tool-call arguments sent as a JSON string
  were dropped.
- Cohere: embed was sent to `/v1/v1/embed` and rerank to `/v1/rerank`. Chat,
  embed and rerank use the v2 API under the base URL (default
  `https://api.cohere.com/v2`, was `https://api.cohere.ai/v1`). A custom
  `BaseURL` without a version segment (a host or a proxy path) still gets `/v2`
  appended, as chat did before; one ending in `/v1` or `/v2` is used as is.
  Embed now sends `embedding_types: ["float"]`, which v2 requires.
- `DoEmbed` panicked on a 2xx response with no embeddings (Cohere, Fireworks,
  Mistral, Ollama, Together, Voyage). It returns an error.
- ACP: `SubmitToolResult` returns a malformed `askUserQuestions` result as an
  error instead of panicking in the caller's goroutine.
- ACP: a malformed `askUserQuestions` result submitted before its question
  arrived settles the turn with an error instead of crashing the process.
- ACP: a turn that settled early no longer leaves the session marked in flight
  after the bridge finishes it, so later prompts are accepted.
- Replicate language model: nil options no longer panic, and a non-2xx poll
  response is a `ProviderError` instead of a 2 minute spin before "timed out".
- `StreamTextResult.Chunks` and `ElementStream` / `ElementStreamWithOutput`:
  the producing goroutine exits when the result is closed, so a consumer that
  stops reading no longer leaks it.
- Google and Vertex Interactions: the background stream fails after 3
  connections in a row that produce no event instead of reconnecting forever.
- `StreamText` with `Timeout.PerStep`: a stream-open error stays that error
  instead of becoming a step timeout and abort, and a later step's open is
  retried like the first.
- `PrepareStep` `ActiveTools` was ignored; it now restricts the step's tools.
  In `GenerateText` the per-step timeout also covers tool execution.
- `WorkflowAgent.Generate` / `Stream` dropped the positional prompt when the
  options struct had an empty `Prompt`.
- OpenAI Responses: a denied provider-executed (MCP) approval sent a
  `function_call_output` as well as the `mcp_approval_response`.
- Code mode stripper: `!!x` lost one `!`, and after `a++ / 2` the rest of the
  line was read as a regex, so later annotations stayed in.
- Bedrock Mantle: SigV4 requests are signed with the `bedrock-mantle` service
  (new `bedrock.NewAWSSignerForService`).

## [0.5.2] - 2026-10-04

### Fixed

- OpenAI Responses (the default `openai.LanguageModel`): a streamed step
  that ends with a function call reports finish reason `tool-calls`. It
  reported `stop`, because the tool-call flag was never passed to the
  mapping. Both paths now follow TS `mapOpenAIResponseFinishReason`: a
  length or content-filter stop wins over tool calls, provider-executed
  tools such as web search don't count as tool calls, and an unknown
  reason is `other`.
- xAI Responses: the finish-reason mapping matches TS
  `mapXaiResponsesFinishReason`. An empty or unknown status, or an
  incomplete response without a reason, is `other` (it was `stop` or
  `length`), and the statuses `max_output_tokens` and `function_call` are
  recognized.

## [0.5.1] - 2026-10-04

Fixes found while building the [Shipyard demo](https://github.com/digitallysavvy/go-ai-shipyard),
a `useChat` frontend on a Go backend, plus a documentation overhaul for
human and agent developers.

### Added

- `ai.PipeUIMessageChunksToResponse` and `ai.CreateUIMessageChunksResponse`
  serve any UI message chunk stream over HTTP, such as one built with
  `CreateUIMessageStreamWithOptions` that writes data parts and merges an
  agent stream (TS `pipeUIMessageStreamToResponse({ stream })` /
  `createUIMessageStreamResponse({ stream })`).
- `agent.PipeAgentUIStreamFromUIMessagesToResponse` and
  `agent.CreateAgentUIStreamResponseFromUIMessages` take `useChat`'s UI
  messages directly (TS `pipeAgentUIStreamToResponse` /
  `createAgentUIStreamResponse` with `uiMessages`).
- `ai.UIMessageStreamHeaders()` (TS `UI_MESSAGE_STREAM_HEADERS`).

- `cmd/goai-docs-mcp`: an MCP server over stdio that lets coding agents
  search and read the docs (`search_docs`, `read_doc`, `list_docs`). It
  embeds the docs, so it works offline:
  `go install github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp@latest`.
- `skills/go-ai`: an agent skill for projects that use the SDK.
- Runnable `Example` functions for `GenerateText`, `StreamText`, structured
  output, tools, `ToolLoopAgent`, `PipeUIMessageStreamToResponse` and the MCP
  client, plus package overviews for the core and provider packages, on
  pkg.go.dev.

### Changed

- On an `http.ResponseWriter`, `PipeUIMessageStreamToResponse`,
  `PipeUIMessageChunksToResponse`, `PipeTextStreamToResponse` and the agent
  `Pipe*` helpers now set the response headers and write the status before
  the body, like their TS counterparts on a Node `ServerResponse`. Before,
  they wrote only the body and the caller had to set the headers. Remove any
  manual header setup or `WriteHeader` call made before these helpers.
  Other `io.Writer`s still get only the body. New
  `PipeTextStreamToResponseWithInit` takes a status and headers.

### Security

- Tool approval fails closed. A `ToolApproval` (or `NeedsApproval`) set to
  a bare function literal, such as
  `func(ctx context.Context, input map[string]interface{}, opts types.ToolNeedsApprovalOptions) bool`,
  matched no case and the tool ran without approval. Unnamed literals with
  the approval function signatures are now treated as their named types,
  and any other unrecognized value, or an unknown status string such as
  `"user_approval"`, is an error: the call is reported to the model as a
  tool error and the tool does not run. This applies to tool-level,
  per-tool map and call-level `ToolApproval` settings.

- The harness credential setup no longer echoes an invalid base URL in its
  error. A base URL can carry credentials (`https://user:token@host`); the
  message is now `Invalid URL`, as in TS.

### Fixed

- `mcp.MCPClient` now matches JSON-RPC responses to pending requests when the
  server echoes the request id as a JSON number. Before, responses decoded as
  `float64` never matched the client's `uint64` request ids, so `Connect` timed
  out against HTTP servers.
- A step that pauses for tool approval keeps the model's finish reason
  (normally `tool-calls`), as in TS. It was reported as `user-approval`,
  which TS `useChat` rejects, so the approval step failed with a type
  validation error in the browser. `types.FinishReasonUserApproval` is
  deprecated and no longer reported.
- `harness.Agent.CreateSession` names the sandbox and its work dir after the
  generated session ID. Sessions created without a `SessionID` all shared
  the work dir `<harness>-%`.
- The default UI message stream headers are canonical, so
  `Header.Get("X-Vercel-AI-UI-Message-Stream")` finds the protocol header on
  responses from `CreateUIMessageStreamResponse`.
- The harness examples (Claude Code, Codex, Cursor, fx, GitHub Copilot,
  Grok Build, workflow) give the local sandbox a port; they failed at
  startup with "needs a TCP port exposed by the sandbox".

### Documentation

- New "Build a chat app" guides: serve `useChat` from Go, tool approval end
  to end, and Claude Code or Codex through the harness. 12 recipes, each
  backed by a program in `examples/recipes/`.
- Rewritten quick start; current model IDs throughout; reference pages for
  MCP, the harness, workflow and UI message chunks; option tables generated
  from the source (`docs/scripts/gen-reference.go`); changelog page on the
  site.
- For coding agents: `/agents.md`, `/sitemap.md`, a restructured `llms.txt`,
  per-section `llms.txt` files and `llms-core.txt`.
- CI compiles every Go snippet in the docs (`docs/scripts/compile-snippets.go`)
  and checks the generated reference for drift.

## [0.5.0] - 2026-10-02

TS SDK parity target: `ai@7.0.127` (was `ai@6.0.137` in v0.4.0). Ships
everything merged since the v0.4.0 tag — about 1,000 commits across the May,
June, and September 2026 parity cycles. Condensed from and superseded in
detail by [`release_notes/RELEASE_NOTES_V0.5.0.md`](release_notes/RELEASE_NOTES_V0.5.0.md);
step-by-step upgrade instructions are in
`docs/08-migration-guides/from-v0.4-to-v0.5.mdx`.

### Added

- **New providers**: Voyage AI (embedding/rerank), Fish Audio, Cartesia
  (plus Ink 2 realtime transcription), Rev.ai, Hume, Luma, GMI Cloud, Z.AI,
  MiniMax, TypeSafe AI, QuiverAI, `anthropicaws` (Claude Platform on
  AWS), and Topaz Labs (image enhance/generation, async video).
- **Catch-up to `ai@7.0.127`**:
  - ToolSearch `MaxResults` and custom `Search` ranking
  - UI message stream keepalive (`KeepAliveMs`) and `ConvertDataPart`
  - image-model file/mask input capabilities
  - speech and transcription telemetry (including streaming transcription)
  - GPT-6.1 Sol; Claude Sonnet 5.5 with between-tools thinking
  - Azure MAI-Transcribe / MAI-Voice (including streaming transcription)
  - Bedrock `requestMetadata`
  - MCP `AuthorizationServerMismatchError` and conditional token invalidation
  - harness `ReadHistory`, sub-agent activity events, `WorkDir: "."` and runtime-context forwarding
- **Experimental surfaces**: Batch API, Evaluation, Files API v4, async
  video, streaming transcription/translation, and speech translation, each
  implemented by two or more providers.
- **`pkg/codemode`** (experimental): runs model-written JavaScript in a
  QuickJS-on-WebAssembly sandbox, with signed continuations, interrupts, and
  approval flows; TypeScript annotations are stripped with Node
  `stripTypeScriptTypes` semantics; concurrent tool calls (`Promise.all`)
  that need approval are batched into one interrupt; the tool catalog lists
  tools in declaration order (`ToolCallerDefinition.Bind` /
  `PrepareModelMessage` take an ordered `[]types.Tool`).
- **`pkg/harness`** (Go port of `@ai-sdk/harness`): Agent/AgentSession,
  `StopWhen`, tool approvals, telemetry; adapters for Claude Code, Codex,
  OpenCode, Deep Agents, ACP, Cursor, fx, GitHub Copilot, Grok Build; a
  Vercel Sandbox harness provider; `pkg/workflow` harness integration
  helpers.
- **MCP**: full OAuth `Auth()` flow, 2026 protocol support, elicitation
  requests, resource-template listing, a standing inbound SSE listener for
  legacy servers.
- **Telemetry**: `telemetry.NewOpenTelemetry`, a GenAI-semantic-convention
  integration; `GenerateObject`/`StreamObject` telemetry spans.
- **Core**: stable lifecycle callbacks, `RepairToolCall`, `LogWarnings`,
  `FingerprintTools`/`DetectToolDrift`, `PrepareStep`, stream retries,
  `ToolSearch`/`DeferLoading`, `ExperimentalToolCallers`,
  `UploadFile`/`UploadSkill`, workflow model serialization for every model
  kind, streaming request bodies (`Request.Body` on `StreamText` steps via
  `provider.StreamRequestBody`), optional realtime capability interfaces
  (`RealtimeClientSecretCreator`, `RealtimeWebSocketConfigProvider`), and
  more (full list in the release notes).
- **Providers**: substantial Anthropic, OpenAI Responses, xAI, Google/
  Vertex, Bedrock, Gateway, and Cohere feature additions;
  `openai.Config.TransformRequestBody`; Groq model ID constants; see the
  release notes' New Features section for the per-provider breakdown.
- **Docs for agents and search**: every docs page is published as markdown
  (append `.md` to its URL), with `llms.txt` and `llms-full.txt` indexes,
  Copy page / Open in ChatGPT / Open in Claude actions, per-page Open Graph
  images, `robots.txt`, sitemap dates and schema.org structured data;
  `AGENTS.md` for coding agents; the docs validator now checks YAML
  frontmatter; new logo.

### Changed

- **`GenerateText` / `StreamText` run a single step unless you set
  `StopWhen`** (same as v0.4.0 and the TypeScript SDK's default
  `stopWhen: isStepCount(1)`). If the model calls a tool, the tool runs and its
  result is returned, but the model is not called again. To keep calling
  tools until the model answers, set a stop condition, for example
  `StopWhen: []ai.StopCondition{ai.IsStepCount(5)}`, or use
  `agent.NewToolLoopAgent` (default 20 steps). Pre-release builds of v0.5.0
  briefly looped with no default limit (up to a 1,000-step safety ceiling);
  that regression is fixed, and the docs and examples now set `StopWhen`
  wherever they expect a final answer after a tool call.
- **Minimum Go version is now 1.26** (`go.mod` declares `go 1.26.0`). Go 1.25 is
  end-of-life, and the current `golang.org/x/*` modules require Go 1.26. CI tests
  Go 1.26 and 1.27.
- **Dependencies updated to latest**: OpenTelemetry v1.46.0, echo v4.16.0, chi
  v5.3.2, fiber v2.52.15, grpc v1.84.0, quic-go v0.63.0, and the `golang.org/x/*`
  modules. See the release notes for the full table. wazero stays at v1.9.0:
  v1.10+ cannot reuse a compiled module across runtimes, which the code-mode
  sandbox relies on, and the workaround makes each sandbox start about 5x slower.
- **`StreamText` is now asynchronous**, matching TS: it returns before the
  first model request, and only option-validation errors return from the
  call itself.
- **Full-stream chunk lifecycle redesigned**: `ChunkTypeFinish` now fires
  once per call; steps are bracketed by new `ChunkTypeStartStep` /
  `ChunkTypeFinishStep`.
- **Runtime/tool context split**: `ExperimentalContext` → `RuntimeContext` /
  `ToolsContext`; tool approval is now call-level (`ToolApproval`).
- **File data is a tagged union** (`types.FileData`/`FileDataType*`);
  system messages in `Messages` are rejected by default
  (`AllowSystemMessages` opts back in).
- **Bedrock rebuilt on the Converse API**; **Bedrock-Anthropic rebuilt on
  `anthropic.LanguageModel`**; **xAI Chat Completions API removed** (use the
  Responses API); **Gateway `xai/*` model IDs renamed `spacexai/*`**.
- **Anthropic**: `system`/user content are arrays, `BaseURL` includes
  `/v1`, `DisableParallelToolUse` is `*bool`, `ModelOptions` JSON tags are
  camelCase.
- **OpenAI/Azure**: reasoning-model parameter handling changed
  (`max_completion_tokens`, dropped unsupported params);
  Azure classifies unrecognized base URLs as custom gateways.
- **Google/Vertex**: provider-option key order, thinking-budget formula,
  Imagen removal, Interactions API wire format.
- **Embed/EmbedMany/Rerank**: `MaxRetries` is now `*int`. **Schema**:
  `additionalProperties: false` now enforced. **Perplexity**: migrated to
  the Agent API. **Telemetry**: tracers belong to registered integrations;
  `LegacyOpenTelemetry` span shape overhauled to match TS.
- **Outgoing requests now carry a `User-Agent` header**
  (`ai-sdk-<provider>/<version> go/<goVersion>`, the standards-compliant
  form TS uses, plus `ai/<version>`
  from the non-streaming `pkg/ai` calls; `StreamText`, `StreamObject` and
  `Rerank` add no `ai/` tag), matching the TypeScript SDK.
- Vendored `qjs.wasm` rebuilt from pinned upstream sources with job-queue
  quiescence and module-disabling patches; reproducible via
  `pkg/internal/third_party/qjs/build/build.sh`.
- Internal refactor: the WebSocket transcription and translation streams
  share a session core in `pkg/providerutils/websocket` (`Session[T]`,
  `ReportError`, `PumpAudio`, `PumpAudioAfterReady`). No behavior change.
- Full list of breaking and behavior changes: release notes' Breaking
  Changes and Behavior Changes sections.

### Deprecated

- `ExperimentalContext`, per-tool `NeedsApproval`,
  `ExperimentalFilterActiveTools`, `MaxSteps`, `ExperimentalTelemetry`,
  `OTelTelemetryIntegration`, `ToGoogleMessages`,
  `StreamTextResult.FullStream()`, and several tool-execution event fields
  (excluded from JSON). All have stable replacements; see the release
  notes' Deprecations section.

### Removed

- Bedrock-Anthropic's Go-only `CacheConfig` API and
  `PrepareTools`/`UpgradeToolVersion`/`MapToolName`/`GetBetaHeaders`/
  `IsComputerUseTool`; xAI `ChatCompletionsLanguageModel()`/
  `NewLanguageModel`/`SearchParameters`; Anthropic
  `ToAnthropicFormatWithCache`; HuggingFace's `EmbeddingModel`/`ImageModel`;
  non-Gemini Imagen models on Google/Vertex; `telemetry.Options.Tracer`/
  `WithTracer`; Cerebras's retired model constants.

### Fixed

- Anthropic multi-step tool use, streaming web tool results, mid-stream
  errors, and finish-reason mapping; OpenAI Responses previously-dropped
  output items now decoded; Bedrock rerank key, forced tool choice, and
  retryable stream errors; Cohere tool calling now works end-to-end; MCP
  HTTP transport connection leaks and content-type handling; realtime/
  WebSocket connections now fail instead of finishing silently on a dropped
  connection; telemetry spans no longer leak on error/abort; harness
  Codex/host-tool/turn-release fixes; a stray leading "L" in ~88 error
  strings; tool-caller messages (e.g. code-mode's tool catalog) persist
  across steps; a stack-overflow crash in streaming providers on long runs
  of events with no output; Mistral thinking-mode deltas dropping their
  text; a concurrent map crash in the shared HTTP client when
  setting headers during in-flight requests; SSE lines over 64 KiB no
  longer abort streams (32 MiB limit); a concurrent map write crash and a
  late-write panic in `CreateUIMessageStreamWithOptions`; realtime session
  goroutine leak and double-close panic; harness `AgentSession` concurrent
  turn-start race and host tool executions leaked on cancel; concurrent map
  crashes in the agent subagent/skill registries; MCP stdio, TUI and
  workflow transport races and leaks; Azure system-only prompt panic;
  poller timeouts and cancellation; JSON numeric provider options; Vercel
  Sandbox `Wait` ctx handling and stream error causes; ACP now rejects a
  misconfigured `askUserQuestions` that returns a provider-executed tool
  call instead of silently accepting it; the LangChain adapter's argument
  fallback (it never ran); agent and workflow lifecycle events now fill in
  the new `ToolCall`/`ToolOutput`/`Provider`/`Instructions` fields, not
  only the deprecated ones; Google speech rejects out-of-range sample
  rates; Anthropic extended-thinking signatures were dropped from streamed
  responses, breaking multi-step `StreamText` with thinking and tools; the
  shared streaming tool-call tracker no longer aborts, corrupts, loses or
  misorders calls when providers send unreliable tool-call labels; UI
  message stream and text stream pipes now close their source when the
  client disconnects (no leaked provider connections); Azure's
  OpenAI-protocol transcription model supports streaming
  (`gpt-realtime-whisper`); Google Vertex embeddings honor
  `providerOptions.googleVertex`; harness tool-execution telemetry spans
  start when the tool starts. Docs pages that showed APIs that don't exist were corrected
  against the code. Full list in the release notes' Bug Fixes section.

### Security

- `pkg/codemode`: closed a sandbox escape that let model-written JavaScript
  reach QuickJS's `std` / `os` modules (host files under the working
  directory, environment variables, `exit`, unbounded stdout). The sandbox
  now mounts no filesystem, passes no environment, removes the libc
  globals and caps console output, and the QuickJS engine refuses all
  module imports (no native `qjs:*` modules; the loader rejects every
  specifier), with a source-level `import()` check and `eval` / `Function`
  blocking as extra layers.
- Tool approvals verified on resume (HMAC v1, TS-compatible).
- Downloads: DNS pinning, synced blocklist, bounded reads, credential
  stripping across cross-origin redirects. MCP OAuth discovery SSRF-guarded.
- Dependency bumps: `echo` v4.16.0 (includes the CVE-2026-55677 fix from
  v4.15.4), `chi` v5.3.2, OTel v1.46.0, `grpc` v1.84.0, `x/text` v0.42.0,
  `quic-go` v0.63.0 and the other `golang.org/x` modules — `govulncheck`
  reports no reachable vulnerabilities.
- Resource names, regions and locations that would rewrite the request
  host are rejected (Azure, Bedrock incl. Mantle, Google Vertex incl. MaaS
  and Anthropic on Vertex); the TUI escapes untrusted terminal control
  characters; ACP host-tool execution requires a one-use authorization
  from a matching observed tool call.
- CodeQL clean: allocation sizes are overflow-checked, JSON fragments are
  built with the encoder instead of string splicing, and the examples no
  longer log raw errors or URLs that can carry credentials.
- BFL poll URLs, OpenAI image-edit URL inputs, and Anthropic batch
  `results_url` now fetched through the SSRF-safe download path.
- Removed unused internal download helpers that skipped the SSRF checks.
- Harness bridge dial errors no longer include the bridge token.
- MCP OAuth and OPA policy HTTP responses are read with a 1 MiB limit.
- Bedrock event-stream decoder overflow panic on crafted frames;
  `anthropicaws` SigV4 credential race; WebSocket dial errors no longer
  include query strings or userinfo.
- `provider.SerializableConfig` redacts credential headers (`Authorization`,
  `Proxy-Authorization`, `X-Api-Key`, `Api-Key`, `X-Goog-Api-Key`,
  `Cookie`, `Set-Cookie`, and any `*-api-key` / `*-token` / `*secret*`
  name, case-insensitive) from a serialized model's `Config.Headers`, so
  header-based credentials no longer end up in `SerializedModel.Config`.

## [0.4.0] - 2026-03-29

TS SDK parity — fully compatible with TS AI SDK v6.0.137.
Commit range `ed17fe86d..429b88a79` plus v6.0.137 backports (13 PRDs, 244 tasks).
Full release notes: `release_notes/RELEASE_NOTES_V0.4.0.md`.

### Breaking Changes

- **Streaming tool execution** now deferred to after stream end (not mid-stream)
- **XAI** `LanguageModel()` returns Responses API model; use `ChatCompletionsLanguageModel()` for legacy
- **XAI** removed `grok-2` and `grok-2-vision-1212` model IDs
- **MCP** default redirect mode changed to `MCPRedirectError` (reject)

### Added

#### Core SDK
- **Top-level `Reasoning *types.ReasoningLevel`** on `GenerateTextOptions` and `StreamTextOptions`
  with 7 levels (provider-default, none, minimal, low, medium, high, xhigh)
- **Deferred provider tool results** — `pendingDeferredToolCalls` tracking in generate.go
  and stream.go step loops; `SupportsDeferredResults bool` on `types.Tool`
- **`CustomContent`** and **`ReasoningFileContent`** content types with stream chunks
  (`ChunkTypeCustom`, `ChunkTypeReasoningFile`)
- **`TelemetryIntegration`** global registry with `sync.RWMutex`, `NoopTelemetryIntegration`
  default, `OTelTelemetryIntegration` wrapper; `Fire*` fan-out in generate.go/stream.go
- **Tool-level timeouts** — `TimeoutConfig.ToolMs`, `TimeoutConfig.Tools`, `GetToolTimeout()`
- **Embed/rerank callbacks** — `EmbedOnStartEvent`, `EmbedOnFinishEvent`,
  `RerankOnStartEvent`, `RerankOnFinishEvent` with provider options threading
- **`StreamTextResult.ProviderMetadata()`** — accumulated from stream chunks
- **`ToolResult.Input`** always populated with `call.Arguments`

#### Anthropic Provider
- **`WebSearch20260209(config)`** tool — allowedDomains, blockedDomains, userLocation, maxUses;
  returns `[]WebSearchResult20260209` with encryptedContent
- **`WebFetch20260209(config)`** tool — citations, maxContentTokens; returns discriminated
  `WebFetchSource` (PDF/text) with `IsPDF()`/`IsPlainText()` helpers
- **`EagerInputStreaming *bool`** per-tool option; `tool-input-start/delta/end` stream chunks
- **Error code preservation** — `error.type` surfaces as `ProviderError.Code`
- Beta header `code-execution-web-tools-2026-02-09` auto-injected for 20260209 tools

#### OpenAI Provider
- **GPT-5.4 model family** — `gpt-5.4`, `gpt-5.4-pro`, `gpt-5.4-mini`, `gpt-5.4-nano`, dated variants; `gpt-5.3-chat-latest`
- **Responses API compaction** — parsed as `CustomContent` with `Kind: "openai-compaction"`
- **`response.failed` SSE event** — maps `incomplete_details.reason` to finish reason, falls back to `error`
- **`ToolSearch(args)`** factory — server (default) and client execution modes
- **`CustomTool.Name` removed** — name supplied via `ToTool(name)` method
- **store=false** strips unencrypted reasoning from assistant history

#### XAI Provider
- **Responses API as default** with `ChatCompletionsLanguageModel()` opt-in
- **Grok 4.20 GA models** — multi-agent, reasoning, non-reasoning variants
- Multi-image editing, b64_json output, quality/user params
- `CostInUsdTicks` in image and video metadata
- `ReasoningSummary` option, `ModerationError` type, `Logprobs`/`TopLogprobs`
- Reasoning extraction fix (summary + content fallback)

#### Google Provider
- **VALIDATED** function calling mode when `tool.Strict == true`
- Grounding metadata accumulation across stream chunks
- Multimodal `functionResponse.parts[]` for Gemini 3+ models
- `finishMessage` in Vertex provider metadata
- 7 native Vertex tools (GoogleSearch, UrlContext, CodeExecution, etc.)
- `gemini-embedding-2-preview` with multimodal embedding support

#### Multi-Provider Reasoning
- DeepSeek, Alibaba, Fireworks, Groq, Mistral, Perplexity (warning),
  Cohere (warning), Open Responses, XAI — all wired to top-level `Reasoning`

#### KlingAI Provider
- **v3.0 motion control** — multi-shot, element control, voice control, motion brush
- Model IDs: `kling-v3.0-motion-control`, `kling-v2.6-motion-control`

#### New Provider: Prodia
- **Language model** (img2img) — multipart form-data, 11 aspect ratios
- **Video model** — T2V and I2V with multipart response parsing
- Shared `prodia_api.go` infrastructure

#### Provider Fixes
- Alibaba: single-item content array with cache_control preserves array
- Perplexity: `ProviderMetadata` restructured to `{Images, Usage, Cost}` sub-objects
- MCP: protocol version `2025-11-25` added; `MCPRedirectMode` typed option
- MCP: OAuth state uses `crypto/subtle.ConstantTimeCompare`
- HTTP transport: custom `User-Agent` header removed

### Fixed
- **SSRF protection** — `validateDownloadURL()` rejects private IP redirects
  (IPv4, IPv6, IPv4-mapped-IPv6, localhost, link-local, CGNAT)
- **Streaming tool calls** — accumulate+flush in OpenAI, Groq, DeepSeek, Alibaba;
  no mid-stream finalization from isParsableJson
- **`isProviderExecutedTool()`** hardcoded name map deleted; replaced with `tool.ProviderExecuted`
- **OpenAI wire format** for multi-turn tool calls (assistant `tool_calls` array,
  tool role `tool_call_id`)

---

## [0.3.0] - 2026-03-01

TypeScript AI SDK parity update — commit range `c123363c0..ed17fe86d`
(124 commits, 18 PRDs, 321 tasks). Also includes the `StopWhen` / `MaxSteps`
agent-loop alignment work (2026-02-16 – 2026-02-19) merged ahead of the PRD
cycle, filled in below from git history.

### ⚠️ Breaking Changes

- **Anthropic / Bedrock**: `output_format` renamed to `output_config.format` in
  request builder — matches the live Anthropic API

### Added

#### New Provider
- **ByteDance (Volcengine)** video generation provider (`pkg/providers/bytedance/`)
  with async-polling `DoGenerate`, all model ID constants, and README

#### Core SDK
- `Output[T]` interface and five factories: `TextOutput`, `ObjectOutput`,
  `ArrayOutput`, `ChoiceOutput`, `JSONOutput`
- `WithOutput` parameter for `GenerateText` and `StreamText`
- Six structured callback event types: `OnStartEvent`, `OnStepStartEvent`,
  `OnToolCallStartEvent`, `OnToolCallFinishEvent`, `OnStepFinishEvent`,
  `OnFinishEvent`
- Panic-safe `Notify[E]` dispatch utility
- Agent callback merging support
- **Stop conditions**: `StopCondition` type and built-in `StepCountIs`/
  `HasToolCall` conditions; `StopWhen`/`StopReason` fields on `GenerateText`
  and agent (`ToolLoopAgent`) options; `StopWhen` evaluation wired into both
  the `GenerateText` step loop and the agent loop; `MaxSteps` realigned with
  the Vercel AI SDK v5 `stopWhen` approach (deprecation comments removed)

#### Anthropic Provider
- `code-execution-20260120` tool with `programmatic-tool-call`,
  `bash_code_execution`, `text_editor_code_execution` input types
- Automatic caching support
- `claude-sonnet-4-6` model ID constant
- `DisableParallelToolUse` model option
- `fine-grained-tool-streaming` and `cache-control` beta header support
- Streaming tool calls via `contentBlocks` state tracker in `anthropicStream`
- Native MCP client: `MCPServerConfig`, `MCPToolConfiguration`, `mcp_servers`
  request field, `mcp_tool_use` / `mcp_tool_result` response blocks
- Agent container & skills: `ContainerConfig`, `ContainerSkill`,
  `Container` / `ContainerID` model options, three beta headers
- `StructuredOutputMode` option (`auto` / `outputFormat` / `jsonTool`)
  with `jsonTool` fallback for older models
- `SendReasoning *bool` model option with `filterReasoningContent()` helper
- `ReasoningContent.Signature` and `ReasoningContent.RedactedData` fields
- `ToAnthropicMessages` handler for `ReasoningContent` (thinking block round-trip)

#### OpenAI Provider
- `CustomTool` with grammar and text format options
- `LocalShell`, `Shell`, `ApplyPatch` container tool types
- MCP approval response type
- `Phase` field on Responses API message items

#### Google Provider
- `gemini-3.1-pro-preview` and `gemini-3.1-flash-image-preview` model constants
- New image aspect ratios and sizes for Google AI and Vertex Imagen

#### KlingAI Provider
- `kling-v3.0-t2v` and `kling-v3.0-i2v` model ID constants

#### Fireworks Provider
- Async image generation for `flux-kontext-*` models with 2s polling loop

#### Gateway
- SSE video streaming with heartbeat / progress / complete / error event handling
- `ProjectID` field and `WithProjectID()` option for request observability

#### Model IDs
- OpenAI: `gpt-5.3-codex` and additional model constants
- XAI: resolution option, image model IDs
- Bedrock: complete Anthropic model ID set
- TogetherAI: `TOGETHER_API_KEY` environment variable

#### Docs & Examples
- Provider architecture guide
- Memory management guide
- Coding agents guide
- `gpt-5.3-codex`, Gemini flash-image, Anthropic context editing examples

### Changed

- `GenerateObject` and `StreamObject` deprecated in favor of `WithOutput`
- Anthropic thinking blocks now strip `temperature`, `topP`, `topK` automatically
- Streaming token counts captured from `message_start` in Anthropic provider
- Alibaba cache control applied to all messages (not just first)
- Cerebras: deprecated model IDs removed

### Fixed

- Unknown tool name in model response produces error `ToolResult` instead of
  duplicate tool part
- Tool choice (`required` / `auto` / specific tool) now always forwarded to provider
- Stream resumption no longer flashes status to `submitted` when no active stream
- `StreamingToolCallDelta.Type` is now `*string` (nullable) for OpenAI streaming
- `WebSearchToolCall.Action` is now `*string` (optional) for OpenAI
- OpenAI reasoning parts with `EncryptedContent` included even without `ItemID`
- Bedrock and Groq now pass `strict: true` in tool definitions when strict mode set
- `chatgpt-image` recognized in OpenAI response format prefix detection
- `compaction_delta` null content no longer panics in Anthropic provider
- `SupportsStructuredOutput()` now correctly returns `false` for pre-4.5 models

---

## [0.2.0] - 2026-02-16

Release v0.2.0: AI SDK v6. Combines the `2025-12-18` v6.0 API synchronization
work and the `2026-02-15` telemetry/audio-provider work (both previously
tracked under stale `[Unreleased]` headings), plus provider and platform
work filled in below from git history that was never written up in either
section.

### 🎉 100% Feature Parity Achieved (2026-02-15)

Closed the final gap to achieve complete feature parity with the TypeScript AI SDK through telemetry integration and audio provider additions.

### Added

#### Telemetry Integration
- **`ExperimentalTelemetry` parameter** added to all core API functions
  - `GenerateText()` - Full span tracking with input/output attributes
  - `StreamText()` - Streaming operation telemetry
  - `GenerateObject()` - Object generation telemetry (all modes)
  - `StreamObject()` - Streaming object telemetry
  - `Embed()` - Single embedding telemetry
  - `EmbedMany()` - Batch embedding telemetry
- **OpenTelemetry span tracking** with automatic instrumentation
  - Input/output attributes captured per operation
  - Usage metrics (tokens, duration) included in spans
  - Finish reason tracking
- **Privacy controls**
  - `RecordInputs` - Control whether to record input data in spans
  - `RecordOutputs` - Control whether to record output data in spans
- **MLflow integration example** updated to demonstrate telemetry usage
- **Comprehensive test suite** - 5 new tests for telemetry functionality

#### Audio Providers (Speech & Transcription)

**Gladia Provider** (Speech-to-Text):
- Full transcription model implementation
- **Features:**
  - Multipart form upload for audio files
  - Word-level timestamps support
  - Multi-language transcription (100+ languages)
  - Automatic language detection
- **Supported formats:** MP3, WAV, M4A, FLAC, OGG
- **Model:** Whisper v3
- **Implementation:**
  - `pkg/providers/gladia/provider.go`
  - `pkg/providers/gladia/transcription_model.go`
- **Tests:** 3 unit tests with mocked HTTP server (all passing)
- **Documentation:**
  - Package README (`pkg/providers/gladia/README.md`)
  - Official docs (`docs/05-providers/31-gladia.mdx`)
- **Examples:**
  - `examples/gladia-transcription/` - Basic transcription
  - `examples/gladia-transcription-timestamps/` - Timestamps demo

**LMNT Provider** (Text-to-Speech):
- Full speech synthesis model implementation
- **Features:**
  - High-quality voice synthesis
  - Multiple voice options (aurora, lily, harper, sage)
  - Speed control (0.5x - 2.0x playback)
  - JSON API with clean interface
- **Output format:** MP3 (audio/mpeg)
- **Implementation:**
  - `pkg/providers/lmnt/provider.go`
  - `pkg/providers/lmnt/speech_model.go`
- **Tests:** 3 unit tests with mocked HTTP server (all passing)
- **Documentation:**
  - Package README (`pkg/providers/lmnt/README.md`)
  - Official docs (`docs/05-providers/32-lmnt.mdx`)
- **Examples:**
  - `examples/lmnt-speech/` - Basic speech synthesis
  - `examples/lmnt-speech-speed/` - Speed control demo

### Documentation

#### Provider Documentation
- **Gladia documentation** (`docs/05-providers/31-gladia.mdx`)
  - Comprehensive setup and configuration guide
  - Available models and features
  - Usage examples (basic, timestamps, multi-language)
  - Supported audio formats reference
  - Error handling and best practices
  - Complete working examples

- **LMNT documentation** (`docs/05-providers/32-lmnt.mdx`)
  - Comprehensive setup and configuration guide
  - Available voices and models
  - Usage examples (basic, speed control, batch generation)
  - Advanced features and performance tips
  - Error handling and best practices
  - Complete working examples

#### Package READMEs
- `pkg/providers/gladia/README.md` - Quick start guide with examples
- `pkg/providers/lmnt/README.md` - Quick start guide with examples

#### Example Applications
- 4 new runnable example applications with README documentation
- All examples compile successfully
- Include setup instructions and expected output

### Testing

- **11 new unit tests added** (all passing)
  - 5 telemetry integration tests
  - 3 Gladia provider tests
  - 3 LMNT provider tests
- **Test infrastructure improvements**
  - Mock HTTP servers for audio provider testing
  - OpenTelemetry span recording for telemetry tests
  - Type-safe attribute comparison helpers
- **No regressions** - All existing tests continue to pass

### Changed

- **Telemetry system** - Now accessible through core API options
- **Audio provider count** - Increased from 3 to 5 providers
  - Existing: ElevenLabs, Deepgram, AssemblyAI
  - New: Gladia, LMNT

### Provider Count Update

Total provider count: **28 providers** (26 → 28)
- Language models: 16 providers
- Image generation: 3 providers
- Speech synthesis: 3 providers (ElevenLabs, LMNT, OpenAI TTS)
- Speech transcription: 4 providers (Deepgram, AssemblyAI, Gladia, OpenAI Whisper)
- Embeddings: 4 providers
- Reranking: 1 provider

### Migration Notes

No breaking changes in this release. All updates are additive:

#### Using Telemetry (Optional)
```go
import "github.com/digitallysavvy/go-ai/pkg/telemetry"

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Hello",
    ExperimentalTelemetry: &telemetry.Settings{
        IsEnabled:     true,
        RecordInputs:  true,
        RecordOutputs: true,
    },
})
```

#### Using New Audio Providers
```go
// Gladia - Speech-to-Text
import "github.com/digitallysavvy/go-ai/pkg/providers/gladia"

provider := gladia.New(gladia.Config{
    APIKey: os.Getenv("GLADIA_API_KEY"),
})
model, _ := provider.TranscriptionModel("whisper-v3")
result, _ := model.DoTranscribe(ctx, &provider.TranscriptionOptions{
    Audio:      audioData,
    MimeType:   "audio/mpeg",
    Timestamps: true,
})

// LMNT - Text-to-Speech
import "github.com/digitallysavvy/go-ai/pkg/providers/lmnt"

provider := lmnt.New(lmnt.Config{
    APIKey: os.Getenv("LMNT_API_KEY"),
})
model, _ := provider.SpeechModel("default")
speed := 1.2
result, _ := model.DoGenerate(ctx, &provider.SpeechGenerateOptions{
    Text:  "Hello world",
    Voice: "aurora",
    Speed: &speed,
})
```

### Performance

- Telemetry integration adds minimal overhead (~1-2% when enabled)
- Audio providers use efficient streaming where applicable
- HTTP connection pooling for audio API requests
- All examples and tests complete in <5 seconds

### Quality Metrics

- **Implementation time:** ~8 hours (vs 10-18 estimated)
- **Test coverage:** 100% for new features
- **Documentation completeness:** 100% parity with TypeScript SDK
- **Breaking changes:** 0 (fully backward compatible)

### 🚀 v6.0 API Synchronization (2025-12-18)

Synchronized with TypeScript AI SDK v6.0 for complete feature parity.

### 💥 Breaking Changes

#### Usage Tracking API Changes
- **All `Usage` fields now use pointers (`*int64`) instead of `int64`**
  - `InputTokens`, `OutputTokens`, `TotalTokens` are now `*int64` to properly distinguish "not set" from "zero"
  - **Migration**: Update comparisons like `if usage.InputTokens != 0` to `if usage.InputTokens != nil && *usage.InputTokens != 0`

#### Tool Execution API Changes
- **`ToolExecutor` function signature changed**
  - Old: `func(ctx context.Context, input map[string]interface{}) (interface{}, error)`
  - New: `func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error)`
  - Added `ToolExecutionOptions` parameter providing `ToolCallID`, `UserContext`, and `Usage`

#### Callback Signature Changes
- **`OnStepFinish` callback signature changed**
  - Old: `func(step types.StepResult)`
  - New: `func(ctx context.Context, step types.StepResult, userContext interface{})`

- **`OnFinish` callback signature changed** (GenerateText, GenerateObject, StreamObject)
  - Old: `func(result *GenerateTextResult)` or `func(result *GenerateObjectResult)`
  - New: `func(ctx context.Context, result *GenerateTextResult, userContext interface{})`
  - New: `func(ctx context.Context, result *GenerateObjectResult, userContext interface{})`

#### GenerateObject API Changes
- **`GenerateObject` now requires explicit `Schema` parameter**
  - Old: `Output: &MyStruct{}`
  - New: `Schema: schema.NewSimpleJSONSchema(...)`
  - Provides better control over JSON schema validation

### Added

#### Detailed Usage Tracking (v6.0)
- **`InputTokenDetails`** - Breakdown of input tokens
  - `NoCacheTokens` - Tokens not from cache
  - `CacheReadTokens` - Tokens read from prompt cache (Anthropic, OpenAI, Google)
  - `CacheWriteTokens` - Tokens written to cache (Anthropic, Bedrock)

- **`OutputTokenDetails`** - Breakdown of output tokens
  - `TextTokens` - Regular text generation tokens
  - `ReasoningTokens` - Reasoning/thinking tokens (OpenAI o1/o3, Google Gemini thinking, DeepSeek R1)

- **`Usage.Raw`** - Raw provider-specific usage data for full transparency

#### Enhanced Tool System (v6.0)
- **New Tool fields**
  - `Title` - Human-readable title for better UX
  - `InputExamples` - Example inputs for better LLM guidance
  - `Strict` - Enable strict schema validation
  - `NeedsApproval` - Require approval before execution
  - `ToModelOutput` - Custom tool output formatting
  - `OnInputStart`, `OnInputDelta`, `OnInputAvailable` - Streaming callbacks

- **`ToolExecutionOptions`** - New context for tool execution
  - `ToolCallID` - Unique identifier for this tool call
  - `UserContext` - Flow user context through tool execution
  - `Usage` - Accumulated token usage
  - `Metadata` - Additional execution metadata

#### Output Objects System (v6.0)
- **`ai.ObjectOutput[T](opts)`** - Type-safe object generation
- **`ai.ArrayOutput[T](opts)`** - Generate arrays of elements
- **`ai.ChoiceOutput[T](opts)`** - Generate enum selections
- **`ai.JSONOutput(opts)`** - Flexible JSON generation
- **`ai.TextOutput()`** - Plain text output (default)

#### Context Flow Management (v6.0)
- **`ExperimentalContext`** - Flow custom context through generation
  - Available in callbacks (`OnStepFinish`, `OnFinish`)
  - Available in tool execution (`ToolExecutionOptions.UserContext`)
  - Enables request-scoped data like user IDs, session info, etc.

#### Provider Updates
All 13 language model providers updated with v6.0 usage tracking:
- **OpenAI** - Full cache + reasoning token support
- **Anthropic** - Cache read + write tokens
- **Google** - Cache + thoughts (reasoning) tokens
- **Azure** - OpenAI-compatible with full support
- **Bedrock** - Unique cache read + write pattern
- **Mistral** - Simple format with input/output details
- **Together AI** - OpenAI-compatible with full support
- **Fireworks** - OpenAI-compatible for OSS models
- **Ollama** - OpenAI-compatible for local LLMs
- **xAI** - OpenAI-compatible for Grok models
- **Perplexity** - OpenAI-compatible with search augmentation
- **DeepSeek** - OpenAI-compatible with reasoning support (R1)
- **Huggingface** - Basic support (no token counts)
- **Groq** - Simple format with token details
- **Cohere** - Simple format with input/output tokens
- **Replicate** - Basic support (no token counts)

### Changed

- **`Usage.Add(other)`** - Now properly handles pointer arithmetic and nil values
- **All provider implementations** - Updated to return detailed usage breakdowns
- **Test infrastructure** - Updated all tests for new Usage pointer types

### Migration Guide

#### Update Usage Comparisons
```go
// Before (v5.0)
if result.Usage.TotalTokens > 0 {
    fmt.Printf("Used %d tokens\n", result.Usage.TotalTokens)
}

// After (v6.0)
if result.Usage.TotalTokens != nil && *result.Usage.TotalTokens > 0 {
    fmt.Printf("Used %d tokens\n", *result.Usage.TotalTokens)
}
```

#### Update Tool Definitions
```go
// Before (v5.0)
Execute: func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
    return doSomething(input)
}

// After (v6.0)
Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
    fmt.Printf("Tool call ID: %s\n", opts.ToolCallID)
    return doSomething(input)
}
```

#### Update Callbacks
```go
// Before (v5.0)
OnStepFinish: func(step types.StepResult) {
    fmt.Printf("Step %d done\n", step.StepNumber)
}

// After (v6.0)
OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
    fmt.Printf("Step %d done\n", step.StepNumber)
}
```

#### Update GenerateObject Calls
```go
// Before (v5.0)
result, _ := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Output: &Recipe{},
})

// After (v6.0)
recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
    },
})
result, _ := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Schema: recipeSchema,
})
```

### Examples

- Added `examples/v6_features/main.go` - Comprehensive v6.0 feature demonstration
- Updated core library tests for v6.0 API
- All provider examples remain compatible

### Documentation

- Updated main README.md with v6.0 API examples
- Added migration guide for v5.0 → v6.0
- Updated tool calling examples
- Updated structured output examples

### Added (additional provider & platform work, filled in from git history)

These commits shipped as part of the v0.2.0 range (`v0.1.0..v0.2.0`) but were
never written up in either of the sections merged above.

#### New Providers
- **Alibaba** — chat provider with streaming support
- **KlingAI** — video generation provider
- **Moonshot** — chat provider
- **OpenResponses** — chat provider
- **xAI** — full provider implementation (previously listed as supported but
  not fully implemented), with examples and docs

#### Provider Updates
- **Google** — image generation support added; Google Vertex updates
- **FAL / Alibaba** — improved image-to-video generation
- **Fireworks AI, xAI** — updated MCP support and examples
- **DeepInfra, MLflow integration** — updates

#### Anthropic Provider
- Advanced features, agent skills, and subagents support
- Context condensing ("condense"), with updated docs and examples

#### Core SDK
- Token usage / tracking API updates
- Session retention support
- Security fixes

#### Gateway
- Video generation gateway support

#### Integrations
- LangChain callbacks now carry run IDs
- LangFlow callback updates

#### Development Tools
- CI workflows and issue/PR templates added

---

## [0.1.0] - 2025-12-15

### 🎉 Initial Release

The first public release of the Go AI SDK - a complete rewrite of the Vercel AI SDK with full server-side feature parity.

### Added

#### Core Features
- `GenerateText()` - Synchronous text generation
- `StreamText()` - Real-time streaming text generation with channels
- `GenerateObject()` - Type-safe structured output generation
- `StreamObject()` - Streaming structured output
- `Embed()` - Single text embedding generation
- `EmbedMany()` - Batch embedding generation
- `GenerateImage()` - Text-to-image generation
- `GenerateSpeech()` - Text-to-speech synthesis
- `Transcribe()` - Speech-to-text transcription
- `Rerank()` - Document reranking for search
- `CosineSimilarity()` - Vector similarity calculations

#### Provider Support (26 Providers)
- OpenAI - GPT-4, GPT-3.5, O1, DALL-E, TTS, Whisper
- Anthropic - Claude 3.5 Sonnet, Claude 3 family
- Google - Gemini Pro, Gemini Flash
- AWS Bedrock - Multi-provider access
- Azure OpenAI - Enterprise deployment
- Mistral - Large, Medium, Small models
- Cohere - Command R+, Command R, embeddings, reranking
- Groq - Ultra-fast inference (Llama, Mixtral)
- xAI - Grok models
- DeepSeek - DeepSeek Chat, Coder
- Perplexity - Sonar models
- Together AI - Open source model hosting
- Fireworks AI - Fast model serving
- Replicate - All hosted models
- Hugging Face - Inference API
- Ollama - Local model support
- Stability AI - Stable Diffusion
- Black Forest Labs - FLUX models
- Fal.ai - Fast image generation
- ElevenLabs - High-quality TTS
- Deepgram - Fast STT
- AssemblyAI - Advanced STT
- Baseten - Model serving
- Cerebras - Ultra-fast inference
- DeepInfra - Model hosting
- Vercel AI - Gateway integration

#### Agent Framework
- `agent.New()` - Create autonomous agents
- `agent.Execute()` - Run multi-step workflows
- Tool loop implementation for autonomous reasoning
- Configurable max steps and instructions
- Step-by-step execution tracking

#### Tool Calling
- JSON schema-based tool definitions
- Function execution with parameter validation
- Multi-tool support
- Provider-agnostic tool calling interface

#### Middleware System
- `WrapLanguageModel()` - Middleware wrapper interface
- Logging middleware with multiple output formats
- Caching middleware with TTL and LRU eviction
- Rate limiting middleware (token bucket, sliding window)
- Retry middleware with exponential backoff
- Telemetry middleware for observability
- Composable middleware chains

#### Provider Registry
- String-based model resolution (e.g., "openai:gpt-4")
- Provider auto-discovery
- Model ID parsing and validation

#### Telemetry
- OpenTelemetry integration
- Trace and span support
- Metrics collection
- Custom instrumentation

#### Error Handling
- `ProviderError` - Provider-specific errors with retry hints
- `ValidationError` - Input validation errors
- `ToolExecutionError` - Tool calling errors
- `StreamError` - Streaming-specific errors
- `RateLimitError` - Rate limit handling
- Sentinel errors for common conditions
- Structured error types with context

#### Context Support
- Native Go context throughout
- Cancellation support
- Timeout handling
- Deadline propagation
- Graceful shutdown

### Documentation

#### Comprehensive Guides (40,000+ Lines)
- Getting Started guides
- Foundation concepts (providers, prompts, tools, streaming)
- Complete API reference for all 12 core functions
- 29 provider-specific guides with examples
- Agent framework documentation
- Middleware implementation guides
- Telemetry and observability guides
- Error handling reference (7 error types)
- Migration guides (TypeScript AI SDK → Go, LangChain → Go)
- Troubleshooting guides (6,396 lines)
  - Common errors and solutions
  - Rate limit handling
  - Debugging techniques
  - Context cancellation patterns

### Examples (50+ Complete Examples)

#### HTTP Servers (5)
- `http-server` - Standard net/http with SSE streaming
- `gin-server` - Gin framework integration
- `echo-server` - Echo framework patterns
- `fiber-server` - Fiber web framework
- `chi-server` - Chi router implementation

#### Structured Output (4)
- `generate-object/basic` - Type-safe generation
- `generate-object/validation` - Schema validation
- `generate-object/complex` - Deep nesting
- `stream-object` - Real-time streaming

#### Provider Features (8)
- OpenAI reasoning (o1 models)
- OpenAI structured outputs
- OpenAI vision
- Anthropic prompt caching
- Anthropic extended thinking
- Anthropic PDF support
- Google Gemini integration
- Azure OpenAI patterns

#### Agents (5)
- `math-agent` - Multi-tool problem solver
- `web-search-agent` - Research and fact-checking
- `streaming-agent` - Real-time step visualization
- `multi-agent` - Coordinated systems
- `supervisor-agent` - Agent orchestration

#### Production Middleware (7)
- Logging (console, JSON, file)
- Caching (in-memory, file-based, LRU)
- Rate limiting (token bucket, sliding window)
- Retry with exponential backoff
- Telemetry and metrics
- Unit testing patterns
- Integration testing patterns

#### Multimodal (5)
- Image generation (DALL-E, Stable Diffusion)
- Text-to-speech examples
- Speech-to-text transcription
- Audio analysis
- Vision (image understanding)

#### Advanced (6)
- Document reranking
- Semantic routing
- Throughput benchmarks
- Latency benchmarks
- MCP over stdio
- MCP over HTTP

### Package Structure

```
pkg/
├── ai/              Core AI SDK functions
├── agent/           Autonomous agent framework
├── provider/        Provider interfaces and types
├── providers/       26 provider implementations
├── middleware/      Middleware system
├── registry/        Provider registry
├── schema/          JSON schema utilities
├── telemetry/       OpenTelemetry integration
├── internal/        Internal utilities
└── testutil/        Testing utilities
```

### Testing
- Comprehensive unit tests
- Integration tests with real providers
- All examples compile and pass `go vet`
- Mock providers for testing
- Test utilities and helpers

### Development Tools
- Contributing guidelines (CONTRIBUTING.md)
- Code of conduct (CODE_OF_CONDUCT.md)
- Issue templates
- PR templates
- Development scripts

### Quality Assurance
- All code follows Go best practices
- Comprehensive error handling throughout
- Type-safe APIs
- Production-ready patterns
- Security best practices (no secrets in code)

## Feature Parity

This release achieves **complete server-side parity** with the Vercel AI SDK:
- ✅ Text generation (streaming and non-streaming)
- ✅ Structured output (streaming and non-streaming)
- ✅ Tool calling
- ✅ Agent framework
- ✅ Embeddings (single and batch)
- ✅ Image generation
- ✅ Speech synthesis and transcription
- ✅ Provider registry
- ✅ Middleware system
- ✅ Telemetry
- ✅ Error handling

**Not Included:** React/UI components (client-side only in TypeScript SDK)

## Performance

- Efficient streaming with automatic backpressure
- Low memory overhead
- Concurrent processing with goroutines
- Automatic connection pooling
- HTTP/2 multiplexing support
- Provider-specific optimizations

## Requirements

- Go 1.25 or higher
- Valid API keys for desired providers

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## License

Apache 2.0 - See LICENSE for details

---

[Unreleased]: https://github.com/digitallysavvy/go-ai/compare/v0.5.3...HEAD
[0.6.0]: https://github.com/digitallysavvy/go-ai/compare/v0.5.3...v0.6.0
[0.5.3]: https://github.com/digitallysavvy/go-ai/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/digitallysavvy/go-ai/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/digitallysavvy/go-ai/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/digitallysavvy/go-ai/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/digitallysavvy/go-ai/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/digitallysavvy/go-ai/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/digitallysavvy/go-ai/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/digitallysavvy/go-ai/releases/tag/v0.1.0
