package ai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TextStreamResponseInit mirrors the TypeScript ResponseInit shape used by
// createTextStreamResponse.
type TextStreamResponseInit struct {
	Status     int
	StatusText string
	Headers    map[string]string
}

// CreateTextStreamResponse creates an HTTP response with a plain-text body from
// stream text chunks.
func CreateTextStreamResponse(ctx context.Context, result *StreamTextResult) (*http.Response, error) {
	return CreateTextStreamResponseWithInit(ctx, result, nil)
}

// CreateTextStreamResponseWithInit creates an HTTP response with optional status,
// status text, and headers.
func CreateTextStreamResponseWithInit(ctx context.Context, result *StreamTextResult, init *TextStreamResponseInit) (*http.Response, error) {
	if result == nil {
		return nil, fmt.Errorf("result is required")
	}
	return CreateTextStreamResponseFromStream(ctx, result.Stream(), init)
}

// CreateTextStreamResponseFromStream creates an HTTP response with a plain-text
// body from a provider text stream.
func CreateTextStreamResponseFromStream(ctx context.Context, stream provider.TextStream, init *TextStreamResponseInit) (*http.Response, error) {
	if stream == nil {
		return nil, fmt.Errorf("stream is required")
	}
	status := http.StatusOK
	statusText := ""
	headers := http.Header{
		"Content-Type": []string{"text/plain; charset=utf-8"},
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
		defer func() { _ = pw.Close() }()
		_ = PipeTextStreamToWriter(ctx, stream, pw)
	}()
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     headers,
		Body:       pr,
	}, nil
}

// PipeTextStreamToResponse writes text chunks to the writer as UTF-8 text.
func PipeTextStreamToResponse(ctx context.Context, result *StreamTextResult, w io.Writer) error {
	if result == nil {
		return fmt.Errorf("result is required")
	}
	return PipeTextStreamToWriter(ctx, result.Stream(), w)
}

// PipeTextStreamToWriter writes text delta chunks from a provider stream to w as
// UTF-8 text.
//
// If writing to w fails (the Go analog of a client disconnecting mid
// response), stream is closed before the error is returned so any upstream
// resources it holds (for example an open provider HTTP connection) are
// released instead of being silently abandoned mid-stream. Mirrors TS
// write-to-server-response.ts's client-disconnect handling, which cancels
// the source reader on a premature close (TS #21578).
func PipeTextStreamToWriter(ctx context.Context, stream provider.TextStream, w io.Writer) error {
	if stream == nil {
		return fmt.Errorf("stream is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	bw := bufio.NewWriter(w)
	// flusher is the Go equivalent of TS write-to-server-response.ts's
	// `(response as FlushableServerResponse).flush` (e.g. a compressing
	// ServerResponse middleware exposing a manual flush): an
	// http.ResponseWriter wrapped by gzip/compression middleware commonly
	// implements http.Flusher so a chunked/compressed write actually reaches
	// the client instead of sitting in the compressor's internal buffer.
	flusher, _ := w.(http.Flusher)

	loopErr := func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			chunk, err := stream.Next()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			if chunk.Type == provider.ChunkTypeText && chunk.Text != "" {
				if _, err := bw.WriteString(chunk.Text); err != nil {
					_ = stream.Close()
					return err
				}
				// Flush after every chunk (audit row b9ac19f, WG-MISC): TS
				// calls response.write()+flush() per chunk instead of
				// buffering, so a consumer streaming this response sees
				// output incrementally instead of in bufio's default 4 KiB
				// blocks.
				if err := bw.Flush(); err != nil {
					_ = stream.Close()
					return err
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}()

	// A failing writer must surface an error even on a short stream: the
	// deferred bw.Flush() this replaced discarded its return value entirely
	// (hand-off/WG-MISC item 7f6650b: "response piping returns [an error] so
	// write errors are catchable").
	if flushErr := bw.Flush(); flushErr != nil && loopErr == nil {
		_ = stream.Close()
		return flushErr
	}
	return loopErr
}

// ToTextStream converts a provider text stream into text delta and error
// channels. Only text-delta chunks are emitted, matching the TypeScript
// toTextStream helper.
func ToTextStream(ctx context.Context, stream provider.TextStream) (<-chan string, <-chan error) {
	out := make(chan string)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errCh)
		if stream == nil {
			errCh <- fmt.Errorf("stream is required")
			return
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
				if err != io.EOF {
					errCh <- err
				}
				return
			}
			if chunk.Type != provider.ChunkTypeText {
				continue
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case out <- chunk.Text:
			}
		}
	}()
	return out, errCh
}

// ConsumeStream drains a text stream until completion.
func ConsumeStream(ctx context.Context, stream provider.TextStream) error {
	if stream == nil {
		return fmt.Errorf("stream is required")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
