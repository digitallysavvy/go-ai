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
  approval and tool-context errors fail the call").
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

Every change that can need an edit is listed below, one line each, with a
link to its entry in the
[v0.5 to v0.6 migration guide](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx). Each guide entry says what
changed, how to tell whether you are affected, what to do, and its kind:
`compile error`, `silent behavior change`, `wire/protocol change` or
`error text only`. The guide starts with a quick-scan table of patterns to
search for and ends with a checklist for coding agents. Entries marked
(v0.5.3) also shipped in v0.5.3.

### Compile-time changes

- **GenerateObject and StreamObject MaxRetries is a pointer** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#object-maxretries-pointer)
- **Telemetry RecordInputs and RecordOutputs are pointers, and unset records** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-record-pointers)
- **Evaluate end-event struct literals use the Decide field names** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#evaluate-event-literals)
- **Unkeyed acp.TextContentBlock literals** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#acp-text-content-block)
- **`DecideOptions.Provider` is `interface{}`** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-options-provider)
- **Some structs are no longer comparable** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#non-comparable-structs)

### Behavior changes that can break callers

#### Core text and streaming

- **StreamTextResult.Text() is the final step's text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-text-final-step)
- **OnFinishEvent aggregates across steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#on-finish-aggregates)
- **Usage.TotalTokens is input + output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#total-tokens)
- **ReasoningText joins parts with no separator** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#reasoning-text)
- **StreamText OnError sees error parts only** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-onerror)
- **A StreamText PrepareStep model applies to one step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-preparestep-model)
- **StreamText fires OnLanguageModelCallStart once per attempt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-call-start-per-attempt)
- **StreamText OnChunk sees every error part before OnError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-onchunk-error-parts)
- **A retried StreamText step drops the failed attempt's raw finish reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-retry-raw-finish-reason)
- **StreamText keeps the request body only when asked** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-request-body)
- **The StreamText full stream drops the Go-only markers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-full-stream-markers)
- **Truncated streams finish with interrupted** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#truncated-streams-interrupted)
- **A caller cancellation is not a TimeoutError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#caller-cancel-not-timeout)
- **The per-step timeout starts before PrepareStep** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#step-timeout-before-preparestep)
- **MaxTokens below 1 is rejected** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#max-tokens-min)
- **Start events report a Prompt string as a user message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#start-event-messages)
- **A PrepareStep that sets only System changes the step instructions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#preparestep-system-override)
- **ExperimentalStreamTranscribe reports the cancel reason after Close** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-transcribe-cancel-reason)
- **Stop conditions run only when the loop would continue** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stop-conditions-only-when-continue)
- **Generated IDs use the TS formats** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generated-id-formats)
- **JSON is not HTML-escaped** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#json-not-html-escaped)
- **The final SSE event needs its blank line** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sse-final-event)

#### Prompts and validation

- **Prompt validation matches TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#prompt-validation)
- **GenerateObject and StreamObject reject system messages in Messages** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#object-system-messages)
- **Invalid instructions are an InvalidPromptError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-instructions)
- **Tool calls without results fail the prompt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#missing-tool-results)
- **The JSON Schema validator enforces more keywords** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#json-schema-validator)

#### Tools

- **Tools in a step run concurrently** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tools-concurrent)
- **An unset ToolChoice reaches the model as auto** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-auto)
- **ToolChoice none is enforced** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-none)
- **A StreamText tool choice violation ends with finish** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-violation-finish)
- **A provider-executed tool call is never run locally** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-executed-not-local)
- **Provider tool results stay in the assistant message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-results-assistant)
- **ToolResults holds no bookkeeping entries** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-results-bookkeeping)
- **Tool approval and tool-context errors fail the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-errors-fail-call)
- **PrepareStep ActiveTools and ProviderOptions are applied** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#preparestep-active-tools)
- **Some provider tools are client-executed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#client-executed-provider-tools)

#### Agents and approvals

