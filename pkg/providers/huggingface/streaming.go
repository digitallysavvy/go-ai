package huggingface

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// hfStream implements provider.TextStream for the Hugging Face Responses
// API's SSE stream, mirroring TS doStream's TransformStream: it reads one
// SSE event per iteration and emits zero or more LanguageModelV4StreamPart
// (StreamChunk) values, finishing with a single "finish" chunk once the
// underlying HTTP body is exhausted (TS's flush()).
type hfStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser

	warnings     []types.Warning
	providerName string

	pending         []*provider.StreamChunk
	streamStartSent bool
	finished        bool
	err             error

	responseID      string
	finishReason    types.FinishReason
	rawFinishReason string
	usage           *hfUsage

	// requestBody/responseHeaders expose the raw request body and response
	// headers to pkg/ai/stream.go via the optional StreamRequestBody
	// interface and the response-metadata chunk respectively.
	requestBody     interface{}
	responseHeaders map[string]string
}

// newHFStream creates a new Hugging Face Responses SSE stream wrapper.
func newHFStream(reader io.ReadCloser, warnings []types.Warning, providerName string) *hfStream {
	return &hfStream{
		reader:       reader,
		parser:       streaming.NewSSEParser(reader),
		warnings:     warnings,
		providerName: providerName,
		finishReason: types.FinishReasonOther,
	}
}

// RequestBody implements provider.StreamRequestBody.
func (s *hfStream) RequestBody() interface{} { return s.requestBody }

// Close implements provider.TextStream.
func (s *hfStream) Close() error {
	s.finished = true
	return s.reader.Close()
}

// Err implements provider.TextStream.
func (s *hfStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next implements provider.TextStream.
func (s *hfStream) Next() (*provider.StreamChunk, error) {
	if len(s.pending) > 0 {
		chunk := s.pending[0]
		s.pending = s.pending[1:]
		return chunk, nil
	}

	if !s.streamStartSent {
		s.streamStartSent = true
		return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings}, nil
	}

	if s.finished {
		return nil, io.EOF
	}

	for {
		event, err := s.parser.Next()
		if err != nil {
			s.finished = true
			if errors.Is(err, io.EOF) {
				return s.buildFinishChunk(), nil
			}
			s.err = err
			return nil, err
		}
		if streaming.IsStreamDone(event) {
			s.finished = true
			return s.buildFinishChunk(), nil
		}

		chunk, err := s.handleEvent(event.Data)
		if err != nil {
			return nil, err
		}
		if chunk != nil {
			return chunk, nil
		}
		// No chunk for this event (e.g. response.completed, an unrecognized
		// item/event type) -- continue reading the next SSE event, matching
		// TS's transform() returning without calling controller.enqueue().
	}
}

