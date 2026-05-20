package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// UIMessageChunk is a lightweight JSON-compatible chunk shape.
type UIMessageChunk map[string]interface{}

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
	if result == nil {
		return nil, fmt.Errorf("result is required")
	}
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_ = PipeUIMessageStreamToResponse(ctx, result, pw)
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                  []string{"text/event-stream"},
			"Cache-Control":                 []string{"no-cache"},
			"Connection":                    []string{"keep-alive"},
			"X-Vercel-AI-UI-Message-Stream": []string{"v1"},
			"X-Accel-Buffering":             []string{"no"},
		},
		Body: pr,
	}, nil
}

// PipeUIMessageStreamToResponse writes UI chunks to the given writer as SSE.
func PipeUIMessageStreamToResponse(ctx context.Context, result *StreamTextResult, w io.Writer) error {
	chunks, errCh := CreateUIMessageStream(ctx, result)
	bw := bufio.NewWriter(w)
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
	select {
	case err := <-errCh:
		return err
	default:
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
