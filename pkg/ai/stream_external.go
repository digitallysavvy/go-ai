package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ExternalStreamOptions configures NewStreamTextResultFromParts: the
// pkg/ai/harness integration seam described in
// state/parity/sep_23_2026/harness.md §3 ("P0 prerequisite"). It mirrors how
// the TypeScript SDK's HarnessAgent builds a StreamTextResult-compatible
// result from a harness bridge session's own recorded part sequence, rather
// than from a provider.LanguageModel call.
type ExternalStreamOptions struct {
	// CallID correlates this result with the caller's own callback/telemetry
	// events. A new one is generated when empty.
	CallID string

	// Provider and ModelID label steps and the response metadata when a
	// chunk does not carry its own provider.ResponseMetadata.
	Provider string
	ModelID  string

	// OnChunk is invoked for every chunk as it is consumed from src, mirroring
	// StreamTextOptions.OnChunk.
	OnChunk func(chunk provider.StreamChunk)

	// OnFinish/OnEnd are invoked once, after src is fully consumed (whether it
	// ended in success or with an error), mirroring StreamTextOptions'
	// callbacks of the same name. OnEnd takes precedence when both are set.
	OnFinish func(result *StreamTextResult)
	OnEnd    func(result *StreamTextResult)

	// Output, when it satisfies the internal outputProcessor interface (any
	// value returned by TextOutput/ObjectOutput/ArrayOutput/ChoiceOutput/
	// JSONOutput), enables structured-output parsing on the returned result:
	// Output()/OutputErr() resolve once src is fully consumed by parsing the
	// final step's accumulated text, and PartialOutput() updates after every
	// text chunk of that step, deduplicated exactly like StreamTextOptions.
	// Output. A value that does not satisfy outputProcessor is ignored (mirrors
	// StreamTextOptions.Output's `interface{}` + type-assertion contract; see
	// stream.go's own opts.Output handling for the pattern this mirrors).
	Output interface{}
}

// NewStreamTextResultFromParts builds a *StreamTextResult by consuming src, a
// caller-supplied provider.TextStream that already carries the complete,
// ordered chunk sequence for every step, instead of driving a
// provider.LanguageModel itself.
//
// src's steps are delimited the same way provider.LanguageModel.DoStream's
// output is within a single step, plus an explicit step boundary:
//   - provider.ChunkTypeStreamStart opens a step (its Warnings, if any, are
//     recorded).
//   - Text/reasoning/tool-call/tool-result/source/file/custom content chunks
//     accumulate into that step exactly as they do for a normal
//     provider.LanguageModel-driven step.
//   - provider.ChunkTypeFinishStep closes a step that is not the last one:
//     the step is appended to Steps(), and consumption continues (expecting
//     another ChunkTypeStreamStart).
//   - provider.ChunkTypeFinish closes the final step the same way when it
//     carries new content of its own (no preceding ChunkTypeFinishStep for
//     that content), and marks the whole result done. But when it arrives
//     with no new content at all — i.e. every step was already closed by its
//     own ChunkTypeFinishStep — it is treated as a pure turn-closing
//     boundary: no extra empty step is appended, and its Usage (when set)
//     OVERRIDES the locally-summed total instead of being added to it. This
//     is the shape a harness.HarnessAgent turn produces: one
//     ChunkTypeFinishStep per underlying model step (each with that step's
//     own usage), then a terminal, content-less ChunkTypeFinish carrying the
//     turn's real totalUsage (see state/parity/sep_23_2026/harness.md WG4,
//     TS 57e0a59).
//   - provider.ChunkTypeAbort closes the in-progress step the same way
//     ChunkTypeError does (preserving whatever it accumulated), marks the
//     result done, and sets Err() to a non-nil error — but with
//     types.FinishReasonOther instead of FinishReasonError, since it
//     represents a clean, caller-initiated stop (TS 86a84c9) rather than a
//     failure. Consumers using ToUIMessageStream see the usual `abort` UI
//     chunk and no `onError`, exactly as for a provider.LanguageModel-driven
//     stream, since every chunk (including this one) is forwarded to
//     opts.OnChunk/the internal chunk buffer unconditionally.
//
// No provider is called and no tools are executed: src is assumed to already
// represent everything that happened (e.g. a harness bridge session that ran
// its own model calls and host tool executions and is replaying the result
// as a single ordered part sequence). Every accessor that StreamText's
// result supports (Text, Steps, ToolCalls, ToolResults, Usage, Sources,
// Files, Warnings, FullStream, ToUIMessageStream, ...) works on the returned
// result the same way.
func NewStreamTextResultFromParts(ctx context.Context, src provider.TextStream, opts ExternalStreamOptions) *StreamTextResult {
	callID := opts.CallID
	if callID == "" {
		callID = newCallID()
	}

	result := &StreamTextResult{
		status:          StreamStatusSubmitted,
		cbCallID:        callID,
		cbModelProvider: opts.Provider,
		cbModelID:       opts.ModelID,
	}
	if op, ok := opts.Output.(outputProcessor); ok {
		result.outputSpec = op
	}
	result.chunkBuf = newChunkBuffer()
	result.processingDone = make(chan struct{})

	go result.consumeExternalParts(ctx, src, opts)

	return result
}

