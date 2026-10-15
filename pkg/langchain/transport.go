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

	// Headers are sent with every request (TS RemoteGraph `headers`).
	Headers map[string]string

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
	headers    map[string]string
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
		headers:    options.Headers,
		stream:     options.Stream,
	}
}

// LangSmithDeploymentTransport implements ai.ChatTransport, matching TS's
// `class LangSmithDeploymentTransport<UI_MESSAGE> implements
// ChatTransport<UI_MESSAGE>` (packages/langchain/src/transport.ts).
var _ ai.ChatTransport = (*LangSmithDeploymentTransport)(nil)

// SendMessages converts AI SDK model messages to LangChain messages, streams the
// remote graph with streamMode ["values", "messages"], and returns UI chunks.
// It implements ai.ChatTransport.SendMessages.
func (t *LangSmithDeploymentTransport) SendMessages(ctx context.Context, req ai.ChatTransportSendMessagesRequest) (<-chan ai.UIMessageChunk, <-chan error) {
	return t.SendMessagesWithCallbacks(ctx, req.Messages, nil)
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

// ReconnectToStream implements ai.ChatTransport.ReconnectToStream, matching
// the current TypeScript adapter behavior: LangSmithDeploymentTransport does
// not support resuming a stream, so it always reports an error (TS:
// `async reconnectToStream(): Promise<...> { throw new Error('Method not
// implemented.'); }`).
func (t *LangSmithDeploymentTransport) ReconnectToStream(context.Context, ai.ChatTransportReconnectToStreamRequest) (<-chan ai.UIMessageChunk, <-chan error) {
	out := closedChunkChannel()
	errs := make(chan error, 1)
	errs <- errors.New("Method not implemented.") //nolint:staticcheck // exact TS parity string (transport.ts)
	close(errs)
	return out, errs
}

// openStream calls the LangGraph Platform API the way TS RemoteGraph does
// (langgraph-sdk RunsClient.stream): a stateless POST {url}/runs/stream with
// the assistant id, the messages as input and stream modes values and
// messages-tuple (plus updates, which RemoteGraph always adds), authenticated
// with the x-api-key header. The server answers with server-sent events.
func (t *LangSmithDeploymentTransport) openStream(ctx context.Context, messages []LangChainMessage) (<-chan StreamEvent, error) {
	if t.stream != nil {
		return t.stream(ctx, messages)
	}
	if t.url == "" {
		return nil, errors.New("langchain: URL is required")
	}
	body, err := json.Marshal(map[string]interface{}{
		"input": map[string]interface{}{"messages": messages},
		"config": map[string]interface{}{
			"tags":         []interface{}{},
			"metadata":     map[string]interface{}{},
			"configurable": map[string]interface{}{},
		},
		"stream_mode":      []string{"values", "messages-tuple", "updates"},
		"stream_subgraphs": false,
		"assistant_id":     t.graphID,
		"if_not_exists":    "create",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+"/runs/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, value := range t.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if t.apiKey != "" {
		req.Header.Set("x-api-key", t.apiKey)
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return readRemoteGraphEvents(ctx, resp.Body), nil
}

// maxRemoteGraphLine bounds one SSE line (a "values" event carries the whole
// graph state).
const maxRemoteGraphLine = 64 << 20

// readRemoteGraphEvents decodes the server-sent events of a run stream and
// maps them like RemoteGraph._streamIterator: "values" and "messages" events
// become [mode, data] events, an "error" event fails the stream, and every
// other event ("metadata", "updates", ...) is dropped.
func readRemoteGraphEvents(ctx context.Context, body io.ReadCloser) <-chan StreamEvent {
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer func() { _ = body.Close() }()
		reader := bufio.NewReaderSize(body, 64<<10)
		var event string
		var data []string
		dispatch := func() bool {
			defer func() { event, data = "", nil }()
			if event == "" && len(data) == 0 {
				return true
			}
			var payload interface{}
			raw := strings.Join(data, "\n")
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				payload = raw
			}
			mode, _, _ := strings.Cut(event, "|")
			switch mode {
			case "values", "messages":
				return sendStreamEvent(ctx, out, StreamEvent{Mode: mode, Data: payload})
			case "error":
				text, ok := payload.(string)
				if !ok {
					encoded, _ := json.Marshal(payload)
					text = string(encoded)
				}
				sendStreamEvent(ctx, out, StreamEvent{Err: errors.New(text)})
				return false
			}
			return true
		}
		for {
			line, err := readSSELine(reader)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					sendStreamEvent(ctx, out, StreamEvent{Err: err})
					return
				}
				dispatch()
				return
			}
			switch {
			case line == "":
				if !dispatch() {
					return
				}
			case strings.HasPrefix(line, ":"):
			default:
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					event = value
				case "data":
					data = append(data, value)
				}
			}
		}
	}()
	return out
}

// readSSELine reads one line without its line terminator, failing on lines
// longer than maxRemoteGraphLine. It returns the last unterminated line before
// io.EOF as a line, then io.EOF.
func readSSELine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return string(line), nil
			}
			return "", err
		}
		line = append(line, chunk...)
		if len(line) > maxRemoteGraphLine {
			return "", errors.New("langchain: event stream line too long")
		}
		if !isPrefix {
			return string(line), nil
		}
	}
}

func sendStreamEvent(ctx context.Context, out chan<- StreamEvent, event StreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- event:
		return true
	}
}

func closedChunkChannel() <-chan ai.UIMessageChunk {
	out := make(chan ai.UIMessageChunk)
	close(out)
	return out
}