- **Legacy ToolLoopAgent approval IDs are generated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-approval-ids)
- **Legacy ToolLoopAgent tool results list provider results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-results-provider)
- **Legacy ToolLoopAgent request messages leave out the instructions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-request-messages)
- **The legacy ToolLoopAgent rejects a negative MaxRetries** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-negative-maxretries)
- **A legacy ToolLoopAgent step timeout is context.DeadlineExceeded** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-step-timeout-deadline)
- **The legacy ToolLoopAgent step timeout covers tool execution** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-step-timeout-tools)
- **An empty ActiveTools leaves no tools active in the legacy loop** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-empty-active-tools)
- **The legacy ToolLoopAgent follows the PrepareCall tools, tool callers and output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-preparecall-tools-output)
- **AI Gateway authentication errors are wrapped** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-auth-error)
- **WorkflowAgent approval IDs are approval-toolCallId** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-approval-ids)
- **Resumed approval results carry their model output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resumed-output)
- **WorkflowAgent PrepareStep overrides carry over to later steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-preparestep-carryover)
- **A failed WorkflowAgent Stream run fires OnEnd** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-failed-onend)
- **WorkflowAgent Stream usage is input + output summed over the steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-usage)
- **The WorkflowAgent Stream root span ends with the run** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-root-span-end)
- **WorkflowAgent.Stream has no default step limit** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-no-step-limit)
- **The WorkflowAgent root span is ai.workflowAgent.stream for Generate too** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-root-span)
- **WorkflowRunMultiplexer ends on a finish chunk** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-multiplexer-finish)

#### UI message streams

- **UI message streams fail on an out-of-sequence chunk** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-out-of-sequence)
- **An out-of-range KeepAliveMs is an error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#keep-alive-range)
- **UI message state follows TS for four chunk edge cases** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-message-state-edge-cases)
- **ToUIMessageStream OnEnd reports the finish chunk's reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-on-end-finish-reason)
- **One error chunk for a StreamText run that ends with an error part** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-single-error-chunk)

#### Telemetry and events

- **An empty Telemetry.Integrations turns telemetry off for the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-integrations)
- **Telemetry spans come from the ai and gen_ai tracers** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tracer-names)
- **EnrichSpan and runtime context attributes follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#enrich-span-attributes)
- **Evaluate telemetry and event names are decide names** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-telemetry-names)
- **A cancelled GenerateText reports OnError to telemetry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generate-cancel-telemetry)
- **Step-start events report the call's tools and the output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#step-start-call-tools)
- **Telemetry OnStepError fires for every failed step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-step-error)

#### Errors

- **Errors are returned without Go prefixes** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#errors-unprefixed)
- **Error messages quote names as TS does** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#error-name-quoting)
- **An unknown decision model ID is a NoSuchModelError** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-unknown-model)
- **Registry resolution errors follow the TS registry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#registry-resolution-errors)
- **Non-JSON error bodies use the HTTP status text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#non-json-error-body)

#### Providers

- **Provider IDs follow TS config.provider** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-ids)
- **Responses and stream chunks are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#response-validation)
- **Invalid provider options fail the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-options-validation)
- **Anthropic, Bedrock and Google content is in response order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#result-content-order)
- **OpenAI Responses promptCacheOptions is validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-prompt-cache-options)
- **OpenAI chat streams fail the call on an error before output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-stream-error)
- **Wrapper providers read only their own API key** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#wrapper-api-keys)
- **Failed generated-media downloads return ProviderError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#media-download-errors)
- **KlingAI VideoModel() accepts any model ID** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#klingai-video-model)
- **Perplexity embedding provider options are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-embedding-options)

#### MCP

- **MCP OAuth rejects unbound pre-registered confidential credentials** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-issuer)
- **MCP clients have no default request timeout** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-no-default-timeout)

#### Code mode

- **Code mode has a default 10 s CPU limit** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-cpu-limit)
- **Code mode caps concurrent scripts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-max-workers)
- **wazero is v1.12.0** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#wazero-version)

#### Harness, ACP and fx

