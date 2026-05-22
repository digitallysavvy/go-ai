package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
	OnStepFinish     UIMessageStreamOnStepFinishCallback
	OnFinish         UIMessageStreamOnFinishCallback
	GenerateMessageID IDGenerator
}

// UIMessageStreamResponseInit mirrors the TypeScript response init shape used by
// createUIMessageStreamResponse.
type UIMessageStreamResponseInit struct {
	Status      int
	StatusText  string
	Headers     map[string]string
	ConsumeSSEStream func(io.Reader) error
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
			wg.Add(1)
			go func() {
				defer wg.Done()
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
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					if err, ok := r.(error); ok {
						errCh <- err
					} else {
						errCh <- fmt.Errorf("ui message stream panicked: %v", r)
					}
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
func CreateUIMessageStream(ctx context.Context, result *StreamTextResult) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		if result == nil {
			errCh <- fmt.Errorf("result is required")
			return
		}
		stream := result.Stream()
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
					return
				}
				errCh <- err
				return
			}
			out <- UIMessageChunk{
				"type":  string(chunk.Type),
				"chunk": chunk,
			}
		}
	}()
	return out, errCh
}

// CreateUIMessageStreamResponse writes UI chunks as SSE to an HTTP response.
func CreateUIMessageStreamResponse(ctx context.Context, result *StreamTextResult) (*http.Response, error) {
	return CreateUIMessageStreamResponseWithInit(ctx, result, nil)
}

// CreateUIMessageStreamResponseWithInit writes UI chunks as SSE to an HTTP response
// with optional status/statusText/headers.
func CreateUIMessageStreamResponseWithInit(ctx context.Context, result *StreamTextResult, init *UIMessageStreamResponseInit) (*http.Response, error) {
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
		_ = PipeUIMessageStreamToResponseWithInit(ctx, result, pw, init)
	}()
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     headers,
		Body:       pr,
	}, nil
}

// PipeUIMessageStreamToResponse writes UI chunks to the given writer as SSE.
func PipeUIMessageStreamToResponse(ctx context.Context, result *StreamTextResult, w io.Writer) error {
	return PipeUIMessageStreamToResponseWithInit(ctx, result, w, nil)
}

// PipeUIMessageStreamToResponseWithInit supports optional SSE side-channel consumption.
func PipeUIMessageStreamToResponseWithInit(ctx context.Context, result *StreamTextResult, w io.Writer, init *UIMessageStreamResponseInit) error {
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
		defer pw.Close() // closes pipe on early-error returns so the goroutine unblocks
		teeWriter = io.MultiWriter(w, pw)
	}

	chunks, errCh := CreateUIMessageStream(ctx, result)
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
	// Flush buffered data into the pipe before closing it, then wait for the
	// side consumer to drain. This must happen before the deferred bw.Flush()
	// and pw.Close() to avoid the deadlock where pw.Close() would only run
	// after we return, but we're blocked waiting for closeSide.
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
