# Go AI SDK v0.6.0 Release Notes

Release date: set at tag time.

## Overview

v0.6.0 tracks TypeScript AI SDK parity target **`ai@7.0.170`** (TS `2ebc78b654`; up from
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
  `MaxOutputTokens`, `Reasoning` and a typed `DecisionProvider` on
  `DecideOptions` (the v0.5 `Provider` field stays, deprecated), `QuestionOrder` and
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
- **`ToolErrorContent.Err` holds the typed tool error** (TS `tool-error` `error` is the thrown value).
- **`StepResult.ReasoningFiles()` returns the step's reasoning file parts** (TS `StepResult.reasoning` holds reasoning-file parts).
- **`OnStepFinishEvent.ExperimentalSteeringMessages`** carries the step's steering messages (TS `onStepEnd` StepResult).
- **`openaitool.ToolSearchOptions.Parameters`** holds the client-mode search schema (TS `args.parameters`).
- **`workflow.SerializeToolSetWithOptions`** passes the tools context and sandbox to each tool's `DescriptionFunc` (TS `serializeToolSet(tools, {toolsContext, experimental_sandbox})`).
- **`anthropic.ProviderToolID`, `anthropic.PrepareProviderTools` and `anthropic.PreparedProviderTools`** select Anthropic provider tools by id and run the Anthropic tool preparation for a host API such as Bedrock (TS `tool.id`, `prepareAnthropicTools`).
- **`responses.FunctionToolDef`, `responses.ShellToolDefEnvironment` and `responses.ShellNetworkPolicy` `MarshalJSON` methods** send an empty, non-nil `AllowedCallers`, `FileIDs`, `Skills`, `AllowedDomains` or `DomainSecrets` list as `[]`; nil is still omitted (TS forwards these lists as given).
- **`types.Tool.ValidateInput`**: a tool can validate its parsed input with its own function in place of the JSON Schema in `Parameters` (TS `jsonSchema(schema, {validate})`); a failure makes the call invalid, with the TS `TypeValidationError` wording in the tool error. GenerateText, StreamText, the legacy loop, the repair path, approval revalidation and `ValidateUIMessages` use it. Tools rebuilt by `workflow.ResolveSerializableTools` carry the Ajv-worded validator TS attaches.
- **`anthropic/tools.BashSandboxOutput`** is the default result of `Bash20250124()` / `Bash20241022()` when they run through the call's `providerutils.SandboxSession` (TS `sandbox.run()` result, `{exitCode, stdout, stderr}`).
- **`deepseek.Config.UncheckedStrictToolCalls`** skips the DeepSeek strict tool call checks, as TS leaves `supportsStrictToolCalls` unset; Azure DeepSeek models set it (TS `createDeepSeekModel`).
- **`types.ToolResult.InvalidToolCall`** (`json:"-"`) marks the tool error the SDK synthesizes for an invalid client tool call; StreamText gives that tool-error part the call's title (TS `streamLanguageModelCall`).
- **`types.EmbeddingUsage.Reported`** (`json:"-"`): set by an embedding provider when the response carried a usage object; telemetry then records a reported 0 (TS `usage.tokens: 0`).
- **`telemetry.DiagnosticEventOnStepError`** (`"onStepError"`): the diagnostic message `FireOnStepError` publishes (TS `onStepError`).
- **`types.ToolApprovalRequestContent.InputSchemaInputSet`** (`json:"-"`): marks a nil `InputSchemaInput` as a present JSON null (TS `hasOwnProperty`), set by `ConvertToModelMessages` and honored by approval revalidation.
- **`streaming.StreamingToolCallTracker.Failed`** reports that a delta raised the error TS throws from `processDelta` (a nameless or mistyped new call); callers end the stream there. **`BufferNamelessIndexedCalls`** makes indexed nameless deltas wait for their name, as TS openai-compatible `pendingToolCalls` (review round 81).
- **`streaming.ToolCallDelta.IDPresent` / `MetadataPresent`** mark a wire delta that carried an `id` / `extra_content`, even empty, so the OpenAI-compatible tracker keeps the first non-null one as TS `pendingToolCalls` (review round 82).
- **`schema.StructValidator` JSON Schema**: `JSONSchema()` / `MarshalJSON` of a struct schema generate the struct's JSON Schema by reflection (TS `zodSchema(z.object(...))` shape; `jsonschema:"description=..."` and `jsonschema:"required"` tags); `schema.OrderedParameters` returns it as an `*OrderedSchema` (review round 82).
- **`openai.Config.MetadataExtractor`** (`openai.MetadataExtractor`, `openai.StreamMetadataExtractor`): TS openai-compatible `metadataExtractor`, for `OpenAICompatible` chat models; generate reads the parsed body, the stream extractor sees every parsed event and builds the finish metadata (review round 82).
- **`prompt.DecodeUTF8Text`** (`pkg/providerutils/prompt`): TS `new TextDecoder().decode(bytes)` for text bytes (leading BOM dropped, one U+FFFD per maximal invalid subpart); used for text file parts across providers and the web fetch tool (review round 83).
- **`prompt.ValidateToolChoiceType`** (`pkg/providerutils/prompt`): the TS `prepareTools` exhaustive check for a custom provider: with tools, a tool choice type other than auto/none/required/tool is an `UnsupportedFunctionalityError` `tool choice type: <type>` (review round 83).
- **`types.FileData.OriginalURL`** (JSON `originalUrl`): the caller's URL string when its WHATWG serialization differs (TS `data.originalUrl`); Google/Vertex send it for `gs:` URLs (review round 84).
- **`ai.TimeoutError.Timeout` / `ErrorName()`**: the configured duration that elapsed (the error then reads like the TS abort reason) and the TS error name `TimeoutError` (review round 84).
- **`prompt.InvalidURLError`** (`pkg/providerutils/prompt`): TS `new URL()`'s `TypeError: Invalid URL` for a URL file part that does not parse (review round 84).
- **`prompt.DeprecatedContentWarnings`, `prompt.ImageContentPartDeprecation`** (`pkg/providerutils/prompt`): the TS `deprecated` warnings for a message's `image` content parts (review round 84).
- **`providerutils.MapReasoningToProviderEffort` rounding, `IsCustomReasoning`, `MapReasoningToProviderBudget` / `ReasoningBudgetOptions`**: the TS `map-reasoning-to-provider` helpers (TS 61a315e333): a provider-option effort wins, otherwise the nearest lower supported level (higher only when none is lower, never `none`); budgets use the TS percentages (round 85 prep).
- **`providerutils.APIKeyFunc`, `ResolveAPIKey` / `ResolveAPIKeyOptions`, `RequestAuthHeaderFunc`**: per-request credentials (TS 7716e189ee `Resolvable<string>` API keys): a func is called with each request's context, its error is returned unchanged, setting it together with the static key is an `InvalidArgumentError`; providers adopt it field by field (round 85 prep).
- **`(*ai.Steering).Queue`, `QueueMessage`**: a follow-up message delivered once the model finishes its current task (TS cf2796985a `steering.queue()`), for GenerateText, StreamText and ToolLoopAgent (review round 85).
- **`telemetry.OpenTelemetryOptions.MeterProvider`, `telemetry.InferenceDurationMetricName`, `telemetry.GenAIMeterName`**: the `gen_ai.client.inference.duration` histogram (TS 59465581b0) (review round 85).
- **`prompt.SecureJSONParse`, `prompt.JSONStringToolInput`, `prompt.SerializeToolCallArguments`** (`pkg/providerutils/prompt`): TS secureJsonParse and the provider tool-argument serializers (TS 4ae6986cd8) (review round 85).
- **`prompt.StringifyToolCallInput`, `prompt.StringToolCallInput`** (`pkg/providerutils/prompt`): the TS `JSON.stringify(part.input)` and `typeof input === 'string' ? input : JSON.stringify(input)` tool-call input serializers (TS 4ae6986cd8) (review round 86).
- **`providerutils.ResolveOpenAICompatibleReasoningEffort`, `OpenAICompatibleReasoningEffortOption`**: the OpenAI-compatible `reasoningEffort` resolution with null (TS 5006d83c4e) (review round 85).
- **`providerutils.SandboxStatter`, `SandboxDirectoryLister`, `SandboxFileDeleter`, `WritableSandboxProcess`, `SandboxFileStat`, `SandboxDirectoryEntry`, `SandboxPathType*`, `SandboxProcessOptions.Stdin`**: optional sandbox file operations and writable stdin (TS 5c247e9b85 / 95348b32c6); the local and Vercel sandboxes implement them (Vercel rejects stdin) (review round 85).
- **`providerutils.JSToLower` / `JSToUpper`**: JavaScript `toLowerCase` / `toUpperCase` (SpecialCasing, Final_Sigma, Unicode 17 pairs; checked against Node over every code point); **`providerutils.JSRegexpSpaceChars` / `JSRegexpSpace` / `JSRegexpDot`**: the JS regexp `\s` and `.` for RE2 ports; **`providerutils.FetchHeaderValue` / `FetchHeaderText` / `IsomorphicDecode` / `FetchURL` / `FetchURLFromHeader`**: TS fetch `headers.get` and `new URL(...)` reads (review round 86b).
- **`types.FileContentBlock.DeprecationWarning`**, **`types.ToolApprovalResponseContent.ReasonSet` / `HasReason`**, **`types.ToolResultOutput.ReasonSet` / `HasReason` / `ReasonOr`**: legacy tool-result content warnings and an empty approval reason kept as given (TS `reason ?? default`) (review round 86b).
- **Request URL metadata** (TS 934100cad4): `provider.StreamRequestURL`, `types.GenerateResult.RequestURL`, `types.StepRequest.URL` and `ai.GenerateStepRequest.URL` (the HTTP origin only), `ai.LanguageModelCallEndEvent.Request` / `ai.LanguageModelCallRequest`, `telemetry.LanguageModelCallEndEvent.RequestURL`, and OpenTelemetry `server.address` / `server.port` on chat spans; OpenAI Chat (and Azure / Bedrock Mantle through it) reports its URL (review round 87).
- **Mistral realtime transcription** (`DoStream`, PCM input, `targetStreamingDelayMs` / `speakerReferences` / `turnDetection`, `ModelVoxtralMiniTranscribeRealtime3` / `...30`; TS b26902ea1a) and the server half of the voice agent (`(*mistral.Provider).ExperimentalRealtime()` with `CreateSession` / `CloseSession`, `mistral.BuildRealtimeSessionConfig`; TS 0912e0b1b9). Gateway: opt-in Vercel OIDC token refresh in memory (`gateway.Config.OIDCTokenFunc`, `gateway.VercelOIDCTokenFunc`, `ContextWithVercelOIDCToken`, `VercelOIDCTokenError`) and `GatewayProviderOptions.ExperimentalStreamingImageDecoding` (TS 89aff56165) (review round 87).
- **`claudecode.Settings.TurnStartTimeout`**, `bridge.HelloCapabilities.IdempotentStart`, `bridge.StartBase.StartID`, `(*bridge.Channel).SendWithOptions` / `bridge.SendOptions` (TS 3bf70f83bc); `vercel.Sandbox.SpawnCommandWithStdin`, `vercel.RemoteProcess.Stdin` (`@vercel/sandbox` 3.9.0 writeStdin / closeStdin); `harnessutil.Subscriptions`; `bridges.ContentHash`, `bridges.AdapterManifest.ContentHash` (review round 87).
- **`vercel.OIDCRefresh`** on `CreateSessionOptions`, `ResumeSessionOptions` and `LegacySettings`: opt-in Vercel OIDC token refresh matching `@vercel/sandbox` 3.9.0 `getCredentials` / `ensureValidToken` (5 minute buffer, in memory, shares the resolver with `gateway.VercelOIDCTokenFunc` through `pkg/internal/verceloidc`); **`vercel.APIClient.ExtendSessionTimeout`** (`POST .../extend-timeout`, used by `Sandbox.Update`) (review round 87b).
- **`providerutils.JSString` / `JSTruthy`**: JavaScript `String(value)` and truthiness for values decoded from JSON (used where TS reads unvalidated `z.any()` values: Open Responses, AI Gateway, You.com, the Vercel token claims) (review round 87b).
- **`youcom.CreateYouClient`** (`youcom.YouClientConfig{APIKey *string, Tools, Profile}`; a nil `APIKey` falls back to `YDC_API_KEY`): connects to the hosted You.com MCP server (`https://api.you.com/mcp`), matching `@youdotcom-oss/ai-sdk-plugin` 3.x `createYouClient`; resolve its tools with `mcp.GetMCPToolsForAgent` (review round 87c).
- **`ai.SetDefaultProvider`** (TS `globalThis.AI_SDK_DEFAULT_PROVIDER`): the provider used for string decision model IDs and batches without a provider, and **`ai.UnsupportedModelVersionError`**, returned when a decision model is not specification `v4` (review round 87e).
- **`UIMessageStreamResponseInit.StreamErrors`**: pass the chunk stream's error channel so `PipeUIMessageChunksToResponse` / `CreateUIMessageChunksResponse` end a failed stream without `data: [DONE]`, as TS (review round 87e).
- **`provider.StreamResponseHeaders`** (`ResponseHeaders() map[string]string`, TS `doStream` `response.headers`), **`providererrors.NewArgumentError`** (the bare-message `@ai-sdk/provider` `InvalidArgumentError`) and **`provider.DecisionQuestionsJSON`** (the JSON form of typed decision criteria) (review round 87e).
- Decide telemetry events carry TS's fields: `DecideStartEvent.MaxOutputTokens` / `Reasoning` / `ProviderOptions`, `DecideEndEvent.Usage` / `Warnings` / `Rounding` / `ProviderMetadata` / `Response`, `DecisionModelCallEndEvent.Warnings` / `Rounding` / `Response` (review round 87e).
- **`VERCEL_OIDC_TOKEN_FILE`** in `pkg/harness/sandbox/vercel` (`@vercel/oidc` 4.0.0, pinned by `@vercel/sandbox` 3.9.0); `pkg/internal/verceloidc` follows 4.0.0 for the sandbox and 3.2.0 for the AI Gateway (review round 87c).
- **`ai.ShellSandbox` `Stat` / `ListDirectory` / `DeleteFile`** (the optional `providerutils.SandboxStatter` / `SandboxDirectoryLister` / `SandboxFileDeleter`, also on `bridgetest.Sandbox`), **`acp.ToolCall.HasName`**, **`websocket.ErrInvalidUTF8Text`** (review round 86b).
- **`Config.APIKeyFunc`** for openai, azure, xai, deepseek, alibaba, moonshot, openresponses, huggingface, gmicloud, together, fireworks, zai, cerebras and deepinfra (TS 7716e189ee), called per request with the request's context; **`openai.Config.HeadersFuncCtx`, `deepseek.Config.HeadersFuncCtx`** for NoAPIKey wrappers (review round 85).
- **`openai.ModelGPT61Terra`, `LanguageModelCapabilities.SupportsChatFunctionToolsWithReasoning`, `openai.SupportsStreamingTranscription`, `openai.StreamingTranscriptionWarnings`, `TranscriptionStreamOpenAIOptionsValue.HasLanguages` / `HasKeywords`, `DecisionModelOptions.PromptCacheKey`** (review round 85).
- **`anthropic.SupportsNativeStructuredOutputSchema`**, **`anthropic.Config.AuthTokenFunc`** (TS 63bf860638) and **`anthropic.Config.APIKeyFunc`** (for wrappers such as MiniMax); **`Config.APIKeyFunc`** for groq, mistral, cohere, perplexity, google, googlevertex (express), bedrock, bedrock mantle and gateway, **`bedrock/anthropic.Config.BearerTokenFunc`** (review round 85).
- **`perplexity.PerplexityMetadata.SearchQueries`**; gateway **`tools.NewDeepSeekSearch` / `NewDeepSeekFetch`** (`Tools.DeepSeekSearch` / `DeepSeekFetch`) and the regenerated gateway model IDs (4 removed ids kept as deprecated constants) (review round 85).
- **`elevenlabs.SpeechModelOptions.WithTimestamps`, `TranscriptionModelOptions.SourceURL`, `MusicWordTimestamp`, `SpeechAlignment`, `SpeechVoiceSegment`**; **`Config.APIKeyFunc`** for elevenlabs, heygen, topaz, voyage, prodia, quiverai and minimax; the new **`pkg/providers/parallel`** decision provider (TS `@ai-sdk/parallel`) (review round 85).
- **`claudecode.Settings.{MaxBudgetUSD, StrictMCPConfig, QuestionTimeoutMs, ExcludeDynamicSections, ToolHints, SkillOverrides}`**, `claudecode.ToolHint`, `claudecode.SkillOverride` (+ constants), the new `claudecode.StartFrame` fields; **`harness.SandboxCreator`, `CreateSessionOptions.SandboxCreator`** (review round 85).
- **`acp.Settings.ResolveHostToolCall`, `acp.ResolveHostToolCallFunc`, `ResolvedHostToolCall`, `ResolveHostToolCallOptions`, `LegacyResolveHostToolCall`, `HostToolCallResolutionRequestFrame`, `TypeHostToolCallResolutionRequest`**; `cursor.ResolveHostToolCall`, `githubcopilot.ResolveHostToolCall`, `grokbuild.ResolveHostToolCall`; **`vercel.PolicyRule.Response`, `vercel.PolicySessionInspector`, `(*vercel.Sandbox).CurrentSessionNetworkPolicyMetadata`, `(*vercel.Session).Stat` / `ListDirectory` / `DeleteFile`** (review round 85).
- **Steering messages in streams** (TS 1a1da8d68c): `provider.ChunkTypeSteeringMessage` parts (`ID`, `SteeringMessage`, `StepNumber`) before the step that received them, the `steering-message` UI chunk and **`ai.SteeringMessageUIPart`**; default steering ids are `steer-` + 24 characters. **`provider.ChunkTypePing`** flows through StreamText and SmoothStream (Anthropic `IncludePings`, AI Gateway pings) and has no UI chunk (TS 9754014925) (review round 88).
- **Transcription word timings** (TS 41465fd1b6): `TranscribeOptions.TimestampGranularities`, `TranscribeResult.Words` / `ai.TranscriptionWord`, filled by OpenAI, Azure, xAI, Gateway and Vertex Gemini; **`provider.TranscriptionURLModel`** used by `ai.Transcribe` (Groq `DoTranscribeFromURL`, TS 36802d48af); **Deepgram live transcription** (`DoStream`, `deepgram.StreamingOptions`, TS 5b89b2560c) (review round 88).
- **Realtime server connections** (TS 892af4dab7): `RealtimeSessionOptions.Intent` (`ai.RealtimeSessionIntentConversation` / `Transcription`) with the optional `provider.RealtimeServerConnectionProvider` / `RealtimeIntentCodec` on OpenAI, OpenAI Live, xAI, Google and the AI Gateway, realtime usage events, and Google `NewServerEventParser` (review round 88).
- **`GenerateVideoResult.UploadedVideos`** and xAI `XAIVideoProviderOptions.Output` (`XAIVideoOutput.UploadURL`) upload delivery (TS 2e44144812) (review round 88).
- **`telemetry.FilterEvent`**, `bridge.StreamPartFrame.StartID`, `claudecode.Settings.HostDisconnectTimeout` / `AdditionalDirectories`, `HostDisconnectTimeout` (codex, opencode, deepagents, acp) and `HostDisconnectTimeoutMS` (cursor, githubcopilot, grokbuild), `harnessutil.HostDisconnectTimeoutFlag`, **`acp.Settings.PrepareSandbox`** / `acp.PrepareSandboxOptions`, `grokbuild.PrepareSandbox`, OpenCode `auth: "direct"` (review round 88).
- **Anthropic** (review round 88): `ModelOptions.IncludePings`, message threads (`ModelOptions.Thread` / `anthropic.ThreadOption`, `Config.SupportsMessageThreads`, `BetaHeaderMessageThreads`; Vertex, Bedrock and MiniMax reject them), inline `toolChanges` tools by value (`prompt.AnthropicInlineTools`, `AnthropicPromptOptions.ConvertInlineTool`, `prompt.AnthropicBetaInlineTools`, `prompt.NewAnthropicCacheControlValidatorWithBreakpoints`), `FilesAPI.GetFileMetadata` / `DownloadFile` / `DeleteFile`, `serviceTier` `standard` / `priority`, and `googlevertex/anthropic.Options.SupportsNativeStructuredOutput`.
- **OpenAI and Azure** (review round 88): GPT Image 2.6 IDs and options (`Complexity`, `SafetyIdentifier`, gif `FrameCount` / `FrameDurationMs`, edit `Markup`, edit `Moderation`), code interpreter `MemoryLimit` / `NetworkPolicy`, MCP `TunnelID`, `prompt.ToOpenAIMessagesWithWarnings` / `ToOpenAIMessagesOptions.MessageNameKey`, `openai.ValidateSpeechProviderOptions`, `azure.NewCustomTool` / `ApplyPatch` / `Shell`, and Together / Fireworks / Hugging Face model ID constants.
- **Other providers** (review round 88): `gateway.GenerationInfo.MarketCost` / `SurchargeCost` / `GatewayCost` and regenerated Gateway model IDs (removed IDs kept as deprecated constants), Cerebras `queueThreshold` header and `serviceTier` metadata, Gladia `Config.PollTimeout` / `PollInterval`, v2 options (`LanguageConfig`, `PiiRedactionConfig`, ...) and `ModelSolaria1` / `ModelSolaria3`, Cartesia speech (`volume`, `emotion`, pronunciation dictionary, `locale`, `normalization`) and Ink 2 keyterms / turn thresholds, Baseten model ID constants, and `providerutils.IsCrossOriginRedirectHop`.
- **Telemetry: GenAI message formatting helpers are exported** (TS 382d0b8eaf): `telemetry.FormatInputMessages`, `FormatModelMessages` (`FormatModelMessagesOptions`), `FormatOutputMessages` (`FormatOutputMessagesOptions`), `FormatObjectOutputMessages`, `FormatSystemInstructions` / `FormatSystemMessages` and the `SemConvPart`, `SemConvInputMessage`, `SemConvOutputMessage`, `SemConvSystemInstruction` types build the OTel GenAI `gen_ai.*.messages` / `gen_ai.system_instructions` shapes outside the built-in integration (review round 89).
- **`ObjectOnStepFinishEvent.MsToFirstChunk`**: StreamObject's time from the start of the stream to its first chunk (TS `msToFirstChunk`); nil for GenerateObject. `WithSteerID("")` is an id like any other, as in TS (review round 89).
- **`anthropicaws.Config.GenerateID`** (TS `createAnthropicAws` `generateId`, source ids), **`openai.Config.CompatErrorStructure`** (an OpenAI-compatible wrapper's TS error structure), **`providererrors.NewSchemaProviderError`** / **`providererrors.ErrorSchema`** (TS createJsonErrorResponseHandler with an error schema) and **`InvalidArgumentError.Value`** (the TS error's `value`) (review round 89).
- **Claude Fable 5.2** (TS 84208104a4): `anthropic.ClaudeFable5_2`, Bedrock `ModelAnthropicClaudeFable5_2` / `ModelUSAnthropicClaudeFable5_2` / `ModelGlobalAnthropicClaudeFable5_2`, Vertex Anthropic `ClaudeFable5_2`, Gateway `anthropic/claude-fable-5.2` (+ `-batch`), with the Fable 5.1 capabilities (128k output, adaptive thinking only, xhigh / max effort, no sampling parameters, no forced tool use) (review round 90).
- **OpenAI-compatible `PrepareRequest` / `ConvertResponse`** (TS c9c6402cbd): on `openai.LanguageModel` (the OpenAI-compatible chat model of the wrappers), Fireworks, Together, GMI Cloud and Cerebras chat models: the body and warnings `DoGenerate` would send, without a network call, and the generate result of a Chat Completions body. `providerutils.OpenAICompatibleErrorData` / `PickSchemaKeys` (review round 90).
- **Gateway** (review round 90): takoSearch `VariableCost`, `MaxVariableCostUSD`, `NoMaxVariableCost`, `ExtraBody` (TS 0e869b40fa); typed `GatewayProviderOptions.Headquarters` (TS 44db2252f0); `meta/muse-ember` (TS 6d65092d9e). `anthropicaws.Config.APIKeyFunc` (TS `apiKey: Resolvable<string>`), `cohere.Config.GenerateID`, `mistral.Config.GenerateID`, `anthropic.Config.ResolveConfigHeaders` (per-call config headers for wrapper providers), `topaz.Error`, `providerutils.FormDataFilePartHeader` / `CreateFormDataFile` / `WriteFormDataField`, `InvalidToolApprovalSignatureError.ErrorName()`.
- **Bridges pinned at ai@7.0.167** (TS 9f7a842ae1): acp 1.0.118, claude-code 1.0.182, codex 1.0.180, deepagents 1.0.178, github-copilot 1.0.75, grok-build 1.0.117, opencode 1.0.181, cursor 1.0.93 (review round 90).
- **Tencent Cloud provider** (TS 1c7a311d53): `pkg/providers/tencent` with TokenHub chat, TokenHub Kling video and VOD AIGC video signed with Tencent Cloud API 3.0 (review round 91).
- **DeepSeek Responses API** (TS f9d43bdaa7): `deepseek.Provider.ResponsesModel`, `deepseek.NewResponsesLanguageModel`, `deepseek.ResponsesLanguageModel`, the `deepseek.WebSearch` tool (`WebSearchToolID`, `Provider.Tools()` / `DeepSeekTools`) (review round 91).
- **OpenAI Responses positioned format updates and cache diagnostics** (TS 5a645e92d2): `textFormat`, the system-message `textFormatUpdate`, `promptCacheOptions.comparisonResponseId`, `providerMetadata.openai.promptCacheDiagnostics`; `responses.ParseTextFormat` / `NormalizeTextFormat`; `openai.ModelGPTTranscribe`, `ModelGPTLiveTranscribe`, `ModelGPTRealtimeWhisper` (review round 91).
- **Google** (review round 91): native Gemini decisions (`google.ModelGeminiDecision1`, TS f70336158e), streamed File Search / RAG document sources, header service tier, `gemini-4-carbon` (Google, Vertex, Gateway) and the new Gateway model IDs (claude-fable-5.2-fast, deepseek-v4.1-flash-1017-ultrafast, humansand/ampersand-v0.1, nace-ai drex, parallel-decisions-fast).
- **MiniMax H3 image generation and editing** (TS c4c8523cc3): `minimax.ImageModel` / `Image`, `Config.ImageBaseURL`. Vercel Sandbox git and tarball sources (review round 91).
- **Core and harness API** (review round 91): `ai.DownloadOptions.Transport`, `types.FileContentBlock.LegacyContentType()`, `langchain.LangGraphConnectionError` / `LangGraphHTTPError`, `(*providererrors.ProviderError).ResponseHeader(name)`, `harness.AgentSettings.CallOptionsSchema`, `claudecode.Settings.AutoMode` (TS 5c3842f9d0), MCP `RegisterOAuthClientWithOptions` / `TrustedOrigin`, `ElicitationRequest.Extra` / `ElicitResult.Extra`, `providerutils.NormalizeProviderOptionsObject`, JSON tags on `alibaba.AlibabaVideoModelOptions` / `AlibabaVideoMediaItem`.
- **Bridges pinned at ai@7.0.170** (TS 2ebc78b654): acp 1.0.121, claude-code 1.0.185, codex 1.0.183, deepagents 1.0.181, github-copilot 1.0.78, grok-build 1.0.120, opencode 1.0.184, cursor 1.0.96 (review round 91).
- **Streamed array output is parsed incrementally** (TS 91a67c2f56): `StreamText` ArrayOutput and `StreamObject` array mode validate each element once instead of reparsing the whole text per chunk (review round 91).
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

- **Harness bridges authenticate with a per-session public key** (TS c849d37dac, review round 91). The sandbox received a shared bridge token (`BRIDGE_CHANNEL_TOKEN`, `agent_bridge_token`), so code in the sandbox could act as the host. Every bridge adapter (claude-code, acp, cursor, github-copilot, grok-build, codex, opencode, deepagents) now generates an Ed25519 key pair per spawn, gives the sandbox only `BRIDGE_AUTH_PUBLIC_KEY`, and signs the bridge challenge with the host-only private key; `MintBridgeToken` is ignored (see [Breaking changes](#breaking-changes-with-migration)).
- **Code mode approvals fail closed** (review round 91). Code mode ran a host tool without approval when `ToolApproval` was an unnamed func literal or a `ToolApprovalStatus` / string, even `"denied"`; it now resolves them as `GenerateText` does, and `Tool.ValidateInput` runs before `Execute`.
- **Cyclic tool outputs, deeply nested JSON and map-valued validation errors no longer crash the process** (review round 91): `ToJSONValue` detects cycles, `jsonorder.Parse` and the Gemini ordered decoder stop past 10000 nesting levels, and `WrapValidationError` no longer panics on map or slice values.
- **Cross-origin redirects drop `Cookie` and `Proxy-Authorization`** (undici fetch, review round 91), in the shared provider client and the MCP and Vercel Sandbox clients, beside `Authorization`. The Google realtime client-secret error no longer includes the `?key=` URL.
- **Downloads stay DNS-pinned by default** (TS 2ebc78b654, review round 91): TS made connect-time DNS protection opt-in; Go keeps it on and adds `ai.DownloadOptions.Transport` as the opt-in counterpart (see Known differences).
- **Telemetry recording flags are honored by every integration and diagnostic subscriber** (TS 8da48db187, review round 88). With `RecordInputs` / `RecordOutputs` false, custom integrations, `SubscribeDiagnostic` handlers and the object callback events on the diagnostic channel still received prompts, outputs, tool arguments, raw errors, provider metadata and bodies. They now get a filtered copy, and OpenAI MCP / code interpreter / shell credentials are redacted from telemetry tool definitions (TS 7bb309d132).
- **MCP OAuth binds tokens to the server and rejects downgrades** (TS ae13fcea03, dc0f7f243d, f608c056a2, review round 88): `resource` is always sent, an unbound refresh token is cleared before a new client registration can lend it a binding, and discovery rejects https to http redirects.
- **Validated downloads reject HTTPS to HTTP redirects** (TS f608c056a2, review round 88), trusted-origin hops included.
- **Credential transports stay on their origin** (review round 88): Vertex MaaS (TS 977231e913), Vertex OAuth and express, AI Gateway, Vertex Anthropic, Bedrock Anthropic, Bedrock Mantle, Anthropic on AWS, the Azure Entra token transport, the Vercel Sandbox API client and the MCP transports no longer send credentials to a cross-origin redirect hop.
- **Harness bridges resynced to ai@7.0.165** (review round 88): Deep Agents and OpenCode ask before file access outside the work directory, claude-code ignores project `.claude/` settings and hooks, Cursor and Grok Build ignore repository configs, hooks and MCP servers, and Grok Build reads go through ACP approval.
- **Anthropic computer toolset member calls are checked** (TS a161c0a665, review round 88): the API's member is the action, and unknown, disabled or undeclared members fail closed, also in batch results.
- **OpenCode GitLab credential brokering is limited to the chat token exchange** (TS 138d66eb78, review round 87). The real `GITLAB_TOKEN` was injected into every sandbox request to the GitLab host that carried the placeholder bearer, so sandboxed code could call any GitLab API with the user's token. It is now injected only into `POST <instance>/api/v4/ai/third_party_agents/direct_access` with the exact placeholder header.
- **Approval continuations rebuild the tool's metadata from the server tool** (TS 857ca45151, review round 85). `ToolMetadata`, `Dynamic` and `Title` of an approved call came from client-supplied history, so a forged history could change what a `ToolApproval` policy, input refiner or the tool saw (for example a read-only flag a policy auto-approves on). They now come from the current server tool before signature checks, refinement and the policy.
- **Topaz input-video uploads reject redirects** (TS c66402d340, review round 85). net/http resent the whole video body to a 307/308 target.
- **Vercel sandbox policy mutations no longer drop native response (deny) rules** (TS 44a4261e52, review round 85), and an API-shaped custom session policy is no longer read as allow-all.
- **Harness bridges resynced to ai@7.0.162** (review round 85): Claude Code file tools outside the work directory need approval and skill files are trusted only under the new `readableSkillRoots` start-frame field; ACP file approvals are scoped to the workspace; OpenCode ignores a repository's `opencode.json` / `.opencode/` settings.
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
- **Some structs are no longer comparable** (compile error). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#non-comparable-structs)

### Behavior changes that can break callers

#### Core text and streaming

- **StreamTextResult.Text() is the final step's text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-text-final-step)
- **Output is the text when no Output option is set** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#output-default-text)
- **PartialOutput is the step's text when no Output option is set** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#partial-output-default-text)
- **OnFinishEvent aggregates across steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#on-finish-aggregates)
- **Step content tool results keep the raw tool output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#step-content-tool-result-raw-output)
- **Usage.TotalTokens is input + output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#total-tokens)
- **ReasoningText joins parts with no separator** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#reasoning-text)
- **StreamText OnError sees error parts only** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-onerror)
- **A StreamText PrepareStep model applies to one step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-preparestep-model)
- **StreamText fires OnLanguageModelCallStart once per attempt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-call-start-per-attempt)
- **StreamText OnChunk sees every error part before OnError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-onchunk-error-parts)
- **StreamText OnError runs for the error parts the stream delivers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-onerror-transformed-parts)
- **A retried StreamText step drops the failed attempt's raw finish reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-retry-raw-finish-reason)
- **A stream retry with a transform keeps only the recovered attempt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-retry-transform-content)
- **A later StreamText step that fails to start ends the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-continuation-failure-end)
- **StreamText keeps the request body only when asked** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-request-body)
- **The StreamText full stream drops the Go-only markers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-full-stream-markers)
- **Truncated streams finish with interrupted** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#truncated-streams-interrupted)
- **A caller cancellation is not a TimeoutError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#caller-cancel-not-timeout)
- **A cancel during tool-call repair returns the abort reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#repair-abort-reason)
- **The per-step timeout starts before PrepareStep** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#step-timeout-before-preparestep)
- **MaxTokens below 1 is rejected** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#max-tokens-min)
- **Start events report a Prompt string as a user message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#start-event-messages)
- **Deprecated tool execution callbacks run only when the new one is unset** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-execution-callback-aliases)
- **A PrepareStep that sets only System changes the step instructions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#preparestep-system-override)
- **ExperimentalStreamTranscribe reports the cancel reason after Close** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-transcribe-cancel-reason)
- **Stop conditions run only when the loop would continue** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stop-conditions-only-when-continue)
- **Generated IDs use the TS formats** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generated-id-formats)
- **JSON is not HTML-escaped** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#json-not-html-escaped)
- **The final SSE event needs its blank line** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sse-final-event)
- **Rerank `OnEnd` gets its own ranking** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#rerank-onend-ranking-copy)
- **EmbedMany returns the first batch failure in time** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#embedmany-first-failure)
- **Response headers fall back to GenerateResult.ResponseHeaders** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#response-headers-fallback)
- **StreamObject logs warnings only after a finish part** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamobject-warning-logging)
- **StreamObject provider metadata comes from the finish part** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamobject-finish-provider-metadata)
- **StreamText steps carry their warnings** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-step-warnings)
- **A cancellation during tool execution aborts the StreamText step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-abort-during-tools)
- **StreamText ExperimentalContext follows a PrepareStep runtime context** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-experimental-context-prepared)
- **Embed rejects only a missing embedding, with InvalidResponseDataError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#embed-no-embeddings-invalid-response)
- **A StreamText step's response headers are its own** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-step-headers-per-step)
- **Timeout.PerChunk starts with the first output chunk; FirstChunk bounds the wait for it** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-per-chunk-after-first-output)
- **ExperimentalStreamTranslate reports an unknown part type with the TS text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-translate-unsupported-part-text)
- **TimeoutError reads like the TS abort reason ("Chunk timeout of 50ms exceeded", "The operation was aborted due to timeout")** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#timeout-error-text)
- **The abort part and UI abort chunk reason use the TS text ("AbortError: This operation was aborted", "TimeoutError: ...")** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#abort-part-reason)
- **StreamText reports a step or total timeout as TimeoutError during stream retries, downloads, tool input callbacks, parsing and approvals** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-timeout-classification)
- **A tool that returns after its timeout keeps its result (TS only aborts the signal)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-timeout-result)
- **StreamText with an Output merges the first text block's deltas until the parsed partial changes, and a delta without metadata repeats the block's latest provider metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-output-text-deltas)
- **The SmoothStream prefix error names the match and the buffer, as TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#smooth-stream-prefix-error)
- **jsonparser.FixJSON repairs a partial array start such as `[-` to `[]`** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fix-json-array-start)
- **StreamText streams each tool result when its execution settles (completion order with parallel tools; a result that settled before a cancel is kept)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-live-tool-results)
- **StreamText step `ToolResults` follow the step content order (streamed provider results before local ones)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamtext-tool-results-content-order)
- **StreamText reports resumed approved tool results in completion order, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-resume-completion-order)
- **StreamText forwards the tool-call part before an approval failure and the step's parts through finish-step before a ToModelOutput failure, as TS does** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-failure-part-order)
- **A SmoothStream chunk detector failure ends the StreamText run with the error instead of an error part, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#smooth-stream-detector-error-ends-run)
- **StreamObject reports the provider stream's request body** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamobject-request-body)
- **A failing ToModelOutput no longer skips the remaining limited tool calls** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tomodeloutput-error-concurrency)
- **Middleware-wrapped streams keep the provider request body and URL** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#middleware-request-metadata)
- **Timeout contexts end with the TS TimeoutError cause (context.Cause), also for StreamText chunk and first-chunk timeouts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#timeout-abort-reason)
- **StreamText OutputErr() reports the TS no-output rejection for a failed run** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-output-err-no-output)
- **StreamTextResult.PipeTextStreamToResponse writes status 200 and the text/plain content type first** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-pipe-text-head)
- **Reasoning-file parts marshal in the TS message shape (data string, no fileData)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#reasoning-file-message-json)
- **StreamObject stream failure calls only OnError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#streamobject-stream-failure-callbacks)
- **Transcribe ignores a generic downloaded content type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#transcribe-audio-media-type)
- **Transcribe sends URLs to providers that take them** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#transcribe-native-url)
- **Uploaded videos are counted, not converted** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#video-uploaded-outcome)
- **GenerateText lists invalid tool call errors before executed results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generate-text-invalid-tool-errors-first)
- **StreamObject publishes a null partial** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-object-null-partial)
- **Tool choice is checked only when the model call finishes** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-tool-choice-no-finish)
- **Empty text block IDs are unique across steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-empty-part-id)
- **CosineSimilarity length error text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cosine-similarity-error-text)
- **GenerateVideo drives start/status-only video models itself** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#video-start-status-only-models)
- **Downloads have no client timeout; set a context deadline** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#download-no-timeout)
- **OutputErr reports a stream failure or abort** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-output-err-failure)
- **Cancelling during a step's end callbacks aborts the run** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-abort-at-step-end)
- **Provider-executed MCP calls no longer get a client tool-not-found result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-executed-mcp-no-client-error)
- **GenerateText has no 1000-step ceiling** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generate-text-no-step-ceiling)
- **GenerateObject and StreamObject call IDs start with aiobj-** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#object-call-id-aiobj)
- **StreamObject calls OnError for preparation failures** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-object-onerror-prep)
- **Generated file URLs use ExperimentalDownload** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generated-file-experimental-download)
- **Sandbox description no longer added to the system prompt** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sandbox-description-not-in-prompt)
- **DefaultSettingsMiddleware and PrepareStep merge typed provider options** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#default-settings-typed-options-merge)
- **tool-input-start carries the tool title and metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-input-start-title)
- **Deltas after a block ends are flagged, not recorded** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#orphan-block-delta-error)
- **StreamText output errors carry the final step's usage and response** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-output-final-step-context)
- **Zero timeouts use the TS error text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#zero-timeout-text)
- **Decision question and option keys follow JS key order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decision-js-key-order)
- **testutil.MockTextStream.Read follows io.Reader** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mock-text-stream-read)