- **ACP lifecycle state persisted by v0.5.x does not resume** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#acp-state)
- **A failed harness stop-condition suspension fails the turn** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-stop-suspend)
- **Harness adapters identify as ai-sdk-harness-id** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-client-app)
- **With AgentSettings.Telemetry unset, the harness agent fires no telemetry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-telemetry-unset)
- **Harness usage is the turn total, and a failed OAuth refresh fails start** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-usage-oauth)
- **fx: the built-in terminal tool is removed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fx-terminal-removed)
- **fx has an opt-in libfx runtime** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fx-libfx-opt-in)

#### Policy

- **An OPA HTTP policy response without result is not-applicable** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#opa-undefined-result)

#### Already changed in v0.5.3

- **Cohere uses the v2 API (v0.5.3)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#v053-cohere-v2)
- **Every provider retries (v0.5.3)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#v053-retries)
- **The default HTTP client has no whole-request timeout (v0.5.3)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#v053-http-timeout)
- **A call to an unknown tool does not stop the loop (v0.5.3)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#v053-unknown-tool)

---

## Behavior changes

These match TS and can change output. The ones that can need an edit are
also listed, with a link to their migration entry, under
[Breaking changes](#breaking-changes-with-migration).

### Core

- **`StreamText` step performance is the model call's** (review round 63).
  `StepResult.Performance.ResponseTimeMs` and the rates built on it run from
  just before the model call to its finish part, as the
  `OnLanguageModelCallEnd` performance does; they no longer include tool
  execution or leave out the time to open the stream. `StepTimeMs` runs from
  after `PrepareStep`. Output timing follows chunk arrival. A step whose
  stream ended without a finish part reports only `StepTimeMs` and
  `ToolExecutionMs`. A stream retry fires `OnLanguageModelCallStart` again.
  So does each retried attempt to open the model stream (`MaxRetries`,
  review round 64), which also restarts the model call's clock; the step
  start still fires once.
- **`GenerateText` step performance starts at the model call** (review round
  64). As TS, `GenerateText` and the legacy `ToolLoopAgent` loop take one
  timestamp after `OnStepStart`, just before `OnLanguageModelCallStart`, and
  measure both `ResponseTimeMs` and `StepTimeMs` from it. `StepTimeMs` no
  longer includes `PrepareStep` or prompt conversion (file downloads), and
  `ResponseTimeMs` now includes the `OnLanguageModelCallStart` callback.
- **UI stream `MessageMetadata` sees the call's finish part** (review round
  63): the `finish` input's part carries the raw finish reason and total
  usage. A start chunk that gives the response another message id ends the
  continuation (`isContinuation` false, the original message kept).
- **A provider tool-approval-request in a generate response takes its call**
  (review round 60): in `GenerateText`, batch text results and the legacy
  `ToolLoopAgent` loop, the `ToolApprovalRequestContent` an MCP provider
  returns carries the parsed `ToolCall`, as in `StreamText` and TS
  `convertLanguageModelContent`; one that names no call of the response fails
  with `ToolCallNotFoundForApprovalError`. The legacy loop now pauses on such
  a request (it read `ToolCall.ProviderExecuted`, which was empty).
