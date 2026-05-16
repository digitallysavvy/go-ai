package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

type interactionsStream struct {
	ctx             context.Context
	provider        *Provider
	reader          io.ReadCloser
	parser          *streaming.SSEParser
	interactionID   string
	headers         map[string]string
	responseHeaders map[string]string
	warnings        []types.Warning
	timeoutMs       int
	err             error
	buffer          []*provider.StreamChunk
	open            map[int]*interactionOpenBlock
	startEmitted    bool
	completed       bool
	finishStatus    string
	usage           *interactionsUsage
	serviceTier     string
	hasFunction     bool
	lastEventID     string
	resumable       bool
	finished        bool
}

type interactionOpenBlock struct {
	kind         string
	id           string
	blockType    string
	toolCallID   string
	toolName     string
	args         map[string]interface{}
	signature    string
	data         string
	mimeType     string
	uri          string
	result       interface{}
	callID       string
	isError      bool
	startEmitted bool
}

func newInteractionsStream(ctx context.Context, p *Provider, interactionID string, headers map[string]string, warnings []types.Warning, responseHeaders map[string]string, timeoutMs int) (provider.TextStream, error) {
	s := &interactionsStream{
		ctx:             ctx,
		provider:        p,
		interactionID:   interactionID,
		headers:         headers,
		responseHeaders: responseHeaders,
		warnings:        warnings,
		timeoutMs:       timeoutMs,
		open:            map[int]*interactionOpenBlock{},
		resumable:       true,
	}
	if err := s.openGETStream(); err != nil {
		return nil, err
	}
	return s, nil
}

func newInteractionsEventStream(ctx context.Context, p *Provider, reader io.ReadCloser, interactionID string, headers map[string]string, warnings []types.Warning, responseHeaders map[string]string, timeoutMs int) provider.TextStream {
	return &interactionsStream{
		ctx:             ctx,
		provider:        p,
		reader:          reader,
		parser:          streaming.NewSSEParser(reader),
		interactionID:   interactionID,
		headers:         headers,
		responseHeaders: responseHeaders,
		warnings:        warnings,
		timeoutMs:       timeoutMs,
		open:            map[int]*interactionOpenBlock{},
	}
}

func newSynthesizedInteractionsStream(response interactionsResponse, warnings []types.Warning, headers http.Header) provider.TextStream {
	content, _, hasFunctionCall := (&InteractionsLanguageModel{}).parseOutputs(response.Outputs, normalizedInteractionID(response.ID))
	chunks := make([]*provider.StreamChunk, 0, len(content)+2)
	if len(warnings) > 0 {
		chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: warnings})
	}
	for i, part := range content {
		id := fmt.Sprintf("%s:%d", firstNonEmpty(response.ID, "interaction"), i)
		switch p := part.(type) {
		case types.TextContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
			if p.Text != "" {
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: p.Text})
			}
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id, ProviderMetadata: p.ProviderMetadata})
		case types.ReasoningContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id})
			if p.Text != "" {
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: p.Text})
			}
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: id, ProviderMetadata: p.ProviderMetadata})
		case types.GeneratedFileContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &p, ProviderMetadata: p.ProviderMetadata})
		case types.SourceContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &p, ProviderMetadata: p.ProviderMetadata})
		case types.ToolResultContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: p.ToolCallID, ToolName: p.ToolName, Result: p.Result}})
		case types.ToolCallContent:
			chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments, RawArguments: p.Input, ProviderExecuted: p.ProviderExecuted, ProviderMetadata: providerMetaMap(p.ThoughtSignature, normalizedInteractionID(response.ID)), ThoughtSignature: p.ThoughtSignature}, ProviderMetadata: p.ProviderMetadata})
		}
	}
	chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: mapInteractionsFinishReason(response.Status, hasFunctionCall), Usage: usagePtr(convertInteractionsUsage(response.Usage)), ProviderMetadata: finishMetadata(response.ID, response.ServiceTier, response.Usage)})
	return &sliceTextStream{chunks: chunks}
}

