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
		defer pw.Close()
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
func PipeTextStreamToWriter(ctx context.Context, stream provider.TextStream, w io.Writer) error {
	if stream == nil {
		return fmt.Errorf("stream is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()

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
		if chunk.Type == provider.ChunkTypeText {
			if _, err := bw.WriteString(chunk.Text); err != nil {
				return err
			}
		}
	}
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
