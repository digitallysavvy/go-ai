package ai

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// UIMessageChunk is a lightweight JSON-compatible chunk shape.
type UIMessageChunk map[string]interface{}

// UIMessageStreamOnStepFinishCallback is invoked when a logical step finishes.
type UIMessageStreamOnStepFinishCallback func(ctx map[string]interface{})

// UIMessageStreamOnStepEndCallback is the canonical name for UIMessageStreamOnStepFinishCallback.
type UIMessageStreamOnStepEndCallback = UIMessageStreamOnStepFinishCallback

// UIMessageStreamOnEndCallback is invoked when the UI stream ends.
type UIMessageStreamOnEndCallback func(ctx map[string]interface{})

// UIMessageStreamOnFinishCallback is invoked when the UI stream ends.
//
// Deprecated: use UIMessageStreamOnEndCallback.
type UIMessageStreamOnFinishCallback = UIMessageStreamOnEndCallback

// UIMessageStreamWriter is used by CreateUIMessageStreamWithOptions to write chunks.
type UIMessageStreamWriter struct {
	writeFn func(UIMessageChunk)
	mergeFn func(<-chan UIMessageChunk)
	onError func(error) string
}

// Write appends a data stream part.
func (w UIMessageStreamWriter) Write(part UIMessageChunk) {
	if w.writeFn != nil {
		w.writeFn(part)
	}
}

// Merge forwards all chunks from another stream into this stream.
func (w UIMessageStreamWriter) Merge(stream <-chan UIMessageChunk) {
	if w.mergeFn != nil {
		w.mergeFn(stream)
	}
}

// UIMessageStreamOptions configures custom UI-message stream creation.
type UIMessageStreamOptions struct {
	Execute          func(writer UIMessageStreamWriter)
	OnError          func(error) string
	OriginalMessages []UIMessageChunk
	OnStepEnd        UIMessageStreamOnStepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish UIMessageStreamOnStepFinishCallback
	OnEnd        UIMessageStreamOnEndCallback
	// Deprecated: use OnEnd.
	OnFinish          UIMessageStreamOnFinishCallback
	GenerateMessageID IDGenerator
}

// UIMessageStreamResultOptions controls result-to-UI projection settings.
type UIMessageStreamResultOptions struct {
	OriginalMessages  []UIMessageChunk
	GenerateMessageID IDGenerator
	ResponseMessageID string
	Tools             []types.Tool
	MessageMetadata   func(part map[string]interface{}) map[string]interface{}
	SendReasoning     *bool
	SendSources       *bool
	SendStart         *bool
	SendFinish        *bool
	OnStepEnd         UIMessageStreamOnStepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish UIMessageStreamOnStepFinishCallback
	OnEnd        UIMessageStreamOnEndCallback
	// Deprecated: use OnEnd.
	OnFinish UIMessageStreamOnFinishCallback
	OnError  func(error) string
}

// UIMessageStreamResponseInit mirrors the TypeScript response init shape used by
// createUIMessageStreamResponse.
type UIMessageStreamResponseInit struct {
	Status           int
	StatusText       string
	Headers          map[string]string
	ConsumeSSEStream func(io.Reader) error
}

func getDefaultMessageErrorHandler(onError func(error) string) func(error) string {
	if onError == nil {
		return func(err error) string {
			return "An error occurred."
		}
	}
	return onError
}

func boolOption(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func appendErrorChunk(onError func(error) string, write func(UIMessageChunk), err error) {
	if err == nil {
		return
	}
	write(UIMessageChunk{
		"type":      "error",
		"errorText": onError(err),
	})
}

func resolveUIMessageStreamOnStepEnd(onStepEnd, onStepFinish UIMessageStreamOnStepFinishCallback) UIMessageStreamOnStepFinishCallback {
	if onStepEnd != nil {
		return onStepEnd
	}
	return onStepFinish
}

func resolveUIMessageStreamOnEnd(onEnd, onFinish UIMessageStreamOnEndCallback) UIMessageStreamOnEndCallback {
	if onEnd != nil {
		return onEnd
	}
	return onFinish
}

// CreateUIMessageStreamWithOptions creates a UI message stream with writer callbacks.
func CreateUIMessageStreamWithOptions(ctx context.Context, options UIMessageStreamOptions) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)

		if options.Execute == nil {
			errCh <- fmt.Errorf("execute is required")
			return
		}

		onError := getDefaultMessageErrorHandler(options.OnError)

		var mu sync.Mutex
		var closed bool
		safeEnqueue := func(part UIMessageChunk) {
			mu.Lock()
			isClosed := closed
			mu.Unlock()
			if isClosed {
				return
			}
			defer func() {
				_ = recover()
			}()
			select {
			case out <- part:
			case <-ctx.Done():
			}
		}
		enqueueError := func(err error) {
			appendErrorChunk(onError, safeEnqueue, err)
		}

		generateID := options.GenerateMessageID
		if generateID == nil {
			generateID = newCallID
		}
		uiState := newUIMessageCallbackState(options.OriginalMessages, generateID())

		onEnd := resolveUIMessageStreamOnEnd(options.OnEnd, options.OnFinish)
		callOnEnd := func() {
			if onEnd == nil {
				return
			}
			finishEvent := map[string]interface{}{
				"isContinuation":  uiState.isContinuation,
				"isAborted":       ctx.Err() != nil || uiState.isAborted,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
				"finishReason":    uiState.finishReason,
			}
			defer func() {
				_ = recover()
			}()
			onEnd(finishEvent)
		}

		onStepEnd := resolveUIMessageStreamOnStepEnd(options.OnStepEnd, options.OnStepFinish)
		callOnStepFinish := func() {
			if onStepEnd == nil {
				return
			}
			stepEvent := map[string]interface{}{
				"isContinuation":  uiState.isContinuation,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
			}
			defer func() {
				_ = recover()
			}()
			onStepEnd(stepEvent)
		}
		processAndEnqueue := func(part UIMessageChunk) {
			uiState.apply(part, onError)
			if part["type"] == "finish-step" {
				callOnStepFinish()
			}
			safeEnqueue(part)
		}

		var wg sync.WaitGroup
		merge := func(stream <-chan UIMessageChunk) {
			if stream == nil {
				enqueueError(errors.New("merge stream is nil"))
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						if err, ok := r.(error); ok {
							enqueueError(err)
						} else {
							enqueueError(fmt.Errorf("ui message stream panicked: %v", r))
						}
					}
				}()
				for {
					select {
					case <-ctx.Done():
						return
					case chunk, ok := <-stream:
						if !ok {
							return
						}
						processAndEnqueue(chunk)
					}
				}
			}()
		}

		writer := UIMessageStreamWriter{
			writeFn: processAndEnqueue,
			mergeFn: merge,
			onError: onError,
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					var err error
					if e, ok := r.(error); ok {
						err = e
					} else {
						err = fmt.Errorf("ui message stream panicked: %v", r)
					}
					enqueueError(err)
				}
			}()
			options.Execute(writer)
		}()

		wg.Wait()

		mu.Lock()
		closed = true
		mu.Unlock()

		callOnEnd()
	}()

	return out, errCh
}

