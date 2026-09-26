# P0-3b handoff (items 2–5 of P0-3 + review F5/F6)

Branch: `worktree-agent-aaa94ce10c253f5b9`, based on `sep23-integration` @ aa4d7d0.
Follow `/Users/arlene/Dev/side-projects/go-ai/state/parity/sep_23_2026/IMPL_INSTRUCTIONS.md`.
PRD: `prds/p0-critical/P0-3-Core-Foundations-Never-Ported-Sep23-2026.md` (items 2, 3, 4, 5).

## Done

Commit `ec0f9cd`: feat(ai): parse, validate and repair tool calls in generateText, plus stable repairToolCall and language-model-call callbacks.

- New `pkg/ai/tool_call_pipeline.go`:
  - Error types: `NoSuchToolError`, `ToolCallRepairError`. `InvalidToolInputError` already existed in `validate_tool_approvals.go`.
  - Repair API: `ToolCallRepairOptions`/`ToolCallRepairFunction`, with Instructions, InstructionMessages, deprecated System, Messages, InputSchema(toolName) and Error.
  - `ParseToolCall` is the port of TS parseToolCall:
    - It checks for a missing tool and for invalid JSON or JSON-schema input, then repairs.
    - The repair runs in a goroutine selected against `ctx.Done()`, so cancelling returns `ctx.Err()` (6317504).
    - Calls that stay invalid come back as `Invalid=true, Dynamic=true, Error`.
  - Helpers:
    - `parseToolCalls`
    - `invokeToolInputCallbacks` (generate path)
    - `streamToolInputCallbacks.handle` (stream path, ported from invoke-tool-callbacks-from-stream.ts; written and unit-ready but NOT wired into stream.go yet)
    - `invalidToolCallResult`
- `pkg/provider/types/tool.go`: this file is outside my scope, but the change was required.
  - `OnInputStartFunc` now takes `OnInputStartOptions{ToolCallID, Messages, Context}`. Nothing called it before, so no existing caller breaks.
  - `OnInputDeltaOptions` gained `InputTextDelta`, `ToolCallID`, `Messages` and `Context`.
  - `OnInputAvailableOptions` gained `Input`, `ToolCallID`, `Messages` and `Context`.
  - The old fields were kept and marked deprecated.
- `pkg/ai/callback_events.go`:
  - Start and step-start events gained `Provider`, `Instructions`, `InstructionMessages`, `ToolOrder`, `ToolChoice`, `ProviderOptions`, `Timeout` and `Reasoning`.
  - Tool events gained `ToolCall`, `ToolContext`, `ToolOutput` and `ToolExecutionMs`. `StepNumber`, `ModelProvider`, `ModelID`, `Args`, `Result`, `Error` and `DurationMs` are kept but marked deprecated. This is the e1bfb9c decision: I deprecated instead of removing, for Go API compatibility.
  - New types: `ToolExecutionStartEvent`/`EndEvent` aliases, `LanguageModelCallStartEvent`/`EndEvent`, and callback types.
- `pkg/ai/instructions.go`: validation and prepend helpers for `InstructionMessages` (d775a57). The union is expressed as a separate field because `Instructions *string` already exists.
- `pkg/ai/generate.go`:
  - New options: `InstructionMessages`, `RepairToolCall` (plus deprecated `ExperimentalRepairToolCall`), `OnLanguageModelCallStart/End` (plus deprecated `Experimental*`). `PrepareStepOptions` gained `InstructionMessages` and `InitialInstructionMessages`. `stream.go` has the same option fields, but they are NOT wired yet.
  - Step flow order:
    1. OnStepStart
    2. OnLanguageModelCallStart, then telemetry
    3. doGenerate
    4. parseToolCalls
    5. OnLanguageModelCallEnd. This uses the response model ID and falls back to the request model (015acb4). It also carries ProviderMetadata.
    6. refine
    7. OnInputStart, then OnInputAvailable (valid calls only)
    8. approvals/execute
  - `executeTools`:
    - Invalid client calls get a dynamic tool-error result.
    - Invalid provider-executed calls get nothing (7bd6bdd).
    - Tools with a nil `Execute` are skipped. This fixes a nil-func panic.
    - Empty slots are always compacted.
  - After the loop, the final shortcuts (Text, Reasoning, FinishReason, RawFinishReason, ProviderMetadata) always come from the last step. They were previously empty when the loop ended after a tool step.
