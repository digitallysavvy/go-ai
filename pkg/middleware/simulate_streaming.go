package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// providerMetadataRawJSON marshals a provider-metadata map into the
// json.RawMessage shape used on StreamChunk.ProviderMetadata. Returns nil for
// an empty/nil map so the "finish" chunk omits the field rather than
// carrying an empty "{}"  object.
func providerMetadataRawJSON(metadata map[string]interface{}) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	data, err := jsonstr.Marshal(metadata)
	if err != nil {
		return nil
	}
	return data
}

// SimulateStreamingMiddleware returns middleware that converts non-streaming
// generate responses into simulated streams.
//
// This is useful for providers that don't support streaming natively, or for
// testing streaming behavior with non-streaming responses.
//
// Example:
//
//	middleware := SimulateStreamingMiddleware()
//	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)
//
//	// Now stream calls will use generate internally and simulate streaming
//	stream, err := wrapped.DoStream(ctx, opts)
func SimulateStreamingMiddleware() *LanguageModelMiddleware {
	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",

		// Only wrap stream, not generate
		WrapStream: func(
			ctx context.Context,
			doGenerate func() (*types.GenerateResult, error),
			doStream func() (provider.TextStream, error),
			params *provider.GenerateOptions,
			model provider.LanguageModel,
		) (provider.TextStream, error) {
			// Call generate instead of stream
			result, err := doGenerate()
			if err != nil {
				return nil, err
			}

			// Create a simulated stream from the result
			return &simulatedStream{
				result:  result,
				chunks:  nil, // Will be built lazily
				current: 0,
			}, nil
		},
	}
}

// toolResultFromResultContent converts a types.ToolResultContent (the
// ordered-content representation used in GenerateResult.Content) into the
// types.ToolResult shape provider.StreamChunk.ToolResult expects.
func toolResultFromResultContent(rc types.ToolResultContent) types.ToolResult {
	var errVal error
	if rc.Error != "" {
		errVal = errors.New(rc.Error)
	}
	var providerMetadata map[string]interface{}
	if len(rc.ProviderMetadata) > 0 {
		_ = jsonexact.Unmarshal(rc.ProviderMetadata, &providerMetadata)
	}
	return types.ToolResult{
		ToolCallID:       rc.ToolCallID,
		ToolName:         rc.ToolName,
		Title:            rc.Title,
		Input:            rc.Input,
		Result:           rc.Result,
		ModelOutput:      rc.Output,
		Error:            errVal,
		Dynamic:          rc.Dynamic,
		Preliminary:      rc.Preliminary,
		ProviderExecuted: rc.ProviderExecuted,
		ProviderMetadata: providerMetadata,
		ToolMetadata:     rc.ToolMetadata,
	}
}

// toolCallFromCallContent converts a tool-call content part into the
// stream's ToolCall: the decoded Arguments win, otherwise the raw JSON
// Input is parsed.
func toolCallFromCallContent(part types.ToolCallContent) types.ToolCall {
	args := part.Arguments
	if args == nil && part.Input != "" {
		var parsed map[string]interface{}
		if err := jsonexact.Unmarshal([]byte(part.Input), &parsed); err == nil {
			args = parsed
		}
	}
	var providerMetadata map[string]interface{}
	if len(part.ProviderMetadata) > 0 {
		_ = jsonexact.Unmarshal(part.ProviderMetadata, &providerMetadata)
	}
	return types.ToolCall{
		ID:                  part.ToolCallID,
		ToolName:            part.ToolName,
		Title:               part.Title,
		Arguments:           args,
		RawArguments:        part.Input,
		ProviderExecuted:    part.ProviderExecuted,
		ProviderExecutedSet: part.ProviderExecuted,
		ProviderMetadata:    providerMetadata,
		ToolMetadata:        part.ToolMetadata,
		ThoughtSignature:    part.ThoughtSignature,
		Dynamic:             part.Dynamic,
		Invalid:             part.Invalid,
	}
}

// simulatedStream simulates a streaming response from a GenerateResult
type simulatedStream struct {
	result  *types.GenerateResult
	chunks  []*provider.StreamChunk
	current int
	closed  bool
}

