# P1-3 (Anthropic + Bedrock) handoff

Branch `worktree-agent-aada2621bcb27615e`, based on `sep23-integration` @ aa4d7d0. Nothing merged yet. There is one WIP commit (see below).

Inputs: PRD `prds/p1-high/P1-3-Anthropic-Bedrock-Sep23-2026.md`. Audit rows: `state/parity/sep_23_2026/google-anthropic-bedrock.md` (WG-A1/A2/B1/B2/B3 tables, lines 289-307). Rules: `state/parity/sep_23_2026/IMPL_INSTRUCTIONS.md`. TS source: `/Users/arlene/Dev/side-projects/go-ai/ai/packages/{anthropic,amazon-bedrock}/src`. The sandbox blocks `git -C ai`, `cd ai`, and `sed $VAR`, so read the TS working tree with the Read tool (absolute paths). The working tree is 5 commits past `ai@7.0.113`.

## Status at handoff
- `go build ./...` passes.
- **`pkg/providers/anthropic` tests do not compile** ("setup failed"). Run `go vet ./pkg/providers/anthropic/` to see the errors. They are probably leftover test references (for example, the `wantBudget` case in `advanced_features_test.go:226` dereferences `tt.thinking.BudgetTokens`, which is nil for "enabled thinking without budget"; it now expects 1024).
- `anthropicaws` fails `TestAnthropicAWSAPIKeyHeaders`: base URL/path semantics changed (see Gotcha 1).
- `googlevertex/anthropic` fails `TestLanguageModelNonStreamingRequest`: max_tokens, beta, or strict expectations changed. Check the test and update it to TS behaviour. Do not revert the fix.
- `bedrock/...` passes (not touched yet).

## Done (in the WIP commit, not verified by tests yet)
WG-A1 and most of WG-A2 on the request side, plus the P0-1 deferral "shared cache-control validator":
- `anthropic/model_capabilities.go`: `GetModelCapabilities` is a port of TS `getModelCapabilities`. It has all 9 flags, the opus-5-5/opus-5/fable-5-1/fable-5/sonnet-5 entries, the `claude-sonnet-4(-|@)` and `claude-opus-4(-|@)` regexes, the legacy regex (lookaheads expanded) and unknown-Claude defaults. Also `HasDynamicFilteringWebToolWithoutCodeExecution`.
- `anthropic/request.go`: `prepareRequest` is a port of TS `AnthropicLanguageModel.prepareRequest` (anthropic-language-model.ts ~L302-1063). It covers:
  - penalty/seed warnings and temperature clamping;
  - the unknown-model maxOutputTokens warning;
  - rejectsSamplingParameters drops;
  - rejectsForcedToolUse falling back to outputFormat;
  - the jsonTool `disableParallelToolUse:false` warning;
  - reasoning resolution (`resolveReasoningConfig`: adaptive `display:"summarized"`, xhigh vs max, budget percentages, `none`, and `rejectsThinkingDisabled` falling back to effort low);
  - thinking disabled/enabled rewrites and effort lowering;
  - the thinking `display` and `block_binding`;
  - dynamic `max_tokens` (model max, plus the thinking budget, clamped for known models with a warning);
  - temperature/topP made mutually exclusive only for Claude;
  - safeguards, `service_tier`, `fallbacks:"default"`;
  - the TS beta order: prompt, then options, then tools, then user headers, then `AnthropicBeta`;
  - default `eager_input_streaming` when streaming;
  - one `AnthropicCacheControlValidator` shared by `prompt.ConvertToAnthropicPrompt` and tool preparation.