#### Prompts and validation

- **Prompt validation matches TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#prompt-validation)
- **GenerateObject and StreamObject reject system messages in Messages** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#object-system-messages)
- **Invalid instructions are an InvalidPromptError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-instructions)
- **Tool calls without results fail the prompt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#missing-tool-results)
- **The JSON Schema validator enforces more keywords** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#json-schema-validator)
- **Struct-tag schemas validate the decoded value** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#struct-schema-validates-decoded-value)
- **Struct-tag schemas send their fields to the model** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#struct-schema-json-schema)
- **URL file parts are WHATWG-normalized (as TS url.toString()); an unparseable URL fails with "Invalid URL"; FileData.OriginalURL added** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#url-file-part-normalized)
- **Reasoning-file data URLs take their media type; unsupported reasoning-file variants use the TS error text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#reasoning-file-data-url-media-type)
- **types.ImageContent prompt parts log the TS "image" content part deprecation warning** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#image-part-deprecation-warning)
- **ExperimentalDecide reports non-finite numbers and cycles with the TS texts and no longer overflows the stack on a cyclic value** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-serializable-json)
- **Base64 string file data (`FileData.DataString`) gets its detected image media type, and data that is not base64 fails the call, as TS detectMediaType** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#file-data-string-media-type)
- **Legacy tool-result content types (`file-data`, `file-url`, `image-url`, ...) log the TS deprecation warning and drop the `filename` / `mediaType` TS drops** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-result-content-deprecation)
- **`GetTextFromDataURL` returns the TS atob string when no charset is declared and decodes a charset like TS `TextDecoder`** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#data-url-text-ts)
- **Empty text parts with provider metadata stay in response messages** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-text-provider-metadata)
- **Empty text parts are removed from prompts before the model call** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#prompt-empty-text-parts)
- **Validated downloads reject HTTPS to HTTP redirects** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#download-https-downgrade)
- **Data URLs without a ;base64 marker are accepted** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#data-url-without-base64-marker)
- **ExperimentalDownload is called on every prompt conversion** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#download-called-with-empty-plan)
- **File media types are passed on as given** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#file-media-type-unvalidated)
- **A data URL in the data file variant reports the TS text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#data-variant-data-url-text)
- **Legacy image-data and file-data tool results keep their media type** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-result-media-type)