// CreateUIMessageStream converts a StreamTextResult into a channel of UI chunks.
func CreateUIMessageStream(ctx context.Context, result *StreamTextResult, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	if result == nil {
		out := make(chan UIMessageChunk)
		errCh := make(chan error, 1)
		close(out)
		errCh <- fmt.Errorf("result is required")
		close(errCh)
		return out, errCh
	}
	return ToUIMessageStream(ctx, result.Stream(), opts...)
}

// ToUIMessageStream converts a provider text stream into UI message chunks.
func ToUIMessageStream(ctx context.Context, stream provider.TextStream, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		if stream == nil {
			errCh <- fmt.Errorf("stream is required")
			return
		}

		options := UIMessageStreamResultOptions{}
		if len(opts) > 0 {
			options = opts[0]
		}

		onError := getDefaultMessageErrorHandler(options.OnError)
		sendStart := boolOption(options.SendStart, true)
		sendFinish := boolOption(options.SendFinish, true)
		sendReasoning := boolOption(options.SendReasoning, true)

		streamMessageID := responseUIMessageID(options)
		callbackMessageID := streamMessageID
		if callbackMessageID == "" && options.GenerateMessageID != nil {
			callbackMessageID = options.GenerateMessageID()
		}

		uiState := newUIMessageCallbackState(options.OriginalMessages, callbackMessageID)

		onEnd := resolveUIMessageStreamOnEnd(options.OnEnd, options.OnFinish)
		callOnEnd := func(finishReason types.FinishReason) {
			if onEnd == nil {
				return
			}
			finishEvent := map[string]interface{}{
				"isContinuation":  uiState.isContinuation,
				"isAborted":       ctx.Err() != nil || uiState.isAborted,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
				"finishReason":    finishReason,
			}
			defer func() {
				_ = recover()
			}()
			onEnd(finishEvent)
		}

		onStepEnd := resolveUIMessageStreamOnStepEnd(options.OnStepEnd, options.OnStepFinish)
		callOnStepFinish := func() {
			if onStepEnd == nil {
				return
			}
			stepEvent := map[string]interface{}{
				"isContinuation":  uiState.isContinuation,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
			}
			defer func() {
				_ = recover()
			}()
			onStepEnd(stepEvent)
		}

		safeEnqueue := func(part UIMessageChunk) {
			select {
			case <-ctx.Done():
				return
			case out <- part:
			}
		}

		processChunk := func(chunk UIMessageChunk) {
			uiState.apply(chunk, onError)
			if chunk["type"] == "finish-step" {
				callOnStepFinish()
			}
			safeEnqueue(chunk)
		}
		messageMetadataInput := func(part provider.StreamChunk, overrideType string) map[string]interface{} {
			partType := string(part.Type)
			if overrideType != "" {
				partType = overrideType
			} else {
				switch part.Type {
				case provider.ChunkTypeStreamStart:
					partType = "start-step"
				case provider.ChunkTypeFinish, provider.ChunkTypeStreamFinish:
					partType = "finish-step"
				case provider.ChunkTypeText:
					partType = "text-delta"
				case provider.ChunkTypeReasoning:
					partType = "reasoning-delta"
				}
			}
			metadataPart := part
			metadataPart.Type = provider.ChunkType(partType)
			return map[string]interface{}{
				"type": partType,
				"part": metadataPart,
			}
		}
		processMessageMetadata := func(part provider.StreamChunk) {
			if options.MessageMetadata == nil {
				return
			}
			input := messageMetadataInput(part, "")
			partType, _ := input["type"].(string)
			metadata := options.MessageMetadata(input)
			if metadata != nil && partType != "start" && partType != "finish" {
				processChunk(UIMessageChunk{
					"type":            "message-metadata",
					"messageMetadata": metadata,
				})
			}
		}

		var startMetadata map[string]interface{}
		if options.MessageMetadata != nil {
			startMetadata = options.MessageMetadata(messageMetadataInput(provider.StreamChunk{Type: provider.ChunkType("start")}, "start"))
		}
		if sendStart {
			startEvent := map[string]interface{}{
				"type": "start",
			}
			startMessageID := streamMessageID
			if startMessageID == "" && options.GenerateMessageID != nil {
				startMessageID = callbackMessageID
			}
			if startMessageID != "" {
				startEvent["messageId"] = startMessageID
			}
			if startMetadata != nil {
				startEvent["messageMetadata"] = startMetadata
			}
			processChunk(startEvent)
		}

		finishReason := types.FinishReason("")
		sawTerminal := false
		sawFinishChunk := false
		sawOutput := false
		activeTextIDs := map[string]bool{}
		activeReasoningIDs := map[string]bool{}
		textID := ""
		reasoningID := ""
		textSeq := 0
		reasoningSeq := 0
		ensureTextStart := func(id string) string {
			if id == "" {
				if textID == "" {
					textSeq++
					textID = fmt.Sprintf("text-%d", textSeq)
				}
				id = textID
			}
			if !activeTextIDs[id] {
				activeTextIDs[id] = true
				processChunk(UIMessageChunk{"type": "text-start", "id": id})
			}
			return id
		}
		ensureReasoningStart := func(id string) string {
			if id == "" {
				if reasoningID == "" {
					reasoningSeq++
					reasoningID = fmt.Sprintf("reasoning-%d", reasoningSeq)
				}
				id = reasoningID
			}
			if !activeReasoningIDs[id] {
				activeReasoningIDs[id] = true
				processChunk(UIMessageChunk{"type": "reasoning-start", "id": id})
			}
			return id
		}
		closeOpenParts := func() {
			for id := range activeTextIDs {
				processChunk(UIMessageChunk{"type": "text-end", "id": id})
				delete(activeTextIDs, id)
			}
			if sendReasoning {
				for id := range activeReasoningIDs {
					processChunk(UIMessageChunk{"type": "reasoning-end", "id": id})
					delete(activeReasoningIDs, id)
				}
			} else {
				activeReasoningIDs = map[string]bool{}
			}
		}
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}
			chunk, err := stream.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				if isAbortErr(ctx, err) {
					abortPart := provider.StreamChunk{Type: provider.ChunkTypeAbort}
					abortChunk := UIMessageChunk{"type": "abort"}
					if err.Error() != "" {
						abortPart.AbortReason = err.Error()
						abortChunk["reason"] = abortPart.AbortReason
					}
					processChunk(abortChunk)
					processMessageMetadata(abortPart)
					callOnEnd(finishReason)
					return
				}
				errCh <- err
				appendErrorChunk(onError, safeEnqueue, err)
				callOnEnd(finishReason)
				return
			}

			if chunk.Type == provider.ChunkTypeFinish {
				sawTerminal = true
				sawFinishChunk = true
				finishReason = chunk.FinishReason
			}
			if chunk.Type == provider.ChunkTypeError {
				sawTerminal = true
				if finishReason == "" {
					finishReason = types.FinishReasonError
				}
			}
			if isModelOutputChunkType(chunk.Type) {
				sawOutput = true
			}
			if chunk.Type == provider.ChunkTypeText && chunk.Text == "" {
				continue
			}
			chunkForConversion := *chunk
			switch chunk.Type {
			case provider.ChunkTypeTextStart:
				id := chunk.ID
				if id == "" {
					textSeq++
					id = fmt.Sprintf("text-%d", textSeq)
					textID = id
				}
				activeTextIDs[id] = true
				chunkForConversion.ID = id
			case provider.ChunkTypeText:
				chunkForConversion.ID = ensureTextStart(chunk.ID)
			case provider.ChunkTypeTextEnd:
				id := chunk.ID
				if id == "" {
					id = textID
				}
				chunkForConversion.ID = id
				delete(activeTextIDs, id)
				if id == textID {
					textID = ""
				}
			case provider.ChunkTypeReasoningStart:
				if !sendReasoning {
					break
				}
				id := chunk.ID
				if id == "" {
					reasoningSeq++
					id = fmt.Sprintf("reasoning-%d", reasoningSeq)
					reasoningID = id
				}
				activeReasoningIDs[id] = true
				chunkForConversion.ID = id
			case provider.ChunkTypeReasoning:
				if sendReasoning {
					chunkForConversion.ID = ensureReasoningStart(chunk.ID)
				}
			case provider.ChunkTypeReasoningEnd:
				if !sendReasoning {
					break
				}
				id := chunk.ID
				if id == "" {
					id = reasoningID
				}
				chunkForConversion.ID = id
				delete(activeReasoningIDs, id)
				if id == reasoningID {
					reasoningID = ""
				}
			case provider.ChunkTypeFinish:
				closeOpenParts()
			}
			converted := toUIMessageChunks(chunkForConversion, options)
			for _, uiChunk := range converted {
				processChunk(uiChunk)
			}
			processMessageMetadata(*chunk)
		}

		if !sawTerminal {
			if !sawOutput {
				err := newIncompleteModelStreamError()
				appendErrorChunk(onError, processChunk, err)
				processMessageMetadata(provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: err.Error(),
				})
				callOnEnd(finishReason)
				return
			}
			finishReason = types.FinishReasonOther
		}
		closeOpenParts()
		if !sawFinishChunk && finishReason != "" {
			processChunk(UIMessageChunk{"type": "finish-step"})
			processMessageMetadata(provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: finishReason,
			})
		}
		var finishMetadata map[string]interface{}
		if options.MessageMetadata != nil {
			finishMetadata = options.MessageMetadata(messageMetadataInput(provider.StreamChunk{
				Type:         provider.ChunkType("finish"),
				FinishReason: finishReason,
			}, "finish"))
		}
		if sendFinish {
			finishEvent := map[string]interface{}{
				"type":         "finish",
				"finishReason": string(finishReason),
			}
			if finishMetadata != nil {
				finishEvent["messageMetadata"] = finishMetadata
			}
			processChunk(finishEvent)
		}
		callOnEnd(finishReason)
	}()
	return out, errCh
}