// isEmpty reports whether no content has been accumulated for this step
// since it was last reset. Used to distinguish a terminal ChunkTypeFinish
// that closes a step's own content (the general case) from one that is a
// pure turn-closing boundary carrying no new content of its own (the harness
// case: every step was already closed by its own ChunkTypeFinishStep, and
// the terminal ChunkTypeFinish exists only to carry the turn's totalUsage and
// mark the stream done). See consumeExternalParts's ChunkTypeFinish handling
// below.
func (s *externalStepAccum) isEmpty() bool {
	return s.text == "" && len(s.content) == 0 && len(s.reasoning) == 0 &&
		len(s.files) == 0 && len(s.sources) == 0 && len(s.toolCalls) == 0 &&
		len(s.toolResults) == 0 && len(s.warnings) == 0
}

// externalStepAccum holds the in-progress state for one step while
// consumeExternalParts reads src.
type externalStepAccum struct {
	text             string
	content          []types.ContentPart
	reasoning        []types.ReasoningContent
	files            []types.GeneratedFileContent
	sources          []types.SourceContent
	toolCalls        []types.ToolCall
	toolResults      []types.ToolResult
	warnings         []types.Warning
	usage            types.Usage
	finishReason     types.FinishReason
	rawFinishReason  string
	providerMetadata map[string]interface{}
	response         types.StepResponse
}

