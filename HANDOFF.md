# HANDOFF — P0-3 items 6/7/8/9 (EmbedMany, SmoothStream, PruneModelMessages, LogWarnings)

PRD: `prds/p0-critical/P0-3-Core-Foundations-Never-Ported-Sep23-2026.md` items 6–9.
Instructions: `state/parity/sep_23_2026/IMPL_INSTRUCTIONS.md`. The branch is based on `sep23-integration` @ aa4d7d0.
TS reference is at `/Users/arlene/Dev/side-projects/go-ai/ai/packages/...`. `git -C <ai>` is blocked from the worktree, so read the files in its working tree directly.

## Done (committed)
- **8c0c844** `feat(ai): LogWarnings` — `pkg/ai/log_warnings.go` + `log_warnings_test.go` (pass).
  - API: `LogWarnings(LogWarningsOptions{Warnings,Provider,Model})`, `LogWarningsFunction`, `SetLogWarnings(fn)` (nil restores the default), `DisableLogWarnings()`, env `AI_SDK_LOG_WARNINGS=false`, `FormatWarning`, `FirstWarningInfoMessage`.
  - The default logger writes to stderr and prints the banner once. Internal helpers: `logModelWarnings(...)` and the test-only `resetLogWarningsState(w)`.
  - Covers rows 4873966, 2e2224b and 008271d (the deprecated format).
- **e9ab825** `wip: EmbedMany ...` (it builds, and the pkg/ai and google tests pass):
  - `pkg/provider/errors/invalid_response_data.go` — new `InvalidResponseDataError{Data,Message}`. With no message it defaults to the TS message.
  - `pkg/provider/embedding_capabilities.go` — optional interfaces `EmbeddingModelMaxInputBytesPerCall` and `EmbeddingModelProviderOptionsTransformer` (`TransformEmbeddingProviderOptions(ctx, EmbeddingProviderOptionsTransformInput{ProviderOptions,Values,StartIndex,EndIndex})`).
  - `pkg/middleware/embedding_model_middleware.go` — the wrapper forwards both capabilities.
  - `pkg/providers/openai|azure/embedding_model.go` — `MaxInputBytesPerCall() = 300_000`. This is row #107.
  - `pkg/providers/google/embedding_model.go`:
    - `MaxEmbeddingsPerCall` is now 100 (was 2048), matching TS and the Gemini batch limit. The test in `embedding_model_http_test.go` was updated.
    - New `TransformEmbeddingProviderOptions` slices `google.content` per batch. It handles the struct, pointer and map forms, and validates length against the full input using the TS error message.
    - `handleError` now maps `HTTPStatusError` to a `ProviderError` with its status and headers, so retries work. This is row #3.
  - `pkg/ai/embed.go`:
    - `RuntimeContext` was added to `EmbedOptions`, `EmbedManyOptions`, `EmbedOnStartEvent` and `EmbedOnFinishEvent`. Callbacks get it unfiltered; telemetry gets it filtered through `telemetryRuntimeContext`.
    - Also added: `EmbedManyOptions.MaxParallelCalls` (0 means unlimited, negative gives `InvalidArgumentError{Field:"chunkSize"}` in the parallel path), `EmbedResult.ProviderMetadata/Response`, and `EmbedManyResult.ProviderMetadata/Responses`.
    - Embed retries (`withEmbedRetry`). If the model returns no embedding it fails with `InvalidResponseDataError("No embedding generated.")`, checked inside the retry loop after `FireOnEmbedEnd`.
    - Both functions call `logModelWarnings`. Negative `MaxRetries` is rejected.
  - `pkg/ai/embed_many_batching.go`:
    - Implements `embedManyCalls`: a single-call path when there are no limits; otherwise `splitByEmbeddingLimits` splits by count and UTF-8 bytes.
    - Parallel groups run in goroutines. Per-batch options are transformed before the group's calls and before retry, so a mismatch fails before any request.
    - Each batch is retried and checked with `validateEmbeddingCount`. Usage is summed, provider metadata is shallow-merged per key, and responses are one per call (a placeholder is added when the provider returns none).
    - `withEmbedRetry` has a 2s initial delay, backoff factor 2, no jitter, and uses `isGatewayCallRetryable` (408/409/429/5xx, respects retry-after headers).
  - `pkg/ai/embed_many_batching_test.go` — ports from embed-many.test.ts, embed-many-google.test.ts and embed.test.ts.
  - `pkg/ai/telemetry_test.go` — the test mock got `MaxEmbeddingsPerCall` and `SupportsParallelCalls`. It embeds a nil interface and panicked without them.