// ToUIMessageChunk converts a single provider stream chunk into a UI message
// chunk. The boolean return is false for stream parts that do not produce UI
// message chunks, matching TypeScript's undefined result.
func ToUIMessageChunk(part provider.StreamChunk, opts UIMessageStreamResultOptions) (UIMessageChunk, bool) {
	chunks := toUIMessageChunks(part, opts)
	if len(chunks) == 0 {
		return nil, false
	}
	return chunks[0], true
}

func toUIMessageChunks(part provider.StreamChunk, opts UIMessageStreamResultOptions) []UIMessageChunk {
	onError := getDefaultMessageErrorHandler(opts.OnError)
	sendReasoning := boolOption(opts.SendReasoning, true)
	sendSources := boolOption(opts.SendSources, false)
	switch part.Type {
	case provider.ChunkTypeStreamStart:
		return []UIMessageChunk{{"type": "start-step"}}
	case provider.ChunkTypeFinish:
		return []UIMessageChunk{{"type": "finish-step"}}
	case provider.ChunkTypeAbort:
		chunk := UIMessageChunk{"type": "abort"}
		if part.AbortReason != "" {
			chunk["reason"] = part.AbortReason
		}
		return []UIMessageChunk{chunk}
	}

	chunks := convertProviderChunkToUIMessageChunks(part, resultChunkConversionOptions{
		SendReasoning: sendReasoning,
		SendSources:   sendSources,
		OnError:       onError,
		Tools:         opts.Tools,
	})
	return chunks
}

func responseUIMessageID(options UIMessageStreamResultOptions) string {
	if options.ResponseMessageID != "" {
		return options.ResponseMessageID
	}
	if options.OriginalMessages == nil {
		return ""
	}
	if len(options.OriginalMessages) > 0 {
		last := options.OriginalMessages[len(options.OriginalMessages)-1]
		if role, _ := last["role"].(string); role == "assistant" {
			id, _ := last["id"].(string)
			return id
		}
	}
	if options.GenerateMessageID != nil {
		return options.GenerateMessageID()
	}
	return ""
}

type uiMessageCallbackState struct {
	originalMessages []UIMessageChunk
	message          UIMessageChunk
	isContinuation   bool
	isAborted        bool
	finishReason     interface{}
	activeText       map[string]UIMessageChunk
	activeReasoning  map[string]UIMessageChunk
	partialTools     map[string]*uiPartialToolCall
}

type uiPartialToolCall struct {
	text         string
	toolName     string
	dynamic      bool
	title        interface{}
	toolMetadata interface{}
}