- **An approved call that does not run keeps its approval parts** (review
  round 63): in `GenerateText` and the legacy `ToolLoopAgent` loop, a local
  call whose `ToolApproval` is approved but that does not run (its tool has
  no `Execute`, or the step's finish reason does not allow execution) records
  its automatic `ToolApprovalRequestContent` and `ToolApprovalResponseContent`,
  as `StreamText` and TS do; the legacy loop also gives it a generated
  approval ID, in call order. It is still not a tool result, and the run
  stops after the step.
- **A legacy `ToolLoopAgent` `ToolApproval` denial without a reason leaves it
  unset** (review round 60), as `GenerateText` and TS do, so the provider's
  default denial text reaches the model instead of "Tool call to X was denied
  by ToolApproval policy." (the Go-only `ToolApprover` keeps that text).
- **A legacy `ToolLoopAgent` provider-executed call adds no null tool result**
  (review round 61): a call to a configured provider tool (web search, code
  execution) without an approval put a second `ToolResultContent` with a
  JSON `null` output into the step content and the assistant response
  message, next to the provider's own result. The step now holds only the
  provider's result, as `GenerateText` and TS `convertLanguageModelContent`.
- **Legacy `ToolLoopAgent` prompts leave out local approval parts** (review
  round 61): the tool-approval-request and -response parts of an
  automatically decided (or resumed) local approval reached the next model
  call, where the OpenAI Responses converter sent an `mcp_approval_response`
  for them. They are removed before the call, keeping a provider-executed
  call's approval response, as `GenerateText`, `StreamText` and TS
  `convertToLanguageModelPrompt` (WorkflowAgent's generate mode too).
- **`StreamText` terminal parts pass through `ExperimentalTransform`**
  (review round 59), as TS's outer-stream parts do: the `abort` chunk and the
  `error` chunks of an incomplete model stream, a step that fails to open or
  prepare, and a run that ends before its first stream (with its `start`).
  A stream retry whose model call fails to open now ends the step with an
  `error` chunk, `finish-step` (finish reason `error`) and `finish`, after the
  failed attempt's open text and reasoning blocks are closed; it used to end
  the run with no step. An aborted step no longer fires the telemetry
  `OnStepError` (TS skips it once the signal fired); the telemetry `OnAbort`
  ends its spans, and a later step aborted while it opened reports only
  `OnAbort`.
- **An `Output` response format that cannot be built fails the model call**
  (review round 59), as TS awaits it inside `doGenerate`/`doStream`: in
  `GenerateText`, `StreamText` and the legacy `ToolLoopAgent` loop the step
  start and the language model call start come first, and the step ends with
  `OnStepError`. `StreamText` used to ignore the failure on a later step.
- **Deferred provider tool results carry the tool's metadata** (review round
  59): a tool result or error with no call in the response takes
  `ToolMetadata` from the tool definition (TS `toolMetadata: tool.metadata`)
  in `GenerateText`, batch results and the legacy agent loop. Batch text
  results build their content like `GenerateText`: model tool results are
  provider-executed and carry their call's input. The legacy agent loop orders
  a step's tool outputs like `GenerateText` (outputs without approval, approval
  requests, approval responses, outputs with approval).
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
- **A `StreamText` run that fails before its first model stream opens** (an
  invalid prompt, a missing tool result, a failed approval resume, invalid
  `PrepareStep` instructions or settings) now calls `OnError` and its full
  stream carries `start` and an `error` chunk, as TS streamText reports these
  through the same error part as a model call that fails to open. A later
  step that cannot be prepared (its prompt conversion, `PrepareStep`
  instructions or settings) now ends the full stream with an `error` chunk
  too. `Err()` is unchanged. When the context is already canceled at that
  point (whatever the error), the run ends like TS's abort instead: before
  the first stream, `OnAbort` and a `start` and an `abort` chunk (also when
  the first model call fails to open after a cancel, which used to emit no
  chunks); at a later step, an `abort` chunk; no `OnError` and no `error`
  chunk either way. Telemetry integrations get `OnError` and then `OnAbort`
  for a run aborted before its first stream, as TS's pipeline catch.
