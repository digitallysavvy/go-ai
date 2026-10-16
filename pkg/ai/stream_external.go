package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
//   - The public full stream carries a ChunkTypeStartStep (with the step's
//     warnings) before the first content of every step, whether or not src
//     sends ChunkTypeStreamStart (which stays internal), as TS
//     HarnessStreamTextResult does.
//   - A tool-result chunk with Preliminary set is recorded in the step like any
//     other tool-result (content, ToolResults, response messages), exactly as
//     TS HarnessStreamTextResult appends every tool-result part, including
//     preliminary ones, to the step content; the final result for the same
//     call does not supersede or replace them.
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
	result.uiChunkBuf = newChunkBuffer()
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
	return s.text == "" && len(s.content) == 0 &&
		len(s.files) == 0 && len(s.sources) == 0 && len(s.toolCalls) == 0 &&
		len(s.toolResults) == 0 && len(s.warnings) == 0
}

// startText begins a new text block for id, always as its own content part.
func (s *externalStepAccum) startText(id string, md json.RawMessage) {
	if s.textIdx == nil {
		s.textIdx = map[string]int{}
	}
	s.textIdx[id] = len(s.content)
	s.content = append(s.content, types.TextContent{ProviderMetadata: md})
}

// addText appends a text delta to the active block for id (creating it if no
// text-start was seen). An empty id (a source that does not tag blocks)
// falls back to merging into the trailing text part.
func (s *externalStepAccum) addText(id, text string, md json.RawMessage) {
	if id == "" {
		s.content = appendTextPart(s.content, text, md)
		return
	}
	i, ok := s.textIdx[id]
	if !ok {
		s.startText(id, md)
		i = s.textIdx[id]
	}
	tc := s.content[i].(types.TextContent)
	tc.Text += text
	if len(md) > 0 {
		tc.ProviderMetadata = md
	}
	s.content[i] = tc
}

func (s *externalStepAccum) endText(id string, md json.RawMessage) {
	i, ok := s.textIdx[id]
	if !ok {
		return
	}
	if len(md) > 0 {
		tc := s.content[i].(types.TextContent)
		tc.ProviderMetadata = md
		s.content[i] = tc
	}
	delete(s.textIdx, id)
}

func (s *externalStepAccum) startReasoning(id string, md json.RawMessage) {
	if s.reasoningIdx == nil {
		s.reasoningIdx = map[string]int{}
	}
	s.reasoningIdx[id] = len(s.content)
	s.content = append(s.content, types.ReasoningContent{ProviderMetadata: md})
}

func (s *externalStepAccum) addReasoning(id, text string, md json.RawMessage) {
	if id == "" {
		s.content = appendReasoningPart(s.content, text, md)
		return
	}
	i, ok := s.reasoningIdx[id]
	if !ok {
		s.startReasoning(id, md)
		i = s.reasoningIdx[id]
	}
	rc := s.content[i].(types.ReasoningContent)
	rc.Text += text
	if len(md) > 0 {
		rc.ProviderMetadata = md
	}
	s.content[i] = rc
}

func (s *externalStepAccum) endReasoning(id string, md json.RawMessage) {
	i, ok := s.reasoningIdx[id]
	if !ok {
		return
	}
	if len(md) > 0 {
		rc := s.content[i].(types.ReasoningContent)
		rc.ProviderMetadata = md
		s.content[i] = rc
	}
	delete(s.reasoningIdx, id)
}

// reasoningParts returns every reasoning part in content, in order.
func (s *externalStepAccum) reasoningParts() []types.ReasoningContent {
	var out []types.ReasoningContent
	for _, part := range s.content {
		if rc, ok := part.(types.ReasoningContent); ok {
			out = append(out, rc)
		}
	}
	return out
}

