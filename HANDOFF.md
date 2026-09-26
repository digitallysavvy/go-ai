# P1-4 Google/Vertex handoff

**Branch:** `worktree-agent-ab7e72777d5023dca`. Based on `sep23-integration` @ aa4d7d0. WIP commit: c92f69a.
**Instructions:** `state/parity/sep_23_2026/IMPL_INSTRUCTIONS.md`. **PRD:** `prds/p1-high/P1-4-Google-Vertex-Sep23-2026.md`. **Rows:** `state/parity/sep_23_2026/google-anthropic-bedrock.md` (google-vertex and google tables, WG-G1..G5, §Model ID gaps).
**TS source:** read `/Users/arlene/Dev/side-projects/go-ai/ai/packages/{google,google-vertex}/src` as plain files. `git -C ai` is blocked from the worktree, and the working tree is at 7.0.113.
**Scope:** `pkg/providers/{google,googlevertex,gemini}` and the Google parts of `pkg/providerutils/prompt`. Other agents own core, anthropic/bedrock and harness.

## Build/test status (at c92f69a)
- `go build ./...` passes. `go vet` passes on gemini, google, googlevertex and providerutils.
- Tests pass for gemini, googlevertex and every providerutils package.
- **One failing test:** `pkg/providers/google` `TestImageModel_DoGenerate_Gemini_WithFilesOptionsAndWarnings` expects `thinkingLevel "low"` and gets nil. Cause: the rewritten `buildRequest` (gemini/request.go) handles `thinkingConfig`, and the google image model probably relies on the old behavior. Two likely causes: `resolveThinkingConfig` or the thinkingConfig merge, or the image model passing reasoning through GenerateOptions in a way the new code drops. Investigate `google/image_model.go` against `gemini/request.go` ("Thinking config" block).

## Done (committed in c92f69a, WG-G1 request side)
- `prompt/google.go` (new): full port of TS `convertToGoogleMessages`.
  - Entry point `ConvertToGoogleMessages(msgs, GoogleMessagesOptions) (GooglePrompt, error)`. `ToGoogleMessages` is kept as a deprecated wrapper.
  - System messages go to `systemInstruction`. It errors if a system message follows a non-system message. Gemma folds the system text into the first user message.
  - Per-part provider options come from `providerOptionsNames` (Vertex `googleVertex`, `vertex`), with a cross-namespace fallback. ProviderOptions win over ProviderMetadata.
  - `IncludeFunctionCallIDs`: false on Vertex (c57a353).
  - The sentinel moved from gemini into the converter. It is skipped for an unsigned call when a sibling standard call in the same model message is signed (5e5453c).
  - Code-execution replay: `executableCode` / `codeExecutionResult` (2db5621).
  - Server `toolCall` / `toolResponse` replay via `serverToolCallId` and `serverToolType`. A tool-role server result is appended to the previous model content.
  - The legacy tool-result path adds "returned this file/image as a response" (17d66c5).
  - Execution-denied default text is now TS "Tool call execution denied.".
  - Tool-result values are sent raw, not `%v`-formatted.
  - Text files become base64 inlineData.
- `prompt/converter.go`: the Google functions were removed (moved to google.go). `converter_helpers_test.go` and `converter_test.go` were adjusted (denial text).
- `gemini/capabilities.go`: `GetModelCapabilities` (f126649) and `minimumThinkingLevelForGemini3Model` (f69920a). Go regexp has no lookahead, so the `-lite` exclusion is done in code.
- `gemini/schema.go`: `sanitizeResponseJSONSchema` (const becomes enum; the input is never mutated).
- `gemini/prepare_tools.go`: port of TS prepareTools.
  - Function declarations use `parametersJsonSchema`.
  - Forced choices always use ANY (8e90283).
  - Capability gating, with a warning for each unsupported provider tool.
  - Gemini 3 mixed tools use `includeServerSideToolInvocations`, non-Vertex only (01fa606/84f36e0).
- `gemini/request.go`: `buildRequest(ctx, opts, streaming) (body, headers, warnings, error)` ports TS prepareRequest.
  - Warnings: vertex_rag_store, streamFunctionCallArguments, serviceTier, and sharedRequestType/requestType.
  - Vertex-only imageConfig fields are stripped with a warning (5b4a299).
  - Gemini 2.5 on the Developer API drops penalties with warnings (e9bc618).
  - Standalone `threshold` expands to safetySettings (96d40bc).
  - `responseJsonSchema` (2a32459).
  - thinkingConfig = resolved reasoning merged with, and overridden by, providerOptions.thinkingConfig.
  - toolConfig merge (stream args, retrievalConfig).
  - `toolNameMapping` helper (for 63533eb): defined but **not yet used** in response parsing.
- `gemini/reasoning.go`: rewritten to TS `resolveThinkingConfig`.
  - Gemini 3+ (`usesGemini3Features`, excluding only `gemini-3-pro-image`) uses thinkingLevel, with a compatibility warning when the level is remapped.
  - Gemini 2.5 uses thinkingBudget = min(modelMax, round(65536*pct)), no 1024 floor. modelMax is 32768 for 2.5-pro and gemini-3-pro-image, otherwise 24576.
  - `isGemini3Model` now delegates to the capabilities check.