#### Tools

- **Tools in a step run concurrently** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tools-concurrent)
- **An unset ToolChoice reaches the model as auto** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-auto)
- **ToolChoice none is enforced** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-none)
- **A StreamText tool choice violation ends with finish** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-choice-violation-finish)
- **A provider-executed tool call is never run locally** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-executed-not-local)
- **Provider tool results stay in the assistant message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-results-assistant)
- **ToolResults holds no bookkeeping entries** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-results-bookkeeping)
- **Provider tool calls are validated against the tool's Parameters** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-input-validation)
- **Tools with an OrderedSchema or a bare JSONSchemaValidator are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-input-ordered-schema-validation)
- **Provider tool output schemas match TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-output-schemas)
- **Provider error results are tool errors** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-error-results)
- **Streamed provider tool results carry the tool's metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-provider-result-tool-metadata)
- **Response messages replay tool errors and outputs like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#response-tool-error-replay)
- **ToolResults holds only successful calls** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-results-exclude-failures)
- **Tool approval and tool-context errors fail the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-errors-fail-call)
- **PrepareStep ActiveTools and ProviderOptions are applied** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#preparestep-active-tools)
- **Some provider tools are client-executed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#client-executed-provider-tools)
- **Tool output NaN and Infinity become null in typed containers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-output-non-finite-typed)
- **A tool that returns an error value is a tool result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-error-value-result)
- **Invalid tool call errors carry no provider metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-tool-call-no-provider-metadata)
- **GenerateText invalid tool call errors carry no tool metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generate-invalid-tool-error-no-tool-metadata)
- **StreamText invalid tool call errors carry the tool's title** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-invalid-tool-error-title)
- **An invalid tool call's error follows the call in the stream** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-invalid-tool-error-position)
- **ToolCallRepairError messages show the cause as TS does** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-call-repair-error-message)
- **InvalidToolInputError shows its cause as TS does** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-tool-input-error-cause)
- **GenerateText resolves a deferred provider call from any tool-result or tool-error part** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deferred-result-any-result-part)
- **RepairToolCall InputSchema of a tool without Parameters is the TS empty object schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#repair-input-schema-empty-object)
- **A tool without Parameters is sent the TS empty object schema (additionalProperties: false)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-without-parameters-schema)
- **Schema-valued and bare validator tool Parameters reach moonshot, Open Responses and OpenAI Responses as JSON Schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#schema-value-tool-parameters)
- **An invalid tool call's tool error reads `AI_NoSuchToolError: ...` / `AI_InvalidToolInputError: ...` as TS, and an unknown tool's empty input stays empty** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-tool-call-error-text)
- **Web fetch reports an invalid redirect Location as a `network_error` result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#web-fetch-invalid-redirect)
- **Invalid tool calls take `Title` and `ToolMetadata` from the tool only, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-tool-call-title-metadata)
- **Sandbox text file encodings are Node Buffer encodings (`latin1`, `base64`, `hex`, ...), not WHATWG labels** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sandbox-text-node-encodings)
- **Executed tool results have no title** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#executed-tool-result-title)
- **A provider tool error without a value keeps its error undefined** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-error-no-value)
- **Model tool calls take title and metadata from the tool** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#model-tool-call-tool-metadata)
- **Anthropic provider tools drop undeclared input keys** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-tool-input-strip-undeclared)
- **toolwebfetch and toolbash count UTF-16 code units** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#utf16-lengths)
- **MCP, Vercel, Topaz, DeepInfra, OpenCode, Codex and Prodia details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tools-r91-details)
- **Anthropic advisor tool input is strict** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#advisor-input-strict)
- **An unset tool-caller entry leaves the tool unaffected** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-callers-unset-entry)

#### Agents and approvals

