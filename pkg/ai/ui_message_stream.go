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

// UIMessageStreamOnFinishCallback is invoked when the UI stream finishes.
type UIMessageStreamOnFinishCallback func(ctx map[string]interface{})

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
	Execute           func(writer UIMessageStreamWriter)
	OnError           func(error) string
	OriginalMessages  []UIMessageChunk
	OnStepFinish      UIMessageStreamOnStepFinishCallback
	OnFinish          UIMessageStreamOnFinishCallback
	GenerateMessageID IDGenerator
}

// UIMessageStreamResultOptions controls result-to-UI projection settings.
type UIMessageStreamResultOptions struct {
	OriginalMessages  []UIMessageChunk
	GenerateMessageID IDGenerator
	MessageMetadata   func(part map[string]interface{}) map[string]interface{}
	SendReasoning     *bool
	SendSources       *bool
	SendStart         *bool
	SendFinish        *bool
	OnStepFinish      UIMessageStreamOnStepFinishCallback
	OnFinish          UIMessageStreamOnFinishCallback
	OnError           func(error) string
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
			if err == nil {
				return "error"
			}
			return err.Error()
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
			select {
			case errCh <- err:
			default:
			}
		}

		generateID := options.GenerateMessageID
		if generateID == nil {
			generateID = newCallID
		}

		callOnFinish := func() {
			if options.OnFinish == nil {
				return
			}
			finishEvent := map[string]interface{}{
				"isContinuation": false,
				"isAborted":      ctx.Err() != nil,
				"responseMessage": map[string]interface{}{
					"id": generateID(),
				},
				"messages":     append([]UIMessageChunk{}, options.OriginalMessages...),
				"finishReason": "",
			}
			defer func() {
				_ = recover()
			}()
			options.OnFinish(finishEvent)
		}

		callOnStepFinish := func() {
			if options.OnStepFinish == nil {
				return
			}
			stepEvent := map[string]interface{}{
				"isContinuation": false,
				"responseMessage": map[string]interface{}{
					"id": generateID(),
				},
				"messages": append([]UIMessageChunk{}, options.OriginalMessages...),
			}
			defer func() {
				_ = recover()
			}()
			options.OnStepFinish(stepEvent)
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
						safeEnqueue(chunk)
					}
				}
			}()
		}

		writer := UIMessageStreamWriter{
			writeFn: safeEnqueue,
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

		callOnStepFinish()
		wg.Wait()

		mu.Lock()
		closed = true
		mu.Unlock()

		callOnFinish()
	}()

	return out, errCh
}