func (s *interactionsStream) Next() (*provider.StreamChunk, error) {
	if len(s.buffer) > 0 {
		chunk := s.buffer[0]
		s.buffer = s.buffer[1:]
		return chunk, nil
	}
	if s.err != nil {
		return nil, s.err
	}
	if !s.startEmitted {
		s.startEmitted = true
		if len(s.warnings) > 0 {
			return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings}, nil
		}
	}
	for {
		event, err := s.parser.Next()
		if err == io.EOF {
			if s.resumable && !s.completed && s.interactionID != "" {
				if err := s.reopenAfterEOF(); err != nil {
					s.err = err
					return nil, err
				}
				continue
			}
			if !s.finished {
				s.closeAll()
				s.appendFinish()
				if len(s.buffer) > 0 {
					return s.Next()
				}
			}
			if s.completed || !s.resumable || s.interactionID == "" {
				s.err = io.EOF
				return nil, io.EOF
			}
		}
		if err != nil {
			s.err = err
			return nil, err
		}
		if event.ID != "" {
			s.lastEventID = event.ID
		}
		if streaming.IsStreamDone(event) {
			continue
		}
		var payload interactionsEvent
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			s.err = fmt.Errorf("failed to parse interactions stream event: %w", err)
			return nil, s.err
		}
		s.processEvent(payload)
		if len(s.buffer) > 0 {
			return s.Next()
		}
	}
}

func (s *interactionsStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *interactionsStream) Close() error {
	if s.resumable && !s.completed && s.interactionID != "" {
		_ = (&InteractionsLanguageModel{provider: s.provider}).cancelInteraction(context.Background(), s.interactionID, s.headers)
	}
	if s.reader == nil {
		return nil
	}
	return s.reader.Close()
}

func (s *interactionsStream) openGETStream() error {
	query := map[string]string{"stream": "true"}
	if s.lastEventID != "" {
		query["last_event_id"] = s.lastEventID
	}
	resp, err := s.provider.client.DoStream(s.ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/interactions/" + url.PathEscape(s.interactionID),
		Query:   query,
		Headers: internalhttp.MergeHeaders(s.headers, map[string]string{"Accept": "text/event-stream"}),
	})
	if err != nil {
		return err
	}
	s.reader = resp.Body
	s.parser = streaming.NewSSEParser(resp.Body)
	return nil
}

func (s *interactionsStream) reopenAfterEOF() error {
	if s.reader != nil {
		_ = s.reader.Close()
	}
	select {
	case <-s.ctx.Done():
		_ = (&InteractionsLanguageModel{provider: s.provider}).cancelInteraction(context.Background(), s.interactionID, s.headers)
		return s.ctx.Err()
	default:
		return s.openGETStream()
	}
}