- `anthropic/prepare_tools.go`: port of TS `anthropic-prepare-tools.ts`. It covers per-tool betas (no beta for code_execution_20260120), strict only when `supportsStrictTools` (warning otherwise), the `structured-outputs-2025-11-13` beta for function tools, toolChoice `none` removing the tools, and forced tool choice falling back to auto when `rejectsForcedToolUse`.
- `anthropic/response_helpers.go`: caller metadata, `toToolsetMemberInput`, `serverToolUseCall` (dynamic code_execution, `programmatic-tool-call`/bash type injection), `convertAdvisorResult` (stopReason), and `rawJSONValue`.
- `anthropic/language_model.go`:
  - DoGenerate/DoStream use `prepareRequest`, forward `opts.Headers`, and set `RawRequest`.
  - convertResponse now emits `server_tool_use` calls; `tool_use` handles `toolset_name` and caller; `mcp_tool_use` is ProviderExecuted and Dynamic; advisor results are mapped; `ResponseMetadata` id/model are set; `safeguardResults` and `inputTransformations` go into metadata.
  - `output_tokens_details.thinking_tokens` becomes ReasoningTokens.
  - Stream:
    - spliced `message_start` is rejected;
    - toolset member input is buffered and emitted at stop;
    - `providerToolInputType` is injected, and `firstDelta` is false when the initial input is non-empty;
    - server tool calls are ProviderExecuted/Dynamic with caller metadata;
    - `safeguard_results`, `input_transformations` and thinking tokens are captured;
    - the finish chunk moved from `message_delta` to `message_stop`, with an EOF fallback.
  - `handleError` uses `HTTPStatusError` and passes the status code through.
- `anthropic/provider.go`:
  - `DefaultBaseURL` is now `https://api.anthropic.com/v1`, with `NormalizeBaseURL` (validation, trailing slash, bare host gets `/v1`, `ANTHROPIC_BASE_URL`); `New` panics on a whitespace-only base URL.
  - Paths are now `/messages`, `/files` and `/skills`.
  - New Config hooks: `TransformRequestBodyWithBetas`, `TransformStreamBody` and `TransformErrorBody`, for Bedrock-Anthropic.
- `anthropic/model_options.go`: `ThinkingConfig.Display` and `.BlockBinding`, `Safeguard`, `FallbacksDefault`, `ServiceTier`, `AnthropicBeta`, and `DisableParallelToolUse` changed to `*bool` (breaking; needed for the tri-state warning 9218ebe). The stale effort-beta doc is fixed (e5c4f40).
- `anthropic/tool_converter.go`: code_execution builtins get `name:"code_execution"`, and the tool_search builtins are added.
- `providerutils/validate_base_url.go` (new; outside my scope, shared): `ValidateBaseURL`, which returns "baseURL must be a non-empty string.". Needed for cd12954. P1-5 may also want it.
- Tests updated to TS expectations: code_execution_test, code_execution_integration_test, reasoning_test (provider thinking option now wins over call-level reasoning, as in TS), skills_path_encoding_test, advanced_features_test, and provider_updates_test (`boolPtr`).

## In progress / left to finish in anthropic
1. Make the anthropic tests compile and pass, then update the vertex-anthropic and anthropicaws tests.
2. `anthropicaws/provider.go`: stop stripping `/v1`. The base URL should be `https://aws-external-anthropic.{region}.api.aws/v1` (TS anthropic-aws-provider.ts L211-217). Fix its test.
3. Streaming emits no reasoning-start/text-start events. That is a pre-existing architecture difference and not in these rows, so leave it.
4. Add new tools under `anthropic/tools/`:
   - `web_search_20260318.go` (maxUses, allowed/blockedDomains, userLocation, responseInclusion);
   - `web_fetch_20260318.go` (adds citations, maxContentTokens, useCache, responseInclusion);
   - `computer_toolset_20260801.go` (configs map `{enabled, defer_loading}`, wire `{type:'computer_toolset_20260801', configs?}` with no name);
   - advisor `MaxTokens` → `max_tokens` (7a9da75).
   - Register all of them in `AnthropicTools`. There are no betas for the 20260318 tools. The name maps already include them in `prompt.anthropicProviderToolNames`.
   - TS files: `anthropic/src/tool/{web-search_20260318,web-fetch-20260318,computer-toolset_20260801,advisor_20260301}.ts`, and `anthropic-prepare-tools.ts` L197-224, L339-407, L425-441.