- **xAI Responses generate keeps tool calls in output order** in the content:
  a function call ahead of a message stays ahead of its text, and a
  provider-executed call (image generation, web/file search) comes before its
  result.
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
- **Error text: names are quoted as TS template literals** (review round
  66). Messages that quote a name use `"a"b"` instead of Go `%q` escaping,
  so the text changes only for a name containing `"`, `\`, a non-ASCII or
  a control character; the error types are unchanged. Messages from v0.5.2:
  `CreateIDGenerator`'s separator error, `ExperimentalToolCallers` ("unknown
  tool", "contains an invalid caller"), tool search validation,
  `ExperimentalStreamTranscribe` for a model without streaming, decision
  (formerly evaluation) answer validation and the decision language
  model's `Question "<id>" ...` messages, and
  `DecisionUnsupportedQuestionTypeError`. The download URL check
  (`CreateURLDownload`, `GenerateVideo` downloads) writes a blocked hostname
  unquoted, as TS (`URL with hostname printer.local is not allowed`; review
  round 67).
- **`OnStartEvent.Messages` holds a `Prompt` string as one user message**
  for `GenerateText` and `StreamText` (v0.5.x: empty), as TS
  `initialPrompt.messages` (review round 66; see the migration guide).

### Providers

- xAI Responses follows TS's schemas for hosted tool output (review round
  59): a `web_search_call` action of an unknown type yields `{}`, a URL source
  with an empty URL is kept and a null `url` or `pattern` stays null; a
  `url_citation` with an empty URL is a source, one with a null title or a
  non-string URL is not, and a malformed annotation no longer drops the
  message text. Only the matched action type's fields are checked.
- Anthropic `DoGenerate` in jsonTool mode keeps a `compaction` block's text
  and puts the json tool's text in its block's place, after any server tool
  call or result before it, as TS (review round 59). A response with only the
  json tool is unchanged: the JSON is in `Text`. A Google Interactions
  tool-result `ImageContentBlock` takes TS's file-with-data path: a bare
  `image` or empty media type is sniffed and a non-image type is dropped with
  TS's warning.
- Anthropic `DoGenerate` in jsonTool mode builds `Text` from the content's
  text parts in order, so a json tool text ahead of a `compaction` block
  stays first (review round 60). Outside jsonTool mode `Text` is unchanged.
  Google Interactions: a stringified assistant tool-call input with a
  `__proto__` key or a `constructor.prototype` member is sent as
  `{value: input}`, as TS `safeParseToolArgs` (review round 60).
- The Anthropic stream emits a finish chunk on every `message_stop`, built
  from the stream-wide finish reason and usage, including a repeated
  `message_stop` with no new `message_start` (review round 61, TS doStream).
  Google Interactions compaction also links an assistant turn through a
  tool-result or reasoning-file part's `interactionId` (TS checks every
  part), and static video offsets use JavaScript number formatting (`1e-7s`,
  `1e+21s`, `0s` for negative zero), as TS's template literal (review round
  61).
- An Anthropic `message_stop` with no earlier `message_start` or
  `message_delta` reports `providerMetadata.anthropic.usage: null` and TS's
  initial usage object (zero token counts, `iterations: null`) as
  `usage.raw` (review round 62, TS `rawUsage ?? usage`).
- An SSE event named `done` that carries a JSON payload is an ordinary chunk in
  every OpenAI-compatible and SSE provider stream; only `data: [DONE]` ends
  the stream, as TS `parseJsonEventStream` ignores event names. OpenAI
  Responses and Open Responses no longer retry the "returned no output" 500
  error (TS `isRetryable: false`) (review round 56).
- **OpenAI Responses and Open Responses `DoGenerate` keep client tool calls
  in output order in `Content`** (TS doGenerate): a function call that came
  before a message is no longer moved after its text, so the next step's
  assistant message replays the items in the order the model produced them
  (review round 57).

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
- **`WorkflowAgent.Stream` emits approval requests after the step's tool
  results** (review round 61): it resolves a step's tool approvals after the
  model stream and writes the `tool-approval-request` parts after the tool
  results, as TS WorkflowAgent (`writeToolResults`, then
  `writeApprovalRequests`). The step content and the assistant message list
  the approval requests after the calls and results, as in generate mode.
  Earlier, each request came right after its tool call (StreamText's order).
  New `InternalOptions.DeferApprovalRequests`.
- **`WorkflowAgent.Generate` forwards a provider-executed denial as is**
  (review round 61): resuming a denied provider (MCP) approval sent the
  model an extra `execution-denied` tool result next to the forwarded
  approval response, because the legacy loop resumed the approvals a second
  time. TS WorkflowAgent forwards only the response, as `Stream` already did
  (new `InternalOptions.ApprovalsResumed`).
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

- The UI message stream's `MessageMetadata` callback runs once per stream
  part, as TS does; the `start` part of a `StreamText` stream called it twice.
- The legacy `ToolLoopAgent` loop (`GenerateAgent`, `Execute`,
  `ExecuteWithMessages`) gives `OnStart` and `OnStepStartEvent` the call ID
  that its end event carries, and the call's operation, active tools, tool
  order, tool choice, timeout, reasoning, retries, headers and output, as TS
  generateText's start events do; they had no call ID.
- `BatchTextRequest.AllowSystemInMessages` (additive) lets a batch text
  request carry system messages in `Messages`, as TS `allowSystemInMessages`
  does; without it they are still rejected.
- `BatchTextRequest.Instructions` and `InstructionMessages` (additive) give a
  batch text request its instructions, as TS `TextBatchRequest` takes
  Prompt's `instructions` (a string or system messages with provider
  options). A batch request's prompt checks (prompt or messages,
  instructions, system messages without `AllowSystemInMessages`) run before
  its tools and settings are checked, as TS `startBatch` runs
  `standardizePrompt` first (review round 65).
- The legacy `ToolLoopAgent` loop (`GenerateAgent`, `Execute`,
  `ExecuteWithMessages`, `WorkflowAgent.Generate`) calls
  `AgentConfig.OnStepError` when a step that started fails (the model call,
  the prompt, tool input callbacks, approvals or tool execution), with the
  step-start event and the error, as TS generateText's `onStepError`; it was
  never called. Its telemetry step end carries the model call's performance,
  as `GenerateText`'s does (review round 65).
- `UIMessageStreamError` messages quote the part, tool call or approval ID
  as TS template literals do (`ID "a"b"`), without Go escaping (review
  round 65).