func (s *interactionsStream) processEvent(event interactionsEvent) {
	switch event.EventType {
	case "interaction.start":
		if event.Interaction != nil {
			if event.Interaction.ID != "" {
				s.interactionID = event.Interaction.ID
			}
			if event.Interaction.ServiceTier != "" {
				s.serviceTier = event.Interaction.ServiceTier
			}
			s.buffer = append(s.buffer, &provider.StreamChunk{
				Type: provider.ChunkTypeResponseMetadata,
				ResponseMetadata: &provider.ResponseMetadata{
					ID:      normalizedInteractionID(event.Interaction.ID),
					ModelID: event.Interaction.Model,
					Headers: s.responseHeaders,
				},
			})
		}
	case "content.start":
		if event.Index == nil || event.Content == nil {
			return
		}
		idx := *event.Index
		block := event.Content
		id := fmt.Sprintf("%s:%d", firstNonEmpty(s.interactionID, "interaction"), idx)
		open := &interactionOpenBlock{kind: block.Type, id: id, blockType: block.Type, toolCallID: firstNonEmpty(block.ID, id), toolName: block.Name, args: block.Arguments, signature: block.Signature, data: block.Data, mimeType: block.MimeType, uri: block.URI, callID: firstNonEmpty(block.CallID, id), result: block.Result, isError: block.IsError != nil && *block.IsError}
		s.open[idx] = open
		switch block.Type {
		case "text":
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
			for _, source := range annotationsToSources(block.Annotations) {
				source := source.(types.SourceContent)
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
			}
		case "thought":
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id})
		case "function_call":
			s.hasFunction = true
			if open.toolName != "" {
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}})
				open.startEmitted = true
			}
		}
	case "content.delta":
		if event.Index == nil || event.Delta == nil {
			return
		}
		open := s.open[*event.Index]
		if open == nil {
			return
		}
		delta := event.Delta
		switch {
		case open.kind == "text" && delta.Type == "text":
			if delta.Text != "" {
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: open.id, Text: delta.Text})
			}
		case open.kind == "text" && delta.Type == "text_annotation":
			for _, sourcePart := range annotationsToSources(delta.Annotations) {
				source := sourcePart.(types.SourceContent)
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
			}
		case open.kind == "thought":
			if delta.Type == "thought_summary" {
				if delta.Content != nil && delta.Content.Type == "text" && delta.Content.Text != "" {
					s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: open.id, Reasoning: delta.Content.Text})
				}
				if delta.Text != "" {
					s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: open.id, Reasoning: delta.Text})
				}
			}
			if delta.Type == "thought_signature" && delta.Signature != "" {
				open.signature = delta.Signature
			}
		case open.kind == "image" && delta.Type == "image":
			open.data = firstNonEmpty(delta.Data, open.data)
			open.mimeType = firstNonEmpty(delta.MimeType, open.mimeType)
			open.uri = firstNonEmpty(delta.URI, open.uri)
		case open.kind == "function_call" && delta.Type == "function_call":
			s.hasFunction = true
			open.toolCallID = firstNonEmpty(delta.ID, open.toolCallID)
			open.toolName = firstNonEmpty(delta.Name, open.toolName)
			if delta.Arguments != nil {
				open.args = delta.Arguments
			}
			open.signature = firstNonEmpty(delta.Signature, open.signature)
			if !open.startEmitted && open.toolName != "" {
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}})
				open.startEmitted = true
			}
		default:
			if delta.Type == open.blockType {
				open.toolCallID = firstNonEmpty(delta.ID, open.toolCallID)
				open.toolName = firstNonEmpty(delta.Name, open.toolName)
				open.callID = firstNonEmpty(delta.CallID, open.callID)
				if delta.Arguments != nil {
					open.args = delta.Arguments
				}
				if delta.Result != nil {
					open.result = delta.Result
				}
				if delta.IsError != nil {
					open.isError = *delta.IsError
				}
			}
		}
	case "content.stop":
		if event.Index == nil {
			return
		}
		open := s.open[*event.Index]
		if open == nil {
			return
		}
		s.stopBlock(open)
		delete(s.open, *event.Index)
	case "interaction.status_update":
		s.finishStatus = event.Status
	case "interaction.complete":
		if event.Interaction != nil {
			s.completed = true
			s.finishStatus = event.Interaction.Status
			s.usage = event.Interaction.Usage
			if event.Interaction.ServiceTier != "" {
				s.serviceTier = event.Interaction.ServiceTier
			}
			if event.Interaction.ID != "" {
				s.interactionID = event.Interaction.ID
			}
		}
		s.closeAll()
		s.appendFinish()
	case "error":
		s.finishStatus = InteractionStatusFailed
		message := "google.interactions stream error"
		if event.Error != nil && event.Error.Message != "" {
			message = event.Error.Message
		}
		s.closeAll()
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: message, AbortReason: message})
		s.appendFinish()
	}
}

