package ai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// CreateTextStreamResponse creates an HTTP response with a plain-text body from
// stream text chunks.
func CreateTextStreamResponse(ctx context.Context, result *StreamTextResult) (*http.Response, error) {
	if result == nil {
		return nil, fmt.Errorf("result is required")
	}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_ = PipeTextStreamToResponse(ctx, result, pw)
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/plain; charset=utf-8"},
		},
		Body: pr,
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