- The legacy `ToolLoopAgent` loop (`GenerateAgent`, `Execute`,
  `ExecuteWithMessages`) returns an unauthenticated AI Gateway error wrapped
  with the message that says how to authenticate, as TS generateText
  rethrows `wrapGatewayError(error)`; the original error stays reachable
  with `errors.As`. Telemetry `onError` still gets the original error
  (review round 66).
- `GenerateText` and `StreamText` start events (`OnStartEvent.Messages`)
  carry the standardized messages, so a `Prompt` string is reported as one
  user message, as TS `initialPrompt.messages`; they were empty for a
  `Prompt` call. `OnStartEvent.Prompt` is still set (review round 66).
- The legacy `ToolLoopAgent` loop reports `InstructionMessages` as the
  instructions: when they are set, the start, step-start and model-call
  events have an empty `Instructions`, as `GenerateText` reports them (TS
  `instructions` is one value) (review round 66).
- The UI message state behind `OnEnd`/`OnStepEnd` and `ReadUIMessages`
  follows TS `processUIMessageStream` in three edge cases: a
  `tool-input-error` chunk's `title` is not applied, a data chunk with an
  empty `id` updates the data part with that id, and a `start` chunk's empty
  `messageId` is applied (review round 66).
- The legacy `ToolLoopAgent` loop checks a response's deferred provider
  tool result against the step's prepared tools, not only the tools active
  in that step, as TS checks it against the call's `tools`: a
  `SupportsDeferredResults` provider tool left out of `ActiveTools` no longer
  fails the step with "Tool call <id> not found.", and the result gets the
  tool's metadata (review round 66).
- Error messages that quote a name use TS template-literal quoting (`"a"b"`)
  instead of Go `%q` escaping: decision answer validation (`Question "<id>"
  ...`), `DecisionUnsupportedQuestionTypeError`, `CreateIDGenerator`'s
  separator error, `ExperimentalToolCallers` and tool search validation,
  `ExperimentalStreamSpeech`/`ExperimentalStreamTranscribe` for a model
  without streaming, and the WorkflowAgent missing provider result warning
  (review round 66).