- **A panicking ToolLoopAgent callback does not skip the other one** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-callback-panic-isolation)
- **Kept approval responses reach the model without providerExecuted** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#approval-response-prompt-shape)
- **Legacy ToolLoopAgent approval IDs are generated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-approval-ids)
- **Legacy ToolLoopAgent tool results list provider results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-results-provider)
- **Legacy ToolLoopAgent tool messages carry the output's JSON value** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-output-json-value)
- **Legacy ToolLoopAgent request messages leave out the instructions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-request-messages)
- **The legacy ToolLoopAgent rejects a negative MaxRetries** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-negative-maxretries)
- **A legacy ToolLoopAgent step timeout is context.DeadlineExceeded** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-step-timeout-deadline)
- **The legacy ToolLoopAgent step timeout covers tool execution** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-step-timeout-tools)
- **An empty ActiveTools leaves no tools active in the legacy loop** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-empty-active-tools)
- **The legacy ToolLoopAgent follows the PrepareCall tools, tool callers and output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-preparecall-tools-output)
- **Legacy ToolLoopAgent steps carry the call ID** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-step-call-id)
- **The agent loop leaves provider-executed calls of unknown tools to the provider** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-provider-dynamic-unknown-tool)
- **AI Gateway authentication errors are wrapped** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-auth-error)
- **WorkflowAgent approval IDs are approval-toolCallId** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-approval-ids)
- **Resumed approval results carry their model output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resumed-output)
- **WorkflowAgent PrepareStep overrides carry over to later steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-preparestep-carryover)
- **A failed WorkflowAgent Stream run fires OnEnd** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-failed-onend)
- **WorkflowAgent Stream usage is input + output summed over the steps** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-usage)
- **The WorkflowAgent Stream root span ends with the run** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-root-span-end)
- **WorkflowAgent.Stream has no default step limit** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-no-step-limit)
- **The WorkflowAgent root span is ai.workflowAgent.stream for Generate too** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-root-span)
- **WorkflowAgent answers a provider tool call that got no result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-missing-provider-result)
- **WorkflowRunMultiplexer ends on a finish chunk** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-multiplexer-finish)
- **WorkflowAgent resumed-tool telemetry carries the tool context** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resumed-tool-telemetry-context)
- **WorkflowAgent `PrepareCall` sees the agent's generation settings** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-preparecall-agent-settings)
- **Resolved provider tools are provider-executed only when marked** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resolve-provider-executed)
- **WorkflowAgent Stream leaves reasoning files out of the conversation** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-reasoning-files)
- **SerializeToolSet resolves DescriptionFunc** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-serialize-description-func)
- **Resolved dynamic tools carry no title, metadata or strict** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resolve-dynamic-tools)
- **WorkflowAgent stream opens one telemetry root** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-single-telemetry-root)
- **WorkflowAgent hook fields left nil keep their value** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-prepare-hook-nil-fields)
- **Rebuilt workflow tools validate their input like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-resolved-tool-input-errors)
- **WorkflowAgent client tool results carry no provider metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-client-tool-result-metadata)
- **WorkflowAgent stream runs tool input callbacks after the model step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-stream-tool-input-callbacks)
- **WorkflowAgent tools see the conversation prompt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-tool-messages-conversation-prompt)
- **WorkflowAgent empty provider results carry no provider metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-empty-provider-result-metadata)
- **Tool-approval requests carry the pre-schema input** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#approval-input-schema-input-raw)
- **Approval revalidation runs for tools without Parameters** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#approval-revalidation-without-parameters)
- **The legacy loop stops waiting for a deferred provider tool on its error result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-deferred-provider-error-result)
- **WorkflowAgent tool input callbacks get the conversation prompt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-input-callbacks-conversation-prompt)
- **The legacy loop's invalid tool call errors carry no tool metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-invalid-tool-error-no-tool-metadata)
- **Approved calls of struct-schema tools are revalidated as they were parsed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#approval-struct-schema-revalidation)
- **Approval continuations take ToolMetadata, Dynamic and Title from the server tool, not from message history** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#approval-continuation-server-tool-metadata)
- **An empty tool approval reason is kept and sent as `""` instead of the provider's default denial text, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-approval-reason)
- **ExperimentalDecide checks the model's specification version** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-model-version)
- **ToolLoopAgent checks the prompt after the call options and PrepareCall** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-prompt-after-prepare-call)
- **The agent loop lists invalid tool call errors first** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-invalid-tool-errors-first)
- **The agent loop keeps the user-approval reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-user-approval-reason)
- **The agent loop parses JSON output from the provider's structured output text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-structured-output-text)
- **WorkflowAgent approval resume error text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-approval-error-text)
- **WorkflowAgent returns step errors without the step prefix** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-agent-errors-unwrapped)
- **LangSmithDeploymentTransport follows the TS LangGraph client** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#langsmith-transport-ts-client)
- **Struct-literal WorkflowAgent stream has no 20-step default** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-literal-stream-steps)

#### UI message streams

- **UI message streams fail on an out-of-sequence chunk** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-out-of-sequence)
- **An out-of-range KeepAliveMs is an error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#keep-alive-range)
- **UI message state follows TS for four chunk edge cases** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-message-state-edge-cases)
- **ToUIMessageStream OnEnd reports the finish chunk's reason** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-on-end-finish-reason)
- **One error chunk for a StreamText run that ends with an error part** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-single-error-chunk)
- **UI message streams end like the source stream** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-finish-and-abort-chunks)
- **UI file chunks carry the file's provider metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-file-provider-metadata)
- **ToUIMessageStream maps start-led part sequences one to one** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-start-part-one-to-one)
- **UI custom, approval and single-part chunks follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-custom-chunk-metadata)
- **Approved tool calls send one approval pair in UI streams** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-auto-approval-no-duplicate)
- **A failing ConsumeSSEStream no longer breaks CreateUIMessageChunksResponse** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-consume-sse-error-body)
- **UI message streams keep empty text deltas with provider metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-empty-text-delta-metadata)
- **UI start chunks carry the response message id as TS does** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-start-message-id)
- **UI message metadata keeps an object over a number or boolean** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-metadata-merge-scalar)
- **A caller header replaces the UI stream default header** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-stream-response-header-override)
- **ConsumeSSEStream no longer holds up the response** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-consume-sse-not-awaited)
- **Pipe helpers do not wait for ConsumeSSEStream** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-pipe-consume-sse-not-awaited)
- **ValidateUIMessages rejects a data part without data** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-validate-data-part-requires-data)
- **ConsumeSSEStream keeps its copy after a client disconnect** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-consume-sse-outlives-disconnect)
- **A failed UI conversion ends the SSE stream without [DONE]** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-failed-stream-no-done)
- **A null inputSchemaInput or tool input is kept** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-null-input-kept)
- **ReadUIMessageStream frames events and decodes UTF-8 like the TS chat transport; an unterminated last event is dropped** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#read-ui-message-stream-sse)
- **UI tool parts decoded without input stay without it, an empty denial reason is kept, and ReadUIMessages writes no snapshot for unknown chunks** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-tool-part-absent-input)
- **`WorkflowChatTransport` skips only an exact `[DONE]` and parses every other SSE data line** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-transport-sse-data)
- **UI message stream SSE events keep the TS key order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-sse-key-order)
- **UI tool-input-start writes dynamic: false for a tool outside the tool set** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-tool-input-start-dynamic-false)
- **UI message metadata merges non-object values like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-metadata-merge-mergeobjects)
- **Agent UI stream helpers end a failed run without [DONE]** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#agent-ui-helpers-stream-errors)
- **Empty text deltas from transforms reach the UI stream** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-stream-empty-text-delta)
- **UI stream callback order and provider tool errors** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-stream-metadata-before-onerror)
- **WorkflowChatTransport reconnect error text for a malformed frame** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-chat-transport-parse-error-text)
- **UI MessageMetadata part types match TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-message-metadata-tool-error)
- **Invalid tool calls produce one UI error chunk and one OnError call** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-invalid-tool-call-once)
- **ValidateUIMessages part type, state and invalid-value errors match TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#validate-ui-messages-selector)

#### Telemetry and events

- **An empty Telemetry.Integrations turns telemetry off for the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-integrations)
- **Telemetry spans come from the ai and gen_ai tracers** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tracer-names)
- **EnrichSpan and runtime context attributes follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#enrich-span-attributes)
- **Evaluate telemetry and event names are decide names** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-telemetry-names)
- **A cancelled GenerateText reports OnError to telemetry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#generate-cancel-telemetry)
- **Step-start events report the call's tools and the output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#step-start-call-tools)
- **Telemetry OnStepError fires for every failed step** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-step-error)
- **Telemetry step-end, end, tool-end, error and abort run after the callbacks** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-callback-order)
- **Telemetry step end runs every OnStepEnd before the deprecated OnStepFinish** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-step-finish-fanout)
- **Rerank telemetry `ai.ranking` uses TS keys** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-ranking-keys)
- **GenAI embeddings span operation name is `embeddings`** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-embeddings-operation-name)
- **generateSpeech records an empty text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-speech-empty-text)
- **ai.prompt.toolChoice and ai.schema honor RecordInputs** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-input-gated-toolchoice-schema)
- **An unset finish reason is not recorded on spans** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-unset-finish-reason)
- **OpenTelemetry span names keep the space for an empty model ID** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-empty-model-id-span-names)
- **OpenTelemetry ends open spans in TS order on abort or error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-abort-error-span-end-order)
- **The legacy embed root span records InputTokens usage** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#embed-root-usage-tokens-input-tokens)
- **A provider-reported 0 embedding token count is recorded** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#embedding-usage-zero-reported)
- **FireOnStepError publishes an onStepError diagnostic message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#diagnostic-on-step-error)
- **Tool input is validated with every input schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-input-every-schema-validated)
- **Legacy doEmbed span `ai.usage.tokens` is an integer** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-embed-usage-tokens-int)
- **A cancelled UI ctx stops message processing** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ui-cancel-stops-processing)
- **ExperimentalStreamTranslate reports the TS cancel reasons** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-translate-cancel-reason)
- **Step spans record the auto tool choice** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-step-tool-choice-auto)
- **WorkflowAgent step spans omit the step prompt and tools** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-step-span-no-prompt)
- **Telemetry ai.prompt.tools and gen_ai.tool.definitions record the tools in the TS shapes and key order (inputSchema, not parameters)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-prompt-tools-shape)
- **The WorkflowAgent GenAI root span is `invoke_agent`, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-agent-operation-name)
- **Chat spans record server.address / server.port from the provider's request origin** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-server-address)
- **Provider usage and runtime context attributes keep one value per name (later key wins)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#otel-usage-key-collision)
- **An empty InputExamples list is kept as [] in tool telemetry and serialized workflow tools** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-definitions-empty-input-examples)
- **User OnStart runs before the telemetry root start** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-start-after-onstart)
- **WorkflowAgent root telemetry follows the TS durable report** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#workflow-root-telemetry-durable)
- **Recording flags filter what integrations and diagnostic subscribers see** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-recording-flags-filter)
- **Telemetry tool definitions redact provider credentials** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-tool-definition-redaction)
- **GenAI output messages keep an empty tool call ID and media type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#genai-output-empty-ids)
- **Telemetry copies redact provider tool credentials** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-credential-redaction)
- **Telemetry: integral numbers are int attributes** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-int-attributes)
- **OnError without an error still marks the span as an error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#telemetry-onerror-nil-status)
- **Span exception.type is the TS error name** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#span-exception-type)
- **http.response.status_code only for the error itself or RetryError.LastError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#span-status-code-unwrap)
- **Legacy ai.prompt.messages is the V4 prompt** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-prompt-messages-v4)
- **Legacy root ai.prompt keeps a string prompt as the user message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-root-prompt-string)
- **Legacy ai.response.object keeps the model key order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-response-object-order)
- **No ai.toolCall.durationMs on legacy tool spans** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-tool-span-duration)
- **No ai.settings attributes on legacy speech and transcription spans** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#legacy-audio-settings)

#### Errors

- **Errors are returned without Go prefixes** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#errors-unprefixed)
- **Error messages quote names as TS does** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#error-name-quoting)
- **An unknown decision model ID is a NoSuchModelError** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decide-unknown-model)
- **Registry resolution errors follow the TS registry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#registry-resolution-errors)
- **Non-JSON error bodies use the HTTP status text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#non-json-error-body)
- **Unparsable structured output has a JSONParseError cause** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#object-parse-json-parse-error)
- **Output validation failures have a TypeValidationError cause** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#output-type-validation-cause)
- **An empty provider error message is reported as is, not replaced by a fallback text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-provider-error-message)
- **Response header maps keep the last Set-Cookie line, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#set-cookie-last-line)
- **Provider-package argument errors have no parameter prefix** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#invalid-argument-bare-message)
- **Error bodies with `__proto__` keys give the status text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#error-body-secure-parse)
- **Provider errors follow each provider's TS error schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#error-schema-message-data)
- **VideoGenerationError text is the provider message** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#video-generation-error-text)
- **Missing settings return LoadSettingError** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#load-setting-error-typed)
- **Error and warning texts quote values like TS (round 90)** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#r90-template-quotes)
- **ProviderError.Data is the TS error schema output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-error-data-schema)
- **Open Responses error message follows its schema** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-error-schema)
- **Generated-image download errors are not wrapped** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#image-download-errors)
- **KlingAI and ByteDance AISDKError texts** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#video-aisdk-error-text)
- **Schema provider error headers are lower case** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#schema-error-response-headers)
- **DownloadError text prefers the cause** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#download-error-cause)
- **Aborted calls return the abort error, not a status-0 ProviderError** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#transport-errors-unchanged)
- **Stream provider error payloads without a string message are kept** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-provider-error-normalization)

#### Providers