5. Add model IDs: `ClaudeOpus5`, `ClaudeOpus5_5`, `ClaudeFable5_1`, `ClaudeSonnet5`, `claude-opus-4-0`, `claude-sonnet-4-0`, `claude-opus-4-1-20250805` (list in `anthropic-language-model-options.ts`).
6. Port TS tests into new Go test files:
   - `anthropic-unknown-model-max-output-tokens.test.ts`
   - `anthropic-message-lifecycle.test.ts` (spliced stream)
   - `convert-anthropic-usage.test.ts` (thinking_tokens)
   - capability table tests
   - the prepare-tools snapshots in `anthropic-prepare-tools.test.ts`
   - safeguards, blockBinding, display, fallbacks default, and the base URL tests in `anthropic-provider.test.ts`
   - `grep -n "safeguard\|blockBinding\|display\|fallbacks" anthropic-language-model.test.ts`
7. Commit as `feat(anthropic): WG-A1/WG-A2 ...`.

## Remaining (suggested order)
1. **WG-B2: Bedrock-Anthropic on `anthropic.LanguageModel`**. Replace the standalone `pkg/providers/bedrock/anthropic/language_model.go`. Mirror `googlevertex/anthropic/provider.go`, using the new Config hooks.
   - TS: `amazon-bedrock/src/anthropic/amazon-bedrock-anthropic-provider.ts` (transformRequestBody) and `amazon-bedrock-anthropic-fetch.ts`.
   - `MessagesPath`: `/model/{encodeURIComponent(id)}/invoke` or `/invoke-with-response-stream`. SigV4 must sign the escaped path.
   - `TransformRequestBodyWithBetas`:
     - drop `model` and `stream`;
     - tool_choice becomes `{type, name?}` (drop disable_parallel);
     - `thinking.block_binding` becomes `{mismatch_behavior}` (9a46ac6);
     - strip `eager_input_streaming` and add the `fine-grained-tool-streaming-2025-05-14` beta (ce5d968);
     - apply the tool version, name and beta maps, including tool_search → `tool-search-tool-2025-10-19` (1921625);
     - set `anthropic_beta: [...]` and `anthropic_version: "bedrock-2023-05-31"`.
   - `SupportsNativeStructuredOutput` and `SupportsStrictTools` come from the Bedrock model-support lists (`amazon-bedrock-anthropic-model-support.ts`; eb16508, bd74b49).
   - `TransformStreamBody` converts the AWS event-stream to SSE: base64 `bytes` becomes `data: <json>`, and exception frames become an error event. Use the shared decoder from WG-B1.
   - `TransformErrorBody` turns a flat `{message}` into `{type:"error",error:{type:"error",message}}` (4b20a5d).
   - Session token: use the env `AWS_SESSION_TOKEN` only when no explicit keys are given (6732c16).
   - `supportedUrls` is empty.
   - **Review addition F6** (`state/parity/sep_23_2026/review-p0-1-p0-2-round1.md`): the current Bedrock-Anthropic drops in-prompt system messages when `Prompt.System` is set, and ignores converter betas and warnings. Reusing `anthropic.LanguageModel` fixes this. Add a test proving that both the system prompt and a mid-conversation system message are sent, and that converter betas and warnings propagate.
   - This also covers the P0-1 deferral "Bedrock-Anthropic system/cache-point parity".
