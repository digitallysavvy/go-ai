package langchain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LangSmithDeploymentTransportOptions configures a LangSmith/LangGraph
// deployment transport. GraphID defaults to "agent".
type LangSmithDeploymentTransportOptions struct {
	URL        string
	APIKey     string
	GraphID    string
	HTTPClient *http.Client

	// Stream, when set, overrides the default HTTP RemoteGraph-compatible
	// streaming call. It exists for applications that already have a LangGraph
	// client and for tests that should not perform network I/O.
	Stream func(context.Context, []LangChainMessage) (<-chan StreamEvent, error)
}

// LangSmithDeploymentTransport adapts LangSmith/LangGraph deployments to AI SDK
// UI message stream chunks.
type LangSmithDeploymentTransport struct {
	url        string
	apiKey     string
	graphID    string
	httpClient *http.Client
	stream     func(context.Context, []LangChainMessage) (<-chan StreamEvent, error)
}

// NewLangSmithDeploymentTransport creates a LangSmith deployment transport.
func NewLangSmithDeploymentTransport(options LangSmithDeploymentTransportOptions) *LangSmithDeploymentTransport {
	graphID := options.GraphID
	if graphID == "" {
		graphID = "agent"
	}
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &LangSmithDeploymentTransport{
		url:        strings.TrimRight(options.URL, "/"),
		apiKey:     options.APIKey,
		graphID:    graphID,
		httpClient: client,
		stream:     options.Stream,
	}
}

// SendMessages converts AI SDK model messages to LangChain messages, streams the
// remote graph with streamMode ["values", "messages"], and returns UI chunks.
func (t *LangSmithDeploymentTransport) SendMessages(ctx context.Context, messages []types.Message) (<-chan ai.UIMessageChunk, <-chan error) {
	return t.SendMessagesWithCallbacks(ctx, messages, nil)
}

// SendMessagesWithCallbacks converts AI SDK model messages to LangChain
// messages, streams the remote graph, and invokes lifecycle callbacks.
func (t *LangSmithDeploymentTransport) SendMessagesWithCallbacks(ctx context.Context, messages []types.Message, callbacks *StreamCallbacks) (<-chan ai.UIMessageChunk, <-chan error) {
	errs := make(chan error, 1)
	if t == nil {
		out := closedChunkChannel()
		errs <- errors.New("langchain: nil LangSmithDeploymentTransport")
		close(errs)
		return out, errs
	}
	baseMessages := ConvertModelMessages(messages)
	streamEvents, err := t.openStream(ctx, baseMessages)
	if err != nil {
		out := closedChunkChannel()
		errs <- err
		close(errs)
		return out, errs
	}
	return ToUIMessageStreamWithCallbacks(ctx, streamEvents, callbacks)
}

// ReconnectToStream matches the current TypeScript adapter behavior.
func (t *LangSmithDeploymentTransport) ReconnectToStream(context.Context, string) (<-chan ai.UIMessageChunk, <-chan error) {
	out := closedChunkChannel()
	errs := make(chan error, 1)
	errs <- errors.New("Method not implemented.")
	close(errs)
	return out, errs
}

func (t *LangSmithDeploymentTransport) openStream(ctx context.Context, messages []LangChainMessage) (<-chan StreamEvent, error) {
	if t.stream != nil {
		return t.stream(ctx, messages)
	}
	if t.url == "" {
		return nil, errors.New("langchain: URL is required")
	}
	body, err := json.Marshal(map[string]interface{}{
		"input":       map[string]interface{}{"messages": messages},
		"streamMode":  []string{"values", "messages"},
		"stream_mode": []string{"values", "messages"},
	})
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/graphs/%s/stream", t.url, t.graphID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/x-ndjson, application/json")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("langchain: stream request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return readRemoteGraphEvents(ctx, resp.Body), nil
}

func readRemoteGraphEvents(ctx context.Context, body io.ReadCloser) <-chan StreamEvent {
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer body.Close()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 1024), 1024*1024)
		var sseData strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				flushRemoteGraphLine(ctx, out, sseData.String())
				sseData.Reset()
				continue
			}
			if strings.HasPrefix(line, "data:") {
				sseData.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				continue
			}
			if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
				continue
			}
			flushRemoteGraphLine(ctx, out, line)
		}
		if sseData.Len() > 0 {
			flushRemoteGraphLine(ctx, out, sseData.String())
		}
	}()
	return out
}

func flushRemoteGraphLine(ctx context.Context, out chan<- StreamEvent, line string) {
	line = strings.TrimSpace(line)
	if line == "" || line == "[DONE]" {
		return
	}
	var tuple []interface{}
	if err := json.Unmarshal([]byte(line), &tuple); err == nil {
		sendStreamEvent(ctx, out, ParseLangGraphEvent(tuple))
		return
	}
	var object map[string]interface{}
	if err := json.Unmarshal([]byte(line), &object); err == nil {
		if mode := stringValue(object["mode"]); mode != "" {
			sendStreamEvent(ctx, out, StreamEvent{Mode: mode, Data: object["data"]})
			return
		}
		if typ := stringValue(object["type"]); typ != "" {
			sendStreamEvent(ctx, out, StreamEvent{Mode: typ, Data: object["data"]})
		}
	}
}

func sendStreamEvent(ctx context.Context, out chan<- StreamEvent, event StreamEvent) {
	if event.Mode == "" {
		return
	}
	select {
	case <-ctx.Done():
	case out <- event:
	}
}

func closedChunkChannel() <-chan ai.UIMessageChunk {
	out := make(chan ai.UIMessageChunk)
	close(out)
	return out
}