- **Provider IDs follow TS config.provider** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-ids)
- **Responses and stream chunks are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#response-validation)
- **Invalid provider options fail the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-options-validation)
- **Anthropic, Bedrock and Google content is in response order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#result-content-order)
- **Empty stop sequences are sent** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-stop-sequences)
- **Provider tools reach the AI Gateway as provider tools** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-provider-tools)
- **AI Gateway tools have no title** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-tool-title)
- **Gateway tool outputs without a value** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-tool-output-null)
- **Anthropic provider tools are matched by id** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-provider-tool-id)
- **Anthropic bash tools run through the sandbox session** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-bash-sandbox-session)
- **Anthropic provider tools on Bedrock** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-anthropic-provider-tools)
- **OpenAI Responses promptCacheOptions is validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-prompt-cache-options)
- **OpenAI chat streams fail the call on an error before output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-stream-error)
- **OpenAI provider tools are dropped by the Chat Completions model** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-drops-provider-tools)
- **OpenAI tool_search calls are named after the tool** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-tool-search-tool-name)
- **OpenAI shell calls in a hosted container are provider-executed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-shell-container-provider-executed)
- **OpenAI provider tool config is validated before the request** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-tool-args-validation)
- **openaitool.ToolSearch keeps the search schema in its options** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-tool-search-parameters)
- **Expanded OpenAI parallel tool calls keep the model's key order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-parallel-call-input-order)
- **OpenAI Responses function and shell tool wire details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-responses-tool-wire-r75)
- **OpenAI Responses stream reads to the end of the stream** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-responses-stream-r76)
- **xAI provider tool config is validated before the request** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-tool-args-validation)
- **xAI Responses function tools without a description omit the key** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-function-tool-description)
- **Wrapper providers read only their own API key** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#wrapper-api-keys)
- **Failed generated-media downloads return ProviderError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#media-download-errors)
- **KlingAI VideoModel() accepts any model ID** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#klingai-video-model)
- **Perplexity embedding provider options are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-embedding-options)
- **Anthropic reasoning parts with an empty signature are replayed** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-empty-reasoning-signature)
- **Bedrock budgetTokens warning for non-Anthropic models** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-budget-tokens-warning)
- **OpenAI Responses replays encrypted reasoning from provider options** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-reasoning-encrypted-content-provider-options)
- **Responses streams skip [DONE]; created_at 0 is a timestamp** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#responses-stream-done-created-at)
- **Chat, completion and OpenAI-compatible streams read past [DONE]** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sse-read-past-done)
- **created: 0 is the epoch, and first-chunk response metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#chat-created-zero-epoch)
- **Anthropic reasoning provider options are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-reasoning-options-validated)
- **Empty serviceTier and echo fields are reported** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#empty-service-tier-reported)
- **The first xAI provider tool with an id names its output** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-provider-tool-name-first)
- **Azure DeepSeek models send strict tools** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-deepseek-strict-tools)
- **Alibaba resolves a top-level or wildcard image media type** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#alibaba-image-media-type-sniffed)
- **Non-stream responses accept fractional numbers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#json-response-fractional-numbers)
- **An empty finish_reason is a finish reason in deepseek, alibaba and moonshot streams** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-empty-finish-reason)
- **Google provider metadata keeps only the keys TS declares** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-metadata-declared-keys)
- **OpenAI chat and completion streams report the response headers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-stream-response-headers)
- **xAI batch results always carry response metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-batch-response-metadata-always)
- **DeepSeek warns on an unknown tool choice type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deepseek-unknown-tool-choice)
- **Alibaba sends a text prompt as a user message** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#alibaba-text-prompt-messages)
- **Cohere generate content lists text parts in wire order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-generate-text-parts-in-order)
- **Cohere stream chunks are parsed like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-stream-chunk-parsing)
- **Cohere sends empty stop sequences** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-empty-stop-sequences)
- **openresponses custom tool inputs keep HTML characters literal** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-custom-tool-input-literal)
- **Perplexity stream ids format numbers like JavaScript** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-stream-ids-js-numbers)
- **Google decodes unpadded and URL-safe base64 payloads** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-base64-forgiving-decode)
- **gmicloud, ollama and alibaba report unknown usage for a response without usage** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-missing-usage-null)
- **OpenAI-compatible generate results keep the TS reasoning and content order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-generate-content-order)
- **A Cohere file part is an image only for the image top-level media type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-image-top-level-media-type)
- **Mistral: an empty choices array is an error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mistral-empty-choices-error)
- **Amazon Bedrock: embedding token counts follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-embedding-token-count)
- **Google Vertex: one embedding response per call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vertex-embedding-one-response)
- **Google Live transcription and translation streams** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-live-close-and-silence)
- **Base64 string file payloads are forwarded as given** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#file-data-string-forwarded)
- **Speech request.body keeps the TS key order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#speech-request-body-key-order)
- **Alibaba, Moonshot and OpenAI completion: an empty choices array is an error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#chat-empty-choices-typeerror)
- **A streamed tool call without a function name ends the stream** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#chat-stream-missing-tool-name-no-finish)
- **OpenAI computer tool input follows TS mapComputerAction** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-computer-input-order)
- **OpenAI-compatible completion results drop an empty text part** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-completion-empty-text)
- **OpenAI-compatible streams keep tool call thought signatures** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-stream-tool-thought-signature)
- **Cerebras reports the request body it sends** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cerebras-request-body-extras)
- **A base64 string file payload is sniffed from its prefix only; a bad prefix is a base64 error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#file-data-string-prefix-sniff)
- **Anthropic: an undetected application file part fails with the TS error** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-application-subtype-error)
- **Anthropic: an empty SSE data event is a stream error part** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-stream-empty-data-error)
- **Google Interactions keeps an empty signature** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-empty-signature)
- **OpenAI Responses: an empty computer_call actions list stays empty** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#responses-computer-empty-actions)
- **OpenAI and xAI file uploads follow the TS form and metadata** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-files-upload-parity)
- **OpenAI skills report an empty default_version** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#skill-empty-default-version)
- **Ollama reports the deprecated openai-compatible key warning** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#ollama-compat-key-warning)
- **OpenAI-compatible completion streams follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-completion-stream-parity)
- **Z.ai requestId and userId lengths count UTF-16 units** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#zai-id-length-utf16)
- **Mistral conversation usage defaults to zero** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mistral-conversation-usage-defaults)
- **Hugging Face detects image types with the TS signatures** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#huggingface-image-signature-sniff)
- **Perplexity: a typed decision state is sent as the native value** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-decision-typed-state)
- **Perplexity streams emit no source for finance results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-stream-finance-no-source)
- **Perplexity: a decision file by URL, reference or text fails with the TS error** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-decision-legacy-file-kind)
- **An unknown tool choice type fails with UnsupportedFunctionalityError like TS instead of being sent as auto** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#unknown-tool-choice-type)
- **Text file parts are decoded like TS TextDecoder (BOM dropped, one U+FFFD per invalid sequence)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#text-file-part-textdecoder)
- **Bedrock reports the TS errors for s3 file parts without a media subtype and for text files in tool results** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-url-media-type-errors)
- **Bedrock converts a text file part to a document whatever its media type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-text-file-document)
- **Google Live transcription takes an empty languageCode like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-live-empty-language-code)
- **Google Live speech translation falls back to the top-level inputTranscription text like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-live-translation-source-fallback)
- **Azure MAI image errors read the message with the TS error schema** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-mai-image-error-message)
- **Moonshot MFJS schema normalization keeps the TS key order and sends an empty tuple as prefixItems []** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#moonshot-mfjs-key-order)
- **Perplexity sends an empty providerOptions thoughtSignature as thought_signature ""** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-empty-thought-signature)
- **Provider SSE data, JSON Lines rows, WebSocket frames, JSON and error bodies are decoded like TS TextDecoder (BOM dropped, one U+FFFD per invalid sequence)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sse-invalid-utf8)
- **Anthropic: a replayed provider tool error result sends any present errorCode, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-tool-result-error-code)
- **Google/Vertex: a present empty thoughtSignature is sent and suppresses the Gemini 3 sentinel** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-empty-thought-signature)
- **Google/Vertex: URL and reference file parts with only a top-level media type fail like TS (must specify subtype)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-reference-file-media-type)
- **Google: an empty continuationToken provider option is sent** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-empty-continuation-token)
- **Google/Vertex Live: an empty server error message is kept, as TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-live-empty-error-message)
- **Google Interactions: replayed steps and options keep empty values as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-step-fields)
- **Google Interactions: synthesized agent stream tool calls carry the part's metadata** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-agent-stream-metadata)
- **OpenAI/xAI realtime client-secret URLs encode the model id like encodeURIComponent** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#realtime-url-model-encoding)
- **Files reference ids and media types use JavaScript trim (OpenAI, xAI, DeepSeek)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#files-id-js-trim)
- **xAI video: empty status values, edit/extend video {} and the request_id error text follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-video-empty-values)
- **xAI image: an empty b64_json is data and an empty revised prompt is omitted, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-image-empty-values)
- **Azure MAI image size and aspect ratio use JavaScript Number parsing** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-mai-image-size-number)
- **QuiverAI sends reasoning none as low and max as xhigh, as TS (rounded to a supported effort)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#quiverai-reasoning-rounding)
- **Top-level Reasoning rounds to the closest supported level on Anthropic, Groq, Mistral, Perplexity, Gemini and Bedrock, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#reasoning-closest-supported-level)
- **SSEParser decodes every field and applies the eventsource-parser id and retry rules** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#sse-field-decoding)
- **A replayed tool input that is a JSON string holding JSON is sent as that JSON, and non-JSON argument text as a JSON string, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#string-tool-input-json)
- **OpenAI Responses and DeepSeek replay non-JSON tool arguments as a JSON string** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tool-call-arguments-text)
- **AI Gateway returns the caller's ctx cancellation or deadline unchanged, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-caller-cancellation)
- **Anthropic structured output uses native output_format only for schemas it can represent, and Claude 5 / 5.5 on Bedrock and Vertex get native output** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-native-structured-output-schema)
- **Anthropic returns the model's text when the JSON response tool is not called, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-json-tool-text-fallback)
- **The Anthropic schema sanitizer keeps definitions and $defs next to a $ref** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-sanitizer-ref-definitions)
- **Bedrock Anthropic models receive disabled thinking for reasoning none, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-anthropic-disabled-thinking)
- **Bedrock named tool choice keeps every function tool and turns parallel tool use off, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-named-tool-choice)
- **Bedrock gives duplicate document names unique suffixes, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-unique-document-names)
- **Gemini in-stream errors with only an RPC status get the mapped status code and retryability** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-stream-error-status-code)
- **Google Interactions sends no tools for tool choice none, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-tool-choice-none)
- **Gemini closes a streamed string argument on its final chunk, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-stream-function-args-close)
- **OpenAI sends reasoningEffort unchanged and rounds a top-level Reasoning to the model's supported efforts, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-reasoning-rounding)
- **OpenAI transcription defaults the response format per model and streams gpt-transcribe and gpt-live-transcribe, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-transcription-defaults)
- **OpenAI reports an unfinished web search as a tool error, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-unfinished-web-search)
- **OpenAI Decisions send JSON state as input_json and support promptCacheKey, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-decisions-input-json)
- **OpenAI strips a type-less propertyNames schema with a warning instead of failing the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-property-names-typeless)
- **xAI Responses emits each citation once, reports length for an incomplete response, and sends none as low on Grok 4.5+** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-responses-citations-finish)
- **DeepSeek and Moonshot round a top-level Reasoning to their supported efforts, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deepseek-moonshot-reasoning-rounding)
- **Azure Speech keeps a protocol-error 502 as a retryable 502, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-speech-502-retryable)
- **Fireworks sends the reasoning effort as given, adjusting only the levels a model family rejects, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fireworks-reasoning-effort)
- **OpenAI-compatible providers send reasoningEffort null as null and pass a raw reasoning_effort through, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-reasoning-effort-null)
- **Cerebras reports the deprecated-key warning once and sends a null reasoning effort as null** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cerebras-r85)
- **Cohere decodes base64 document data before sending it, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-base64-document-data)
- **Perplexity provider metadata reports searchQueries, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-search-queries)
- **ElevenLabs sends wav as wav_44100, routes eleven_v4 through Text to Dialogue and encodes the voice path, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#elevenlabs-wav-and-v4-dialogue)
- **ElevenLabs errors carry the message of the detail body, as TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#elevenlabs-error-detail-messages)
- **HeyGen accepts any positive integer duration and ultraquality prompt enhancement, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#heygen-duration-positive-integer)
- **Topaz input-video uploads reject redirects** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#topaz-upload-redirect)
- **Go-typed provider option values (`[]string`, `[]map[string]interface{}`, `map[string]bool`) read like their JSON form (Azure, Moonshot, Google, OpenAI, Black Forest Labs, Hume)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-options-go-typed-lists)
- **Prodia multipart responses are parsed with the TS rules (no Content-Type sniffing)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#prodia-multipart-r86)
- **Google and Vertex send `responseJsonSchema` in the TS sanitizer's key order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-response-schema-key-order)
- **OpenAI and Azure Responses send a legacy tool call's `RawArguments` when `Arguments` is nil (not `{}`)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-responses-raw-arguments)
- **DeepSeek file uploads parse the provider options first and report `invalid deepseek provider options`** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deepseek-files-options)
- **OpenAI-compatible replayed tool calls follow TS `??` for `thoughtSignature` (an empty provider value wins)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-thought-signature-nullish)
- **Response headers and header regexes follow TS fetch `headers.get` and JS `\s` (joined repeated lines, Latin-1 bytes)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fetch-header-text)
- **Providers follow JavaScript trim, regexp and URL-escaping rules on edge inputs (Unicode spaces, BOM, `~*!'()` in query values)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#r86b-js-whitespace-and-url-escaping)
- **Base URLs lose one trailing slash (TS `withoutTrailingSlash`), not all** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#base-url-one-trailing-slash)
- **OpenAI realtime WebSocket bearer tokens are parsed with the TS regexp (JS `\s`, `.`)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-realtime-bearer-whitespace)
- **OpenAI-compatible provider options names are trimmed with JS trim everywhere** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-compatible-provider-options-name-trim)
- **Batch result rows follow TS `parseJsonLines` (JS-blank rows skipped, others parsed untrimmed)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#jsonl-blank-rows)
- **The xAI streaming STT URL query follows the TS order and URLSearchParams escaping** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-stt-query-order)
- **A WebSocket text frame with invalid UTF-8 fails the connection with close 1007, as Node** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#websocket-invalid-utf8-text-frame)
- **Lowercasing and uppercasing of data follow JavaScript `toLowerCase` / `toUpperCase`** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#js-case-conversion)
- **`Seed: 0` is now sent to image and video models** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#seed-zero-sent)
- **Google/Vertex streams keep text and tool calls in part order** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-stream-part-order)
- **Mistral Large 4 maps reasoning to low/medium/high** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mistral-large-4-reasoning)
- **Open Responses streams report the server's response id, model and timestamp from response.created** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-stream-response-metadata)
- **A raw verbosity provider option is sent to OpenAI-compatible hosts when textVerbosity is unset** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-compatible-raw-verbosity)
- **Open Responses errors keep numeric codes and empty codes / reasons like TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-error-fields)
- **Open Responses events and responses with unexpected field types decode like TS instead of failing** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-lenient-decode)
- **AI Gateway stream parts and generate bodies with unexpected field types decode like TS instead of failing** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-lenient-decode)
- **Stream error codes: Moonshot and xAI keep an empty code, Open Responses reports none when absent** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-error-empty-code)
- **Open Responses custom tools send an empty description** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-custom-tool-empty-description)
- **OpenAI-compatible providers send raw stream / stream_options provider options in generate, like TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-compatible-raw-stream-options)
- **You.com 402 errors interpolate any truthy message / upgrade_url / reset_at value** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#youcom-402-message)
- **AI Gateway missing-credential errors are the GatewayAuthenticationError itself, and connection failures read like TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-transport-errors)
- **Provider credential, signing and key-function errors are returned unchanged and not retried** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-credential-errors-unchanged)
- **Decision models read typed Go criteria from direct DoDecide callers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#decision-questions-json)
- **Google Interactions function_call without an id keeps the empty ID** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-empty-call-id)
- **Gemini usage after a continuation takes the later serviceTier and trafficType** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-usage-merge-null)
- **Google videoMetadata fps errors report inclusive false** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-video-metadata-inclusive)
- **OpenAI compaction parts carry only providerMetadata.openai** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-compaction-metadata-keys)
- **OpenAI transcription sends an empty language or prompt option** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-transcription-empty-options)
- **OpenAI computer calls keep an empty safety-check code or message** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-computer-safety-check-empty)
- **Open Responses stream tool calls match item ids by JSON type** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-item-id-map-keys)
- **Alibaba preserve_thinking spellings warn deprecated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#alibaba-preserve-thinking-deprecated)
- **Open Responses replay sends an empty item id** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-replay-empty-item-id)
- **Open Responses provider metadata itemId keeps the item's id value** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-metadata-item-id)
- **Live WebSocket close errors carry the close code and reason** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#live-websocket-close-code)
- **Provider responses keep a present empty string** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-response-empty-strings)
- **OpenAI-style chat streams send text-start and text-end parts** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#chat-stream-text-blocks)
- **Provider streams report response headers beside the stream** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#stream-response-headers-capability)
- **Cerebras queueThreshold is a validated header** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cerebras-queue-threshold)
- **Anthropic computer toolset member calls are checked** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-toolset-member-guard)
- **Vertex Anthropic, Bedrock Anthropic, Mantle and Anthropic AWS send credentials only to their endpoint** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-credential-redirects)
- **Claude on Vertex reads the legacy vertex options key** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-vertex-legacy-key)
- **Anthropic serviceTier follows the provider's allowed values** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-service-tier)
- **Anthropic MCP connectors are disabled when a tool choice falls back** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-mcp-restricted-choice)
- **Anthropic ProviderError.Data holds the parsed error object** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-error-data)
- **Anthropic file upload metadata keys are present only when sent** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-files-v4)
- **Anthropic sends the structured-outputs beta for tools only with strict tool support** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-strict-tools-beta)
- **Anthropic batch results report toolset member calls as plain tool calls** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-batch-toolset-calls)
- **Vertex MaaS, OAuth, express and AI Gateway credentials stay on their origin** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vertex-maas-credentials-same-origin)
- **Cohere accepts untitled and tool citations; usage without tokens is unset** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-partial-citations)
- **Cohere streams emit source parts for citations** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-stream-sources)
- **Cohere response ID is the v2 id** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-response-id)
- **Cohere streamed null tool arguments become {}** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cohere-null-tool-args)
- **Mistral sends a named tool choice natively** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mistral-native-tool-choice)
- **Perplexity ToolChoiceNone sends no tools** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-tool-choice-none)
- **Google ToolChoiceNone drops provider-defined tools** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-tool-choice-none-provider-tools)
- **Gemini TTS sends the speech language** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-tts-language)
- **Google Interactions sends labels** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-labels)
- **Gemini streamed partial arguments with array gaps are skipped** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-stream-array-gaps)
- **Vertex Gemini transcription honors shared word timestamps** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vertex-gemini-transcription-words)
- **Chat Completions refusals are returned as text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-refusals)
- **Streamed chat logprobs hold every chunk** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-logprobs-all-chunks)
- **Responses refusals are returned as text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-responses-refusals)
- **OpenAI-compatible inline text files are sent as text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-inline-text-files)
- **OpenAI chat sends message names from provider options** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-chat-message-names)
- **Azure merges azure and openai options** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-option-namespaces)
- **OpenAI image edits send moderation in a fixed field order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-image-edit-moderation)
- **OpenAI MCP tool validation names tunnelId** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-mcp-tunnel-refine)
- **OpenAI transcription sends timestamp granularities only for verbose_json** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-transcription-granularities)
- **xAI video output upload URLs are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-video-upload-url)
- **Black Forest Labs webhook image calls poll get_result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bfl-webhook-get-result)
- **Baseten DeepSeek V4 Pro JSON output uses an instruction** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#baseten-deepseek-json)
- **Baseten chat uses ModelURL only for /sync/v1** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#baseten-chat-modelurl)
- **Gladia polls for up to 10 minutes** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gladia-poll-timeout)
- **Gladia uses the v2 options** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gladia-v2-options)
- **Gladia sends solaria model IDs** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gladia-model-id)
- **Gladia provider metadata carries the whole result** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gladia-provider-metadata-addons)
- **Deepgram speech rejects callback** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deepgram-speech-callback)
- **Deepgram batch transcription warns about the streaming option** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#deepgram-batch-streaming-warning)
- **Cartesia speech sends locale, normalization and generation options** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cartesia-speech-options)
- **Cartesia streaming transcription sends keyterms and turn thresholds** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cartesia-ink2-keyterms)
- **Replicate cancels abandoned predictions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#replicate-cancel)
- **AssemblyAI flat error bodies give their message** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#assemblyai-flat-error)
- **Azure Entra tokens are not sent on cross-origin redirects** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#azure-entra-token-redirects)
- **Authorization is dropped on a redirect to another origin** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#redirect-authorization-cross-origin)
- **Anthropic schema constraint descriptions keep the schema's key order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-constraint-key-order)
- **GenerateResult.RawResponse is the raw response JSON (Gemini, Anthropic)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#raw-response-body)
- **Groq API errors keep response headers, body and data** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#groq-error-response-details)
- **HeyGen status polling validates redirects and keeps the API key on its origin** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#heygen-status-redirects)
- **HeyGen error data and response headers follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#heygen-error-data-headers)
- **Perplexity tools without a description send no description key** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#perplexity-tool-description)
- **Typed provider option structs are read** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#typed-provider-options-structs)
- **Replayed Google code execution parts are validated** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-code-execution-replay)
- **Groq usage no longer fills text / image input details** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#groq-usage-input-details)
- **HeyGen, Gateway decision and Vertex embedding option errors use the TS text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-option-error-text-r89)
- **xAI API errors keep the response and use the TS message** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-api-errors)
- **More provider API errors keep response headers, body and data** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provider-error-response-metadata)
- **Responses embedded-error and no-output errors carry the response** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#responses-embedded-error-metadata)
- **OpenAI-family error messages follow the error schema** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-error-message-schema)
- **Together rerank, xAI transcription words and video headers follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-family-r89-details)
- **KlingAI and ByteDance errors unwrap to a ProviderError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#klingai-bytedance-error-response)
- **Gladia poll errors carry data** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gladia-poll-error-data)
- **MiniMax video API errors unwrap to a ProviderError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#minimax-video-api-error-unwrap)
- **Prodia, DeepInfra, Cerebras, QuiverAI and web fetch details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tools-providers-r89-wire)
- **Google Vertex Anthropic: call headers win over provider headers** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vertex-anthropic-header-precedence)
- **Anthropic option checks and forced-tool fallback follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-r89-request-options)
- **Bedrock option fallback, error data and guard content follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-r89-options-and-errors)
- **ElevenLabs conflict text, keyterm length and realtime errors follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#elevenlabs-r89)
- **Anthropic and Bedrock error texts quote values like TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-template-quotes)
- **Anthropic results have one text part per text block** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-text-block-parts)
- **A null Anthropic tool input stays null** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-null-tool-input)
- **Anthropic and Anthropic AWS error texts and types** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-r90-error-texts)
- **Anthropic prompt details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#prova-r90-details)
- **Gemini streamed tool arguments: dotted array indexes** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-dotted-array-indexes)
- **Gateway takoSearch: variable cost options and output schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-tako-search-variable-cost)
- **Mistral: generated reasoning ids and normalized conversation errors** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mistral-r90-stream-ids-errors)
- **Provider request text follows TS JSON.stringify and defaults** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provg-r90-request-text)
- **Numeric options and usage counts follow z.number()** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provg-r90-numbers)
- **Gateway option and response parsing** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-r90-parsing)
- **Google and Vertex transcription and live streams** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-r90-transcription)
- **OpenAI parallel tool call metadata keeps the input once** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-parallel-wrapper-input)
- **xAI video options error text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#xai-video-option-error)
- **OpenAI and Azure media details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openai-azure-r90-details)
- **Multipart uploads use the TS FormData part format** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#multipart-formdata-parts)
- **QuiverAI reasoning history without QuiverAI metadata has no item id** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#quiverai-reasoning-replay-item-id)
- **QuiverAI, MiniMax, Voyage, DeepInfra and Topaz request details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#tools-providers-r90-wire)
- **Cartesia, Replicate, fal and Baseten details follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provc-r90-details)
- **An unfollowed 3xx response is an HTTP error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#http-3xx-status-error)
- **MiniMax video errors follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#minimax-video-errors)
- **Cerebras and DeepInfra unsupported model types return NoSuchModelError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cerebras-deepinfra-model-types)
- **Bedrock connection failures are the retryable Cannot connect to API ProviderError** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bedrock-transport-errors)
- **Anthropic DoStartBatch drops a configured anthropic-beta header** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#anthropic-batch-config-beta)
- **ElevenLabs streaming-only warning names experimental_streamSpeech** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#elevenlabs-streaming-warning-text)
- **AWS credential errors use the TS texts** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#aws-sigv4-credentials-text)
- **Go-typed provider option literals are honored** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#typed-provider-option-literals)
- **OpenAI-compatible embedding batches are not split by bytes** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#compat-embedding-byte-budget)
- **Moonshot and DeepSeek content order is reasoning, tool calls, text** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#moonshot-deepseek-content-order)
- **Prompt.Text is one user message for Responses-style models** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#responses-prompt-text)
- **Fireworks async image errors are ProviderErrors with the status text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fireworks-async-image-errors)
- **Open Responses provider key wins over the openai alias** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#openresponses-provider-key)
- **OpenAI, Azure, xAI, Moonshot, DeepSeek and Open Responses details follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provb-r91-details)
- **ByteDance HTTP error text is the TS message** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bytedance-error-text)
- **Baseten media models return NoSuchModelError** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#baseten-media-no-such-model)
- **BFL, AssemblyAI, fal and Gladia errors follow TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#provc-r91-media-errors)
- **ByteDance usage metadata and status poll key** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bytedance-metadata-poll)
- **Media providers join repeated response headers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#media-response-headers-joined)
- **BFL video polls send the X-Key credential** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bfl-video-poll-auth)
- **Google decision models use the native Gemini Decisions API** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-decision-native)
- **Google Interactions usage has no per-modality input details** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-usage-details)
- **Google Interactions background streams report GET failures in the stream** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-interactions-background-get)
- **Missing API key returns LoadAPIKeyError unwrapped** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#load-api-key-error-unwrapped)
- **Google and Vertex transport failures read Cannot connect to API** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#google-vertex-connect-errors)
- **Gateway batch status omits inconsistent request counts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-batch-request-counts)
- **Gateway invalid success bodies give the TS error text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gateway-success-schema-errors)
- **Gemini sends args: null for a JSON null tool input** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-null-tool-args)
- **Vertex xAI provider headers win and compatible options are validated** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vertex-xai-headers-validation)
- **Gemini streams File Search and RAG document sources** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#gemini-stream-document-sources)
- **Media download errors keep the server reason phrase** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#download-status-reason-phrase)
- **Provider option number keys, integers and URLs follow JS and zod** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#option-schema-js-coercion)
- **Rev.ai job errors, headers and config order follow TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#revai-r91-details)