func newUIMessageCallbackState(original []UIMessageChunk, messageID string) *uiMessageCallbackState {
	state := &uiMessageCallbackState{
		originalMessages: append([]UIMessageChunk(nil), original...),
		message: UIMessageChunk{
			"id":    messageID,
			"role":  "assistant",
			"parts": []interface{}{},
		},
		activeText:      map[string]UIMessageChunk{},
		activeReasoning: map[string]UIMessageChunk{},
		partialTools:    map[string]*uiPartialToolCall{},
	}
	if len(original) > 0 {
		last := original[len(original)-1]
		if role, _ := last["role"].(string); role == "assistant" {
			state.isContinuation = true
			state.message = cloneUIMessageChunk(last)
			if _, ok := state.message["parts"]; !ok {
				state.message["parts"] = []interface{}{}
			}
		}
	}
	return state
}

func (s *uiMessageCallbackState) responseMessage() UIMessageChunk {
	return cloneUIMessageChunk(s.message)
}

func (s *uiMessageCallbackState) messages() []UIMessageChunk {
	messages := append([]UIMessageChunk(nil), s.originalMessages...)
	if s.isContinuation && len(messages) > 0 {
		messages = messages[:len(messages)-1]
	}
	messages = append(messages, s.responseMessage())
	return messages
}

func (s *uiMessageCallbackState) apply(chunk UIMessageChunk, onError func(error) string) {
	chunkType, _ := chunk["type"].(string)
	switch chunkType {
	case "start":
		if id, _ := chunk["messageId"].(string); id != "" {
			s.message["id"] = id
		}
		s.mergeMetadata(chunk["messageMetadata"])
	case "finish":
		if reason, ok := chunk["finishReason"]; ok {
			s.finishReason = reason
		}
		s.mergeMetadata(chunk["messageMetadata"])
	case "message-metadata":
		s.mergeMetadata(chunk["messageMetadata"])
	case "abort":
		s.isAborted = true
	case "text-start":
		id, _ := chunk["id"].(string)
		part := UIMessageChunk{"type": "text", "text": "", "state": "streaming"}
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.activeText[id] = part
		s.appendPart(part)
	case "text-delta":
		id, _ := chunk["id"].(string)
		part := s.activeText[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "text-delta", id)
			return
		}
		part["text"] = stringValue(part["text"]) + stringValue(chunk["delta"])
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
	case "text-end":
		id, _ := chunk["id"].(string)
		part := s.activeText[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "text-end", id)
			return
		}
		part["state"] = "done"
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		delete(s.activeText, id)
	case "reasoning-start":
		id, _ := chunk["id"].(string)
		part := UIMessageChunk{"type": "reasoning", "text": "", "state": "streaming"}
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.activeReasoning[id] = part
		s.appendPart(part)
	case "reasoning-delta":
		id, _ := chunk["id"].(string)
		part := s.activeReasoning[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "reasoning-delta", id)
			return
		}
		part["text"] = stringValue(part["text"]) + stringValue(chunk["delta"])
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
	case "reasoning-end":
		id, _ := chunk["id"].(string)
		part := s.activeReasoning[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "reasoning-end", id)
			return
		}
		part["state"] = "done"
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		delete(s.activeReasoning, id)
	case "file", "reasoning-file":
		part := UIMessageChunk{"type": chunkType}
		copyIfPresent(part, chunk, "mediaType", "mediaType")
		copyIfPresent(part, chunk, "url", "url")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "source-url":
		part := UIMessageChunk{"type": "source-url"}
		copyIfPresent(part, chunk, "sourceId", "sourceId")
		copyIfPresent(part, chunk, "url", "url")
		copyIfPresent(part, chunk, "title", "title")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "source-document":
		part := UIMessageChunk{"type": "source-document"}
		copyIfPresent(part, chunk, "sourceId", "sourceId")
		copyIfPresent(part, chunk, "mediaType", "mediaType")
		copyIfPresent(part, chunk, "title", "title")
		copyIfPresent(part, chunk, "filename", "filename")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "custom":
		part := UIMessageChunk{"type": "custom"}
		copyIfPresent(part, chunk, "kind", "kind")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "start-step":
		s.appendPart(UIMessageChunk{"type": "step-start"})
	case "finish-step":
		s.activeText = map[string]UIMessageChunk{}
		s.activeReasoning = map[string]UIMessageChunk{}
	case "tool-input-start":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		s.partialTools[toolCallID] = &uiPartialToolCall{
			toolName:     toolName,
			dynamic:      dynamic,
			title:        chunk["title"],
			toolMetadata: chunk["toolMetadata"],
		}
		s.updateToolPart(toolCallID, toolName, dynamic, UIMessageChunk{
			"state": "input-streaming",
			"input": nil,
		}, chunk)
	case "tool-input-delta":
		toolCallID := stringValue(chunk["toolCallId"])
		partial := s.partialTools[toolCallID]
		if partial == nil {
			reportMissingToolInput(onError, "tool-input-delta", toolCallID)
			return
		}
		partial.text += stringValue(chunk["inputTextDelta"])
		s.updateToolPart(toolCallID, partial.toolName, partial.dynamic, UIMessageChunk{
			"state": "input-streaming",
			"input": parseToolInputPartial(partial.text),
		}, UIMessageChunk{"title": partial.title, "toolMetadata": partial.toolMetadata})
	case "tool-input-available":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		s.updateToolPart(toolCallID, toolName, dynamic, UIMessageChunk{
			"state": "input-available",
			"input": chunk["input"],
		}, chunk)
	case "tool-input-error":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		update := UIMessageChunk{"state": "output-error", "errorText": chunk["errorText"]}
		if dynamic {
			update["input"] = chunk["input"]
		} else {
			update["input"] = nil
			update["rawInput"] = chunk["input"]
		}
		s.updateToolPart(toolCallID, toolName, dynamic, update, chunk)
	case "tool-output-available":
		toolCallID := stringValue(chunk["toolCallId"])
		part := s.findToolPart(toolCallID)
		if part == nil {
			reportMissingToolInvocation(onError, toolCallID)
			return
		}
		toolName, dynamic := toolInfoFromPart(part)
		s.updateToolPart(toolCallID, toolName, dynamic, UIMessageChunk{
			"state":       "output-available",
			"input":       part["input"],
			"output":      chunk["output"],
			"preliminary": chunk["preliminary"],
		}, chunk)
	case "tool-output-error":
		toolCallID := stringValue(chunk["toolCallId"])
		part := s.findToolPart(toolCallID)
		if part == nil {
			reportMissingToolInvocation(onError, toolCallID)
			return
		}
		toolName, dynamic := toolInfoFromPart(part)
		s.updateToolPart(toolCallID, toolName, dynamic, UIMessageChunk{
			"state":     "output-error",
			"input":     part["input"],
			"rawInput":  part["rawInput"],
			"errorText": chunk["errorText"],
		}, chunk)
	case "tool-output-denied":
		toolCallID := stringValue(chunk["toolCallId"])
		if part := s.findToolPart(toolCallID); part != nil {
			part["state"] = "output-denied"
		} else {
			reportMissingToolInvocation(onError, toolCallID)
		}
	case "tool-approval-request":
		toolCallID := stringValue(chunk["toolCallId"])
		if part := s.findToolPart(toolCallID); part != nil {
			part["state"] = "approval-requested"
			approval := UIMessageChunk{"id": chunk["approvalId"]}
			if auto, ok := chunk["isAutomatic"].(bool); ok && auto {
				approval["isAutomatic"] = true
			}
			copyIfPresent(approval, chunk, "signature", "signature")
			part["approval"] = approval
		} else {
			reportMissingToolInvocation(onError, toolCallID)
		}
	case "tool-approval-response":
		approvalID := stringValue(chunk["approvalId"])
		if part := s.findToolPartByApprovalID(approvalID); part != nil {
			part["state"] = "approval-responded"
			approval := UIMessageChunk{"id": chunk["approvalId"], "approved": chunk["approved"]}
			copyIfPresent(approval, chunk, "reason", "reason")
			if previous, _ := part["approval"].(UIMessageChunk); previous != nil {
				if auto, _ := previous["isAutomatic"].(bool); auto {
					approval["isAutomatic"] = true
				}
			}
			part["approval"] = approval
			copyIfPresent(part, chunk, "providerExecuted", "providerExecuted")
			copyIfPresent(part, chunk, "providerMetadata", "callProviderMetadata")
		} else {
			reportMissingApproval(onError, approvalID)
		}
	case "error":
		if onError != nil {
			_ = onError(errors.New(stringValue(chunk["errorText"])))
		}
	default:
		if strings.HasPrefix(chunkType, "data-") {
			if transient, _ := chunk["transient"].(bool); transient {
				return
			}
			s.upsertDataPart(chunk)
		}
	}
}