- Tests: `pkg/ai/tool_call_pipeline_test.go`
  - Ported parse-tool-call cases, the repair cases and the cancel-during-repair case.
  - generateText: repair (stable and alias), invalid calls, input-callback order and context, LM-call callbacks (order, alias, response model).
  - `result_fields_test.go` was updated to pass tools, because under TS semantics tool calls made without tools are invalid.
- Status: `go build ./...` OK. `go test -race ./pkg/ai/` passes. pkg/agent, pkg/workflow and pkg/middleware were green at baseline and have not been re-run since my change; re-run them.

## In progress

Nothing is uncommitted.

## Remaining (suggested order)

1. **Stream path, items 2/3/4** (`pkg/ai/stream.go`):
   - In `StreamText` (initial step):
     - Resolve instruction messages, repair and the LM callbacks the same way generate.go does.
     - Populate the new OnStart/OnStepStart fields.
     - Pass `InstructionMessages` into PrepareStep.
     - Call `prependInstructionMessages` after normalization.
     - Notify `LanguageModelCallStartEvent`.
     - Store the repair function, instruction messages and LM callbacks on `StreamTextResult`.
     - Do the same for the next-step block in `processStream` (around the `nextGenOpts` build).
   - In `processStream`, at the `ChunkTypeToolCall` chunk:
     - Run `ParseToolCall` against stepTools, then `RefineToolCalls` on that single call. Keep the pre-refinement copy for `inputSchemaInputs`, and drop the post-stream RefineToolCalls.
     - Replace `chunk.ToolCall`.
   - After forwarding each chunk, call `streamToolInputCallbacks.handle` and set r.err on error. TS forwards the chunk first, then calls the callback. Reset its state per step.
   - Track `responseModelID` from `ChunkTypeResponseMetadata`.
   - At finish, notify `LanguageModelCallEndEvent` with the parsed step content and the decoded chunk.ProviderMetadata.
   - Invalid calls are turned into tool errors by `executeTools` after the stream ends. TS emits a `tool-error` in-stream instead; either is acceptable, so document the choice.
   - `ReadAll` path: see item 2.
   - TS reference: `generate-text/stream-language-model-call.ts` (the tool-call and finish cases), `invoke-tool-callbacks-from-stream.ts` (plus its test), and `stream-text.ts`.
2. **Bug "StreamText without callbacks never executes tools": CONFIRMED, NOT FIXED.**
   - Cause: `processStream` only starts when a callback is set (`StreamText`, the `if opts.OnChunk != nil || ...` block). `ReadAll` and `Stream()` read the raw provider stream, so they never execute tools and never run multi-step. TS always runs the pipeline.
   - Planned fix:
     - Also start processing when any tool has `Execute`, `SupportsDeferredResults` or an `OnInput*` hook, or when LM-call end callbacks are set.
     - Feed every `onChunk` chunk into a replay buffer: a mutex+cond slice, closed at the end of processStream.
     - `Stream()` and `Chunks()` return a new reader over that buffer (tee semantics). Reader `Close()` calls `r.Close()`.
     - `ReadAll()` waits on `processingDone` and returns `r.text, r.err`.
   - Watch `stream_test.go` and `ui_message_stream` tests that count chunks. The buffer will include `ai.stream.firstChunk` and `ai.stream.finish`, which the UI stream already handles.
3. **F6 (review)**: Move the approval resume out of `StreamText` (currently `resumeToolApprovals(... streaming: true)` runs before return and its chunks are prefixed). It should run inside `processStream` before step 0, execute approved tools in parallel (goroutines, results in input order), and surface signature errors as stream errors instead of `StreamText`'s return error.
   - Files: `pkg/ai/tool_approval_resume.go` (the `executeTools` call around line 146).
   - TS: the initial tool-execution stream in `stream-text.ts`, `collect-tool-approvals.ts` and `validate-tool-approvals.ts`.
   - Update `tool_approval_resume_test.go` expectations.