#### MCP

- **MCP OAuth rejects unbound pre-registered confidential credentials** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-issuer)
- **MCP clients have no default request timeout** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-no-default-timeout)
- **An explicit MCPToolSchema.InputSchema schema is the tool's Parameters, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-explicit-input-schema)
- **MCP tool, prompt and resource calls answer input_required through OnElicitationRequest, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-input-required)
- **MCP OAuth token bodies and the event callback query follow URLSearchParams order and escaping** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-form-order)
- **MCP HTTP transport reports send failures to OnError as often as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-http-onerror-count)
- **MCP HTTP transport starts once** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-http-connect-once)
- **MCP SSE transport stays connected after a stream read error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-sse-read-error-connected)
- **MCP stdio transport disconnects when the server exits** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-stdio-server-exit)
- **MCP OAuth always sends the resource parameter** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-resource-always)
- **MCP OAuth clears refresh tokens without an authorization server binding** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-unbound-refresh-token)
- **MCP OAuth discovery rejects HTTPS to HTTP redirects** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-oauth-https-downgrade)
- **MCP responses match requests by JS Number(id)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-response-id-number)
- **MCP elicitation params follow the TS schema** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-elicitation-schema)
- **MCP send errors are unwrapped** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-send-errors)
- **MCP does not fall back to initialize after a discover network error** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#mcp-discover-network-error)

#### Code mode

- **Code mode has a default 10 s CPU limit** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-cpu-limit)
- **Code mode caps concurrent scripts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-max-workers)
- **wazero is v1.12.0** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#wazero-version)
- **Code mode validates a bare schema.Validator Parameters** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-bare-validator-validated)
- **Code mode rejects `import(` written with JavaScript whitespace or multi-line comments** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-dynamic-import-js-whitespace)
- **Code mode detached bridge request errors name the tool, as TS** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-detached-tool-name)
- **Code mode host calls start when observed, run concurrently and fail the run when left unawaited or pending, as TS run** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-host-call-bridge)
- **Code mode resumes fail with a ProtocolError when the replay diverges from the recorded ledger** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-replay-checks)
- **Code mode Date.now() and Math.random() are deterministic per run and replayed on resume** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-deterministic-clock)
- **Code mode freezes the JavaScript intrinsics before the script runs, as TS run** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-frozen-intrinsics)
- **Code mode tools names inherited from Object.prototype call those functions** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-prototype-tool-names)
- **A code mode script awaiting a promise nothing settles times out** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-never-settling-timeout)
- **Code mode tool arguments devalue cannot serialize fail at the call** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-unserializable-arguments)
- **Code mode honors approval funcs and statuses and fails closed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-approval-fail-closed)
- **Code mode honors Tool.ValidateInput** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-validate-input)
- **GetCodeModeInterrupt reads Go generate results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-interrupt-go-results)
- **Code mode script errors keep their own text** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codemode-error-text)

#### Harness, ACP and fx