func (s *uiMessageCallbackState) mergeMetadata(metadata interface{}) {
	if metadata == nil {
		return
	}
	if existing, ok := asUIMap(s.message["metadata"]); ok {
		if incoming, ok := asUIMap(metadata); ok {
			s.message["metadata"] = mergeUIMaps(existing, incoming)
			return
		}
	}
	s.message["metadata"] = cloneUIValue(metadata)
}

func (s *uiMessageCallbackState) appendPart(part UIMessageChunk) {
	if typedParts, ok := s.message["parts"].([]UIMessageChunk); ok {
		parts := make([]interface{}, 0, len(typedParts)+1)
		for _, typedPart := range typedParts {
			parts = append(parts, typedPart)
		}
		s.message["parts"] = append(parts, part)
		return
	}
	parts, _ := s.message["parts"].([]interface{})
	s.message["parts"] = append(parts, part)
}

func (s *uiMessageCallbackState) upsertDataPart(chunk UIMessageChunk) {
	id, hasID := chunk["id"].(string)
	if hasID && id != "" {
		for _, raw := range uiParts(s.message) {
			part, ok := raw.(UIMessageChunk)
			if !ok {
				if m, mapOK := raw.(map[string]interface{}); mapOK {
					part = UIMessageChunk(m)
				}
			}
			if part != nil && part["type"] == chunk["type"] && part["id"] == id {
				part["data"] = cloneUIValue(chunk["data"])
				return
			}
		}
	}
	s.appendPart(cloneUIMessageChunk(chunk))
}

func (s *uiMessageCallbackState) updateToolPart(toolCallID, toolName string, dynamic bool, update UIMessageChunk, source UIMessageChunk) {
	part := s.findToolPart(toolCallID)
	if part == nil {
		if dynamic {
			part = UIMessageChunk{"type": "dynamic-tool", "toolName": toolName, "toolCallId": toolCallID}
		} else {
			part = UIMessageChunk{"type": "tool-" + toolName, "toolCallId": toolCallID}
		}
		s.appendPart(part)
	}
	if toolName != "" && part["type"] == "dynamic-tool" {
		part["toolName"] = toolName
	}
	for key, value := range update {
		part[key] = value
	}
	copyIfPresent(part, source, "providerExecuted", "providerExecuted")
	copyIfPresent(part, source, "title", "title")
	copyIfPresent(part, source, "toolMetadata", "toolMetadata")
	if _, isOutput := update["output"]; isOutput {
		copyIfPresent(part, source, "providerMetadata", "resultProviderMetadata")
	} else if update["state"] == "output-error" {
		copyIfPresent(part, source, "providerMetadata", "resultProviderMetadata")
	} else {
		copyIfPresent(part, source, "providerMetadata", "callProviderMetadata")
	}
}

func (s *uiMessageCallbackState) findToolPart(toolCallID string) UIMessageChunk {
	for _, raw := range uiParts(s.message) {
		part, ok := raw.(UIMessageChunk)
		if !ok {
			if m, ok := raw.(map[string]interface{}); ok {
				part = UIMessageChunk(m)
			}
		}
		if part != nil && part["toolCallId"] == toolCallID {
			return part
		}
	}
	return nil
}

func (s *uiMessageCallbackState) findToolPartByApprovalID(approvalID string) UIMessageChunk {
	for _, raw := range uiParts(s.message) {
		part, ok := raw.(UIMessageChunk)
		if !ok {
			if m, ok := raw.(map[string]interface{}); ok {
				part = UIMessageChunk(m)
			}
		}
		if part == nil {
			continue
		}
		approval, _ := part["approval"].(UIMessageChunk)
		if approval == nil {
			if m, ok := part["approval"].(map[string]interface{}); ok {
				approval = UIMessageChunk(m)
			}
		}
		if approval != nil && approval["id"] == approvalID {
			return part
		}
	}
	return nil
}

func toolInfoFromPart(part UIMessageChunk) (string, bool) {
	if part == nil {
		return "", false
	}
	if part["type"] == "dynamic-tool" {
		return stringValue(part["toolName"]), true
	}
	typeName := stringValue(part["type"])
	return strings.TrimPrefix(typeName, "tool-"), false
}