// handleEvent processes one SSE event's raw JSON data and returns the
// StreamChunk to emit (nil if the event produces no chunk). Additional
// chunks beyond the first are queued on s.pending.
func (s *hfStream) handleEvent(data string) (*provider.StreamChunk, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		// Mirrors TS's `if (!chunk.success) { ... enqueue({type:'error', error: chunk.error}); return; }`
		s.finishReason = types.FinishReasonError
		s.rawFinishReason = ""
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: err.Error(),
			Err:  providererrors.NewStreamProviderError(err.Error(), s.providerName, "", nil, nil, nil, nil),
		}, nil
	}

	if streamErr := createHFStreamError(raw, s.providerName); streamErr != nil {
		s.finishReason = types.FinishReasonError
		if streamErr.Code != nil {
			s.rawFinishReason = fmt.Sprintf("%v", streamErr.Code)
		} else {
			s.rawFinishReason = streamErr.Type
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: streamErr.Message,
			Err:  streamErr,
		}, nil
	}

	var event hfStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, nil //nolint:nilerr // malformed known-shape event: skip, matching TS's silent fallthrough
	}

	switch event.Type {
	case "response.created":
		if event.Response != nil {
			s.responseID = event.Response.ID
			return &provider.StreamChunk{
				Type: provider.ChunkTypeResponseMetadata,
				ResponseMetadata: &provider.ResponseMetadata{
					ID:        event.Response.ID,
					Timestamp: time.Unix(event.Response.CreatedAt, 0).UTC(),
					ModelID:   event.Response.Model,
					Headers:   s.responseHeaders,
				},
			}, nil
		}
		return nil, nil

	case "response.output_item.added":
		if event.Item == nil {
			return nil, nil
		}
		switch event.Item.Type {
		case "message":
			if event.Item.Role == "assistant" {
				return &provider.StreamChunk{
					Type:             provider.ChunkTypeTextStart,
					ID:               event.Item.ID,
					ProviderMetadata: hfItemMetadata(event.Item.ID),
				}, nil
			}
		case "function_call":
			return &provider.StreamChunk{
				Type:     provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{ID: event.Item.CallID, ToolName: event.Item.Name},
			}, nil
		case "reasoning":
			return &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               event.Item.ID,
				ProviderMetadata: hfItemMetadata(event.Item.ID),
			}, nil
		}
		return nil, nil

	case "response.output_item.done":
		if event.Item == nil {
			return nil, nil
		}
		switch event.Item.Type {
		case "message":
			if event.Item.Role == "assistant" {
				return &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: event.Item.ID}, nil
			}
		case "function_call":
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(event.Item.Arguments), &args) //nolint:errcheck

			extra := []*provider.StreamChunk{
				{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:           event.Item.CallID,
						ToolName:     event.Item.Name,
						Arguments:    args,
						RawArguments: event.Item.Arguments,
					},
				},
			}
			if event.Item.Output != "" {
				extra = append(extra, &provider.StreamChunk{
					Type: provider.ChunkTypeToolResult,
					ToolResult: &types.ToolResult{
						ToolCallID: event.Item.CallID,
						ToolName:   event.Item.Name,
						Result:     event.Item.Output,
					},
				})
			}
			s.pending = append(s.pending, extra...)
			return &provider.StreamChunk{
				Type:     provider.ChunkTypeToolInputEnd,
				ToolCall: &types.ToolCall{ID: event.Item.CallID},
			}, nil
		}
		return nil, nil

	case "response.completed":
		if event.Response != nil {
			s.responseID = event.Response.ID
			reason := "stop"
			if event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason != "" {
				reason = event.Response.IncompleteDetails.Reason
				s.rawFinishReason = reason
			} else {
				s.rawFinishReason = ""
			}
			s.finishReason = mapHuggingFaceResponsesFinishReason(reason)
			if event.Response.Usage != nil {
				s.usage = event.Response.Usage
			}
		}
		return nil, nil

	case "response.reasoning_text.delta":
		return &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: event.ItemID, Reasoning: event.Delta}, nil

	case "response.reasoning_text.done":
		return &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: event.ItemID}, nil

	case "response.output_text.delta":
		return &provider.StreamChunk{Type: provider.ChunkTypeText, ID: event.ItemID, Text: event.Delta}, nil

	default:
		return nil, nil
	}
}

// buildFinishChunk mirrors TS's flush(): always exactly one terminal
// "finish" chunk carrying the finishReason/usage/providerMetadata state
// accumulated across every event seen so far.
func (s *hfStream) buildFinishChunk() *provider.StreamChunk {
	var responseID interface{}
	if s.responseID != "" {
		responseID = s.responseID
	}
	meta, _ := json.Marshal(map[string]interface{}{
		hfProviderOptionsKey: map[string]interface{}{"responseId": responseID},
	})
	usage := convertHuggingFaceResponsesUsage(s.usage)
	return &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.finishReason,
		RawFinishReason:  s.rawFinishReason,
		Usage:            &usage,
		ProviderMetadata: meta,
	}
}

// createHFStreamError mirrors TS createHuggingFaceResponsesStreamError. It
// inspects the raw decoded event for a `type: "error"` or `type:
// "response.failed"` envelope carrying a `message` string, and if found
// returns a *providererrors.StreamProviderError with Data set to the whole
// raw event (matching TS's `data: event`). Returns nil when the event isn't
// a recognized stream error shape.
func createHFStreamError(raw map[string]interface{}, providerName string) *providererrors.StreamProviderError {
	outerType, _ := raw["type"].(string)
	if outerType != "error" && outerType != "response.failed" {
		return nil
	}

	var details map[string]interface{}
	switch outerType {
	case "response.failed":
		if resp, ok := raw["response"].(map[string]interface{}); ok {
			if errObj, ok := resp["error"].(map[string]interface{}); ok {
				details = errObj
			}
		}
	case "error":
		if errObj, ok := raw["error"].(map[string]interface{}); ok {
			details = errObj
		} else {
			details = raw
		}
	}

	if details == nil {
		return nil
	}
	message, ok := details["message"].(string)
	if !ok || message == "" {
		return nil
	}

	var code interface{}
	switch c := details["code"].(type) {
	case string:
		code = c
	case float64:
		code = c
	}

	return providererrors.NewStreamProviderError(message, providerName, outerType, code, nil, nil, raw)
}
