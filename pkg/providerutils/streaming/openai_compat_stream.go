package streaming

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OpenAICompatStream implements the accumulate-and-flush streaming pattern for
// any provider that uses the OpenAI chat completions SSE format.
//
// Security: tool call arguments are accumulated across all SSE deltas and only
// emitted when finish_reason is received, preventing early finalization based on
// JSON parsability of intermediate fragments.
//
// Embed this struct in provider-specific stream types to inherit Next/Err/Close.
type OpenAICompatStream struct {
	reader             io.ReadCloser
	parser             *SSEParser
	err                error
	toolCallTracker    *StreamingToolCallTracker
	flushQueue         []*provider.StreamChunk
	finishReasonMapper func(string) types.FinishReason
	pendingEventData   string
	// IncludeRawChunks emits the raw parsed SSE event before normal processing,
	// matching providers that expose TypeScript's includeRawChunks behavior.
	IncludeRawChunks bool
	// OnExtraDelta is an optional hook called with raw SSE event bytes before
	// standard delta processing. If it returns (chunk, true), that chunk is
	// returned immediately. Return (nil, false) to fall through to standard
	// processing. Use this to handle provider-specific delta fields (e.g. xAI's
	// reasoning_content).
	OnExtraDelta func(eventBytes []byte) (*provider.StreamChunk, bool)

	// OnBeforeDelta is an optional hook called with raw SSE event bytes before
	// standard delta processing. Any chunks it returns are prepended to the
	// flush queue; standard processing continues regardless. Use this to inject
	// additional chunks from top-level event fields that standard processing
	// does not handle (e.g. xAI's top-level citations array).
	OnBeforeDelta func(eventBytes []byte) []*provider.StreamChunk

	// OnReasoningDelta extracts reasoning text from the raw SSE event bytes.
	// Called before standard text/tool processing. If it returns (text, true)
	// where text != "", the stream manages reasoning-start/delta/end blocks.
	// If it returns ("", true), hook handled it but no reasoning text (no-op).
	// If it returns ("", false), standard processing continues unchanged.
	OnReasoningDelta func(eventBytes []byte) (text string, ok bool)

	// isActiveReasoning tracks whether we are inside a reasoning block.
	isActiveReasoning bool

	// finishPending is set once a finish_reason has been observed. The actual
	// ChunkTypeFinish chunk is not enqueued until the SSE stream truly ends
	// ([DONE] or EOF), so that a trailing choices-less usage event (the
	// stream_options.include_usage tail chunk) can be merged into it. This
	// matches TS openai-compatible, whose TransformStream only enqueues
	// `finish` in flush(), after every chunk (including a trailing usage-only
	// one) has been processed.
	finishPending       bool
	pendingFinishReason string
	pendingUsage        *openAICompatStreamUsage

	// streamErrored is set once a "soft" error chunk has already been
	// emitted for this stream: a JSON parse failure or a provider "error"
	// field on an SSE event. Both correspond to a TS branch that sets
	// `finishReason = { unified: 'error', ... }` and returns normally from
	// transform() (openai-compatible-chat-language-model.ts:574-575,
	// 582-586) -- flush() still runs to completion afterward, so endStream
	// must still emit the terminal finish chunk, just without re-injecting
	// the separate "no finish_reason ever observed" error on top of it.
	streamErrored bool

	// toolCallErrored is set once toolCallTracker.Flush() has produced a
	// ChunkTypeError (a tool call whose function.name never arrived), either
	// mid-stream (flushToolCallsAndFinish, when a finish_reason did arrive)
	// or at true stream end (endStream, for a stream that never observed
	// one). TS's toolCallTracker.flush() *throws* in this case, aborting
	// flush() before its terminal `finish` enqueue is ever reached -- so
	// endStream must never emit a finish chunk once this is set, unlike the
	// "soft" streamErrored case above.
	toolCallErrored bool
}