2. **WG-B1: Bedrock Converse client** (the largest item). Replace `bedrock/language_model.go`, which today posts to `/invoke` and fakes streaming.
   - TS: `amazon-bedrock-chat-language-model.ts`, `convert-to-amazon-bedrock-chat-messages.ts`, `amazon-bedrock-prepare-tools.ts`, `amazon-bedrock-anthropic-model-support.ts`, `amazon-bedrock-event-stream-decoder.ts`, `amazon-bedrock-event-stream-response-handler.ts`, `amazon-bedrock-stream-error.ts`, `convert-amazon-bedrock-usage.ts`, `amazon-bedrock-error.ts`, `resolve-amazon-bedrock-base-url.ts`, `normalize-tool-call-id.ts`, `amazon-bedrock-reasoning-metadata.ts`, `map-amazon-bedrock-finish-reason.ts`. Fixtures are in `__fixtures__/`.
   - New package `bedrock/eventstream` holds the decoder, moved from `bedrock/anthropic/stream.go`. It must handle CRC/decode errors, the "Incomplete Amazon Bedrock event-stream frame: N buffered bytes remain at end of stream." error, and `:exception-type`.
   - Rows: 31fb009, fbdda09, 4b75a77, d746e16, c559a12, 132bdae (Bedrock filter), d82eac2, 5191b61, d0b6d6d, 770c214, 1ee6b1f, bd74b49, 6aa2401, 051a41d, 030b4e1, b2eb608, 9921a2f, fd49828, dee4c16, df45e67, 8ca1352, aabc617, 1ac9af8, e0776b9, a4ecd1c, 52e22a7, ebd31b8, eb16508, 1f92bdb, 8fcb72c, b72fc7c, ce56626, 43fc411, b0e9d24, cf06314, bc8ed78, ed60b47, cdc15f3.
   - Use `anthropic.GetModelCapabilities` for rejectsSampling and rejectsForcedToolUse.
3. **WG-B3**:
   - Cohere embeddings: 96 per call, batched with `texts` (1daf48b).
   - `EmbeddingOptions.ModelFamily` (5d2229e).
   - Typed, validated `ImageModelOptions` (5cd0e38).
   - Shared `bedrockAPIError` for embedding, image, rerank and LM (c559a12).
   - Mantle: `/openai/v1` for `^(openai\.gpt-(?!oss-)|google\.gemma-4|xai\.)` (b7bc639).
   - 0de8886 needs `openai.Config.SupportsWebSearchSourcesInclude` in the openai area. Coordinate with P1-5 or mark it Deferred.
   - 85a80fc (rerank key) was already fixed in P0-1.
   - TS: `amazon-bedrock-embedding-model*.ts`, `amazon-bedrock-image-model-options.ts`, `mantle/bedrock-mantle-provider.ts`.
4. **Model IDs (WG-M share)**: add the Bedrock `anthropic.`/`us.` IDs for sonnet-5, fable-5, fable-5-1, opus-5, opus-5-5 and opus-4-7, plus `global.anthropic.claude-fable-5-1`.
5. **Deferred** (record as Deferred): the batch half (e6415bd, 5a7e647, a9782e1) and the evaluation half (d4d96bf). Both need core interfaces from P1-2.

## Gotchas and decisions
1. **Base URL now includes `/v1`** (TS 679c52a). Custom `BaseURL` values (including httptest `srv.URL`) now receive `/messages` instead of `/v1/messages`. This is a breaking change for proxies; the bare `https://api.anthropic.com` is normalized to keep it compatible. Tests that assert `/v1/...` paths need updating. It needs a release note.
2. `DisableParallelToolUse` changed from `bool` to `*bool` (breaking; needs a release note).
3. Provider-level `ModelOptions.Thinking` now wins over call-level `Reasoning` (TS: provider options win). Reasoning fills in only the missing thinking, and sets effort unless `Effort` is set.
4. No beta for code_execution_20260120 (TS test "without beta header"). `structured-outputs-2025-11-13` is added for any function tool on a structured-output model, so tests that expect an empty beta header with function tools change.
5. The Go stream's finish chunk is now emitted on `message_stop`, or at EOF if `message_stop` is missing. Any test that stops reading right after `message_delta` still gets the finish chunk.
6. `GenerateResult.Text` still takes only the first text block (pre-existing, out of scope).
7. `TransformRequestBody(body, stream)` is kept for Vertex. The new `TransformRequestBodyWithBetas` runs before it.
8. The spliced-stream error is `ProviderError{ErrorCode:"invalid_response_data"}`, because Go has no InvalidResponseDataError type.
9. The Go tool name equals the provider ID (for example `anthropic.web_search_20250305`). Wire names come from `prompt.AnthropicProviderToolName`.

## Commits
- WIP commit: "wip(anthropic): P1-3 WG-A1/A2 request prep (tests do not compile)", plus the commit that adds this file.