## In progress
Nothing is uncommitted. The EmbedMany part is functionally complete. The only gap is the self-review re-diff against `embed/embed-many.ts`.

## Remaining (suggested order)
1. **Rerank** (`pkg/ai/rerank.go`; rows #62 and #64/WG10 index check):
   - Add `RuntimeContext interface{}` to `RerankOptions`, `RerankOnStartEvent` and `RerankOnFinishEvent`. Pass it filtered to `FireOnStart` (it is currently hardcoded `{}`).
   - Wrap `DoRerank` in `withEmbedRetry(ctx, opts.MaxRetries, ...)`, including `FireOnRerankStart/End` inside the retry. Reject negative `MaxRetries`.
   - After the call, add `validateRankingIndices`: `InvalidResponseDataError{Data: ranking, Message: "Invalid ranking index %d. Expected an integer between 0 and %d."}` (len-1). This must run BEFORE `documentsSlice[item.Index]`, which currently panics on a bad index.
   - Call `logModelWarnings`.
   - Optional: on empty documents TS still fires onStart/onEnd. Coordinate with the callbacks agent.
   - TS: `rerank/rerank.ts` (validateRankingIndices, empty-docs path), `rerank/rerank.test.ts`.
2. **SmoothStream** (new `pkg/ai/smooth_stream.go`; rows #84 a51cc94, #100 e604532; WG13). TS: `generate-text/smooth-stream.ts` and `.test.ts`.
   - Go's `StreamTransformFunc(ctx, chunk) []chunk` returns a slice. Delays inside it would therefore batch every output chunk, and the smoothing would do nothing.
   - The planned fix is a minimal hook. In `stream.go` (the ExperimentalTransform block around line 1136), build an emit pipeline and put an emitter into ctx (exported `StreamTransformEmitterFromContext(ctx)`).
   - SmoothStream emits incrementally, with a ctx-aware sleep between matches, when the emitter is present. Otherwise it returns the slice.
   - `stream.go` is owned by a parallel agent, so keep that diff tiny and list it in the report.
   - API: `SmoothStream(SmoothStreamOptions{DelayInMs *int (nil means 10, &0 means none), Chunking interface{} ("word"|"line"|*regexp.Regexp|ChunkDetector)}) (StreamTransformFunc, error)`, where `ChunkDetector func(buffer string) (string, bool)`.
   - Create one per StreamText call, because the transform holds state.
   - Regexes. JS `\s` is wider than RE2, so use an explicit class `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`:
     - word: `[^WS]+[WS]+`
     - line: `\n+`
     - match = `buffer[:loc[1]]`
   - Error on an empty regex match ("Chunking RegExp must not match an empty string."), an empty detector result, or a non-prefix detector result. Emit these as a `ChunkTypeError` chunk, since the transform cannot return an error.
   - Deltas are `ChunkTypeText` (Text) and `ChunkTypeReasoning` (Reasoning field, falling back to Text).
   - Flush when the type or ID changes, or on any non-delta chunk. Flush an empty buffer too if it carries ProviderMetadata, and never carry metadata from one part to another.
   - Intl.Segmenter and hidden-document are TS-only.
3. **PruneModelMessages** (new `pkg/ai/prune_messages.go`; rows #9 9a98fd9, #38 03c3e33, a2750db; WG23/WG-MISC). TS: `generate-text/prune-messages.ts` and `.test.ts`.
   - Keep the existing token pruner `PruneMessages` in `pruning.go` unchanged.
   - API: `PruneModelMessages(messages []types.Message, opts PruneModelMessagesOptions{Reasoning "none"|"all"|"before-last-message"; ToolCalls []PruneToolCallsRule{Type "all"|"before-last-message"|"before-last-N-messages", Tools []string}; EmptyMessages "remove"(default)|"keep"}) ([]types.Message, error)`. Return an error for invalid rule types.
   - Reasoning removal drops both `ReasoningContent` and `ReasoningFileContent`.
   - Treat these as tool parts: `ToolCallContent`, `ToolResultContent`, `ToolErrorContent` (Go-only), `ToolApprovalRequestContent` and `ToolApprovalResponseContent`. For approval requests and responses, the tool call ID is `ToolCallID`, or `ToolCall.ID` when that is empty.
   - Also filter `Message.ToolCalls` (the legacy top-level field) with the same rules.
   - Use global maps: toolCallId → name, approvalId → toolCallId, and approvalId → name (built only from tool-call/tool-result parts, as in TS).
   - Kept approvals add their tool call ID to the kept set. "before-last-0" scans all messages, a TS quirk.
   - A message is "empty" when both Content and ToolCalls are empty.
   - Never mutate the input.
   - Add a doc comment explaining the difference from `PruneMessages`.
4. **LogWarnings call sites**. Done: embed/embedMany. Still to do:
   - rerank (step 1).
   - `generate_image.go`, `generate_speech.go`, `transcribe.go` and `generate_video.go`: one-line `logModelWarnings(warnings, model.Provider(), model.ModelID())`.
   - `generate.go`, `stream.go` and `object.go` are owned by parallel agents. Report the exact call sites for the coordinator instead of editing them. The same goes for the 349afe7 GenerateText `timeout.chunkMs` warning.
5. Final steps:
   - Run `go build ./...`, `go vet ./...`, `go test -race` on pkg/ai, pkg/middleware, pkg/providers/{google,openai,azure} and pkg/provider/..., then `go test ./pkg/...`.
   - Re-diff each row against TS.
   - Write the final report with the tracking table.

## Gotchas / decisions
- **Zero-value defaults differ from TS.** `MaxRetries int`: 0 means no retries (TS defaults to 2; Go can't tell unset from 0). `MaxParallelCalls` 0 means unlimited.
- **Empty inputs.** `EmbedMany` with no inputs still returns "at least one input is required", because an existing test asserts it. TS returns an empty result.
- **Limits.** `MaxEmbeddingsPerCall() <= 0` means unlimited (the Go interface doc says so), where TS would throw.
- **Retries.** `retryutil.Do` with MaxRetries 0 would use DefaultConfig (3 retries). `withEmbedRetry` therefore calls fn directly when maxRetries <= 0.
- **Other providers.** Bedrock, HuggingFace and Ollama have `MaxEmbeddingsPerCall = 1` and are now called once per value. Results are the same.
- **Direct OTel spans.** Embed/EmbedMany still create spans themselves. Removing them is row 118b953, which is not in this slice.
- **Log noise.** Tests that produce provider warnings now print to stderr. That is harmless.
- **Out-of-scope files touched.** These must be listed in the report: google, openai and azure `embedding_model.go`, `pkg/middleware/embedding_model_middleware.go`, `pkg/ai/telemetry_test.go`, plus the new `pkg/provider/*` files.

## Status
`go build ./...` has not been re-run since e9ab825. Only these were built:
- `pkg/ai`, `pkg/provider/...`, `pkg/middleware`, `pkg/providers/{google,openai,azure}` all build.
- `go test ./pkg/ai` passes, and `-race` on the new embed tests passes.
- `go test ./pkg/providers/google` passes.