- UI message `source-url` and `source-document` chunks carry the source's
  own `providerMetadata` (for example Anthropic's `pageAge`), as TS does;
  only chunk-level metadata was forwarded.
- `toolbash` keeps its truncation notice past 2^31 output characters on
  32-bit builds.
- `GenerateText`'s end event reports the final step's response headers when
  the run ends on a tool-call step (it had none), and `ResponseHeaders`
  follows the final step.
- `GenerateText` accepts a deferred provider tool result whose tool
  `ActiveTools` leaves out of the step ("Tool call <id> not found." before),
  as TS checks the call's tools.
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
- `ToUIMessageStream` calls `OnEnd` once with a failed outcome carrying the
  error when a `MessageMetadata` or `OnError` callback panics, as TS reports
  those failures (review round 67).
- `StreamText` error parts for a failed prompt, step preparation or model
  call open carry the error itself; only `OnError` gets it through
  `wrapGatewayError`, also for a later step that fails to prepare (review
  round 67).
- `ExperimentalDecide` telemetry end events carry every start field (state,
  questions, max retries, headers), as TS `{ ...startEvent, ...result }`,
  and `DecisionRefusalError` quotes question IDs as `JSON.stringify` (no
  `\u003c` or U+2028 escaping) (review round 67).

### Providers

- The Vertex Gemini image model infers a URL input image's media type from
  the URL path: a query string or fragment after the extension
  (`y.png?sig=1`, `gs://b/y.webp#v`) no longer falls back to `image/jpeg`.
  A caller-supplied full media type still wins (review round 66).
- A failed Vertex Gemini image call's `ProviderError.Provider` is
  `google.vertex.image`, the image model's provider (it said
  `google.vertex.chat`), as TS builds the image model's inner language model
  with the image model's provider.
- `google.Config` and `googlevertex.Config` take a `GenerateID` (TS
  `generateId` provider setting). It reaches the chat, Vertex image and
  Interactions models, which use it for tool calls without an id, sources
  and processing steps, and to the Google batch, which names the batch
  `ai-sdk-batch-<id>`; before, only `InteractionsConfig.GenerateID` could
  set it.
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
- Anthropic streams keep `contextManagement` stream-wide, as TS: a later
  message's finish without its own `context_management` reports the earlier
  value (the Go-only `usage.context_management` fallback stays). The stream's
  raw `message_delta` usage drops unknown keys in `iterations` and
  `output_tokens_details`, as `Generate` does. Bedrock streams treat a
  `toolUse` delta without a string `input` as a `reasoningContent` delta;
  without tools, the prompt is filtered only when it holds tool calls or
  results (including legacy `Message.ToolCalls`), so empty messages are no
  longer dropped otherwise; an empty `additionalModelRequestFields` option is
  sent as `{}`. Google interactions compaction drops the results of inline
  tool-call parts, a thought summary item without a string `text` is skipped,
  and an image tool-result file with text data is dropped with TS's warning
  (review round 57).
- Google interactions pick a file part's block kind from the exact
  top-level media type, as TS `getTopLevelMediaType`: `images/png` or
  `applications/json` is dropped with the unsupported-media-type warning
  instead of being sent as an image or document. Bedrock tools see the
  reasoning budget after resolution, so a disabled `reasoningConfig` with
  `budgetTokens` under a portable `Reasoning` level no longer sends Anthropic
  tool fields and betas for an application inference profile. An Anthropic
  stream whose first part is an unparsable or schema-invalid frame fails
  `DoStream` with TS's plain "Expected a normalized Anthropic stream error"
  (not a status-500 `*ProviderError`); it unwraps to the frame's
  `*JSONParseError` or `*ValidationError` (review round 58).
- Google and Vertex Gemini image models send a caller's empty
  `imageConfig: {}` as `generationConfig.imageConfig: {}`, as TS
  (`userImageConfig != null`); it was dropped (review round 65).

### Workflow, MCP, code mode and harness

- `workflow.ToUIMessageChunk` forwards a source's own `providerMetadata`
  and always writes a document source's `title` (required by the UI message
  schema), as TS does.
- `WorkflowChatTransport`: a reconnect stream that fails after its `finish`
  chunk (a malformed frame or a read error) counts as a consecutive error, as
  TS's catch does, so reaching `MaxConsecutiveErrors` still fails the
  reconnect (review round 60).
- `WorkflowChatTransport` and `WorkflowRunMultiplexer` return a missing
  `x-workflow-run-id` header or an `OnChatSendMessage` error at once instead
  of reading the rest of the run's stream first (TS throws). The multiplexer
  puts at most 4 KiB of a failed response's body into its error (review round
  59).
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
  TS strips (and keeps the function-call `namespace` key in tool-call
  provider metadata), Groq/DeepSeek/Moonshot/Z.AI read `openaiCompatible` options, and
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
  rejecting them would only turn valid JSON into errors. This covers
  provider responses and stream chunks only: Google Interactions prompt
  conversion follows TS and sends such a stringified tool-call input as
  `{value: input}`.