func uiParts(message UIMessageChunk) []interface{} {
	if parts, ok := message["parts"].([]interface{}); ok {
		return parts
	}
	if typedParts, ok := message["parts"].([]UIMessageChunk); ok {
		parts := make([]interface{}, 0, len(typedParts))
		for _, part := range typedParts {
			parts = append(parts, part)
		}
		return parts
	}
	return nil
}

func copyIfPresent(dst UIMessageChunk, src UIMessageChunk, from, to string) {
	if value, ok := src[from]; ok && value != nil {
		dst[to] = value
	}
}

func stringValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func parseToolInputPartial(text string) interface{} {
	if text == "" {
		return nil
	}
	var value interface{}
	if err := json.Unmarshal([]byte(text), &value); err == nil {
		return value
	}
	return nil
}

func reportMissingUIMessagePart(onError func(error) string, chunkType, chunkID string) {
	if onError == nil {
		return
	}
	partType := "text"
	if strings.HasPrefix(chunkType, "reasoning-") {
		partType = "reasoning"
	}
	startType := partType + "-start"
	_ = onError(fmt.Errorf(`Received %s for missing %s part with ID %q. Ensure a "%s" chunk is sent before any "%s" chunks.`, chunkType, partType, chunkID, startType, chunkType))
}

func reportMissingToolInput(onError func(error) string, chunkType, toolCallID string) {
	if onError == nil {
		return
	}
	_ = onError(fmt.Errorf(`Received %s for missing tool call with ID %q. Ensure a "tool-input-start" chunk is sent before any "%s" chunks.`, chunkType, toolCallID, chunkType))
}

func reportMissingToolInvocation(onError func(error) string, toolCallID string) {
	if onError == nil {
		return
	}
	_ = onError(fmt.Errorf("No tool invocation found for tool call ID %q.", toolCallID))
}

func reportMissingApproval(onError func(error) string, approvalID string) {
	if onError == nil {
		return
	}
	_ = onError(fmt.Errorf("No tool invocation found for approval ID %q.", approvalID))
}

func cloneUIMessageChunk(in UIMessageChunk) UIMessageChunk {
	if in == nil {
		return nil
	}
	out := make(UIMessageChunk, len(in))
	for key, value := range in {
		out[key] = cloneUIValue(value)
	}
	return out
}

func cloneUIValue(value interface{}) interface{} {
	switch v := value.(type) {
	case UIMessageChunk:
		return cloneUIMessageChunk(v)
	case map[string]interface{}:
		return cloneUIMessageChunk(UIMessageChunk(v))
	case []UIMessageChunk:
		out := make([]UIMessageChunk, len(v))
		for i, item := range v {
			out[i] = cloneUIMessageChunk(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = cloneUIValue(item)
		}
		return out
	default:
		return v
	}
}

func asUIMap(value interface{}) (map[string]interface{}, bool) {
	switch v := value.(type) {
	case UIMessageChunk:
		return map[string]interface{}(v), true
	case map[string]interface{}:
		return v, true
	default:
		return nil, false
	}
}

func mergeUIMaps(base, incoming map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(incoming))
	for key, value := range base {
		out[key] = cloneUIValue(value)
	}
	for key, value := range incoming {
		if key == "__proto__" || key == "constructor" || key == "prototype" {
			continue
		}
		if existing, ok := asUIMap(out[key]); ok {
			if next, ok := asUIMap(value); ok {
				out[key] = mergeUIMaps(existing, next)
				continue
			}
		}
		out[key] = cloneUIValue(value)
	}
	return out
}

// CreateUIMessageStreamResponse writes UI chunks as SSE to an HTTP response.
func CreateUIMessageStreamResponse(ctx context.Context, result *StreamTextResult, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	return CreateUIMessageStreamResponseWithInit(ctx, result, nil, opts...)
}

// CreateUIMessageStreamResponseWithInit writes UI chunks as SSE to an HTTP response
// with optional status/statusText/headers.
func CreateUIMessageStreamResponseWithInit(ctx context.Context, result *StreamTextResult, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	if result == nil {
		return nil, fmt.Errorf("result is required")
	}

	status := http.StatusOK
	statusText := ""
	headers := http.Header{
		"Content-Type":                  []string{"text/event-stream"},
		"Cache-Control":                 []string{"no-cache"},
		"Connection":                    []string{"keep-alive"},
		"X-Vercel-AI-UI-Message-Stream": []string{"v1"},
		"X-Accel-Buffering":             []string{"no"},
	}
	if init != nil {
		if init.Status != 0 {
			status = init.Status
		}
		statusText = init.StatusText
		for key, value := range init.Headers {
			headers.Set(key, value)
		}
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_ = PipeUIMessageStreamToResponseWithInit(ctx, result, pw, init, opts...)
	}()
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     headers,
		Body:       pr,
	}, nil
}

// PipeUIMessageStreamToResponse writes UI chunks to the given writer as SSE.
func PipeUIMessageStreamToResponse(ctx context.Context, result *StreamTextResult, w io.Writer, opts ...UIMessageStreamResultOptions) error {
	return PipeUIMessageStreamToResponseWithInit(ctx, result, w, nil, opts...)
}