- `gemini/downloads.go`: Vertex tool-result URL downloads (f88c7dc) via `Config.ToolResultDownloadMaxBytes` / `ToolResultDownload`. **Not yet wired.** Set `ToolResultDownloadMaxBytes: gemini.DefaultToolResultDownloadMaxBytes` in `googlevertex/language_model.go`, add a `googlevertex.Config.ToolResultDownloads{MaxBytes}` option, and add a test.
- `gemini/config.go`: new fields `IsVertex`, `MetadataKeys`, `SupportedURLs`, `ToolResultDownloadMaxBytes`, `ToolResultDownload`, `GenerateID`. Only IsVertex and MetadataKeys are consumed so far.
- `googlevertex/language_model.go`: `ProviderOptionsKeys` changed to [googleVertex, vertex, google], plus `MetadataKeys` [googleVertex, vertex] and `IsVertex`. This matches TS read order, and TS writes metadata under both keys. **Response code still writes only `MetadataKey`**; switch it to `providerOptionsNames()` in WG-G2.
- Tests: `gemini/request_test.go`, `prompt/google_test.go`, and reasoning tests rewritten from TS values.
- `tool.ToGoogleFormat` in providerutils/tool is still on the `parameters` key and is no longer used by gemini. Optionally switch it to `parametersJsonSchema` and fix `tool/additional_test.go`.

## Remaining (suggested order)
1. **Fix the failing google image test** (above). Commit as `feat(google): WG-G1 …`, maybe squashing the WIP commit.
2. **WG-G1 leftovers.**
   - 63533eb: use `newToolNameMapping(opts.Tools).toCustomToolName("code_execution")` for code-exec calls and results in generate and stream.
   - a70b027: do not clear `lastCodeExecID` after a result.
   - Remove the `SupportsCodeExecution` gating; TS parses code execution for Vertex too.
3. **WG-G2 response/usage/metadata.** Files: `gemini/language_model.go` (convertResponse), `gemini/streaming.go` (port the TS doStream transform), `gemini/types.go`. TS reference: `google-language-model.ts` `convertGenerateContentResponse` and `doStream`, `convert-google-usage.ts`, `google-json-accumulator.ts`, `google-error.ts`, `google-provider.ts` getSupportedUrls.
   - a3ce307: add `responseId` to Response. Set `ResponseMetadata.ID` on generate, and emit one `ChunkTypeResponseMetadata` in the stream.
   - 56d492f / 2b391f8: add `isConfirmedPromptBlockReason` (excludes "", BLOCK_REASON_UNSPECIFIED and BLOCKED_REASON_UNSPECIFIED). With no candidate finishReason plus a confirmed block, the finish is content-filter. Metadata is always full, with null values. Go has no raw-finish-reason field on GenerateResult (core), so the raw reason goes only in rawResponse/metadata.
   - a22b5b2 (stream): once a prompt block is confirmed, ignore the content of later chunks, but keep usage and metadata. **The Go stream emits finish when finishReason arrives and never emits Usage.** Emit finish at stream end (flush) with `Usage: convertUsage(last)`, like TS.
   - 1284569 / 3ad9da9: usage input = prompt + toolUsePromptTokenCount. Always set noCache/cacheRead and output text/reasoning. `Usage.Raw` = the full usageMetadata decoded as a map (keep raw JSON).
   - a05109d: TS has no audio/video usage fields. Mark it Already Implemented: `modalityTokenCounts` metadata already covers audio/video, and Raw now keeps everything.
   - 8a2482d: `handleError` should parse `{error:{code,message,status,details}}` into `ProviderError.Data`. Check how `internalhttp` errors expose the body.
   - aeea161: read the `x-gemini-service-tier` response header (Google) and prefer it for serviceTier.
   - 949ef93 / 7401c2c: add `SupportedURLs() map[string][]string` on `gemini.LanguageModel` via `cfg.SupportedURLs`.
     - Google `'*'`: the fixed files pattern, `^{baseURL}/files/.*$`, and two YouTube patterns. For gemini-* models except 2.0, also add `^https://.*$` for the TS media list: text/html, css, plain, xml, csv, rtf, javascript, application/json, pdf, image/bmp, jpeg, png, webp, video/mp4, mpeg, quicktime, avi, x-flv, mpg, webm, wmv, 3gpp.
     - Vertex `'*'`: `^https?://.*$` and `^gs://.*$`.
   - 5036db8 / a2609df / cfca634: streamed functionCall `partialArgs` / `willContinue` / terminal chunk / no-args call, with a port of GoogleJSONAccumulator. It must preserve key insertion order (write an ordered map; Go map marshaling sorts keys and would break `closingDelta`), use SetEscapeHTML(false), and track `nullValue` presence via json.RawMessage. Change `Part.FunctionCall` to a named type with `Args json.RawMessage` or interface, plus PartialArgs and WillContinue. Existing tests build anonymous struct literals and must be rewritten. TS treats each complete call independently (no cross-chunk accumulation), so drop the Go `toolInputState` accumulation and its test `TestStream_FunctionCallArgumentsAreAccumulatedAcrossChunks`. Tool-call IDs without an id should use a generated ID (`streaming.GenerateID()`), not the name.
   - Also in TS (not audit rows, but cheap while rewriting): server `toolCall`/`toolResponse` parts (toolName `server:<type>`, dynamic, providerExecuted), grounding `extractSources` into SourceContent, and a text-only thoughtSignature attached to the previous content.
   - 8dbe0be: add `finishReason` to each Gemini image metadata entry (`google/image_model.go`, TS `google-image-model.ts`).