// externalStepAccum holds the in-progress state for one step while
// consumeExternalParts reads src.
type externalStepAccum struct {
	text    string
	content []types.ContentPart
	// textIdx/reasoningIdx map an active text/reasoning block's stream ID to
	// its index in content, so interleaved blocks (a-start, b-start, a-delta,
	// b-delta, a-delta) accumulate into their own parts instead of merging
	// into whichever part is last. Cleared at every step boundary (a fresh
	// externalStepAccum). Mirrors TS HarnessStreamTextResult's
	// currentStepTextContent/currentStepReasoningContent maps (TS #23086).
	textIdx          map[string]int
	reasoningIdx     map[string]int
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
		if r.uiChunkBuf != nil {
			r.uiChunkBuf.close(r.err)
		}
	}()

	onEnd := opts.OnEnd
	if onEnd == nil {
		onEnd = opts.OnFinish
	}

	var accumulatedText []string
	stepNumber := 0
	step := &externalStepAccum{}
	stepStarted := false

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
		reasoning := step.reasoningParts()
		// Response messages are built per step and concatenated in step
		// order, so a tool-call step followed by an answer step yields
		// assistant/tool/assistant rather than a flattened, reordered
		// assistant/tool pair (TS #23085). Provider-executed results stay on
		// the assistant message; only host (client-executed) results go on
		// the tool message.
		stepResponseMsgs := providerutils.ConvertToResponseMessages(step.toolCalls, content, step.toolResults)
		step.response.Messages = stepResponseMsgs
		// A chunk-supplied response model (the runtime-resolved one) wins over
		// the requested one, as TS's step modelId follows the result's.
		stepModelID := opts.ModelID
		if step.response.ModelID != "" {
			stepModelID = step.response.ModelID
		}
		result := types.StepResult{
			CallID:           r.cbCallID,
			StepNumber:       stepNumber,
			Model:            types.StepModel{Provider: opts.Provider, ModelID: stepModelID},
			Text:             step.text,
			Content:          content,
			Reasoning:        reasoning,
			ReasoningText:    reasoningText(reasoning),
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
			ResponseMessages: stepResponseMsgs,
		}

		accumulatedText = append(accumulatedText, step.text)
		lastStepText = step.text
		hasPublishedPartial = false
		lastPartialJSON = ""

		r.mu.Lock()
		r.cbSteps = append(r.cbSteps, result)
		r.cbResponseMessages = append(r.cbResponseMessages, stepResponseMsgs...)
		r.text = strings.Join(accumulatedText, "")
		// TS HarnessStreamTextResult.text resolves to the final step's text.
		r.finalText = step.text
		r.hasFinalText = true
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
		stepStarted = false
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
		// TS HarnessStreamTextResult.startStep(): the first part of every step
		// (and an otherwise empty step's finish-step) is preceded by a
		// 'start-step' part carrying the step's warnings. The source's
		// stream-start is internal, so the step opener is synthesized here for
		// the public stream and the UI conversion alike.
		if chunk.Type == provider.ChunkTypeStartStep {
			stepStarted = true
		} else if !stepStarted && opensExternalStep(chunk, step, stepNumber) {
			stepStarted = true
			warnings := append([]types.Warning{}, step.warnings...)
			startStep := provider.StreamChunk{Type: provider.ChunkTypeStartStep, Warnings: warnings}
			if r.uiChunkBuf != nil {
				r.uiChunkBuf.push(startStep)
			}
			if r.chunkBuf != nil {
				r.chunkBuf.push(startStep)
			}
		}
		if r.uiChunkBuf != nil {
			r.uiChunkBuf.push(*chunk)
		}
		// The public full stream carries the TS part set only (as StreamText).
		if r.chunkBuf != nil && !isInternalStreamMarker(chunk.Type) {
			r.chunkBuf.push(*chunk)
		}

		switch chunk.Type {
		case provider.ChunkTypeStreamStart:
			step.warnings = append(step.warnings, chunk.Warnings...)

		case provider.ChunkTypeTextStart:
			step.startText(chunk.ID, chunk.ProviderMetadata)

		case provider.ChunkTypeTextEnd:
			step.endText(chunk.ID, chunk.ProviderMetadata)

		case provider.ChunkTypeReasoningStart:
			step.startReasoning(chunk.ID, chunk.ProviderMetadata)

		case provider.ChunkTypeReasoningEnd:
			step.endReasoning(chunk.ID, chunk.ProviderMetadata)

		case provider.ChunkTypeText:
			if chunk.Text != "" || len(chunk.ProviderMetadata) > 0 {
				step.addText(chunk.ID, chunk.Text, chunk.ProviderMetadata)
			}
			if chunk.Text != "" {
				step.text += chunk.Text

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
			reasoningDelta := chunk.Reasoning
			if reasoningDelta == "" {
				reasoningDelta = chunk.Text
			}
			if reasoningDelta != "" || len(chunk.ProviderMetadata) > 0 {
				step.addReasoning(chunk.ID, reasoningDelta, chunk.ProviderMetadata)
			}

		case provider.ChunkTypeToolCall:
			if chunk.ToolCall != nil {
				step.toolCalls = append(step.toolCalls, *chunk.ToolCall)
				step.content = append(step.content, contentPartFromToolCall(*chunk.ToolCall))
			}

		case provider.ChunkTypeToolResult:
			// TS HarnessStreamTextResult.appendToCurrentStepContent records
			// every tool-result part, preliminary ones included (unlike core
			// streamText, which keeps preliminary results stream-only).
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
			// Keep the typed error when the source carries one (TS rejects the
			// result promises with the original error object), so errors.As
			// works on Err().
			if chunk.Err != nil {
				r.err = chunk.Err
			} else {
				r.err = fmt.Errorf("%s", chunk.Text)
			}
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

// opensExternalStep reports whether chunk is the first step content of an
// external stream's step, i.e. whether a 'start-step' part must precede it.
// Lifecycle markers and a pure turn-closing finish (no content since the last
// step boundary) do not open a step; error/abort chunks end the stream.
func opensExternalStep(chunk *provider.StreamChunk, step *externalStepAccum, stepNumber int) bool {
	switch chunk.Type {
	case provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeFirstChunk, provider.ChunkTypeStreamFinish,
		provider.ChunkTypeError, provider.ChunkTypeAbort, provider.ChunkTypeRaw:
		return false
	case provider.ChunkTypeFinish:
		return !step.isEmpty() || stepNumber == 0
	}
	return true
}