// consumeExternalParts reads src to completion, accumulating steps and the
// result's aggregate fields exactly like processStream's per-chunk
// bookkeeping, but without ever calling a provider.LanguageModel or
// executing a tool.
func (r *StreamTextResult) consumeExternalParts(ctx context.Context, src provider.TextStream, opts ExternalStreamOptions) {
	defer close(r.processingDone)
	defer func() {
		if r.chunkBuf != nil {
			r.chunkBuf.close(r.err)
		}
	}()

	onEnd := opts.OnEnd
	if onEnd == nil {
		onEnd = opts.OnFinish
	}

	var accumulatedText []string
	stepNumber := 0
	step := &externalStepAccum{}

	// lastStepText/hasPublishedPartial/lastPartialJSON support r.outputSpec
	// (see ExternalStreamOptions.Output doc): lastStepText is the most
	// recently completed step's own text, used for the terminal parse below
	// (mirrors StreamText's outputSpec.parseCompleteOutput call, scoped to
	// the final step only — audit row 2a5ed55 / WG4). hasPublishedPartial/
	// lastPartialJSON dedupe PartialOutput() updates within the current step,
	// reset at every step boundary, mirroring stepLastPartialJSON in stream.go.
	var lastStepText string
	var hasPublishedPartial bool
	var lastPartialJSON string

	finishStep := func(terminal bool) {
		content := append([]types.ContentPart(nil), step.content...)
		result := types.StepResult{
			CallID:           r.cbCallID,
			StepNumber:       stepNumber,
			Model:            types.StepModel{Provider: opts.Provider, ModelID: opts.ModelID},
			Text:             step.text,
			Content:          content,
			Reasoning:        step.reasoning,
			ReasoningText:    reasoningText(step.reasoning),
			Files:            step.files,
			ToolCalls:        step.toolCalls,
			ToolResults:      step.toolResults,
			FinishReason:     step.finishReason,
			RawFinishReason:  step.rawFinishReason,
			Usage:            step.usage,
			Warnings:         step.warnings,
			Sources:          step.sources,
			Response:         step.response,
			ProviderMetadata: step.providerMetadata,
		}

		accumulatedText = append(accumulatedText, step.text)
		lastStepText = step.text
		hasPublishedPartial = false
		lastPartialJSON = ""

		r.mu.Lock()
		r.cbSteps = append(r.cbSteps, result)
		r.text = strings.Join(accumulatedText, "")
		r.finishReason = step.finishReason
		if step.rawFinishReason != "" {
			r.rawFinishReason = step.rawFinishReason
		}
		r.usage = r.usage.Add(step.usage)
		r.toolCalls = append(r.toolCalls, step.toolCalls...)
		r.toolResults = append(r.toolResults, step.toolResults...)
		r.sources = append(r.sources, step.sources...)
		r.files = append(r.files, step.files...)
		r.warnings = append(r.warnings, step.warnings...)
		if len(step.providerMetadata) > 0 {
			if raw, err := json.Marshal(step.providerMetadata); err == nil {
				r.providerMetadata = raw
			}
		}
		r.stepResponse = step.response
		r.status = StreamStatusStreaming
		if terminal {
			r.status = StreamStatusDone
		}
		r.mu.Unlock()

		stepNumber++
		step = &externalStepAccum{}
	}

consumeLoop:
	for {
		if ctx.Err() != nil {
			r.err = ctx.Err()
			break
		}

		chunk, err := src.Next()
		if err != nil {
			if err != io.EOF {
				r.err = err
			}
			break
		}

		if opts.OnChunk != nil {
			opts.OnChunk(*chunk)
		}
		r.mu.Lock()
		if r.status == StreamStatusSubmitted {
			r.status = StreamStatusStreaming
		}
		r.mu.Unlock()
		if r.chunkBuf != nil {
			r.chunkBuf.push(*chunk)
		}

		switch chunk.Type {
		case provider.ChunkTypeStreamStart:
			step.warnings = append(step.warnings, chunk.Warnings...)

		case provider.ChunkTypeText:
			if chunk.Text != "" {
				step.text += chunk.Text
				step.content = append(step.content, types.TextContent{Text: chunk.Text})

				// Update partial output after each text chunk (deduplicated),
				// scoped to the in-progress step's own text only. Mirrors the
				// identical block in stream.go's processStream.
				if r.outputSpec != nil {
					partial, hasPartial, partialErr := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
						Text: step.text,
					})
					if partialErr != nil {
						r.err = partialErr
						break consumeLoop
					}
					if hasPartial {
						newJSONStr, ok := partialOutputDedupKey(partial)
						if ok && (!hasPublishedPartial || newJSONStr != lastPartialJSON) {
							hasPublishedPartial = true
							lastPartialJSON = newJSONStr
							r.mu.Lock()
							r.partialOutput = partial
							r.mu.Unlock()
						}
					}
				}
			}

		case provider.ChunkTypeReasoning:
			if chunk.Reasoning != "" {
				rc := types.ReasoningContent{Text: chunk.Reasoning}
				step.reasoning = append(step.reasoning, rc)
				step.content = append(step.content, rc)
			}

		case provider.ChunkTypeToolCall:
			if chunk.ToolCall != nil {
				step.toolCalls = append(step.toolCalls, *chunk.ToolCall)
				step.content = append(step.content, contentPartFromToolCall(*chunk.ToolCall))
			}

		case provider.ChunkTypeToolResult:
			if chunk.ToolResult != nil {
				step.toolResults = append(step.toolResults, *chunk.ToolResult)
				step.content = append(step.content, toolResultContentFromToolResult(*chunk.ToolResult))
			}

		case provider.ChunkTypeSource:
			if chunk.SourceContent != nil {
				step.sources = append(step.sources, *chunk.SourceContent)
				step.content = append(step.content, *chunk.SourceContent)
			}

		case provider.ChunkTypeFile:
			if chunk.GeneratedFileContent != nil {
				step.files = append(step.files, *chunk.GeneratedFileContent)
				step.content = append(step.content, *chunk.GeneratedFileContent)
			}

		case provider.ChunkTypeReasoningFile:
			if chunk.ReasoningFileContent != nil {
				step.content = append(step.content, *chunk.ReasoningFileContent)
			}

		case provider.ChunkTypeCustom:
			if chunk.CustomContent != nil {
				step.content = append(step.content, *chunk.CustomContent)
			}

		case provider.ChunkTypeToolApprovalRequest:
			if chunk.ToolApprovalRequest != nil {
				step.content = append(step.content, *chunk.ToolApprovalRequest)
			}

		case provider.ChunkTypeResponseMetadata:
			if chunk.ResponseMetadata != nil {
				step.response = types.StepResponse{
					ID:        chunk.ResponseMetadata.ID,
					Timestamp: chunk.ResponseMetadata.Timestamp,
					ModelID:   chunk.ResponseMetadata.ModelID,
					Headers:   chunk.ResponseMetadata.Headers,
				}
			}

		case provider.ChunkTypeFinishStep, provider.ChunkTypeFinish:
			// A terminal ChunkTypeFinish that arrives with no step content
			// accumulated since the last step boundary is a pure turn-closing
			// boundary, not a fresh step of its own: every real step was
			// already appended to Steps() by its own ChunkTypeFinishStep.
			// This is exactly the shape a harness.HarnessAgent turn produces
			// (harness.md WG4, TS 57e0a59 "ensure finish chunk's total usage
			// is actually coming from total usage"): the terminal `finish`
			// bridge part carries the run's totalUsage, which must OVERRIDE
			// the locally summed total rather than being added to it, and
			// must not create a spurious empty extra step. Handle that case
			// distinctly instead of running it through finishStep, which
			// always appends a step and always sums usage.
			//
			// Guarded by stepNumber > 0 so a caller whose *only* step ends
			// directly in ChunkTypeFinish (no ChunkTypeFinishStep ever seen —
			// the general, non-harness shape: a single real step that
			// happens to have produced no visible content) still gets that
			// step appended to Steps() and its usage summed, exactly as
			// before this change. Without this guard, a legitimately empty
			// first-and-only step would be silently dropped from Steps()
			// instead of recorded. A harness turn always closes every model
			// step, including a lone one, with its own ChunkTypeFinishStep
			// before the terminal ChunkTypeFinish (run_prompt.go rejects a
			// terminal `finish` with unclosed step content), so stepNumber is
			// always >= 1 by the time the real turn-closing boundary arrives.
			if chunk.Type == provider.ChunkTypeFinish && step.isEmpty() && stepNumber > 0 {
				r.mu.Lock()
				if chunk.Usage != nil {
					r.usage = *chunk.Usage
				}
				r.finishReason = chunk.FinishReason
				if chunk.RawFinishReason != "" {
					r.rawFinishReason = chunk.RawFinishReason
				}
				if len(chunk.ProviderMetadata) > 0 {
					var md map[string]interface{}
					if jsonErr := json.Unmarshal(chunk.ProviderMetadata, &md); jsonErr == nil {
						if raw, err := json.Marshal(md); err == nil {
							r.providerMetadata = raw
						}
					}
				}
				r.status = StreamStatusDone
				r.mu.Unlock()
				break
			}

			if chunk.Usage != nil {
				step.usage = *chunk.Usage
			}
			step.finishReason = chunk.FinishReason
			step.rawFinishReason = chunk.RawFinishReason
			if len(chunk.ProviderMetadata) > 0 {
				var md map[string]interface{}
				if jsonErr := json.Unmarshal(chunk.ProviderMetadata, &md); jsonErr == nil {
					step.providerMetadata = md
				}
			}
			finishStep(chunk.Type == provider.ChunkTypeFinish)

		case provider.ChunkTypeAbort:
			// Mirrors ChunkTypeError's handling below: preserve whatever
			// content the in-progress step accumulated instead of silently
			// dropping it, and stop consuming src immediately. Unlike
			// ChunkTypeError, this is a clean, caller-initiated stop (TS
			// 86a84c9: "settle a turn aborted by the caller's abortSignal
			// with an `abort` stream part instead of an [error]"), so
			// FinishReason is FinishReasonOther rather than
			// FinishReasonError. Err() is still set so callers awaiting
			// completion observe the abort rather than hanging — TS's
			// equivalent note is that "the delayed promise accessors still
			// reject with the underlying error". ChunkTypeAbort was already
			// forwarded to opts.OnChunk and r.chunkBuf above (unconditional,
			// like every other chunk type), so ToUIMessageStream's own
			// abort handling (isAborted, an `abort` UI chunk, no onError)
			// applies to this result exactly as it does to a
			// provider.LanguageModel-driven one.
			if step.finishReason == "" {
				step.finishReason = types.FinishReasonOther
			}
			if step.rawFinishReason == "" {
				step.rawFinishReason = "aborted"
			}
			reason := chunk.AbortReason
			if reason == "" {
				reason = "aborted"
			}
			// Wraps context.Canceled (rather than a plain fmt.Errorf string)
			// so that generic abort-detection helpers downstream —
			// isAbortErr, used by ToUIMessageStream to decide whether a
			// terminal stream error is an abort (no "error" chunk, no
			// errCh error) or a real failure — classify it correctly via
			// errors.Is(err, context.Canceled) regardless of which ctx
			// instance a later ToUIMessageStream call happens to be given:
			// a harness turn's own ctx (the abort's actual cause) is often
			// no longer the same ctx used to read back the already-buffered
			// result afterward (e.g. an HTTP handler keeps writing the
			// response with a live ctx after the generation ctx aborted).
			r.err = fmt.Errorf("%s: %w", reason, context.Canceled)
			finishStep(true)
			break consumeLoop

		case provider.ChunkTypeError:
			// An error chunk ends the step (and the whole result) the same
			// way ChunkTypeFinish does, so any text/tool calls/usage
			// accumulated in the in-progress step before the failure are
			// still preserved on Steps()/Text()/Usage() rather than
			// silently dropped. Explicitly stop consuming src instead of
			// relying on it returning io.EOF next, since a caller-supplied
			// src is not guaranteed to end immediately after an error part.
			if step.finishReason == "" {
				step.finishReason = types.FinishReasonError
			}
			if step.rawFinishReason == "" {
				step.rawFinishReason = "error"
			}
			r.err = fmt.Errorf("%s", chunk.Text)
			finishStep(true)
			break consumeLoop
		}
	}

	// Resolve the final typed output, if a spec was provided, from the last
	// step's own text — same scope and unconditional-on-finishReason contract
	// as StreamText's own terminal output resolution (audit row 2a5ed55 /
	// WG4); skipped when the turn itself failed, matching stream.go's r.err
	// != nil early return before that point.
	if r.err == nil && r.outputSpec != nil {
		parsed, parseErr := r.outputSpec.parseCompleteOutput(ctx, ParseCompleteOutputOptions{
			Text:         lastStepText,
			FinishReason: r.finishReason,
			Usage:        &r.usage,
		})
		r.mu.Lock()
		if parseErr == nil {
			r.outputResult = parsed
		}
		r.outputErr = parseErr
		r.mu.Unlock()
	}

	r.mu.Lock()
	if r.status != StreamStatusDone {
		r.status = StreamStatusDone
	}
	r.mu.Unlock()

	if onEnd != nil {
		onEnd(r)
	}
}

// reasoningText concatenates reasoning content parts' text, matching
// types.StepResult.ReasoningText's meaning elsewhere in this package.
func reasoningText(reasoning []types.ReasoningContent) string {
	if len(reasoning) == 0 {
		return ""
	}
	var parts []string
	for _, r := range reasoning {
		if r.Text != "" {
			parts = append(parts, r.Text)
		}
	}
	return strings.Join(parts, "")
}