// buildChunks creates the sequence of chunks that simulate streaming,
// mirroring TS simulateStreamingMiddleware: stream-start, response-metadata,
// then per-content-part text-start/delta/end and reasoning-start/delta/end
// blocks (each with the source part's ProviderMetadata), other content parts
// forwarded as-is, tool calls, and a final finish chunk carrying the
// top-level ProviderMetadata (audit row 4775577 / WG12).
func (s *simulatedStream) buildChunks() {
	if s.chunks != nil {
		return
	}

	var chunks []*provider.StreamChunk

	chunks = append(chunks, &provider.StreamChunk{
		Type:     provider.ChunkTypeStreamStart,
		Warnings: s.result.Warnings,
	})

	if s.result.ResponseMetadata != nil {
		rm := s.result.ResponseMetadata
		chunks = append(chunks, &provider.StreamChunk{
			Type: provider.ChunkTypeResponseMetadata,
			ResponseMetadata: &provider.ResponseMetadata{
				ID:        rm.ID,
				Timestamp: rm.Timestamp,
				ModelID:   rm.ModelID,
				Headers:   rm.Headers,
			},
		})
	}

	nextID := 0
	newID := func() string {
		id := strconv.Itoa(nextID)
		nextID++
		return id
	}

	sawTextContent := false
	emittedCalls := map[string]bool{}
	for _, part := range s.result.Content {
		switch p := part.(type) {
		case types.TextContent:
			sawTextContent = true
			if len(p.Text) == 0 && len(p.Citations) == 0 {
				continue
			}
			id := newID()
			chunks = append(chunks,
				&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id, ProviderMetadata: p.ProviderMetadata},
				&provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: p.Text},
				&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id, Citations: p.Citations},
			)
		case types.ReasoningContent:
			id := newID()
			chunks = append(chunks,
				&provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id, ProviderMetadata: p.ProviderMetadata},
				&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: p.Text},
				&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: id},
			)
		case types.SourceContent:
			part := p
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &part})
		case types.GeneratedFileContent:
			part := p
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &part})
		case types.ReasoningFileContent:
			part := p
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &part})
		case types.CustomContent:
			part := p
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeCustom, CustomContent: &part})
		case types.ToolResultContent:
			// Provider-executed tool results (e.g. Anthropic tool-search,
			// xAI file/web search) live only in Content: unlike tool calls,
			// types.GenerateResult has no flat ToolResults field to fall
			// back on, so dropping this case would silently lose them.
			result := toolResultFromResultContent(p)
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &result})
		case types.ToolApprovalRequestContent:
			part := p
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &part})
		case types.ToolCallContent:
			// Emitted in content order (TS enqueues every other part as is).
			// The flat GenerateResult.ToolCalls entry carries the full
			// provider-facing call; calls missing from Content follow below.
			matched := false
			for i := range s.result.ToolCalls {
				if s.result.ToolCalls[i].ID == p.ToolCallID && !emittedCalls[p.ToolCallID] {
					emittedCalls[p.ToolCallID] = true
					chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &s.result.ToolCalls[i]})
					matched = true
					break
				}
			}
			if !matched {
				// A tool-call part with no flat ToolCalls entry is still
				// enqueued (TS enqueues every tool-call content part).
				call := toolCallFromCallContent(p)
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &call})
			}
		}
	}

	// Older/simpler providers only populate result.Text, not a matching
	// types.TextContent in result.Content. Synthesize a single text block
	// from it in that case so their output still streams.
	if !sawTextContent && len(s.result.Text) > 0 {
		id := newID()
		chunks = append(chunks,
			&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id},
			&provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: s.result.Text},
			&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id},
		)
	}

	// Emit tool calls
	for i := range s.result.ToolCalls {
		if emittedCalls[s.result.ToolCalls[i].ID] {
			continue
		}
		chunks = append(chunks, &provider.StreamChunk{
			Type:     provider.ChunkTypeToolCall,
			ToolCall: &s.result.ToolCalls[i],
		})
	}

	// Emit finish chunk, carrying the response's provider metadata like TS.
	chunks = append(chunks, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.result.FinishReason,
		Usage:            &s.result.Usage,
		ProviderMetadata: providerMetadataRawJSON(s.result.ProviderMetadata),
	})

	s.chunks = chunks
}

// Next returns the next chunk in the simulated stream
func (s *simulatedStream) Next() (*provider.StreamChunk, error) {
	if s.closed {
		return nil, io.EOF
	}

	// Build chunks on first access
	if s.chunks == nil {
		s.buildChunks()
	}

	// Check if we've reached the end
	if s.current >= len(s.chunks) {
		return nil, io.EOF
	}

	chunk := s.chunks[s.current]
	s.current++
	return chunk, nil
}

// Close closes the simulated stream
func (s *simulatedStream) Close() error {
	s.closed = true
	return nil
}

// Err returns any error from the stream (always nil for simulated streams)
func (s *simulatedStream) Err() error {
	return nil
}

// RequestBody is the wrapped DoGenerate result's request body (TS
// simulateStreamingMiddleware returns request: result.request).
func (s *simulatedStream) RequestBody() interface{} {
	if s.result == nil {
		return nil
	}
	return s.result.RawRequest
}

// RequestURL is the wrapped DoGenerate result's request URL (TS 934100cad4).
func (s *simulatedStream) RequestURL() string {
	if s.result == nil {
		return ""
	}
	return s.result.RequestURL
}

// ResponseHeaders implements provider.StreamResponseHeaders: TS
// simulateStreamingMiddleware returns `response: result.response` beside the
// stream (review round 87e).
func (s *simulatedStream) ResponseHeaders() map[string]string {
	if s.result == nil {
		return nil
	}
	return s.result.ResponseHeaders
}