- **ACP lifecycle state persisted by v0.5.x does not resume** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#acp-state)
- **A failed harness stop-condition suspension fails the turn** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-stop-suspend)
- **Harness adapters identify as ai-sdk-harness-id** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-client-app)
- **With AgentSettings.Telemetry unset, the harness agent fires no telemetry** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-telemetry-unset)
- **Harness usage is the turn total, and a failed OAuth refresh fails start** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-usage-oauth)
- **fx: the built-in terminal tool is removed** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fx-terminal-removed)
- **fx has an opt-in libfx runtime** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#fx-libfx-opt-in)
- **Harness continuations accept pointer content parts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-continuation-pointer-parts)
- **Harness tool specs send a tool's input schema in its key order (ToolSpec.InputSchema may be an *schema.OrderedSchema)** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-tool-spec-schema-order)
- **ACP sessions need ResolveHostToolCall and IsMcpToolCall; acp.LegacyResolveHostToolCall keeps the v0.5 matching** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#acp-host-tool-resolver)
- **A harness without a bootstrap recipe sets up its sandbox on the first sandbox operation, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-deferred-sandbox-setup)
- **Denied host tool results carry the execution-denied model output, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-denied-tool-result)
- **Harness tool calls carry nativeToolName in their provider metadata, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-native-tool-name-metadata)
- **A built-in approval next to a host tool call pauses the turn and reports the unechoed host results, as TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-builtin-approval-host-result-pause)
- **Bridge frames sent while the socket closes are queued and resent after the reconnect, as TS** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bridge-closing-send-queued)
- **The Claude Code start frame carries readableSkillRoots, deferredToolUseId and the new settings, with the ai@7.0.162 bridge** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#claudecode-start-frame-fields)
- **Vercel policy mutations refuse to drop response rules; a custom PolicySandbox needs PolicySessionInspector for object policies** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-response-rules)
- **Vercel sandboxes use v3 templates, keep Persistent, resolve relative working directories against the session cwd, and kill a spawned command when its ctx ends** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-sandbox-r85)
- **Cursor sessions report client-app version 1.0.88** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#cursor-client-app-r86)
- **ACP `name: ""` counts as a name; new `acp.ToolCall.HasName`** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#acp-toolcall-hasname)
- **The OpenCode enterprise domain is the WHATWG URL host** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#opencode-normalize-domain)
- **OpenCode GitLab credential brokering is limited to the chat token exchange (security)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#opencode-gitlab-chat-route)
- **claude-code turns fail after 120 s without a first bridge event (`TurnStartTimeout`), and a start lost on a reconnect is resent safely or fails clearly** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#claude-code-turn-start-timeout)
- **`bridge.WithBridgeToken` serializes the endpoint as a WHATWG URL** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bridge-token-whatwg-url)
- **Credential brokering, GitHub Copilot hosts and Vercel port endpoints parse URLs like WHATWG** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-credential-urls-whatwg)
- **The embedded bridges are TS 3fab50aa9b (idempotent starts, Claude Code gateway model overrides)** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-bridges-3fab50aa9b)
- **Vercel allow-all / deny-all policies are sent as {"mode": ...}** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-network-policy-mode-wire)
- **Vercel Sandbox.Update applies a policy to the session before PATCHing the sandbox** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-policy-update-order)
- **Vercel Sandbox API requests are retried and interrupted streams recovered like @vercel/sandbox 3.9.0** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-sandbox-retries-and-streams)
- **Vercel Sandbox request bodies carry the @vercel/sandbox 3.9.0 defaults** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-sandbox-request-bodies)
- **OpenCode keeps credentials and URLs that are set but empty** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#opencode-empty-credentials)
- **Vercel `Sandbox.Update` extends a running session's timeout** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-update-extends-timeout)
- **A Vercel Sandbox OIDC token sets the team from its owner_id claim** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-jwt-claims)
- **The Vercel Sandbox reads the OIDC token from VERCEL_OIDC_TOKEN_FILE first** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-oidc-token-file)
- **Vercel OIDC token failures are LocalOidcContextError authentication failures, and empty claims are accepted** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-oidc-token-decoding)
- **Vercel Sandbox request queries keep empty values and the TS key order** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-query-empty-values)
- **Vercel network policy inspection lists sessions** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-policy-inspection-list-sessions)
- **Vercel snapshot listing follows every page** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-snapshot-pagination)
- **Vercel array owner_id sends one teamId per element** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-owner-id-array)
- **A pending host tool approval pauses the turn without waiting for other host results** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-host-approval-pause)
- **claude-code turns keep only their own bridge events** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#claude-code-turn-start-id)
- **Bridge suspend closes the WebSocket with the reason suspended** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bridge-suspend-close-reason)
- **claude-code apiKeyHelper runs without sh on PATH** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#claude-code-apikeyhelper-shell)
- **Harness bridges are resynced to ai@7.0.165** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-bridges-0f3a85f92d)
- **Grok Build file reads need ACP approval** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#grok-build-read-approval)
- **Cursor and Grok Build ignore repository configs, hooks and MCP servers** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#repo-config-isolation)
- **OpenCode on Windows reads the XDG auth store first** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#opencode-windows-auth-store)
- **Vercel Sandbox and MCP transports drop credentials on cross-origin redirects** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#redirect-credentials-cross-origin)
- **claude-code skips apiKeyHelper when a credential is set** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#claude-code-apikeyhelper-direct-credential)
- **HarnessAgent PrepareCall tools keep the harness builtins** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-preparecall-tools-builtins)
- **HarnessAgent warns when the Sandbox setting is used** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-sandbox-deprecated)
- **HarnessAgent error texts** (error text only). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-agent-error-texts)
- **Vercel legacy sandbox provider warns and honors a cancelled ctx** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-legacy-provider-deprecation-warnings)
- **Vercel sandbox reads treat file-not-found errors as missing files** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#vercel-sandbox-file-not-found)
- **Codex: an empty CODEX_HOME and empty refresh tokens** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#codex-home-empty)
- **Harness auth files and lifecycle state follow TS** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-r90-details)
- **Harness file parts stay file parts** (silent behavior change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#harness-tool-output-file-part)
- **Harness bridges authenticate with a per-session public key** (wire/protocol change). [Migration](../docs/08-migration-guides/from-v0.5-to-v0.6.mdx#bridge-public-key-auth)

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
- **The registry lists providers in TS `Object.keys` order**: registration order, with array-index names (`"0"`, `"1"`, ...) first in numeric order.
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

- Round 91 (review round 91; TS 2ebc78b654, ai@7.0.170): provider-executed MCP calls get no client tool error; GenerateText has no 1000-step ceiling; StreamText and the agent loop download generated files with `ExperimentalDownload`; code mode approvals fail closed and honor `ValidateInput`; invalid tool calls give one UI error; `ValidateUIMessages` reports TS issues; legacy telemetry writes the TS V4 prompt; span `exception.type` is the TS error name; Anthropic batch betas, Bedrock and Google connection errors, typed provider-option literals in every provider, Gateway batch V4 results, Gemini document sources, BFL video poll authentication, 32-bit seeds (fal, Deepgram, BFL, Stability, Prodia), WebSocket message reassembly, aborted calls returned unchanged by every provider, and Rev.ai / ByteDance / Gladia error codes.
- Step-end events carry the step (review round 72). `OnStepFinishEvent`
  has `Content` and `Performance`, and `GenerateStepRequest` has `Messages`
  (additive fields), filled by `GenerateText`, `StreamText`, the legacy
  `ToolLoopAgent` and the harness from the recorded step; the end event's
  `Request.Messages` is the final step's. TS `onStepEnd` receives the
  `StepResult`, and `onEnd.request` is the final step's request. Set
  `Include.RequestMessages` to get the messages.
- A provider tool result streamed with only `IsError` set is a tool error in
  `StreamText` (UI `tool-output-error`, not in `ToolResults`), and streamed
  provider results carry the call's tool metadata (review round 72).
- A model that returns no result or no stream (`(nil, nil)`) fails
  `ExperimentalDecide`, `GenerateText`, `StreamText`, `GenerateObject` and the
  legacy `ToolLoopAgent` loop with an error; they panicked.
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
- A struct-tag schema (`schema.NewSimpleStructSchema`, `schema.NewStructSchema`) validates the decoded JSON value: `ObjectOutput`, `ElementOutput`, `ArrayOutput` and `GenerateObject` with a struct schema, and code-mode tools with a struct `Parameters`, always failed in v0.5.2 with `validator: (nil map[string]interface {})` (review round 80).
- When one ctx drives `StreamText` and a UI helper, a cancellation no longer writes the abort chunk at random: once the helper ctx is cancelled nothing more is written (review round 80).
- Google inline data, Interactions files and TTS audio decode unpadded and URL-safe base64 like TS `convertBase64ToUint8Array` (review round 80).
- `CreateUIMessageStreamWithOptions` no longer panics ("WaitGroup is reused before previous Wait has returned") when a `writer.Merge` from another goroutine races the end of the last merged stream (review round 81).
- `ChunksContext` with a cancelled ctx returns the chunk to the shared cursor instead of sending it (review round 81).
- The telemetry integrations drop the embed and rerank span map entries on `OnError` / `OnAbort`, so a call without `OnEmbedEnd` / `OnRerankEnd` no longer leaks one entry per span (review round 81).
- Perplexity embeddings: `DoEmbed` no longer panics on a response without embeddings (review round 81).
- Perplexity decisions: an `image/` media type is sniffed from the data (review round 81).
- The repair `InputSchema` callback returns the empty object schema for a tool whose schema value is nil (typed nil pointer, nil `Validator`, nil map) instead of panicking (review round 82).
- Tool conversion (`providerutils/tool`, `schema.ParametersMap`, `schema.ApplyDefaults`) treats a typed nil or zero schema value (`(*schema.SimpleJSONSchema)(nil)`, `&schema.SimpleJSONSchema{}`) as no schema instead of panicking (review round 82).
- Marshaling a nil or zero `*schema.SimpleJSONSchema`, `*schema.JSONSchemaValidator` or `*schema.OrderedSchema` (`json.Marshal`, `MarshalJSON`, `schema.MarshalSchema`) gives `{}` instead of panicking, and workflow `SerializeToolSet`, the tool converters and the Anthropic sanitizer treat such a value as no schema (review round 83).
- A nil or zero `*schema.OrderedSchema` no longer panics in `Validate` / `Keys` (review round 84).
- `ExperimentalStreamTranscribe` / `ExperimentalStreamTranslate` no longer panic when a failed call has no `Audio` stream (review round 82).
- `StreamTextSingleConsumerResult.Usage` doc: it is the total usage, as `StreamTextResult.Usage` (review round 82).
- Response messages' text parts no longer carry the content part's citations (TS `toResponseMessages`) (review round 82).
- A UI stream piped with `PipeUIMessageStreamToResponseWithInit` on the request ctx reports `isCancelled` on a client disconnect every time; Go's child-context cancellation order let StreamText's abort part through about once in 4,000 runs (`isAborted`) (review round 85).
- The harness deferred sandbox session forwards `Stat`, `ListDirectory` and `DeleteFile` (TS `delegateSandboxOperations`) (review round 85).
- Review round 86b (low edge inputs): JS trim / regexp / lowercase / URLSearchParams / one-trailing-slash rules across core, providers, MCP and harness; Ajv u-mode patterns for serializable tools; `jsonorder.Parse` rejects trailing text like `JSON.parse`; `ExtractAPIErrorMessage` keeps the message as sent; the workflow tail index skips a Latin-1 0xA0; `ValidateUIMessages` drops `rawInput: null` from states without it; a panicking tool-end callback keeps live tool results; Decide accepts an `""` choice option and typed criteria; invalid tool calls take `Title` / `ToolMetadata` from the tool; fx checks the WHATWG pathname for Gateway model requests (V061 7).
- **Round 90** (TS 9f7a842ae1): `InvalidArgumentError.Value` is set where TS passes `value` (object generation input, `maxOutputTokens` / `maxRetries` / `streamRetries` / `toolCallConcurrency`, batch requests and provider, data URLs, Output array bounds, `ValidateUIMessages`, tool search, tool callers, steering, Decide input, `ExtractReasoningMiddleware` delimiters, Embed dimensions and chunk size, WorkflowAgent options, bash and web fetch tool options); the Gateway default OIDC path reads the `ContextWithVercelOIDCToken` token before `VERCEL_OIDC_TOKEN`; MCP event subscriptions store the WHATWG href of the callback URL, reject null webhook fields and order the `events/subscribe` params like TS; Vertex MaaS foreign-origin image downloads are pinned by tests to the validated transport (TS acdccac63d); the OpenAI Responses parallel wrapper input is kept once (TS 22b8158276); the Moonshot schema normalizer visits array children once (TS c480f3f726).

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
- Google streams: server tool results carry `ToolResult.ProviderMetadata` (`serverToolCallId`, `serverToolType`), as the stream content and UI tool-output parts read it (review round 82).
- ElevenLabs timestamped speech and music responses report the response JSON as `Response.Body` (TS rawValue) (review round 85).
- Google files upload no longer panics when the `x-goog-upload-url` header does not parse as a URL (the ignored `http.NewRequest` error left a nil request; now the TS fetch error `Failed to parse URL from <url>`); the init and poll requests check their errors too (review round 86b, high).
- Review round 89: a Perplexity 2xx body that fails the response schema no longer crashes the process (the error was its own cause, so `Error()` recursed); Gemini and Anthropic `RawResponse` carry every key the API sent; Deepgram and AssemblyAI response timestamps are taken before the request, as TS; TypeSafe API errors carry `Data` and the HTTP cause.

### Workflow, MCP, code mode and harness

- Codex, Deep Agents and OpenCode turns remove every listener when a replayed `finish` / `error` settles the turn while its listeners register; Deep Agents no longer delivered the next turn's error to the settled turn (review round 87).
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
- Review round 89: a HarnessAgent `ai.ObjectOutput` with a struct or `*schema.OrderedSchema` schema forwards the schema to the adapters again (every adapter had failed structured output after an earlier v0.6 change); harness tool model output errors and image detection follow TS `normalizeHarnessToolModelOutput`; `HARNESS_DEBUG_LEVEL` is used as given.

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
- When the source stream of `ToUIMessageStream` fails (not an abort), Go
  writes one standard `error` chunk (masked text unless `OnError` says
  otherwise), ends the stream cleanly and returns the error; TS errors the
  stream and writes no `error` chunk. Same API and chunk protocol as TS, and
  `useChat` shows the error text. Caveat: client logic that reacts to a
  dropped connection (for example reconnect-on-network-error) sees a clean
  end with an `error` chunk instead (v0.5.x behavior, owner decision). The SSE helpers then end without `data: [DONE]` and return the error, as TS (round 87e).
- A `WorkflowAgent.Stream` run cancelled mid-stream ends its
  `ai.workflowAgent.stream` root with `OnAbort`; TS reports a thrown abort to
  the root `onError` and leaves the root open for an abort its iterator
  returns. Go fires the root telemetry `OnError`/`OnEnd` after the user
  `OnError`/`OnEnd`; TS interleaves them (coordinator ruling, round 68).
- **ToUIMessageStream on a raw provider stream** is framed (start,
  finish-step, finish, incomplete-stream error); start-led sources map one to
  one as in TS (owner ruling 2026-10-30, round 71).
- `RuntimeContext` stays nil when the caller sets none; TS defaults it to
  `{}` (owner preference for idiomatic Go, round 71).
- **Abort keeps the parts produced before it**: the full and UI streams keep the aborted step's parts up to the abort part; TS drops parts still queued in its pipeline (round 72).
- **Tool call input keeps undeclared keys**: Go strips undeclared keys only for the audited flat Anthropic provider tools; other provider tools keep them (S7) and user tools keep them, as TS `jsonSchema()` tools (round 87e).
- **Reasoning lists hold reasoning text only**: reasoning file parts are returned by `StepResult.ReasoningFiles()` and are in `Content` (TS includes them in `reasoning`) (rounds 72, 73).
- **WorkflowAgent root telemetry end**: the `ai.workflowAgent.stream` root gets end attributes and ends; both TS OTel integrations throw while building them (no `finalStep` on the root end event) (round 73).
- **ConsumeStream returns the error**: `StreamTextResult.ConsumeStream()` returns the stream error; TS `consumeStream()` swallows it (round 73).
- **Merged UI message stream order**: chunks of merged UI stream sources interleave by goroutine scheduling, each source in order; TS follows its microtask queue (round 73).
- **Message metadata merge with non-object values**: UI message metadata followed by a string or array (or a non-object followed by any value) is replaced; TS `mergeObjects` merges the value's index keys into a copy of the existing value. A number or boolean after an object keeps the object, as TS (coordinator ruling, round 75).
- **Anthropic batch results of id-named provider tools across processes**: `ExperimentalBatch` restores the `Name` of a provider tool that keeps its constructor id (`anthropic.bash_20250124`) only when the results are read through the Provider that started the batch; results read by another Provider value or process report the wire name (`bash`). Name the tool after its wire name to get the same name everywhere (round 75).
- **UI message stream end event finish reason**: the `OnEnd`/`OnFinish` event of `CreateUIMessageStreamWithOptions` reports the last finish chunk's `finishReason` as written (normally a `string`) or `nil`, and `ToUIMessageStream` a `types.FinishReason` (`""` when unset), the v0.5 shapes; TS reports the chunk's string or `undefined` in both (round 77).
- **StreamText reads the provider stream without a reader**: the model call runs until the `StreamText` ctx ends, also after a client disconnect or after a `ConsumeSSEStream` callback returns, as in v0.5; a TS stream nobody reads stalls under backpressure. Pass `r.Context()` or use `StreamTextLowMemory` (round 79).
- **Invalid tool call input that is not a JSON object**: the raw text is kept in `ToolCall.RawArguments` / `ToolCallContent.Input` only (`Arguments`, `ToolResult.Input`, `ToolErrorContent.Input` are maps), JSON that is not an object is invalid also without `Parameters`, and validator / parser messages are Go's (round 79).
- **Struct-tag schemas: Go-only choices**: floats are a bare `number`, `time.Time` has no Z-only `pattern`, `[]byte` has no base64 keywords, a described recursive reference is `allOf` inline, pointer slice elements are not nullable, and `dive` rules and alternatives are left out (rounds 82-83).
- **Tool output JSON key order in replayed prompts**: serialized tool output JSON keeps Go map key order (three openresponses TS replays skipped) (round 79).
- **Embedding usage across EmbedMany batches**: when a call or batch sent no usage, `Usage.Tokens` is the sum of the reported batches (0 when none), as v0.5.2, and the telemetry attribute is dropped; TS returns NaN (S6, round 87e).
- **Legacy telemetry tool spans on error**: Go's legacy `OnError` ends still-open tool spans with the error status; TS never ends them (round 80).
- **URL parsing: three IDNA edge cases**: Go's WHATWG URL serializer differs from Node on three classes of `xn--` labels (`xn---xii`, a doubly prefixed label, Arabic Extended-B Bidi) (round 84).
- **Typed URL fields**: a Go URL field that does not parse fails with `Invalid URL`; TS treats such a plain string as base64 data (round 84).
- **Prompt file download errors**: the default prompt-file downloader keeps the v0.5 `SSRFError` texts and its up-front DNS check; TS reports `DownloadError` texts (round 84).
- **TS test replays skipped in round 84**: eight TS cases are skipped for harness limits or Go idiom (azure-keyed OpenAI Responses case, `openai.chat` hint text, chat.ts replay start, a call-site spy, channel errors, provider signal after step end) (round 84).
- **Input checks TS removed in ai@7.0.155**: Go keeps the input checks TS removed in ai@7.0.155, with their v0.5 texts (ruling 5, round 85) (round 85).
- **Durable workflow telemetry**: Durable workflow telemetry: only the operation-name mapping is ported (ruling 6, round 85) (round 85).
- **ACP callbacks checked at session start**: ACP `IsMcpToolCall` / `ResolveHostToolCall` are checked when the session starts, not in `CreateACP` (round 85).
- **Vercel policy inspection source**: Vercel policy inspection reads the last session response instead of `listSessions()` (round 85).
- **SSE chunk-boundary artifacts**: SSE parsing ignores chunk boundaries where eventsource-parser does not (round 85).
- **UI streams close open blocks at a step's finish**: ToUIMessageStream synthesizes text-end / reasoning-end for blocks still open at finish-step (round 85).
- **Google videoMetadata validation errors**: An invalid Google `videoMetadata` is a ValidationError, not a raw ZodError (round 85).
- **Deferred sandbox optional operations**: A deferred harness sandbox session always has Stat / ListDirectory / DeleteFile and reports ErrUnsupported when the sandbox lacks them (round 85).
- **TS test replays skipped in round 85**: TS test replays skipped in round 85: harness limits (class functions, async auth tokens, random ids, a cancelled empty stream, deep-argument truncation) (round 85).
- **ReadUIMessages after a terminating error**: `ReadUIMessages` with `TerminateOnError` calls `OnError` once and stops reading (round 86).
- **Live tool results and telemetry order**: StreamText streams a tool result after the user tool-end callback and before the telemetry tool end (round 86).
- **WebSocket handshake headers with an empty value**: WebSocket handshakes do not send a header whose value is `""` (round 86).
- **Speech translation `TargetLanguage: ""`**: An empty speech translation `TargetLanguage` counts as missing (round 86).
- **Serializable tool patterns RE2 cannot compile**: A serializable tool `pattern` with lookaround or a backreference is skipped (RE2) (round 86b).
- **Tool call metadata from custom providers**: A valid tool call keeps a custom provider's `ToolMetadata` / `Title` (v0.5 behavior) (round 86b).
- **Files results: empty filename on the result struct**: File results: an empty returned `filename` reads as absent on the `Filename` field; the metadata map keeps it (round 86b).
- **MCP OAuth client authentication parameters**: MCP OAuth: extra `AddClientAuthentication` form parameters are sent in key order (round 86b).
- **ElevenLabs stream audio format rate**: ElevenLabs: a fractional native output-format rate has no `Rate` (`*int`) (round 86b).
- **Sandbox text: lone surrogates and an empty encoding**: Sandbox text: a UTF-16 lone surrogate reads as U+FFFD and `Encoding: ""` is utf-8 (round 86b).
- **AI Gateway OIDC token refresh**: AI Gateway: OIDC refresh is opt-in (`Config.OIDCTokenFunc`), in memory, and never writes or clears the Vercel CLI files (round 87).
- **WorkflowAgent ExperimentalTransform**: WorkflowAgent `ExperimentalTransform` still works (deprecated); TS removed it (round 87).
- **claude-code TurnStartTimeout zero**: claude-code `TurnStartTimeout: 0` is the 120 s default; a negative value disables it (round 87).
- **Mistral voice agent browser transport**: Mistral voice agent: server session helpers only; the LiveKit browser transport is TS-only (round 87).
- **Mistral realtime transcription WebSocket hint**: Mistral realtime transcription socket errors have no WebSocket-implementation hint (round 87).
- **Gateway streaming image decoding**: Gateway streaming image decoding validates the placeholder skeleton (round 87).
- **Empty string message content**: A user message whose only part is an empty text part loses it; TS keeps a string `content: ''` (round 87).
- **Vercel Sandbox OIDC token refresh**: Vercel Sandbox: OIDC refresh is opt-in (`OIDCRefresh`), in memory, and follows `@vercel/oidc` 4.0.0 (the AI Gateway: 3.2.0) (rounds 87b, 87c).
- **Vercel Sandbox user agent**: Vercel Sandbox sends the Go SDK user agent, not `vercel/sandbox/<version> (Node.js/...)` (round 87b).
- **Vercel Sandbox JSON Lines errors under backpressure**: Vercel Sandbox: a complete invalid JSON Lines record is always a syntax error after every earlier record (the TS fast-consumer result) (round 87b).
- **Provider usage and runtime context attributes: colliding keys in a Go map**: Colliding provider usage / runtime context keys in a Go map: the later key in sorted order wins (round 87b).
- **Empty tool and response format descriptions**: A `Description: ""` tool or response format has no `description` (v0.5.2 string fields) (round 87b).
- **Realtime error events with an empty code**: Realtime error events: `code: ""` reads as no code (`RealtimeServerEvent.Code` is a v0.5.2 string) (round 87b).
- **Open Responses and AI Gateway: field values of an unexpected JSON type**: Open Responses / AI Gateway: typed fields read a mistyped value as `String(value)` or zero; TS passes it on (round 87b).
- **Code-mode tool arguments**: Code mode serializes tool call arguments with `JSON.stringify` (functions dropped, a circular value fails the call when it starts); every `tools` property is a tool; a script returning while an approval is pending always fails (round 87c).
- **Vercel OIDC 4.0.0: keyring login and CLI config**: Vercel Sandbox: the CLI login is read from `auth.json`; the Vercel CLI is never run for keyring storage (round 87c).
- **Vercel OIDC token parse errors**: Vercel OIDC token parse and missing-file causes carry Go's error texts (round 87c).
- **Vercel Sandbox snapshot listing bound**: Vercel Sandbox snapshot listing follows at most 20 pages (round 87c).
- **HTTP status text on a ResponseWriter**: `Pipe*` helpers cannot send a custom status text (round 87c).
- **prepareStep `steps`**: `PrepareStep` gets a copy of the recorded steps, not the live array (round 87c).
- **You.com tools**: You.com REST tools follow the pre-3.0 plugin; `APIKey: ""` falls back to `YDC_API_KEY` (round 87c).
- **TS assertions that cannot apply to Go**: 10 TS provider-test assertions are marked n/a in `tsfixture` (pass-through objects a typed Go result cannot hold, reference identity, Go map key order); every other one is evaluated or hand-ported (round 87d).
- **Google credential loading errors**: Go `oauth2` token source and ADC failures carry a Go prefix, with the error as the cause (round 87d).
- **Errors of a caller-supplied HTTP transport**: outside the AI Gateway, stay the retryable "Cannot connect to API" error; TS returns a fetch rejection unchanged (round 87d).
- **Provider option spellings kept from v0.5**: alibaba accepts the `openai-compatible` keys and raw snake_case option names documented in v0.5.2; zai forwards `user` and `verbosity`; TS drops them (round 82).
- **Hugging Face byte image data**: sent as base64; TS interpolates the bytes as a comma-separated list (round 82).
- **Provider option warning order**: Perplexity unsupported-option warnings follow sorted option names, not the TS object's insertion order (round 82).
- **Upload-only video results**: `Video` is nil and `UploadedVideos` is set; TS throws from a `video` getter (round 88).
- **Gladia model IDs**: IDs other than `""`, `default` and `solaria-*` are not sent and warn, as v0.5.2 documented; TS sends them (round 88).
- **Gladia polling settings**: invalid `PollInterval` / `PollTimeout` fail each call before the upload; TS `createGladia` throws (round 88).
- **Replicate prediction cancel deadline**: the background cancel has a 30 s bound; TS sends it without one (round 88).
- **Deepgram live transcription WebSocket setting**: Go dials with its own client; TS accepts a WebSocket constructor (round 88).
- **Harness HostDisconnectTimeout zero or negative**: the bridge flag is passed only for a positive value (round 88).
- **Grok Build config formatting**: `config.toml` is rewritten by go-toml (round 88).
- **Decision language model version check**: `ai.NewDecisionLanguageModel` does not require a v4 model (S6) (round 89).
- **OpenAI-compatible tool schemas without a type**: `"type": "object"` is added to a tool schema without a type, as v0.5.2 sent (S6) (round 89).
- **Harness diagnostic error name**: an empty `name` prints as `Error` (S1) (round 89).
- **Harness bridge send failures**: a failed write queues the frame for the reconnected socket (S3) (round 89).
- **EmbedMany batch option transform errors**: a failed transform fails before any request of the group is sent (S7) (round 89).
- **Claude Code start frame empty fields**: empty `env` / false flags / empty maps are omitted (S1) (round 89).
- **HarnessAgent PrepareCall and askUserQuestions**: a returned reserved tool is not rejected (S6) (round 89).
- **HarnessAgent empty prompt**: `Prompt: ""` means no prompt (S1) (round 89).
- **Prodia image URL fallback**: the v0.5.2 `imageUrl` fallback stays (S6) (round 89).
- **Anthropic client-side provider tool names**: the caller's tool name is kept (S7) (round 89).
- **Bedrock Converse request details**: empty user blocks, truthy `cachePoint` and top-level `anthropicBeta` are not sent as TS does (S7) (round 89).
- **Bedrock and Anthropic null option values**: explicit `null` is accepted for optional keys (S6) (round 89).
- **Anthropic empty string option fields**: `""` values of five string options are omitted (S1) (round 89).
- **Anthropic Files byte size**: `ByteSize` truncates a fractional size (S1) (round 89).
- **Video response header keys**: xAI / Alibaba keep Go canonical keys (S6) (round 89).
- **Empty binary response**: Fireworks sync images reject an empty 200 body (S3) (round 89).
- **SmoothStream Intl.Segmenter chunking**: `Chunking` takes word, line, a regexp or a `ChunkDetector` (S3) (round 90).
- **Telemetry credential redaction of other providers' MCP tools**: every body with such a wire tool is redacted (S2) (round 90).
- **Custom telemetry redactors**: no `withTelemetryRedaction` hook (S4) (round 90).
- **WorkflowAgent missing provider result warning**: reported through `LogWarnings` (S3) (round 90).
- **Google Interactions warnings**: both `Message` and `Details` are set (S6) (round 90).
- **Fractional counts in integer fields**: truncated into the integer fields (S1) (round 90).
- **Gemini non-streaming string functionCall args**: used verbatim (S7) (round 90).
- **Invalid video operation references**: a plain decode error (S1) (round 90).
- **Gemini streamed tool arguments with conflicting paths**: the stream fails at the conflicting entry (S7) (round 90).
- **Z.AI PrepareRequest / ConvertResponse**: not exposed (S7) (round 90).
- **Vertex Anthropic missing project or location**: an error before sending (S7) (round 90).
- **Bedrock empty endpoint variable**: ignored (S7) (round 90).
- **Bedrock image without a prompt**: `text: ""` is sent (S1) (round 90).
- **Anthropic MCP tool result without its tool use**: converted without a crash (S7) (round 90).
- **Bedrock Converse JSON instruction schema key order**: sorted for a Go map schema (S3) (round 90).
- **Codex tool description ""**: same fingerprint as no description (S1) (round 90).
- **UI message chunk validation on client reads**: any JSON object chunk is accepted (S3) (round 90).
- **DNS protection on by default**: downloads stay validated and DNS-pinned; `DownloadOptions.Transport` opts into a caller transport (S2) (round 91).
- **MCP OAuth: DNS protection without a custom fetch**: no runtime-dependent custom-fetch error (S2) (round 91).
- **toolbash command execution**: Spawn with bounded output and a process-group kill (S2) (round 91).
- **LangSmith deployment stream reconnect**: no stream reconnect; connection error text is always `fetch failed` (S3/S5) (round 91).
- **ExtractJSONMiddleware without text-start**: start-less deltas are extracted and flushed at the end (S6) (round 91).
- **ExtractReasoning separator**: `""` means the default (S1) (round 91).
- **Embed and rerank end events of failed attempts**: an end event per failed attempt (S7) (round 91).
- **WorkflowChatTransport request headers**: JSON content type and event-stream accept kept (S6) (round 91).
- **CreateIDGenerator empty fields**: empty fields mean unset (S1) (round 91).
- **Empty StopWhen slice**: the default stop condition (S6) (round 91).
- **GenAI OpenTelemetry empty-string attributes**: dropped (S1) (round 91).
- **Anthropic custom content parts**: options of any custom part forwarded (S6) (round 91).
- **Chunk schema error text**: compact Go issue text (S3) (round 91).
- **Text and reasoning deltas of never-started blocks**: recorded (S6) (round 91).
- **Non-finite numbers in UI messages**: one ValidationError without a path (S1) (round 91).
- **Claude Code resume state from v0.5.2**: token state resumed by respawning (S6) (round 91).
- **DownloadError cause text**: the cause's message (S3) (round 91).
- **Legacy V4 prompt telemetry details**: normalized messages serialized (S3) (round 91).
- **Bedrock Converse task budget**: forwarded (S6) (round 91).
- **Header-only API key authentication**: no missing-key error (S6) (round 91).
- **Vertex Anthropic labels header key order**: sorted keys (S3) (round 91).
- **xAI cost metadata**: kept (S6) (round 91).
- **xAI video response warnings**: `provider-warning` warnings (S6) (round 91).
- **OpenAI-compatible namespace for provider options**: also read for Z.AI, Fireworks and DeepSeek (S6) (round 91).
- **Ordered schemas inside provider options**: Go map key order (S1) (round 91).
- **Media request body passthrough keys**: sorted (S1) (round 91).
- **Tencent SubAppId and serialization**: decimal string; key never serialized (S1) (round 91).
- **Deepgram error data request id**: `requestId` kept beside `request_id` (S6) (round 91).
- **Realtime transcription WebSocket hint**: widened to ElevenLabs, xAI, Vertex Gemini and the AI Gateway (S3) (round 91).
- **TS test replays skipped in round 91**: the Google Decisions warnings order (Go map order) and the Vertex caller-fetch download tests (DNS protection stays on) (round 91).
- **TS test replays skipped in round 88**: Replicate cancel timing, a Gladia pre-aborted call, DeepInfra and Google tests with injected functions (round 88).
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