- The Gemini prompt converter accepts a string tool-call input with a
  `__proto__` key or a `constructor.prototype` member for `code_execution`
  and server tool calls; TS `secureJsonParse` throws there. In Go the input is
  always the wire text, also where TS holds an object and does not parse it,
  so throwing would reject inputs TS accepts (coordinator ruling, round 61).
- A `StreamText` tool whose `ContextSchema` rejects its tool context ends the
  run with the `ValidationError` (no `finish-step`, `finish` or `OnEnd`;
  `OnError` once), as `GenerateText` and the agents fail the call. TS
  enqueues an `error` part and finishes the run (owner decision).
- A `""` value in some typed string fields of provider responses (Gemini
  `finishMessage`, `serviceTier` and `continuationToken`, interactions
  signatures, `service_tier` and `mime_type`, Anthropic and Bedrock
  `stop_sequence`) counts as absent; TS treats `""` as present. An
  interactions `interaction.created` event with an empty id keeps the earlier
  id, which Go uses to reconnect.
- When the source stream of `ToUIMessageStream` fails (not an abort), Go
  writes one standard `error` chunk (masked text unless `OnError` says
  otherwise), ends the stream cleanly and returns the error; TS errors the
  stream and writes no `error` chunk. Same API and chunk protocol as TS, and
  `useChat` shows the error text. Caveat: client logic that reacts to a
  dropped connection (for example reconnect-on-network-error) sees a clean
  end with an `error` chunk instead (v0.5.x behavior, owner decision). The
  SSE helpers then write `data: [DONE]` and
  `CreateUIMessageStreamResponseWithInit` closes the body cleanly; TS writes
  no `[DONE]` and errors the body.
- Go's single `InvalidArgumentError` type always starts its message with
  "Invalid argument for parameter ..."; TS's provider-package
  `InvalidArgumentError` has the bare text (coordinator ruling, round 67).
- Go does not check a model's specification version (TS throws
  `UnsupportedModelVersionError`), and the `Decide` telemetry events carry a
  subset of the TS fields (Go design, round 67).
- A `WorkflowAgent.Stream` run cancelled mid-stream ends its
  `ai.workflowAgent.stream` root with `OnAbort`; TS reports a thrown abort to
  the root `onError` and leaves the root open for an abort its iterator
  returns. Go fires the root telemetry `OnError`/`OnEnd` after the user
  `OnError`/`OnEnd`; TS interleaves them (coordinator ruling, round 68).
- UI tool parts cannot tell an unset `dynamic` from `dynamic: false` (Go
  `bool`), so Go writes `dynamic` only when true; TS can write
  `dynamic: false` for a tool missing from the tool set.
- The Vertex Gemini image model accepts URL and `gs://` input images and
  sends them as `fileData` with the caller's media type, else one inferred
  from the URL path extension (query string and fragment ignored), else
  `image/jpeg`, as v0.5.2 accepted them and TS before #14733 sent them.
  Current TS rejects them: it labels URL inputs `image/*` and its
  `resolveFullMediaType` (#14733) does not guess a subtype for data that is
  not inline. Set `MediaType` explicitly for a URL without an image
  extension (owner ruling).
- Google Interactions: a non-streaming `function_call` step without an
  `id` gets an ID from the provider's ID generator
  (`Config.GenerateID` or `InteractionsConfig.GenerateID`, else random); TS
  keeps the empty ID, and
  v0.5.2 used a per-response `call_N` counter that repeated across steps.
  Go matches tool results by ID (coordinator ruling, round 62). The stream
  fallback `<interactionId>:<index>` (`interaction:<index>` with
  `store: false`) matches TS and can repeat across steps under
  `store: false`.

---

## Requirements

- **Go 1.26 or later** (`go.mod` declares `go 1.26.0`).
- `github.com/tetratelabs/wazero` is v1.12.0 (was v1.9.0). If you pin
  wazero yourself, move it to v1.12.0.

---

**TS SDK parity target:** `ai@7.0.153`
**Base tag:** `v0.5.3`