// PipeUIMessageStreamToResponseWithInit supports optional SSE side-channel consumption.
func PipeUIMessageStreamToResponseWithInit(ctx context.Context, result *StreamTextResult, w io.Writer, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) error {
	if result == nil {
		return fmt.Errorf("result is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}

	var (
		teeWriter  io.Writer = w
		sideWriter *io.PipeWriter
		closeSide  chan error
		consumeErr error
	)
	if init != nil && init.ConsumeSSEStream != nil {
		pr, pw := io.Pipe()
		sideWriter = pw
		closeSide = make(chan error, 1)
		go func() {
			closeSide <- init.ConsumeSSEStream(pr)
		}()
		defer pw.Close()
		teeWriter = io.MultiWriter(w, pw)
	}

	chunks, errCh := CreateUIMessageStream(ctx, result, opts...)
	bw := bufio.NewWriter(teeWriter)
	defer bw.Flush()
	for chunk := range chunks {
		b, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := bw.WriteString("data: "); err != nil {
			return err
		}
		if _, err := bw.Write(b); err != nil {
			return err
		}
		if _, err := bw.WriteString("\n\n"); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("data: [DONE]\n\n"); err != nil {
		return err
	}
	if sideWriter != nil {
		_ = bw.Flush()
		sideWriter.Close()
		consumeErr = <-closeSide
	}
	select {
	case err := <-errCh:
		if consumeErr != nil {
			return fmt.Errorf("%v; consumeSSEStream error: %w", err, consumeErr)
		}
		return err
	default:
		if consumeErr != nil {
			return consumeErr
		}
		return nil
	}
}

// ReadUIMessageStream reads SSE data lines containing JSON UI chunks.
func ReadUIMessageStream(r io.Reader) ([]UIMessageChunk, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is required")
	}
	sc := bufio.NewScanner(r)
	chunks := make([]UIMessageChunk, 0)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		var chunk UIMessageChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

type resultChunkConversionOptions struct {
	SendReasoning bool
	SendSources   bool
	OnError       func(error) string
	Tools         []types.Tool
}

func convertProviderChunkToUIMessageChunks(chunk provider.StreamChunk, opts resultChunkConversionOptions) []UIMessageChunk {
	out := make([]UIMessageChunk, 0, 2)

	withMeta := func(part map[string]interface{}) {
		if len(chunk.ProviderMetadata) == 0 {
			return
		}
		var providerMetadata map[string]interface{}
		if err := json.Unmarshal(chunk.ProviderMetadata, &providerMetadata); err == nil && len(providerMetadata) > 0 {
			part["providerMetadata"] = providerMetadata
		}
	}
	isDynamicTool := func(toolName string, dynamic bool) bool {
		if dynamic {
			return true
		}
		for _, tool := range opts.Tools {
			if tool.Name == toolName && tool.Type == types.ToolTypeDynamic {
				return true
			}
		}
		return false
	}

	switch chunk.Type {
	case provider.ChunkTypeRaw:
		break
	case provider.ChunkTypeResponseMetadata, provider.ChunkTypeFirstChunk:
		break
	case provider.ChunkTypeTextStart:
		part := map[string]interface{}{"type": "text-start", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeText:
		part := map[string]interface{}{"type": "text-delta", "id": chunk.ID, "delta": chunk.Text}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeTextEnd:
		part := map[string]interface{}{"type": "text-end", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningStart:
		if !opts.SendReasoning {
			break
		}
		part := map[string]interface{}{"type": "reasoning-start", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningEnd:
		if !opts.SendReasoning {
			break
		}
		part := map[string]interface{}{"type": "reasoning-end", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoning:
		if !opts.SendReasoning {
			break
		}
		delta := chunk.Reasoning
		if delta == "" {
			delta = chunk.Text
		}
		part := map[string]interface{}{"type": "reasoning-delta", "id": chunk.ID, "delta": delta}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeSource:
		if chunk.SourceContent == nil || !opts.SendSources {
			break
		}
		if chunk.SourceContent.SourceType == "url" {
			part := map[string]interface{}{
				"type":     "source-url",
				"sourceId": chunk.SourceContent.ID,
				"url":      chunk.SourceContent.URL,
			}
			if chunk.SourceContent.Title != "" {
				part["title"] = chunk.SourceContent.Title
			}
			withMeta(part)
			out = append(out, part)
			break
		}
		if chunk.SourceContent.SourceType == "document" {
			part := map[string]interface{}{
				"type":      "source-document",
				"sourceId":  chunk.SourceContent.ID,
				"mediaType": chunk.SourceContent.MediaType,
			}
			if chunk.SourceContent.Title != "" {
				part["title"] = chunk.SourceContent.Title
			}
			if chunk.SourceContent.Filename != "" {
				part["filename"] = chunk.SourceContent.Filename
			}
			withMeta(part)
			out = append(out, part)
		}
	case provider.ChunkTypeFile:
		fileContent := chunk.GeneratedFileContent
		if fileContent == nil {
			break
		}
		part := map[string]interface{}{
			"type":      "file",
			"mediaType": fileContent.MediaType,
			"url":       deriveFileURL(fileContent.MediaType, fileContent),
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningFile:
		if !opts.SendReasoning {
			break
		}
		fileContent := chunk.ReasoningFileContent
		part := map[string]interface{}{
			"type":      "reasoning-file",
			"mediaType": fileContent.MediaType,
			"url":       deriveFileURLFromReasoning(fileContent, fileContent.MediaType),
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeCustom:
		if chunk.CustomContent == nil {
			break
		}
		part := map[string]interface{}{
			"type": "custom",
			"kind": chunk.CustomContent.Kind,
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolCall:
		if chunk.ToolCall == nil {
			break
		}
		if chunk.ToolCall.Invalid {
			part := map[string]interface{}{
				"type":       "tool-input-error",
				"toolCallId": chunk.ToolCall.ID,
				"toolName":   chunk.ToolCall.ToolName,
				"input":      chunk.ToolCall.Arguments,
				"errorText":  opts.OnError(chunk.ToolCall.Error),
			}
			if chunk.ToolCall.ProviderExecuted {
				part["providerExecuted"] = true
			}
			if len(chunk.ToolCall.ToolMetadata) > 0 {
				part["toolMetadata"] = chunk.ToolCall.ToolMetadata
			}
			if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
				part["dynamic"] = true
			}
			if chunk.ToolCall.Title != "" {
				part["title"] = chunk.ToolCall.Title
			}
			if len(chunk.ToolCall.ProviderMetadata) > 0 {
				part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
			}
			withMeta(part)
			out = append(out, part)
			break
		}
		part := map[string]interface{}{
			"type":       "tool-input-available",
			"toolCallId": chunk.ToolCall.ID,
			"toolName":   chunk.ToolCall.ToolName,
			"input":      chunk.ToolCall.Arguments,
		}
		if chunk.ToolCall.ProviderExecuted {
			part["providerExecuted"] = true
		}
		if len(chunk.ToolCall.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolCall.ToolMetadata
		}
		if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolCall.Title != "" {
			part["title"] = chunk.ToolCall.Title
		}
		if len(chunk.ToolCall.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolInputStart:
		if chunk.ToolCall == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-input-start",
			"toolCallId": chunk.ToolCall.ID,
			"toolName":   chunk.ToolCall.ToolName,
		}
		if chunk.ToolCall.ProviderExecuted {
			part["providerExecuted"] = true
		}
		if chunk.ToolCall.ToolMetadata != nil && len(chunk.ToolCall.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolCall.ToolMetadata
		}
		if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolCall.Title != "" {
			part["title"] = chunk.ToolCall.Title
		}
		if len(chunk.ToolCall.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolInputDelta:
		part := map[string]interface{}{
			"type":           "tool-input-delta",
			"toolCallId":     chunk.ID,
			"inputTextDelta": chunk.Text,
		}
		out = append(out, part)
	case provider.ChunkTypeToolInputEnd:
		break
	case provider.ChunkTypeToolApprovalRequest:
		if chunk.ToolApprovalRequest == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-approval-request",
			"approvalId": chunk.ToolApprovalRequest.ApprovalID,
			"toolCallId": chunk.ToolApprovalRequest.ToolCallID,
		}
		if part["toolCallId"] == "" {
			part["toolCallId"] = chunk.ToolApprovalRequest.ToolCall.ID
		}
		if chunk.ToolApprovalRequest.IsAutomatic {
			part["isAutomatic"] = true
		}
		if chunk.ToolApprovalRequest.Signature != "" {
			part["signature"] = chunk.ToolApprovalRequest.Signature
		}
		out = append(out, part)
	case provider.ChunkTypeToolApprovalResponse:
		if chunk.ToolApprovalResponse == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-approval-response",
			"approvalId": chunk.ToolApprovalResponse.ApprovalID,
			"approved":   chunk.ToolApprovalResponse.Approved,
		}
		if chunk.ToolApprovalResponse.Reason != "" {
			part["reason"] = chunk.ToolApprovalResponse.Reason
		}
		if chunk.ToolApprovalResponse.ProviderExecuted {
			part["providerExecuted"] = true
		}
		out = append(out, part)
	case provider.ChunkTypeToolOutputDenied:
		toolCallID := ""
		if chunk.ToolResult != nil {
			toolCallID = chunk.ToolResult.ToolCallID
		}
		out = append(out, map[string]interface{}{
			"type":       "tool-output-denied",
			"toolCallId": toolCallID,
		})
	case provider.ChunkTypeToolResult:
		if chunk.ToolResult == nil {
			break
		}
		approvalID := chunk.ToolResult.ApprovalID
		if approvalID == "" {
			approvalID = chunk.ToolResult.ToolCallID
		}
		if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusUserApproval ||
			chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved ||
			chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
			request := map[string]interface{}{
				"type":       "tool-approval-request",
				"approvalId": approvalID,
				"toolCallId": chunk.ToolResult.ToolCallID,
			}
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved ||
				chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
				request["isAutomatic"] = true
			}
			if chunk.ToolResult.ApprovalSignature != "" {
				request["signature"] = chunk.ToolResult.ApprovalSignature
			}
			out = append(out, request)
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				break
			}
			response := map[string]interface{}{
				"type":       "tool-approval-response",
				"approvalId": approvalID,
				"approved":   chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved,
			}
			if chunk.ToolResult.ApprovalReason != nil {
				response["reason"] = *chunk.ToolResult.ApprovalReason
			}
			if chunk.ToolResult.ProviderExecuted {
				response["providerExecuted"] = true
			}
			out = append(out, response)
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
				out = append(out, map[string]interface{}{
					"type":       "tool-output-denied",
					"toolCallId": chunk.ToolResult.ToolCallID,
				})
				break
			}
			if chunk.ToolResult.ProviderExecuted && chunk.ToolResult.Result == nil && chunk.ToolResult.Error == nil {
				break
			}
		}
		if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
			part := map[string]interface{}{
				"type":       "tool-output-denied",
				"toolCallId": chunk.ToolResult.ToolCallID,
			}
			out = append(out, part)
			break
		}
		partType := "tool-output-available"
		part := map[string]interface{}{
			"type":       partType,
			"toolCallId": chunk.ToolResult.ToolCallID,
			"output":     chunk.ToolResult.Result,
		}
		if len(chunk.ToolResult.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolResult.ToolMetadata
		}
		if chunk.ToolResult.Error != nil {
			errorText := opts.OnError(chunk.ToolResult.Error)
			if chunk.ToolResult.ProviderExecuted {
				errorText = chunk.ToolResult.Error.Error()
			}
			partType = "tool-output-error"
			part = map[string]interface{}{
				"type":       partType,
				"toolCallId": chunk.ToolResult.ToolCallID,
				"errorText":  errorText,
			}
			if len(chunk.ToolResult.ToolMetadata) > 0 {
				part["toolMetadata"] = chunk.ToolResult.ToolMetadata
			}
		}
		if chunk.ToolResult.ProviderExecuted {
			part["providerExecuted"] = true
		}
		if isDynamicTool(chunk.ToolResult.ToolName, chunk.ToolResult.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolResult.Preliminary {
			part["preliminary"] = true
		}
		if len(chunk.ToolResult.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolResult.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeError:
		out = append(out, map[string]interface{}{
			"type":      "error",
			"errorText": opts.OnError(errors.New(chunk.Text)),
		})
	case provider.ChunkTypeFinish:
		out = append(out, map[string]interface{}{
			"type": "finish-step",
		})
	default:
		if chunk.Type == "start-step" {
			out = append(out, map[string]interface{}{"type": "start-step"})
			break
		}
		if chunk.Type == provider.ChunkTypeStreamStart {
			out = append(out, map[string]interface{}{"type": "start-step"})
			break
		}
		if chunk.Type == provider.ChunkTypeStreamFinish {
			out = append(out, map[string]interface{}{"type": "finish-step"})
			break
		}
	}
	return out
}

func deriveFileURL(mediaType string, file *types.GeneratedFileContent) string {
	if file == nil {
		return ""
	}
	return "data:" + mediaType + ";base64," + generatedFileBase64Data(file)
}

func generatedFileBase64Data(file *types.GeneratedFileContent) string {
	if file == nil {
		return ""
	}
	if file.URL != "" {
		return file.URL
	}
	if file.FileData.Type == "url" && file.FileData.URL != "" {
		return file.FileData.URL
	}
	if file.FileData.Type == "data" {
		if file.FileData.DataString != "" {
			return file.FileData.DataString
		}
		if len(file.FileData.Data) > 0 {
			return base64.StdEncoding.EncodeToString(file.FileData.Data)
		}
	}
	if len(file.Data) > 0 {
		return base64.StdEncoding.EncodeToString(file.Data)
	}
	return ""
}

func deriveFileURLFromReasoning(file *types.ReasoningFileContent, mediaType string) string {
	if file == nil {
		return ""
	}
	return "data:" + mediaType + ";base64," + reasoningFileBase64Data(file)
}

func reasoningFileBase64Data(file *types.ReasoningFileContent) string {
	if file == nil {
		return ""
	}
	if file.FileData.Type == "url" && file.FileData.URL != "" {
		return file.FileData.URL
	}
	if file.FileData.Type == "data" {
		if file.FileData.DataString != "" {
			return file.FileData.DataString
		}
		if len(file.FileData.Data) > 0 {
			return base64.StdEncoding.EncodeToString(file.FileData.Data)
		}
	}
	if len(file.Data) > 0 {
		return base64.StdEncoding.EncodeToString(file.Data)
	}
	return ""
}