func (s *interactionsStream) stopBlock(open *interactionOpenBlock) {
	switch open.kind {
	case "text":
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: open.id, ProviderMetadata: providerMetaRaw("", s.interactionID)})
	case "thought":
		s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: open.id, ProviderMetadata: providerMetaRaw(open.signature, s.interactionID)})
	case "image":
		if open.data != "" {
			data, _ := base64.StdEncoding.DecodeString(open.data)
			meta := providerMetaRaw("", s.interactionID)
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: firstNonEmpty(open.mimeType, "image/png"), Data: data, ProviderMetadata: meta}, ProviderMetadata: meta})
		} else if open.uri != "" {
			meta := providerMetaRaw("", s.interactionID)
			fileData := types.FileData{Type: types.FileDataTypeURL, URL: open.uri, MediaType: firstNonEmpty(open.mimeType, "image/png")}
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: fileData.MediaType, FileData: fileData, URL: open.uri, ProviderMetadata: meta}, ProviderMetadata: meta})
		}
	case "function_call":
		argsJSON, _ := json.Marshal(defaultMap(open.args))
		if open.toolName == "" {
			open.toolName = "unknown"
		}
		if !open.startEmitted {
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}})
		}
		s.buffer = append(s.buffer,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName}, Text: string(argsJSON)},
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: open.toolCallID}},
			&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: open.toolName, Arguments: defaultMap(open.args), RawArguments: string(argsJSON), ThoughtSignature: open.signature, ProviderMetadata: providerMetaMap(open.signature, s.interactionID)}, ProviderMetadata: providerMetaRaw(open.signature, s.interactionID)},
		)
	default:
		if isBuiltinInteractionsToolCall(open.blockType) {
			argsJSON, _ := json.Marshal(defaultMap(open.args))
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: open.toolCallID, ToolName: firstNonEmpty(open.toolName, stringsTrimCall(open.blockType)), Arguments: defaultMap(open.args), RawArguments: string(argsJSON), ProviderExecuted: true}})
		} else if isBuiltinInteractionsToolResult(open.blockType) {
			block := interactionsContentBlock{Type: open.blockType, CallID: open.callID, Name: open.toolName, Result: open.result}
			s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: open.callID, ToolName: firstNonEmpty(open.toolName, stringsTrimResult(open.blockType)), Result: open.result, ProviderExecuted: true}})
			for _, sourcePart := range builtinToolResultSources(block) {
				source := sourcePart.(types.SourceContent)
				s.buffer = append(s.buffer, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
			}
		}
	}
}

func (s *interactionsStream) closeAll() {
	for idx, open := range s.open {
		s.stopBlock(open)
		delete(s.open, idx)
	}
}

func (s *interactionsStream) appendFinish() {
	if s.finished {
		return
	}
	s.finished = true
	s.buffer = append(s.buffer, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     mapInteractionsFinishReason(s.finishStatus, s.hasFunction),
		Usage:            usagePtr(convertInteractionsUsage(s.usage)),
		ProviderMetadata: finishMetadata(s.interactionID, s.serviceTier, s.usage),
	})
}

func usagePtr(u types.Usage) *types.Usage {
	return &u
}

func finishMetadata(interactionID, serviceTier string, usage *interactionsUsage) json.RawMessage {
	google := pruneMap(map[string]interface{}{
		"interactionId": emptyToNil(interactionID),
		"serviceTier":   emptyToNil(serviceTier),
	})
	if google == nil {
		google = map[string]interface{}{}
	}
	raw, _ := json.Marshal(map[string]interface{}{"google": google})
	return raw
}

func defaultMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

func stringsTrimCall(v string) string {
	return stringsTrimSuffix(v, "_call")
}

func stringsTrimResult(v string) string {
	return stringsTrimSuffix(v, "_result")
}

func stringsTrimSuffix(v, suffix string) string {
	if len(v) >= len(suffix) && v[len(v)-len(suffix):] == suffix {
		return v[:len(v)-len(suffix)]
	}
	return v
}

type sliceTextStream struct {
	chunks []*provider.StreamChunk
	err    error
}

func (s *sliceTextStream) Next() (*provider.StreamChunk, error) {
	if len(s.chunks) == 0 {
		s.err = io.EOF
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *sliceTextStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *sliceTextStream) Close() error { return nil }