// CreateUIMessageStream converts a StreamTextResult into a channel of UI chunks.
func CreateUIMessageStream(ctx context.Context, result *StreamTextResult, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		if result == nil {
			errCh <- fmt.Errorf("result is required")
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
		sendSources := boolOption(options.SendSources, false)

		generateID := options.GenerateMessageID
		if generateID == nil {
			generateID = newCallID
		}
		messageID := generateID()

		accumText := strings.Builder{}

		callOnFinish := func(finishReason types.FinishReason) {
			if options.OnFinish == nil {
				return
			}
			isContinuation := false
			if len(options.OriginalMessages) > 0 {
				lastMsg := options.OriginalMessages[len(options.OriginalMessages)-1]
				if lastID, ok := lastMsg["id"].(string); ok && lastID == messageID {
					isContinuation = true
				}
			}
			responseMessage := map[string]interface{}{
				"id":      messageID,
				"role":    "assistant",
				"content": []interface{}{map[string]interface{}{"type": "text", "text": accumText.String()}},
			}
			messages := append([]UIMessageChunk{}, options.OriginalMessages...)
			if !isContinuation {
				messages = append(messages, responseMessage)
			}
			finishEvent := map[string]interface{}{
				"isContinuation":  isContinuation,
				"isAborted":       ctx.Err() != nil,
				"responseMessage": responseMessage,
				"messages":        messages,
				"finishReason":    finishReason,
			}
			defer func() {
				_ = recover()
			}()
			options.OnFinish(finishEvent)
		}

		callOnStepFinish := func() {
			if options.OnStepFinish == nil {
				return
			}
			isContinuation := false
			if len(options.OriginalMessages) > 0 {
				lastMsg := options.OriginalMessages[len(options.OriginalMessages)-1]
				if lastID, ok := lastMsg["id"].(string); ok && lastID == messageID {
					isContinuation = true
				}
			}
			responseMessage := map[string]interface{}{
				"id":      messageID,
				"role":    "assistant",
				"content": []interface{}{map[string]interface{}{"type": "text", "text": accumText.String()}},
			}
			messages := append([]UIMessageChunk{}, options.OriginalMessages...)
			if !isContinuation {
				messages = append(messages, responseMessage)
			}
			stepEvent := map[string]interface{}{
				"isContinuation":  isContinuation,
				"responseMessage": responseMessage,
				"messages":        messages,
			}
			defer func() {
				_ = recover()
			}()
			options.OnStepFinish(stepEvent)
		}

		safeEnqueue := func(part UIMessageChunk) {
			select {
			case <-ctx.Done():
				return
			case out <- part:
			}
		}

		if sendStart {
			startEvent := map[string]interface{}{
				"type": "start",
			}
			if messageID != "" {
				startEvent["messageId"] = messageID
			}
			if options.MessageMetadata != nil {
				metadata := options.MessageMetadata(map[string]interface{}{"type": "start"})
				if metadata != nil {
					startEvent["messageMetadata"] = metadata
				}
			}
			safeEnqueue(startEvent)
		}

		stream := result.Stream()
		finishReason := types.FinishReason("")
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
				errCh <- err
				appendErrorChunk(onError, safeEnqueue, err)
				callOnFinish(finishReason)
				return
			}

			if chunk.Type == provider.ChunkTypeText {
				accumText.WriteString(chunk.Text)
			}
			if chunk.Type == provider.ChunkTypeFinish {
				finishReason = chunk.FinishReason
			}
			for _, converted := range convertProviderChunkToUIMessageChunks(*chunk, resultChunkConversionOptions{
				SendReasoning: sendReasoning,
				SendSources:   sendSources,
				OnError:       onError,
			}) {
				safeEnqueue(converted)
				if options.MessageMetadata != nil && chunk.Type != provider.ChunkTypeStreamStart && chunk.Type != provider.ChunkTypeStreamFinish {
					metadata := options.MessageMetadata(map[string]interface{}{
						"type": string(chunk.Type),
						"part": chunk,
					})
					if metadata != nil && string(chunk.Type) != "start" && string(chunk.Type) != "finish" {
						safeEnqueue(UIMessageChunk{
							"type":            "message-metadata",
							"messageMetadata": metadata,
						})
					}
				}
			}
		}

		if sendFinish {
			finishEvent := map[string]interface{}{
				"type":         "finish",
				"finishReason": string(finishReason),
			}
			if options.MessageMetadata != nil {
				metadata := options.MessageMetadata(map[string]interface{}{
					"type":         "finish",
					"finishReason": string(finishReason),
				})
				if metadata != nil {
					finishEvent["messageMetadata"] = metadata
				}
			}
			safeEnqueue(finishEvent)
		}
		callOnStepFinish()
		callOnFinish(finishReason)
	}()
	return out, errCh
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

	switch chunk.Type {
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
		part := map[string]interface{}{"type": "reasoning-delta", "id": chunk.ID, "delta": chunk.Text}
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
				"title":    chunk.SourceContent.Title,
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
				"title":     chunk.SourceContent.Title,
				"filename":  chunk.SourceContent.Filename,
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
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolResult:
		if chunk.ToolResult == nil {
			break
		}
		partType := "tool-output-available"
		part := map[string]interface{}{
			"type":       partType,
			"toolCallId": chunk.ToolResult.ToolCallID,
			"output":     chunk.ToolResult.Result,
		}
		if chunk.ToolResult.Error != nil {
			partType = "tool-output-error"
			part = map[string]interface{}{
				"type":       partType,
				"toolCallId": chunk.ToolResult.ToolCallID,
				"errorText":  opts.OnError(chunk.ToolResult.Error),
			}
		}
		if chunk.ToolResult.ProviderExecuted {
			part["providerExecuted"] = true
		}
		if chunk.ToolResult.Dynamic {
			part["dynamic"] = true
		}
		if chunk.ToolResult.Preliminary {
			part["preliminary"] = true
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeError:
		out = append(out, map[string]interface{}{
			"type":      "error",
			"errorText": opts.OnError(errors.New(chunk.Text)),
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
		out = append(out, UIMessageChunk{
			"type":  string(chunk.Type),
			"chunk": chunk,
		})
	}
	return out
}

func deriveFileURL(mediaType string, file *types.GeneratedFileContent) string {
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
			return "data:" + mediaType + ";base64," + file.FileData.DataString
		}
		if len(file.FileData.Data) > 0 {
			return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(file.FileData.Data)
		}
	}
	if len(file.Data) > 0 {
		return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(file.Data)
	}
	return ""
}

func deriveFileURLFromReasoning(file *types.ReasoningFileContent, mediaType string) string {
	if file == nil {
		return ""
	}
	if file.FileData.Type == "url" && file.FileData.URL != "" {
		return file.FileData.URL
	}
	if file.FileData.Type == "data" {
		if file.FileData.DataString != "" {
			return "data:" + mediaType + ";base64," + file.FileData.DataString
		}
		if len(file.FileData.Data) > 0 {
			return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(file.FileData.Data)
		}
	}
	if len(file.Data) > 0 {
		return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(file.Data)
	}
	return ""
}
