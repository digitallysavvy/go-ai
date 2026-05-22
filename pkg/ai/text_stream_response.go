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
		_ = PipeTextStreamToResponse(ctx, result, pw)
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
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if chunk.Type == provider.ChunkTypeText && chunk.Text != "" {
			if _, err := bw.WriteString(chunk.Text); err != nil {
				return err
			}
		}
	}
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
