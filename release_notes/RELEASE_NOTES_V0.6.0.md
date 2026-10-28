# Go AI SDK v0.6.0 Release Notes

Release date: set at tag time.

## Overview

v0.6.0 tracks TypeScript AI SDK parity target **`ai@7.0.153`** (up from
`ai@7.0.127` in v0.5.x). It is built on v0.5.3 and includes every fix
shipped there.

Most v0.5.x code compiles and runs unchanged. A few option types changed to
pointers so that "unset" and "explicitly off" can be told apart, as in TS,
and a set of behavior changes now match TS where Go used to differ. Both are
listed below under [Breaking changes](#breaking-changes-with-migration).

The upgrade guide, with before/after code, is
[`docs/08-migration-guides/from-v0.5-to-v0.6.mdx`](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx).

## Installation

```bash
go get github.com/digitallysavvy/go-ai@v0.6.0
```

---

## Highlights

- **Experimental steering.** `ai.NewSteering()` and the `ExperimentalSteering`
  option on `GenerateText`, `StreamText` and the agent let you inject messages
  into a running tool loop between steps. Injected messages are reported on
  `StepResult.ExperimentalSteeringMessages`.
- **`OnStepError`.** `GenerateText`, `StreamText` and the agent report a step
  that started but failed before producing a result. The agent also gains
  `ErrorHandler` and `AbortHandler`.
- **Streaming speech.** `ai.ExperimentalStreamSpeech` takes `Text` or a text
  stream (`NewChannelSpeechTextStream`) and returns an audio stream (or a full
  event stream) with usage, warnings and provider metadata. The AI Gateway
  and ElevenLabs implement it over WebSocket. Models opt in through the
  optional `provider.SpeechStreamer` interface; `SpeechModel` is unchanged.
  See [`stream-speech`](../docs/07-reference/ai/stream-speech.mdx).
- **`pkg/toolbash` and `pkg/toolwebfetch`.** A bash tool that runs over a
  sandbox session (bounded streamed output, process-group kill on timeout),
  and a web fetch tool with SSRF checks on every redirect, DNS pinning,
  size-limited reads and HTML-to-Markdown conversion. See
  [`tool-bash`](../docs/07-reference/ai/tool-bash.mdx) and
  [`tool-web-fetch`](../docs/07-reference/ai/tool-web-fetch.mdx).
- **MCP events and webhooks.** `MCPClientConfig.ExperimentalEvents`,
  `(*MCPClient).ExperimentalEvents()` (event catalog plus webhook
  subscriptions backed by an `MCPEventStore`),
  `ExperimentalManagedEvents()`, and `ExperimentalCreateMCPEventWebhook`, an
  `http.Handler` that verifies Standard Webhooks v1 signatures. Example:
  [`examples/mcp/events`](../examples/mcp/events).
- **fx on libfx (opt-in).** `fx.Settings{Runtime: fx.RuntimeLibfx}` runs the
  TS `@ai-sdk/harness-fx` core in process on wazero. It authenticates with
  the AI Gateway or a Codex/Grok subscription found on the host, connects
  HTTP and SSE MCP servers, and keeps the Gateway key on the host. The
  sandbox `fx acp` runtime stays the default. The pinned `fx-core.wasm` is
  downloaded on first use and checked against its hash.
- **Decide additions.** A `box` question type (`provider.DecisionBox`),
  `MaxOutputTokens` and `Reasoning` on `DecideOptions`, `QuestionOrder` and
  `CriteriaOrder` to keep question order (Go maps do not), and a Perplexity
  decision model.
- **HeyGen provider.** `pkg/providers/heygen` adds text-, image- and
  reference-to-video. Examples are in
  [`examples/providers/heygen`](../examples/providers/heygen).
- **New models.** Claude Haiku 5.5 (Anthropic, Bedrock, Vertex Anthropic,
  Gateway), Gemini `gemini-4-argon` with automatic long-decode continuation,
  `gemini-nano-banana-2.1`, MiniMax M3.1, Moonshot `kimi-k3.1-pro`, Mistral
  Voxtral 3 speech/transcription and GLM 5.3 reasoning (`zai-glm-5-3`), xAI `grok-imagine-image-2.0`,
  BFL flux-3, Azure MAI-Image, and a regenerated AI Gateway catalog (removed
  IDs stay as deprecated constants).
- **32-bit platforms.** The SDK builds and vets on linux/386 and linux/arm,
  and CI checks both. The gateway package had failed to build on 32-bit
  since v0.5.2.
- **Faster code mode startup.** QuickJS is compiled once per process into a
  shared wazero runtime; each run still gets its own instance, memory and
  host bindings. A trivial run takes about 2.4 ms instead of 4.3 ms (Apple
  M1). wazero is now v1.12.0.

Also new:

- `ai.StreamTextLowMemory`: a single-consumer `StreamText` with a bounded
  buffer and backpressure.
- Tools in a step run concurrently by default, as in TS (see
  [Behavior changes](#behavior-changes)). Set `ToolCallConcurrency: 1` for
  sequential execution.
- Image prompt input for the Claude Code, Codex, OpenCode, Deep Agents, ACP
  and GitHub Copilot harness adapters.
- `types.FinishReasonInterrupted`, `ToolResultContent.IsError`,
  `GenerateResult.StructuredOutputText`, `ToolCall.InputEncoding`.
- OpenAI Responses: `promptCacheOptions` 12h TTL and prewarm, shell tool
  `timeoutMs`/`maxOutputLength`, `additionalTools` and
  `omitFromRequestTools`. Azure `ToolSearch` and per-deployment model
  settings. Mistral Conversations API. Groq browser search.
- MCP: per-request timeouts (`mcp.WithRequestOptions`),
  `InitializationOptions`, and an opt-in stdio `Shell` mode.
- Cohere `EmbeddingType` and Embed v5 dimensions. ElevenLabs music, sound
  effects and dialogue.

---

## Security

Most of these fixes were also released in v0.5.3. If you are on v0.5.2 or
earlier, upgrade to either.

- **Code mode: the TypeScript stripper is bounded.** It runs on model-written
  code before the sandbox starts, outside its timeout. Deep nesting, long
  `a<b,` lists and long runs of unterminated regex candidates could keep a
  core busy for tens of seconds per snippet. The work is now bounded, and
  source nested deeper than 256 levels is passed to the engine unstripped.
- **Code mode: sandboxed scripts no longer see Go stack traces** or host
  source paths when a host function panics or a WASM call fails. A panic in
  a host tool is a tool failure instead of a process crash.
- **Legacy `ToolLoopAgent` loop fails closed on approvals.** `Execute` and
  `GenerateAgent` ran a tool when its approval resolved to an unknown status
  (a typo such as `"aproved"`) or `ToolApproval` held an unsupported type.
  Both now fail the run with an error, as in `GenerateText` (see "Tool
  approval and tool-context errors fail the call"). The resolver is exported
  as `ai.ResolveToolApproval`.
- **Gladia no longer sends the API key to a foreign `result_url`.** The poll
  URL comes from the server's response; credentials now go only to the
  configured API origin.
- **Wrapper providers no longer send another provider's API key.** With no
  key of their own, Baseten, Cerebras, DeepInfra, Fireworks, Together,
  Vercel, Vertex MaaS and the Azure delegates fell back to `OPENAI_API_KEY`,
  and Azure DeepSeek to `DEEPSEEK_API_KEY`. Each now reads only its own key
  (see [Breaking changes](#breaking-changes-with-migration)).
- **Download SSRF blocklist.** `.internal` hostnames are blocked before DNS
  resolution, hostnames are mapped through IDNA (UTS #46) first so fullwidth
  forms such as `ｌｏｃａｌｈｏｓｔ` cannot bypass the checks, and a trailing
  dot (`localhost.`) no longer bypasses them.
- **MCP OAuth.** Metadata URLs with `javascript:`, `data:` or `vbscript:`
  schemes are rejected. The protected-resource check compares normalized
  paths, so `https://host/api/../admin` no longer counts as being under
  `https://host/api`. Pre-registered confidential clients must be bound to
  an issuer (see [Breaking changes](#breaking-changes-with-migration)).
- **Multipart uploads strip CR/LF from part header values**, so a file name
  cannot inject header lines (OpenAI and xAI file uploads, OpenAI batch).
  Low severity.
- **Vertex Gemini Live transcription** dials the host derived from the
  location, not `BaseURL`, and validates the location before sending the
  bearer token. Low severity.
- **Bedrock event stream** rejects a declared frame length over 16 MiB
  instead of buffering it.
- **Harness credentials.** ACP (and fx) authentication files are written
  with mode 0600 and must be relative paths without `..`. Local sandbox
  session directories are owner-only, and the new `Options.HostEnv`
  allowlist limits the inherited environment. Harness JSON parsing rejects
  `__proto__` and `constructor.prototype` keys.
- **Response-derived IDs** (ByteDance, KlingAI, fal transcription, Luma,
  Rev.ai) are encoded as single URL path segments, so an ID cannot change
  the request path. BFL no longer forwards custom headers to its image
  delivery hosts, and OpenAI-compatible image downloads send credentials
  only to a same-origin URL.

---

## Breaking changes, with migration

Each item has before/after code in the
[migration guide](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx).

### Compile-time changes

- **`GenerateObjectOptions.MaxRetries` and `StreamObjectOptions.MaxRetries`
  are `*int`**, like every other entry point and TS `maxRetries`. nil uses
  the default of 2, an explicit `0` now disables retries (before, `0`
  selected the default), and a negative value returns
  `*errors.InvalidArgumentError`.
  **Migration:** `MaxRetries: 5` becomes `retries := 5` and
  `MaxRetries: &retries`. Remove a `MaxRetries: 0` that was meant as the
  default.
- **`telemetry.Options.RecordInputs` and `RecordOutputs` are `*bool`, and
  unset now records** (TS `recordInputs`/`recordOutputs` default to true).
  This also applies to the deprecated `telemetry.Settings`.
  `DefaultSettings()`, `WithRecordInputs(bool)` and `WithRecordOutputs(bool)`
  keep their signatures; `ShouldRecordInputs()`/`ShouldRecordOutputs()`
  report the effective value.
  **Privacy:** in v0.5.x a hand-built `telemetry.Options{}` recorded
  nothing. In v0.6.0 the same literal records prompts, messages, tool
  arguments, generated text, objects and tool results in your spans.
  **Migration:** wrap literals in `telemetry.Bool(...)`, and set both fields
  to `telemetry.Bool(false)` wherever spans must not contain prompts or
  outputs.
- **Evaluate end-event struct literals use the Decide field names.**
  Experimental evaluate is now decide, as in TS. The old type names remain
  as deprecated aliases, so their embedded field is named after the new
  type: `telemetry.EvaluateEndEvent{DecideStartEvent: ...}`,
  `telemetry.EvaluationModelCallEndEvent{DecisionModelCallStartEvent: ...}`,
  `ai.EvaluateOnEndEvent{DecideOnStartEvent: ...}`. Reading promoted fields
  still compiles. Wire and telemetry names also changed (`ai.decide*`,
  `ai.decision.*`, `gen_ai.operation.name=decide`).
- **Unkeyed `acp.TextContentBlock` literals no longer compile.** The type is
  an alias of `acp.ContentBlock`, which adds `Data` and `MimeType` for image
  blocks. Use keyed fields: `acp.TextContentBlock{Type: "text", Text: "x"}`.

### Behavior changes that can break callers

- **A caller cancellation is not a `*ai.TimeoutError`** (review round 56).
  With `Timeout.Total` or `Timeout.PerStep` set, a cancelled context came
  back as a step or total `*ai.TimeoutError` (also in v0.5.x); it now comes
  back as the cancellation error, as TS rejects with the abort reason. A
  total deadline that fires during a step reports `Reason: total` instead of
  `step`. **Migration:** check `errors.Is(err, context.Canceled)` before
  `errors.As(err, &timeoutErr)`.
- **`WorkflowAgent` `PrepareStep` overrides carry over to later steps**, as
  TS's model-call iterator (`currentModel`, `conversationPrompt`,
  `currentGenerationSettings`): the next step's hook starts from the previous
  step's model, instructions, tool choice, active tools, runtime and tools
  contexts and generation settings. v0.5.x reset them each step in
  `Generate` and kept only the model and instructions in `Stream`. The
  `OnEnd` messages follow a `PrepareStep` messages or instructions override.
  In `Stream`, a `PrepareStep` `Headers` or `MaxRetries` now reaches the
  step's model call (it was ignored), and a negative `MaxRetries` fails the
  step with `*errors.InvalidArgumentError`, as in `Generate`.
  A `PrepareStep` `ActiveTools` narrows the call's active tools instead of
  widening them in `Generate`, and `PrepareCall`/`PrepareStep` see the
  agent's `ActiveTools` when the call sets none.
  A failed (not aborted) `Stream` run now fires `OnEnd` after `OnError`,
  with the steps completed so far, as TS stream mode does. When the run
  failed by a thrown error (a model call that failed to open, a failed
  `PrepareStep`), its `Messages` are the input messages only, as TS falls
  back to `prompt.messages`. A `PrepareStep` that returns different
  `Headers` replaces the call's, dropping the `ai-sdk-agent/workflow`
  user-agent suffix (TS). A run whose context is already cancelled fires
  `OnStart` before `OnAbort`.
  **Migration:** a hook that overrides a value for one step must set it back
  on later steps.

- **`MaxTokens` below 1 is rejected** with `*errors.InvalidArgumentError`
  ("maxOutputTokens must be >= 1") by `GenerateText`, `StreamText`,
  `GenerateObject`, `StreamObject`, batch text requests, `PrepareStep`
  overrides and the legacy `ToolLoopAgent`, as TS. v0.5.x sent it to the
  provider.
  **Migration:** leave `MaxTokens` nil, or set it to at least 1.
- **The legacy `ToolLoopAgent` rejects a negative `MaxRetries`**
  ("maxRetries must be >= 0") before the model call, as TS. v0.5.x
  silently made no retries.
  **Migration:** use 0 to disable retries.
- **Prompt validation matches TS `standardizePrompt`.** `GenerateText`,
  `StreamText`, `GenerateObject`, `StreamObject`, batch text requests and the
  agent calls return `*errors.InvalidPromptError` when neither or both of
  `Prompt` and `Messages` are set, or when `Messages` is an empty non-nil
  slice. v0.5.x sent an empty prompt, or ignored `Prompt` when `Messages`
  was set.
  **Migration:** pass exactly one of them.
- **`GenerateObject` and `StreamObject` reject system messages in
  `Messages`** unless `AllowSystemMessages` (alias `AllowSystemInMessages`)
  is set, as `GenerateText` already did. The error is
  `*prompt.UnsupportedSystemMessageError` with the TS text, now shared by
  all four entry points.
  **Migration:** put the system prompt in the new `Instructions` field (or
  `System`), or set `AllowSystemMessages: true` for trusted messages.
- **ACP lifecycle state persisted by v0.5.x does not resume.** As in TS, the
  persisted identity is now `agentIdentity`, and the identity, configuration
  fingerprint and authentication digest are computed the TS way (including
  the client app version, so TS too rejects state after a package upgrade).
  Old state fails with "agentIdentity must be a string" or "ACP lifecycle
  state is incompatible with the configured ...". Affects the Cursor, GitHub
  Copilot, Grok Build and fx adapters.
  **Migration:** discard saved ACP lifecycle state and start fresh sessions.
- **KlingAI `VideoModel()` accepts any model ID.** As in TS, an unknown or
  empty ID is reported by `DoStart` (and so `GenerateVideo`) as
  `*providererrors.NoSuchModelError`, not by `VideoModel()`.
  **Migration:** check the error from the call, not only from `VideoModel()`.
- **Code mode caps concurrent scripts.** Without a `SetMaxWorkers` call, at
  most floor(available memory / (`ExecutionPolicy.MemoryLimitBytes` +
  48 MiB)) invocations run at once, at least 1 and at most 32. A script over
  the limit fails at once with `*codemode.ConcurrencyError` ("Code mode
  maxWorkers limit reached (N)."). v0.5.x had no limit.
  **Migration:** raise the limit with `codemode.SetMaxWorkers(n)` (`0` keeps
  the v0.5.x unlimited behavior) or retry on `*ConcurrencyError`.
  `codemode.ResetMaxWorkers()` restores the default.
- **Code mode has a default 10 s CPU limit.** `ExecutionPolicy.CPUTimeoutMs`
  (default 10,000) bounds guest JavaScript time per invocation; time in host
  tools does not count. Exceeding it fails with `*codemode.CPUTimeoutError`.
  v0.5.x allowed pure JS to run up to the 30 s `TimeoutMs`.
  **Migration:** raise `CPUTimeoutMs` (and `TimeoutMs`) for long scripts.
- **OpenAI Responses `promptCacheOptions` is validated.** Only `mode`
  (`implicit`/`explicit`), `ttl` (`30m`/`12h`) and `prewarm` are sent;
  unknown keys are dropped, and invalid values such as `ttl: "24h"` fail
  before the request with `InvalidArgumentError`.
  **Migration:** use the documented values.
- **Failed generated-media downloads in BFL, Luma and fal return
  `*providererrors.ProviderError`**, unwrapped, with the HTTP status text as
  the message, as TS. They were a wrapped `*DownloadError`.
  **Migration:** match with `errors.As` on `*providererrors.ProviderError`
  and check `StatusCode`.
- **Wrapper providers read only their own API key.** Baseten, Cerebras,
  DeepInfra, Fireworks, Together, Vercel, Vertex MaaS and the Azure
  delegates no longer fall back to `OPENAI_API_KEY` (Azure DeepSeek no
  longer falls back to `DEEPSEEK_API_KEY`). A missing key fails with
  `LoadAPIKeyError`.
  **Migration:** set the provider's own variable (for example
  `FIREWORKS_API_KEY`, `TOGETHER_API_KEY`, `CEREBRAS_API_KEY`) or pass
  `APIKey`.
- **A failed harness stop-condition suspension fails the turn.** When a
  `StopWhen` condition is met in an `Agent` or `AgentSession` and the
  adapter's `DoSuspendTurn` returns an error (including
  `CapabilityUnsupportedError`), the turn fails with that error, as in TS.
  v0.5.x swallowed the error and finished the turn.
  **Migration:** handle the turn error, or drop `StopWhen` for adapters that
  cannot suspend.
- **MCP OAuth rejects unbound pre-registered confidential credentials.**
  Client information with a client secret (or custom authenticator) but no
  `Issuer` and no stored authorization server information fails `mcp.Auth`.
  **Migration:** set `Issuer` to the authorization server the secret was
  registered with, or return it from
  `OAuthAuthorizationServerInformationStore`.
- **`ToolChoice: none` is enforced.** A tool call under `none` fails with
  `*ai.ToolChoiceViolationError` (check with `ai.IsToolChoiceViolationError`)
  instead of running the tool. Any tool-choice violation ends the step with
  finish reason `error`.
- **Truncated streams finish with `interrupted`.** A provider stream that
  ends before its finish event reports `types.FinishReasonInterrupted`
  instead of `FinishReasonOther`, and the step runs no tools.
  **Migration:** check `FinishReasonInterrupted` if you detect truncation by
  finish reason.
- **`StreamText` keeps the provider request body only when asked.** As in
  TS (`include.requestBody` defaults to false), the step request, the
  `start-step` chunk and `Request()` carry the body only with
  `Include: &ai.IncludeOptions{RequestBody: true}`, the default
  `GenerateText` already had. The `GenerateText` step-end event's request
  and response bodies follow `Include` the same way.
  **Migration:** set `Include.RequestBody` (and `ResponseBody`) where you
  read the bodies.
- **An OPA HTTP policy response without `result` is not-applicable, not
  denied**, as TS. A tool call that no rule decides falls through to the
  other approval rules.
  **Migration:** policies that relied on "no match means deny" must return an
  explicit deny (for example a Rego `default` rule).
- **`EnrichSpan` and runtime context span attributes follow TS.** An
  `EnrichSpan` object, struct, `nil` or mixed-type array value is dropped
  instead of written as a JSON string, and a runtime context object is
  flattened into `ai.settings.context.<path>` attributes.
  **Migration:** queries on the old JSON-string attributes must read the
  flattened keys, or `EnrichSpan` must return primitives.
- **Perplexity embedding provider options are validated like TS.** A
  `dimensions` that is not a positive integer or an unknown `encodingFormat`
  fails with `*providererrors.InvalidArgumentError`.
  **Migration:** pass a positive integer and `base64_int8` or
  `base64_binary`.
- **A legacy `ToolLoopAgent` step timeout is `context.DeadlineExceeded`**,
  not `context.Canceled`, and a `PrepareCall` `Timeout` override now applies
  to the step, as in TS.
  **Migration:** check `errors.Is(err, context.DeadlineExceeded)` for step
  timeouts.
- **The legacy `ToolLoopAgent` per-step timeout covers the step's tool
  execution**, as the TS step timeout aborts the signal the tools get.
  v0.5.x ended the step deadline when the model call returned, so a slow
  tool could keep running past it. A tool that used to finish after the
  step deadline is now cancelled: its ctx is done at the deadline, and a
  step whose timeout expired while its tools ran ends the run with the
  step deadline (`context.DeadlineExceeded`) before the next step, even
  when the tool swallowed the cancellation.
  **Migration:** size `Timeout.PerStep` for the model call plus its tools,
  or bound the run with `Timeout.Total` or a timeout inside the tool.
- **UI message streams fail on an out-of-sequence chunk**, as TS: while
  `CreateUIMessageStreamWithOptions`/`ToUIMessageStream` build the message for
  an `OnEnd`/`OnStepEnd` callback, a chunk with no matching part (a
  `text-delta` without `text-start`, a tool result for an unknown call) stops
  the stream with a `*UIMessageStreamError` on the error channel and as the
  failed `OnEnd` outcome; it used to go to `OnError` and the stream went on.
  Without those callbacks the chunks are not processed at all, so `OnError`
  is no longer called for them, nor a second time for an error chunk.
  **Migration:** emit each part's start chunk before its deltas, and treat a
  `*UIMessageStreamError` on the error channel as the stream's failure.
- **An empty, non-nil `Telemetry.Integrations` means no integrations for
  that call** (owner decision 2026-10-28), as TS `integrations: []`. v0.5.x
  treated an empty slice like nil and used the globally registered
  integrations. nil still uses the global ones, and a non-empty list
  replaces them.
  **Migration:** to use the global integrations, leave `Integrations` nil
  instead of setting an empty slice.
- **Default telemetry tracer names match TS** (owner decision 2026-10-28).
  Without a tracer of its own, `LegacyOpenTelemetry` (and `GetTracer`) uses
  the global tracer named `"ai"` and the GenAI `OpenTelemetry` integration
  the one named `"gen_ai"`, as TS `trace.getTracer('ai')` /
  `trace.getTracer('gen_ai')`; v0.5.x used `"ai-sdk"`, and child spans
  (steps, tool calls, embed, rerank, decide) used a separate `"go-ai"`
  tracer. Child spans now use the root span's tracer, so a caller-supplied
  `Tracer` names every span of the call. New constants `LegacyTracerName`
  and `GenAITracerName`; `TracerName` is deprecated and unused.
  **Migration:** update instrumentation-scope filters or sampler rules that
  match `ai-sdk` or `go-ai`, or pass your own `Tracer`.
- **Tool approval and tool-context errors fail the call** (owner decision
  2026-10-28), as TS. When a tool approval cannot be resolved (an approval
  function whose tool context fails its `ContextSchema`, an unknown status,
  an unsupported `ToolApproval` value) or a tool context fails its
  `ContextSchema` before execution, `GenerateText` returns the error,
  `StreamText` errors the stream, and the agents and the legacy
  `ToolLoopAgent` loop fail the run. v0.5.x sent these to the model as
  tool-error results and continued. `GenerateText` also resolves every
  call's approval before running any tool, as TS. Errors returned by a
  tool's `Execute` still go to the model as tool errors.
  **Migration:** fix the tool context or approval configuration, or handle
  the returned `*errors.ValidationError` (field `tool context`).
- **The `WorkflowAgent` root span is `ai.workflowAgent.stream` for
  `Generate` too, and opens after the resumed history tools**, as TS, whose
  `generate` and `stream` share one path. v0.5.x named the generate root
  `ai.workflowAgent.generate`. The approved tools from the message history
  now run before the root opens, so the root's messages are the cleaned
  history and those resumed tool calls get no tool span (TS has no root for
  them yet); an error before that point reports no telemetry.
  **Migration:** update queries or dashboards that match
  `ai.workflowAgent.generate`.

---

## Behavior changes

These match TS and are unlikely to need code changes, but can change output.

### Core

- **SSE streams follow TS eventsource-parser 3.0.8 at the end of a
  stream.** A final event with no closing blank line (or a last line with
  no terminator) is dropped, as in TS; v0.5.x dispatched it. This applies to
  every SSE provider and the AI Gateway video reader, which now uses the
  shared parser (multi-line data joined, `[DONE]` skipped).
- **A call that fails before its loop starts still ends its steering
  run**, as TS: `GenerateText` and `StreamText` start the
  `ExperimentalSteering` run before any validation, so a reused steering
  object is the first error and waiting `Steer` handles resolve as not
  delivered (`error`) instead of waiting forever (review round 55).
- **`GenerateText` and the legacy `ToolLoopAgent` loop stop before the next
  step when the call was aborted after a step ended** (for example while a
  tool that ignores `ctx` ran), as TS `throwIfAborted`: no `PrepareStep` /
  `PrepareCall`, step start or step error is reported for that step. A
  `PrepareStep` `MaxOutputTokens` override below 1 is now rejected after the
  step's tools are prepared (and, in `GenerateText`, after the prompt is
  converted), as TS `prepareStepCallSettings` (review round 55).
- **Batch tool definitions are compared as TS does.** `ExperimentalStartBatch`
  compares each request's prepared tools (resolved `DescriptionFunc`
  descriptions, the JSON input schema, input examples, provider options,
  strict; provider tool ID and args) and no longer compares `Title` or
  `OutputSchema`. Requests are validated one at a time, so the first
  request's error is reported first (review round 55).
- **OpenAI-compatible streams (Fireworks, Together, Ollama, GMI Cloud, Vertex
  xAI, Z.AI) take the response ID, model and timestamp from the first event
  that has them**, skipping a placeholder event with empty values, as TS. With
  response headers set they used to come from the first event, so a
  placeholder made them empty (review round 55).
- **With `IncludeRawChunks`, the raw chunk for a payload that is not valid
  JSON has a nil `Raw`** (TS `rawValue: undefined`), in every SSE provider
  and Bedrock. It used to carry the payload text.
- **`StreamObject` keeps reading after a provider error part**, as TS. The
  first error part is the call's error (finish reason `error`), `OnError`
  fires once per error part, and the call's telemetry ends through `OnEnd`
  rather than `OnError`. `ObjectOnStartEvent.Schema` and `ai.schema` carry
  the wrapper schema the model is asked for in array and enum mode.
- **The legacy `ToolLoopAgent` ends its root span with the settings it
  started it with**, so a `PrepareCall` telemetry override no longer leaks
  it, and opens one when an override turns telemetry on.
- **Telemetry: the GenAI integration records no tool result for a tool
  without one, and `EnrichSpan` values are sanitized by Go kind** (empty
  arrays and byte slices dropped, pointers and named types read as their
  value).
- **A harness turn's result takes the raw finish reason and provider
  metadata of the adapter's terminal `finish`, also when it has none**, as TS
  `finish(input)` does, instead of keeping the last step's. A turn that ends
  without an adapter `finish` (host-input pause, stop condition, suspension)
  has a finish part carrying the last step's provider metadata and the summed
  step usage, like TS `finish()`.
- **A harness turn fails with the unclosed-step error only when the open step
  recorded content** (text, reasoning, a tool call, result or approval), as TS
  `currentStepContent.length > 0`. A step that only saw tool-input, source,
  file or replayed parts before the terminal `finish` no longer fails it.
- A harness step's `Response.Timestamp` is the time the step finished, as TS
  `finishStep` builds the response, not the time it opened.
- **A harness turn suspended before the adapter's `finish` still ends with a
  finish part** (finish reason `other`, the open step discarded), as TS
  `result.finish(undefined)`. `ToUIMessageStream` maps a call-level finish
  that follows step parts to the UI `finish` part only, with no
  `finish-step` and no closing of open text or reasoning blocks.
- **A legacy `ToolLoopAgent` `PrepareCall` telemetry override that switches
  integrations gives the new integrations a root span of their own** (reused
  if a later step switches back); every root ends with the call.
- **`GenerateObject` and `StreamObject` check the output settings before
  `MaxRetries`**, so invalid output settings are reported first, as in TS.
- **Telemetry:** the GenAI object root records the prompt in
  `gen_ai.input.messages`; SemConv message parts keep the TS key order
  (`{"type":...,"content":...}`); a provider-executed tool error that is not
  an `error` sets the tool span's error status without a message; the legacy
  integration records an empty object text as `ai.response.object` and
  `ai.response.reasoning` for any reasoning part, also one with empty text
  (new `TelemetryStepEndEvent.ReasoningParts`); harness telemetry joins
  reasoning parts with a newline.
- **fx:** the sandbox `realpath` output is decoded like Node
  `Buffer#toString('utf8')` (one U+FFFD per ill-formed sequence).
- **A harness turn's terminal finish opens no step**: after a suspended
  turn discards its open step the full stream no longer repeats
  `start-step` before `finish`, and a turn with no step ends with the
  `finish` part only. A general `NewStreamTextResultFromParts` stream whose
  only step ends directly in `finish` keeps its UI `finish-step` and
  `OnStepEnd`.
- **A harness turn paused for host input ends with the last step's finish
  reason** (`tool-calls` for the step closed at the pause, `other` before any
  step), like TS `result.finish()`, instead of an empty one.
- **`CreateUIMessageStreamWithOptions` injects the response message id into
  every `start` chunk that has none**, like TS: an earlier `start` with its
  own id no longer changes what later ones get, and an explicit empty id is
  kept.
- **A legacy `ToolLoopAgent` `PrepareCall` override that switches
  integrations keeps the loop's context for later steps** (total timeout,
  run id, tool-call telemetry and event call ids); only the new root's span
  comes from the sibling root.
- **Telemetry:** the legacy integration records `ai.response.reasoning` for a
  step (and call) whose only reasoning is a reasoning file, as TS counts
  reasoning-file parts (new `ReasoningFiles` on the step-end and finish
  events).
- **Telemetry: a canceled or timed-out `GenerateText` reports `OnError`, not
  `OnAbort`**, as TS `generateText` has no abort telemetry: the root and step
  spans carry the error status. The legacy `ToolLoopAgent` loop does the
  same. `GenerateTextOptions.OnAbortEvent`/`OnAbort` still fire.
- **Telemetry:** legacy `ai.prompt` records instructions given as system
  messages (`InstructionMessages`) as the message array
  (`{"system":[{"role":"system","content":...}],...}`), as TS
  `JSON.stringify({system: instructions})`, instead of their joined text. A
  string `System` is still a string. The legacy `ToolLoopAgent` root now
  carries `AgentConfig.InstructionMessages` (also the GenAI
  `gen_ai.system_instructions` of its root), and its steps pass the step's
  instruction messages to the chat span (`gen_ai.system_instructions`) and
  to the legacy step span's `ai.prompt.messages`.
- **Telemetry:** a harness continuation turn (no fresh prompt) records its
  empty message list like TS `messages: []`: legacy `ai.prompt` is
  `{"messages":[]}` and the GenAI root `gen_ai.input.messages` is `[]`
  instead of being left out.
- **Telemetry:** a harness step's legacy span records `ai.response.id`
  (`{callId}-{n}`), `ai.response.model`, `ai.response.timestamp` and the
  matching `gen_ai.response.*`, as TS `lifecycle.stepEnd` passes the step's
  response.
- **GenAI telemetry:** the text root and chat spans format the caller-facing
  messages like TS `formatModelMessages` (image parts as `uri`/`blob`, parts
  the TS converter does not map, such as an assistant custom part or a tool
  approval response, by type only); instruction messages are one
  `gen_ai.system_instructions` part each instead of system messages in
  `gen_ai.input.messages`; the chat span reports the root call's request
  settings, not a `PrepareStep` override; file data that is an http(s)
  string is a `uri` part. New `Messages` and `InstructionMessages` fields on
  `LanguageModelCallStartEvent` and `InstructionMessages` on
  `TelemetryStartEvent` carry the inputs.
- **fx `glob`:** braces expand like minimatch (`{1..3}`, `{a..e..2}`,
  padded and reverse sequences, `${...}`, `{a},b}`, at most 100000
  expansions), and patterns are preprocessed like Node `path.matchesGlob`
  (`.` and empty segments dropped, `a/../b` resolved, `**/..` and
  second-phase merges). A pattern over 64 KiB fails with "pattern is too
  long".
- **Telemetry ignores events for calls it never saw start.** As in TS, the
  OpenTelemetry integrations create call state only at an operation start.
  An event for an unknown call id no longer creates state or falls back to
  the span in `ctx`, which could end the caller's span or another
  integration instance's spans. The legacy `GenerateAgent` loop now sends
  the same `user-agent` tags as `Generate`.
- **Generated IDs use the TS formats.** Approval IDs come from the call's
  ID generator (`InternalOptions.GenerateID`), and the defaults are TS's:
  `aitxt-` plus 24 characters for text calls and the agent loop, `aiobj-`
  plus 24 for object calls, and 16 characters for `CreateUIMessageStream`
  message IDs. v0.5.x used `call-` plus 24 characters. Code that matches
  the `call-` prefix on these IDs must change; telemetry call IDs keep it.
- **A `StreamText` step that ends in an error part finishes with `error`**,
  also after a finish part or when an earlier step finished otherwise.
- **Tools in a step run concurrently by default** (TS `Promise.all`) in
  `GenerateText`, `StreamText`, `ToolLoopAgent` (including the legacy
  `Execute`/`GenerateAgent` loop), approval resumes and `WorkflowAgent`.
  Tool `Execute` functions and callbacks must be safe to run concurrently.
  Results stay in call order. Set `ToolCallConcurrency: 1` to run them one
  at a time.
- **Errors are returned as is**, without Go-added prefixes such as
  "... failed at step N:", "generation failed:", "output parsing failed:" or
  "failed to start stream:". Use `errors.As`/`errors.Is` instead of matching
  text. `InvalidArgumentError` text is now
  `Invalid argument for parameter <field>: <message>`, and
  `TypeValidationError` text is on one line.
- **`StreamTextResult.Text()`** (and the end event and telemetry text) is
  the final step's text, as TS `result.text`. `ReadAll()` still returns
  every step's text. The same applies to harness and external streams.
- **`OnFinishEvent` aggregates across steps** (usage, tool calls and
  results, sources, files, warnings) and adds `Content`, `FinalStep` and
  `ResponseMessages`.
- **`ToolResults` no longer holds bookkeeping entries** for calls awaiting
  approval, auto-denied calls or provider-executed calls without a result;
  read the approval content parts in `Content` instead.
- **`Usage.TotalTokens` is input + output tokens** for every provider, as TS
  computes it, instead of the wire `total_tokens`.
- **`ReasoningText` joins reasoning parts with no separator**, as TS.
- **Prompts that contain tool calls without results** fail with
  `*types.MissingToolResultsError` (provider-executed calls are exempt). A
  tool result that answers no tool call fails with "Tool call <id> not
  found."
- **`PrepareStep` `ActiveTools` is honored**, and `ToolLoopAgent` passes the
  full tool set so `PrepareStep` can widen it. `PrepareStep`
  `ProviderOptions` are deep-merged onto the call's options.
- **Stop conditions run only when the loop would otherwise continue.**
- **`StreamText` full stream** keeps `reasoning-start`/`reasoning-end` by
  default and no longer carries the Go-only markers (`stream-start`,
  `response-metadata`); `OnChunk` still receives them.
- **Callbacks and telemetry integrations that panic no longer break the
  call**, as TS ignores callback errors.
- **Telemetry spans follow the TS GenAI shape**, and telemetry `callId` is
  `call-` plus 24 alphanumerics.
- **The registry lists providers in registration order.**
- **The JSON Schema validator** enforces `if`/`then`/`else`, `contains`,
  `dependencies` and `dependentSchemas`; input that ignored them now fails.
- **`Rerank` and `Embed` accept an empty query, documents or input**, as TS.
- **JSON written to requests, telemetry and UI streams is not HTML-escaped**
  (`<`, `>` and `&` stay literal), as `JSON.stringify`. Query strings use the
  `URLSearchParams` byte set (`~` is encoded, `*` is not).
- **`Transcribe` returns an empty transcript** instead of an error when the
  model returns no text.
- **`GenerateImage` runs model calls in parallel** when `N` exceeds the
  model's per-call limit, and sums token usage across calls.

- **The legacy `ToolLoopAgent` loop runs the first step's `PrepareCall`
  before it opens its root span**, as TS `ToolLoopAgent` applies
  `prepareCall` before `generateText` starts: the root (`ai.generateText`
  start event), `OnStartEvent`, the total timeout and the tool-approval
  resumption use the prepared model, instructions, messages, settings,
  headers, retry count and tools, and step 0 runs with that configuration
  (`PrepareCall` is not called a second time for it). A first-step override
  that disables telemetry opens no root. The root's end event reports the
  last step's model, runtime context and tools context, like TS `onEnd`.

- **`GenerateText` and the legacy `ToolLoopAgent` loop notify each tool call
  and resolve its approval in one pass, call by call**, as TS
  `generateText`: `OnInputStart`, `OnInputAvailable`, then the approval of
  the first call before the next call's callbacks. The legacy loop now
  resolves every approval before running any tool, so an approval that
  cannot be resolved fails the run before a tool executes (tool calls run
  concurrently); input callbacks use the execution tool set, like TS
  `stepExecutionTools`.
- **An empty, non-nil `ActiveTools` leaves no tools active in the legacy
  `ToolLoopAgent` loop**, as TS `filterActiveTools` and `GenerateText`;
  v0.5.x ignored it and sent every tool. Only a nil `ActiveTools` keeps
  all tools. A tool call against such an emptied set is a
  `NoSuchToolError` ("Available tools: .") that `RepairToolCall` can
  repair, as in TS.
- **The `GenerateText`/`StreamText` per-step timeout starts before
  `PrepareStep`**, as TS arms its step timer at the top of each step: time
  spent in `PrepareStep` counts toward `Timeout.PerStep`.

### Providers

- An SSE event named `done` that carries a JSON payload is an ordinary chunk in
  every OpenAI-compatible and SSE provider stream; only `data: [DONE]` ends
  the stream, as TS `parseJsonEventStream` ignores event names. OpenAI
  Responses and Open Responses no longer retry the "returned no output" 500
  error (TS `isRetryable: false`) (review round 56).

- **Provider IDs follow TS `config.provider`**: `groq.chat`, `mistral.embedding`,
  `openai.image` and so on (they were the bare provider name). Telemetry
  `ai.model.provider` and `SerializedModel.Provider` change accordingly;
  provider option and metadata keys do not, and payloads serialized by
  earlier versions still restore.
- **Responses and stream chunks are validated against the TS schemas.**
  About 25 more providers reject a 2xx body that does not match with
  "Invalid JSON response", and malformed stream chunks become error parts.
  Some bodies accepted before now fail, such as Cohere's legacy plain-array
  `embeddings`.
- **Invalid provider options fail with `*InvalidArgumentError`** across chat,
  embedding, media and transcription providers, as TS `parseProviderOptions`.
- **A missing API key fails at request time with `LoadAPIKeyError`** for
  every provider that loads one in TS.
- **Unsupported model types return `*NoSuchModelError`** with the TS text.
- **Anthropic, Bedrock and Google `result.Content` is in response order.**
- **OpenAI Responses computer, local shell, shell and apply-patch tools, and
  Anthropic computer, text editor and memory tools, are client-executed.**
- **`googlevertex.New` falls back to Application Default Credentials** when
  no API key or credentials are set.
- **OpenAI-compatible providers** send assistant text as one string, replay
  reasoning as `reasoning_content`, and default `SupportsStructuredOutputs`
  to false.
- **Gemini uses its JSON response tool only for `gemini-` model ids** (TS
  `/(^|\/)gemini-/i`): a `.`-prefixed id such as `eu.gemini-2.0-flash` sends
  `responseJsonSchema` with the function tools.
- **Perplexity ignores loose `providerOptions.perplexity` keys named
  `temperature`, `top_p`, `max_output_tokens` or `response_format`**; use the
  call settings, as TS assigns those body keys from them. Perplexity
  embeddings decode url-safe and unpadded base64, as TS does.
- **Non-2xx responses with an empty or non-JSON body** use the HTTP status
  text as `ProviderError.Message` (`Bad Gateway`); the body stays in
  `ResponseBody`.
- **OpenAI chat streams fail the call on an error before output.** An error
  frame after the empty first `{role:"assistant",content:""}` chunk or a
  finish-only chunk now fails the call (retryable), as TS, instead of
  streaming an error part. OpenAI-compatible chat models are unchanged.
- **OpenAI Responses `IncludeRawChunks` yields one raw part per event.** Raw
  parts were duplicated after the first output event, and those of
  `response.in_progress` and unmodeled early events were dropped. The finish
  `responseId` comes only from `response.created`.

### Workflow, MCP, code mode and harness

- **`WorkflowAgent.Stream` has no default step limit** (TS defaults only
  generate to 20 steps). Set `StopWhen` to bound a stream.
- **MCP clients have no default request timeout**, JSON-RPC request IDs
  start at 0, `MCPClientError.Error()` is the bare message, and the HTTP and
  stdio transports probe `server/discover` first.
- **Code mode**: runaway recursion throws a catchable
  `InternalError: stack overflow`; TypeScript with `import`/`export` or an
  angle-bracket assertion runs unmodified, as Node does.
- **fx: the built-in `terminal` tool is removed**, as in TS;
  `inactiveTools: ["terminal"]` now returns an error.
- **Harness adapters identify as `ai-sdk-harness-<id>/<version>`** in
  `User-Agent` and `x-client-app`.
- **A failed OAuth refresh fails start** for Claude Code and Codex instead of
  being treated as "no credential".
- **With `AgentSettings.Telemetry` unset, the harness agent fires no
  telemetry**, even to globally registered integrations.
- **`harness.Agent.Generate(...).Usage` is the turn total.**
- **A harness host call for a builtin name is denied, not a turn failure.**
  As in TS, a host tool call is checked against the active user tools only.
  A host call that names a builtin other than `askUserQuestions` gets an
  `execution-denied` result with the builtin-filtering reason and the turn
  continues; before, the turn failed with "could not validate host tool".
- **Harness tool-execution-end callbacks see the runtime's result as sent.**
  As in TS, `OnToolExecutionEnd` and the model-call-end content get the
  provider-executed result before the session work dir is stripped (the
  consumer stream still sees the stripped copy), and a runtime tool error's
  text is the `getErrorMessage` of its value (JSON for an object) instead of
  Go `%v` formatting.
- **The `WorkflowAgent` root span's start event carries the call's
  messages, request settings (`maxOutputTokens`..`seed`), `maxRetries`
  (default 2), headers (tagged `ai-sdk-agent/workflow`) and tools context**,
  as TS `telemetryDispatcher.onStart`, so the legacy root records
  `ai.prompt`, `ai.settings.*` and `ai.request.headers.*` and the GenAI root
  its request attributes. `PrepareCall` now always runs before the root
  opens (it ran early only when approvals were resumed), so the root reports
  the prepared model and settings; messages a `PrepareCall` returns replace
  the prompt, as in TS.
- **The `WorkflowAgent` `OnEnd` event's `Messages` start from the
  conversation the loop ran on**, as TS: the instructions as a system
  message, the input messages after `PrepareCall` and the approval resume
  (resolved approvals replaced by their tool results, consecutive tool
  messages combined), then each step's messages. Drop the leading system
  message (or set `AllowSystemInMessages`) before passing them back as
  `Messages`.

---

## Fixes

The notable fixes are below. The full list is in
[`CHANGELOG.md`](../CHANGELOG.md). Fixes marked (v0.5.3) were also released
in v0.5.3.

### Core

- `toolbash` keeps its truncation notice past 2^31 output characters on
  32-bit builds.
- `StreamText` no longer writes a step's response messages into a caller's
  message slice (a `PrepareStep` result with spare capacity).
- `GenerateObject`/`StreamObject` sent the response format type as
  `json_schema`/`json_object` instead of `json`, so many providers dropped
  JSON mode or the schema. (v0.5.3)
- Tool parameters built with `schema.NewSimpleJSONSchema` reached providers
  as `{}`.
- Streams were cut off by the default HTTP client's 60 s timeout, and slow
  non-streaming calls failed. The default client has no whole-request
  timeout; bound calls with a context. (v0.5.3)
- Retries apply to every provider, not only the AI Gateway, and 400/401
  errors are no longer retried. Provider errors carry the real HTTP status,
  headers and body. (v0.5.3)
- `StreamTextResult.Chunks()` and element streams no longer leak their
  goroutine when the consumer stops reading. (v0.5.3)
- `ToUIMessageStream` no longer blocks forever after a failed callback, and
  a panicking `OnError` or metadata callback fails the stream instead of
  crashing the process.
- `StreamText` with `Timeout.PerStep` keeps a stream-open error as is, and
  later steps retry the stream open. (v0.5.3)
- The SDK reports its own version in `User-Agent` and telemetry (v0.5.1 and
  v0.5.2 said 0.5.0).
- `StreamTextResult.NewStream()` returns an independent stream, so the UI
  message stream works after the full stream was consumed.

### Providers

- Gemini, Google and Vertex `doGenerate` and batch results no longer fail
  on a stray `error` key (TS `responseSchema`).
- Gemini, Google and Vertex: a long-decode response continued over several
  requests reports the last request's body as `RawResponse`, not one with
  the usage summed across requests.
- AI Gateway: the language model speaks the V4 wire protocol; V4 responses
  failed to decode.
- Bedrock Anthropic (InvokeModel) streaming returned no content. Bedrock
  SigV4 signs model IDs containing `:` correctly (v0.5.3), and Bedrock Mantle
  is signed as `bedrock-mantle` (v0.5.3).
- Anthropic: `contextManagement` edits are sent with the API's snake_case
  names, and function tool schemas are sent untouched. `Generate` no longer
  puts text before thinking in `Content`, which broke the next turn with
  thinking enabled (v0.5.3).
- OpenAI chat streaming returns usage. (v0.5.3)
- OpenAI-compatible chat: file parts with only `FileData` produced empty
  image URLs. (v0.5.3)
- Alibaba and Moonshot dropped non-streaming tool-call arguments. (v0.5.3)
- Cohere called the wrong endpoints for embed and rerank. (v0.5.3)
- Gemini: parallel calls to one function no longer share an ID. (v0.5.3)
- `DoEmbed` no longer panics on a 2xx response with no embeddings (Cohere,
  Fireworks, Mistral, Ollama, Together, Voyage). (v0.5.3)
- Stability no longer panics on nil options, and the Replicate language model
  no longer panics on nil options (v0.5.3).
- Bedrock picks up rotated AWS credentials without a restart.
- Perplexity decodes fractional token counts, and its embedding model
  validates `dimensions` and `encodingFormat` like TS.
- Open Responses `Generate` returns a text part (with the item ID) for a
  refusal content part, as TS does.
- Google batch: an empty `metadata.state` stays pending, and the inline
  versus file-upload size check counts the whole creation body like TS.
- Gemini: a part with only a `thoughtSignature` no longer attaches it to the
  previous content, and a function call without a name is dropped.
- Google and Vertex requests always carry `generationConfig` (`{}` when
  nothing is set), as TS does.
- Google and Vertex `Generate` keeps tool calls and code execution parts
  that the API flags `thought: true`; they were dropped.
- Google and Vertex `providerMetadata.serviceTier` comes only from
  `usageMetadata.serviceTier`, as in TS; a top-level `serviceTier` in the
  response body no longer fills it.
- Gemini, Google and Vertex response parts keep only the fields of the first
  TS part shape they match: in `Generate` a part with a function call (or a
  valid `toolCall`) and text is only the call, as in the stream, and a
  `toolCall`/`toolResponse` without a string `id` or an `inlineData` without
  `mimeType` and `data` is dropped. With `IncludeRawChunks`, a failed
  long-decode continuation emits an empty raw chunk before its error.
- Open Responses ignores a stream payload that is valid JSON but not an
  object, and a `null` payload errors the stream, as TS. An xAI Responses
  stream without usage reports explicit zeros (`cacheWrite` 0), as its
  `Generate`.
- Anthropic streams map pre-populated `message_start` `tool_use` names through
  the provider tool-name map. Gemini treats a `functionCall` with `name: ""`
  as a call (new `FunctionCall.NameSet`) and, in `Generate`, a thought part
  with code execution as code execution. Google interactions decode a known
  event whose payload fails its shape loosely instead of failing the frame,
  keep empty step names and ignore `service_tier` on `interaction.created`.
  Bedrock clears the raw finish reason on a stream error and keeps an empty
  `redactedContent` and an empty `toolUse.name` in `Generate`
  (review round 56).

### Workflow, MCP, code mode and harness

- fx `glob`: the pattern compile (brace expansion and minimatch's
  preprocessing) stops when the tool call's context ends, returning the
  context's error; a pattern such as `{a,b}` repeated 17 times took tens of
  seconds of CPU regardless. Results are unchanged.
- fx `glob` with no files found returns `[]` without compiling the pattern,
  as TS (which compiles it per file in `path.matchesGlob`), so a too long
  pattern no longer fails an empty search.
- ACP mapping and authentication settings were serialized with Go field
  names and never took effect. (v0.5.3)
- A malformed ACP `askUserQuestions` tool result no longer crashes the host,
  and a turn that settled early no longer blocks the session. (v0.5.3)
- Code mode stripper: `!!x` lost one `!`. (v0.5.3)
- Code mode counts real sandbox thread CPU time on Linux, macOS and Windows.
- MCP `HTTPTransport` works again after `Close` then `Connect`.
- MCP `SSETransport`: a closed stream's reader no longer disturbs the stream
  of a later `Connect`, and a `Connect` interrupted by `Close` returns an
  error instead of hanging. `HTTPTransport.Close` closes stream bodies, so a
  custom `SSEClient` body that ignores the request context cannot hang it.
- MCP `HTTPTransport.Close` called from `OnError`, `OnSessionIDChange` or
  `OnSessionExpired` no longer deadlocks (v0.5.x too). `SSETransport.Send`
  reports token-load, encoding and OAuth refresh failures through
  `OnError`, and both transports' `Send` with an already-canceled context
  returns `ctx.Err()` itself, as TS `throwIfAborted` does.
- MCP `SSETransport.Close` during `Connect`'s GET aborts it, and `Connect`
  returns the transport-closed error. A second `Connect` in flight
  supersedes the first. A failed SSE connect request is reported through
  `OnError`, as in TS. `HTTPTransport.Connect` interrupted by `Close` returns
  `MCP HTTP Transport Error: Transport closed while starting` and leaves the
  transport closed. A `Connect` that starts while `HTTPTransport.Close` is
  still tearing down stays connected; before, the `Close` could mark it
  disconnected after it returned while its inbound SSE GET kept running.
  (v0.5.3)
- MCP `HTTPTransport` delivers no message and reports no error from an SSE
  stream after `Close` (including a `Close` from `OnError`) or a caller
  abort. `Close` no longer cancels, or waits for, an OAuth authorization
  that the inbound SSE GET started after a 401; its failure is reported with
  its own error, as in TS.
- `WorkflowAgent.Generate`/`Stream` keep the positional prompt when the
  options struct has an empty `Prompt`. (v0.5.3)
- Google and Vertex Interactions background streams stop after 3 empty
  reconnects instead of reconnecting forever. (v0.5.3)
- fx on libfx rejects `MemoryLimitBytes` above 4 GiB at `CreateFx` instead
  of crashing on the first turn.
- fx `bash` validates its `timeout` like Node `AbortSignal.timeout` (an
  integer number of milliseconds up to 4294967295) instead of overflowing
  for a huge value.
- Harness date parsing reads a local time skipped by a daylight-saving
  change with the offset before the change, as V8 does. Like Node, it also
  stops at a NUL character, reads U+2028/U+2029 as word characters, and
  accepts a UTC offset of up to 2^31-1 seconds.
- fx `glob` parses character classes like minimatch: an out-of-order range
  such as `[z-a]` matches nothing instead of failing the tool, `[a-a]` is
  the literal `a`, and `[[:print:]]` is no longer negated.
- The harness file reporter writes TS's record shapes: usage detail
  objects, an empty `modelId`, the prompt text as a string message, and a
  runtime tool error's value.
- MCP `NewHTTPTransport` no longer modifies the `HTTPClient` you pass
  (v0.5.2 set its `Timeout` and `CheckRedirect` in place, changing every
  other use of a shared client such as `http.DefaultClient`); it uses a
  copy with the MCP redirect policy and timeout.
- MCP `HTTPTransport` reports no read error from a POST event stream after
  `Close`, and ignores an inbound GET's `mcp-session-id` once the protocol
  is 2026-07-28, as TS.
- MCP `NewSSETransport` no longer modifies the `HTTPClient` you pass either
  (v0.5.2 set its `Timeout` and `CheckRedirect` in place); it uses a copy.
- MCP `HTTPTransport` and `SSETransport` with an `AuthProvider`: a request
  waiting on the shared OAuth recovery no longer fails with another
  request's cancellation; it runs the flow again, as TS `authorizeOnce` is
  tied to no request's signal. An inbound SSE reconnect resumes from the
  last event id seen when its backoff timer fires.
- MCP `SSETransport`: a 401 whose token re-read fails returns the
  `AuthProvider`'s `Tokens` error instead of running the OAuth flow, as TS.
  The shared OAuth flow and the `OAuthConfig` token refresh are released
  when a provider callback or `RefreshTokenFunc` panics, so later requests
  no longer hang on them.

---

## Known differences from TS

Most remaining differences come from Go zero values, which cannot always
express "unset" the way TS `undefined` does, and from Go maps, which have no
insertion order. The full list is in
[`docs/08-migration-guides/known-differences.mdx`](../docs/08-migration-guides/known-differences.mdx).
Notable ones:

- An empty API-key environment variable counts as missing
  (`LoadAPIKeyError`); TS sends the empty key.
- Several empty-string options and IDs are treated as absent (for example
  an empty xAI `pagination_token` or an empty OpenAI Responses
  `encrypted_content`).
- Where TS keeps insertion order, Go uses sorted order: query parameters,
  `Decide` failures (unless `QuestionOrder` is set), harness tool lists and
  some warnings.
- `codemode.SetMaxWorkers(0)` means unlimited (kept from v0.5.x); TS rejects
  0. On platforms other than Linux, macOS and Windows the default worker cap
  is 32 with no memory reservation.
- Media downloads fail with `DownloadError` where TS throws `APICallError`,
  except the BFL, Luma and fal generated-media downloads.
- `ProviderError.Error()` text differs from the TS `APICallError` message.
- Some Go extensions from v0.5.x are kept: Open Responses forwards options
  TS strips, Groq/DeepSeek/Moonshot/Z.AI read `openaiCompatible` options, and
  Mistral accepts a call-level `Language`.
- Streaming speech "closed before completion" errors omit the WebSocket
  close code and reason.
- `StreamText` returns an invalid `Instructions` value synchronously, as its
  other validation errors; TS reports it from the stream. `WorkflowAgent`
  streams reject `MaxTokens` below 1, which the TS WorkflowAgent stream
  path does not check.
- When a `StreamObject` stream itself fails (`Next()` returns an error), Go
  still runs the end callbacks and ends telemetry; TS skips them.
- When a `StreamObject` stream ends without a finish part, Go still parses
  the object; TS reports no object and leaves `object` unsettled.
- Google batch `Cursor` and `WebhookURL`, and the response `nextPageToken`,
  treat `""` as absent; Perplexity usage counts such as `12.5` are truncated.
- After a read error on its stream, MCP `SSETransport` is disconnected and
  `Send` fails with "Not connected"; TS stays connected, but replies over the
  dead stream never arrive.
- JSON payloads with a `__proto__` key or a `constructor.prototype` member
  are accepted. TS `secureJsonParse` rejects them to guard against
  JavaScript prototype pollution; Go `encoding/json` has no prototypes, so
  rejecting them would only turn valid JSON into errors.
- A `""` value in some typed string fields of provider responses (Gemini
  `finishMessage`, `serviceTier` and `continuationToken`, interactions
  signatures, `service_tier` and `mime_type`, Anthropic and Bedrock
  `stop_sequence`) counts as absent; TS treats `""` as present. An
  interactions `interaction.created` event with an empty id keeps the earlier
  id, which Go uses to reconnect.

---

## Requirements

- **Go 1.26 or later** (`go.mod` declares `go 1.26.0`).
- `github.com/tetratelabs/wazero` is v1.12.0 (was v1.9.0). If you pin
  wazero yourself, move it to v1.12.0.

---

**TS SDK parity target:** `ai@7.0.153`
**Base tag:** `v0.5.3`