4. **WG-G3.**
   - bd8d172: google embedding `MaxEmbeddingsPerCall` returns 100.
   - 1d9b13b / 427a596: Vertex returns 250 for embeddings. `gemini-embedding-2*` uses `:embedContent` with max 1 per call. TS: `google-vertex-embedding-model.ts`.
   - e369c4d: Gemini image models get `MaxImagesPerCall()` = 1 and no N>1 error (google and vertex image models).
   - f88c7dc: wire the downloads (above).
   - 2d1f05d: Llama-4 MaaS gets `max_tokens=8192` when unset (`maas/google-vertex-maas-provider.ts`, `googlevertex/maas_provider.go`).
   - 7ac79e7: model IDs starting with `endpoints/` skip `/publishers/google`. With an API key this is an error (see google-vertex-provider-base.ts).
5. **WG-G4 Interactions** (`google/interactions_*.go`; TS `google/src/interactions/*`, `google-vertex-provider-base.ts` createInteractionsModel):
   - 662ddfc: agents keep their tools.
   - e40118c: `text/*` provider-reference files map to document.
   - d2d9324: topK plus penalty warnings.
   - ca29e9b: video response_format.
   - dc1eb8d: video output plus `outputTokensByModality`.
   - 18ad19c: processing_call / processing_result custom parts.
   - 7f04802: `InteractionsManagedAgent`.
   - d20f0dc: `vertex.Interactions()` with a location-scoped base URL and an express-key error.
6. **WG-G5.**
   - Realtime (`google/realtime_model.go`; TS `realtime/*`):
     - 4a994ad: thinkingConfig default low for live thinking models, `defaultToolBehavior`, and interactionStatus/waitingForInput events.
     - d93e295: wrap functionResponse.response in `{output}`.
     - 66b7151: goAway, sessionResumptionUpdate and generationComplete custom events.
   - 1f7835c: Gemini 3.5 Transcribe (`google/src/transcription/*`, `google-vertex/src/gemini-transcription/*`).
   - 6d9951b: Chirp 3 HD TTS (`google-vertex-cloud-tts-speech-model.ts`).
   - **Deferred:** c49380c and bbd9b31 (speech translation). They need a core SpeechTranslationModel interface that does not exist yet.
7. **Model IDs.**
   - google: gemini-3.5-flash-lite, 3.6-flash, 3.7-flash, 3.8-flash, the lyria-3 IDs, and agents agentic/static. Speech: gemini-3.8-flash-tts and -lite-tts. Transcription: gemini-3.5-transcribe and -live. Veo IDs.
   - vertex: models.go gets the same four gemini IDs, legacy textembedding-gecko and text-multilingual-embedding-002, chirp-3-hd, chirp_3, telephony, and Veo.
   - Vertex-Anthropic IDs belong to the anthropic agent, unless assigned.
8. **Deferred, no change:** batch rows (e7fc90e, ab6e9f9, 048ce06, a9782e1) and evaluation (d4d96bf). TS-only: 0246209, 8bedb2c, 5878b40, c6f5e62, 46d1149, add4326, 6190649, a3757d7, 1f4058f. Duplicates: 92e08e6, 6c5a1ed, da78d58 and bb0cf2e (covered by 2a32459 / f69920a).
9. Final steps: `go test -race` on touched packages, `go test ./pkg/...`, the self-review loop against TS, and the tracking-table rows in the final report (per IMPL_INSTRUCTIONS).

## Gotchas / decisions
- zsh: quote `--include='*.go'`. The sandbox rejects compound or `sed -i` multi-step commands; use python3 heredocs for edits.
- Warnings: TS `{type:'other', message}` maps to Go `Warning{Type:"other", Message, Details}` (both set). Unsupported warnings set `Feature`.
- `GenerateResult` has no raw finish reason field. Don't add core fields; they belong to the core agent.
- The Go converter reuses no helpers from `prompt/anthropic.go`, which the anthropic agent owns; the Google converter has its own copies (`resolveGoogleToolOutput` etc.).
- The Vertex metadata/options key order changed to TS (googleVertex first). Mention it in the report.
- Budget math changed (no 1024 floor, 65536 base). This is intentional TS parity; mention it in the report.