// NewOpenAICompatStream creates a new OpenAICompatStream.
// mapper converts a raw finish_reason string to a types.FinishReason.
// Pass providerutils.MapOpenAIFinishReason for the standard mapping, or a
// custom function for providers that extend the standard set (e.g. Mistral's
// "model_length").
func NewOpenAICompatStream(reader io.ReadCloser, mapper func(string) types.FinishReason) *OpenAICompatStream {
	return &OpenAICompatStream{
		reader:             reader,
		parser:             NewSSEParser(reader),
		toolCallTracker:    NewStreamingToolCallTracker(),
		finishReasonMapper: mapper,
	}
}

// Close closes the underlying HTTP response body.
func (s *OpenAICompatStream) Close() error {
	return s.reader.Close()
}

// Err returns any non-EOF error that occurred during streaming.
func (s *OpenAICompatStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next returns the next chunk from the stream.
//
// Text deltas are returned immediately. Tool call argument deltas are buffered
// only until function.name is available, then tool input start/delta chunks are
// emitted live. When finish_reason is received, open tool inputs are closed and
// finalized tool-call chunks are enqueued before the finish chunk.
func (s *OpenAICompatStream) Next() (*provider.StreamChunk, error) {
	// Drain any fully-assembled chunks before reading more SSE events.
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}

	if s.err != nil {
		return nil, s.err
	}

	eventData := s.pendingEventData
	if eventData != "" {
		s.pendingEventData = ""
	} else {
		event, err := s.parser.Next()
		if err != nil {
			if err == io.EOF {
				return s.endStream(io.EOF)
			}
			s.err = err
			return nil, err
		}

		if IsStreamDone(event) {
			return s.endStream(io.EOF)
		}

		eventData = event.Data
		if s.IncludeRawChunks {
			s.pendingEventData = eventData
			var raw interface{}
			if err := json.Unmarshal([]byte(eventData), &raw); err != nil {
				raw = eventData
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeRaw,
				Raw:  raw,
			}, nil
		}
	}

	// The OpenAI-compatible SSE format sends choices[0].delta for streaming.
	// Tool call deltas include an "index" field used to correlate fragments
	// belonging to the same tool call across multiple events.
	var chunkData struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    *int    `json:"index"`
					ID       string  `json:"id"`
					Type     *string `json:"type"` // nullable mid-stream
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
					ExtraContent map[string]interface{} `json:"extra_content,omitempty"`
				} `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Error json.RawMessage          `json:"error,omitempty"`
		Usage *openAICompatStreamUsage `json:"usage,omitempty"`
	}

	if err := json.Unmarshal([]byte(eventData), &chunkData); err != nil {
		// Matches TS's chunk-schema-parse-failure branch (openai-compatible-
		// chat-language-model.ts:574-575), which sets finishReason to the
		// unified "error" reason right when this fires -- so endStream must
		// not additionally treat a later clean EOF as a "truncated stream".
		s.streamErrored = true
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		}, nil
	}
	if len(chunkData.Error) > 0 {
		// Matches TS's `'error' in chunk.value` branch (line 582-586), which
		// likewise sets finishReason to the unified "error" reason.
		s.streamErrored = true
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: rawStreamErrorText(chunkData.Error),
		}, nil
	}

	// Usage may arrive on the finish_reason event itself, or as a trailing
	// choices-less event (stream_options.include_usage); the latest value
	// wins, matching TS's `if (value.usage != null) { usage = value.usage; }`.
	if chunkData.Usage != nil {
		s.pendingUsage = chunkData.Usage
	}

	// Pre-delta hook: enqueue extra chunks (e.g. top-level citations) before
	// standard processing. Prepend so they drain before the finish chunk.
	if s.OnBeforeDelta != nil {
		if extra := s.OnBeforeDelta([]byte(eventData)); len(extra) > 0 {
			s.flushQueue = append(extra, s.flushQueue...)
		}
	}

	// Provider-specific delta hook (e.g. xAI reasoning_content).
	if s.OnExtraDelta != nil {
		if chunk, handled := s.OnExtraDelta([]byte(eventData)); handled {
			if chunk != nil && len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}
	}

	// Reasoning delta hook — manage start/end lifecycle.
	if s.OnReasoningDelta != nil {
		if rc, handled := s.OnReasoningDelta([]byte(eventData)); handled && rc != "" {
			if !s.isActiveReasoning {
				s.isActiveReasoning = true
				// Append (not prepend): any chunks OnBeforeDelta already
				// queued for this same event (e.g. a one-shot
				// response-metadata chunk) must drain before this stream's
				// own synthesized reasoning-start/delta, matching TS's
				// order (response metadata is emitted by the core
				// transform before provider-specific delta extraction).
				s.flushQueue = append(s.flushQueue,
					&provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"},
					&provider.StreamChunk{Type: provider.ChunkTypeReasoning, Reasoning: rc, ID: "reasoning-0"},
				)
				return s.Next()
			}
			chunk := &provider.StreamChunk{
				Type:      provider.ChunkTypeReasoning,
				Reasoning: rc,
				ID:        "reasoning-0",
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}
	}

	if len(chunkData.Choices) > 0 {
		choice := chunkData.Choices[0]

		// Text delta — emit immediately.
		if choice.Delta.Content != "" {
			if s.isActiveReasoning {
				s.isActiveReasoning = false
				// Append (not prepend): see the matching comment above for
				// the reasoning-start case.
				s.flushQueue = append(s.flushQueue,
					&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
					&provider.StreamChunk{Type: provider.ChunkTypeText, Text: choice.Delta.Content},
				)
				return s.Next()
			}
			chunk := &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: choice.Delta.Content,
			}
			if len(s.flushQueue) > 0 {
				s.flushQueue = append(s.flushQueue, chunk)
				return s.Next()
			}
			return chunk, nil
		}

		// Tool call delta — accumulate arguments by index, never emit yet.
		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				chunks := s.toolCallTracker.TrackDelta(ToolCallDelta{
					Index:            tc.Index,
					ID:               tc.ID,
					Name:             tc.Function.Name,
					ArgumentsDelta:   tc.Function.Arguments,
					ProviderMetadata: openAICompatToolCallMetadata(tc.ExtraContent),
				})
				s.enqueueChunks(chunks)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				s.flushToolCallsAndFinish(*choice.FinishReason)
			}
			return s.Next()
		}

		// Finish event — flush all accumulated tool calls, then emit finish.
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.flushToolCallsAndFinish(*choice.FinishReason)
			return s.Next()
		}
	}

	// Empty or unrecognised event — skip and fetch the next one.
	return s.Next()
}

func rawStreamErrorText(raw json.RawMessage) string {
	var withMessage struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &withMessage); err == nil && withMessage.Message != "" {
		return withMessage.Message
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func (s *OpenAICompatStream) enqueueChunks(chunks []ToolCallChunk) {
	for _, chunk := range chunks {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
}

func (s *OpenAICompatStream) flushToolCallsAndFinish(finishReason string) {
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append([]*provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"},
		}, s.flushQueue...)
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
		if c.Type == provider.ChunkTypeError {
			// Mirrors TS's toolCallTracker.flush() throwing on a missing
			// function.name: the stream has already errored out here, so
			// endStream must not additionally treat the eventual EOF as a
			// "truncated stream", nor emit any finish chunk at all (no
			// finish_reason ever arrives in that TS throw path either --
			// flush() aborts before its terminal `finish` enqueue).
			s.streamErrored = true
			s.toolCallErrored = true
			return
		}
	}
	// Do not enqueue the finish chunk yet: hold it until the stream truly
	// ends (see endStream) so a trailing usage-only event can be merged in.
	s.finishPending = true
	s.pendingFinishReason = finishReason
}

// endStream is called when the underlying SSE stream is exhausted ([DONE] or
// a genuine EOF from the reader). If a finish_reason was already observed, it
// synthesizes the deferred finish chunk (merging in any usage seen since),
// matching TS's flush()-time `finish` enqueue.
//
// Otherwise this mirrors TS's flush() unconditional tail (openai-compatible-
// chat-language-model.ts, d68139c3bb): close any still-open reasoning block,
// forward any tool-call fragments still buffered in the tracker, and -- only
// if no finish_reason was ever observed *and* no "soft" error chunk (parse
// failure / provider error field) was already emitted for this stream --
// enqueue an InvalidResponseDataError "error" chunk for the missing finish
// reason. In every case that reaches this point without a hard tool-call
// error, TS's flush() still runs to completion, so a terminal `finish` chunk
// (unified "error" reason) is always enqueued alongside it. A hard tool-call
// error (s.toolCallErrored, set here or earlier by flushToolCallsAndFinish)
// is the one exception: TS's toolCallTracker.flush() throws in that case,
// aborting flush() before its terminal `finish` enqueue is ever reached, so
// no finish chunk is emitted at all.
func (s *OpenAICompatStream) endStream(err error) (*provider.StreamChunk, error) {
	if s.finishPending {
		s.finishPending = false
		finishReason := s.pendingFinishReason
		usage := s.pendingUsage
		s.pendingFinishReason = ""
		s.err = err
		return &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: s.finishReasonMapper(finishReason),
			Usage:        convertOpenAICompatStreamUsage(usage),
		}, nil
	}
	if err == io.EOF {
		// Order matches TS flush(): close any still-open reasoning block,
		// then forward pending tool-call fragments through the tracker,
		// before the error/finish chunks.
		var pending []*provider.StreamChunk
		if s.isActiveReasoning {
			s.isActiveReasoning = false
			pending = append(pending, &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0",
			})
		}
		for _, chunk := range s.toolCallTracker.Flush() {
			c := chunk
			pending = append(pending, &c)
			if c.Type == provider.ChunkTypeError {
				s.toolCallErrored = true
			}
		}
		if s.toolCallErrored {
			s.err = err
			if len(pending) == 0 {
				return nil, err
			}
			first := pending[0]
			s.flushQueue = append(s.flushQueue, pending[1:]...)
			return first, nil
		}
		if !s.streamErrored {
			streamErr := providererrors.NewInvalidResponseDataError(nil, "Response stream ended without a finish reason.")
			pending = append(pending, &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: streamErr.Error(),
				Err:  streamErr,
			})
		}
		pending = append(pending, &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: types.FinishReasonError,
			Usage:        convertOpenAICompatStreamUsage(s.pendingUsage),
		})
		s.err = err
		first := pending[0]
		s.flushQueue = append(s.flushQueue, pending[1:]...)
		return first, nil
	}
	s.err = err
	return nil, err
}

// openAICompatStreamUsage mirrors the OpenAI-compatible streaming usage
// payload (stream_options.include_usage), which may arrive on the same event
// as finish_reason or as a trailing choices-less event.
type openAICompatStreamUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

// convertOpenAICompatStreamUsage converts a raw OpenAI-compatible usage
// payload to types.Usage, mirroring TS's convertOpenAICompatibleChatUsage.
// Returns nil when no usage was ever observed on the stream.
func convertOpenAICompatStreamUsage(usage *openAICompatStreamUsage) *types.Usage {
	if usage == nil {
		return nil
	}
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)
	result := &types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}
	var cachedTokens int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cachedTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	}
	if cachedTokens > 0 {
		noCacheTokens := promptTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
		}
	}
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*usage.CompletionTokensDetails.ReasoningTokens)
	}
	if reasoningTokens > 0 {
		textTokens := completionTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}
	result.Raw = map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	return result
}

func openAICompatToolCallMetadata(extraContent map[string]interface{}) map[string]interface{} {
	if len(extraContent) == 0 {
		return nil
	}
	googleRaw, ok := extraContent["google"].(map[string]interface{})
	if !ok {
		return nil
	}
	signature, _ := googleRaw["thought_signature"].(string)
	if signature == "" {
		return nil
	}
	return map[string]interface{}{
		"google": map[string]interface{}{
			"thoughtSignature": signature,
		},
	}
}