4. **Item 4, other APIs** (G3):
   - Add stable `OnStart`/`OnEnd` to `EmbedOptions`, `EmbedManyOptions` and `RerankOptions`, and fire them alongside `ExperimentalOnStart`/`ExperimentalOnEnd`, stable taking precedence. Keep these edits minimal, because another agent edits embed.go/rerank.go for batching.
   - Add stable `OnStart`/`OnStepStart`/`OnStepEnd`/`OnEnd` to `GenerateObjectOptions`/`StreamObjectOptions` (`pkg/ai/object.go`, `object_events.go`).
   - Add type aliases (29d8cf4): `EmbedStartEvent`, `EmbedEndEvent`, `RerankStartEvent`, `RerankEndEvent`, `GenerateObjectStartEvent`, `GenerateObjectStepStartEvent`, `GenerateObjectStepEndEvent`, `GenerateObjectEndEvent`.
   - Response model ID in end events: set `Response.ModelID`/`Timestamp` in the generate `OnStepFinishEvent`/`OnFinishEvent`. They are currently unset there, although `stepResult.Response` has them.
   - TS: `embed/embed.ts`, `embed-many.ts`, `rerank/rerank.ts`, `generate-object/*.ts`, `generate-text/core-events.ts`.
5. **Agent** (`pkg/agent/agent.go` and `toolloop.go`; WG16, #79, #132, 6ec57f5):
   - Add to `AgentConfig`, `PrepareCallConfig` and the call options: `RepairToolCall` (plus the Experimental alias), `OnLanguageModelCallStart/End`, `InstructionMessages`, and `OnEnd`/`OnEndEvent` (keeping `OnFinish` deprecated). Forward them in `Generate`/`Stream` to ai.GenerateText/StreamText.
   - `pkg/workflow/workflow_agent.go`: add `RepairToolCall` (eb49d29).
   - In the legacy `executeWithMessages` loop (toolloop.go around line 1121 `executeStep` and line 1244): use `ai.ParseToolCall` and make the invalid-call synthesis skip `ProviderExecuted` (7bd6bdd).
   - TS: `agent/tool-loop-agent.ts` and `tool-loop-agent-settings.ts`.
6. **F5 (review)**: `ToolLoopAgent.Execute`/`ExecuteWithMessages`/`GenerateAgent` (`executeWithMessages`) sign approvals but never resume them.
   - Export a helper from pkg/ai that wraps `resumeToolApprovals`, and call it at the start of `executeWithMessages`: execute approved tools, report invalid ones and denials, and verify signatures with the agent's secret.
   - Add a test with a history ending in an approved `tool-approval-response`.
   - Details: `state/parity/sep_23_2026/review-p0-1-p0-2-round1.md` F5.
7. **Item 5, middleware**:
   - Add `pkg/middleware/default_instructions.go`, a port of `middleware/default-instructions-middleware.ts`.
   - `DefaultInstructionsMiddleware(text string, messages ...types.Message)` or an options struct. Its `TransformParams` does nothing when the defaults are empty, or when the prompt already has `Prompt.System != ""` or any system message. Otherwise it prepends the system messages (with ProviderOptions); plain string instructions can simply set `Prompt.System`.
   - Port `default-instructions-middleware.test.ts`.
   - Also add a generate/stream test that the InstructionMessages ProviderOptions reach `provider.GenerateOptions.Prompt.Messages`.
8. Parity-surface compile test (`pkg/ai/parity_surface_test.go`) for the new names, then the self-review diff against TS, then `go vet ./...` and `go test -race` on pkg/ai, pkg/agent, pkg/workflow and pkg/middleware, then `go test ./pkg/...`.

## Gotchas / decisions

- `toolCallInputValidator` validates only JSON-schema tools: a map `Parameters` or `*schema.JSONSchemaValidator`. `StructValidator.Validate` on a map would fail through go-playground. Provider-executed and `Type=="provider"` tools are never validated.
- A provider-executed call with no matching tool is accepted as dynamic even when `Dynamic` is false. TS also requires `dynamic`; some Go providers do not set it.
- Refinement stays a separate `RefineToolCalls` pass after parsing. Refine errors still abort generation; in TS they make the call invalid instead.
- Loop continuation is unchanged. Unknown-tool calls do not continue the loop, because Go has no default `stepCountIs(1)` and could otherwise loop up to 1000 steps.
- `InstructionMessages` take precedence over `Instructions`/`System`. In that case `system` is set to "", and the sandbox description still goes into `Prompt.System`.
- `pkg/telemetry/registry.go` was not touched, so telemetry LM-call-end still uses the requested model ID. Other agents own that file.
- Parallel agents own `ui_message_stream.go` plus the new UIMessage files, and `embed.go`/`rerank.go` (batching). Keep edits to those files minimal.
- A read of the TS working tree works directly under `/Users/arlene/Dev/side-projects/go-ai/ai/packages/ai/src/...`. `git -C` on the ai repo is blocked from this worktree, and so is `sed` with a `$VAR` path, so use literal paths.
- Commit with `git -c commit.gpgsign=false commit` and the trailer `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
