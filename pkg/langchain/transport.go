package langchain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
	// langgraph-sdk BaseClient: apiUrl.replace(/\/$/, "") (one trailing
	// slash) || "http://localhost:8123".
	url := strings.TrimSuffix(options.URL, "/")
	if url == "" {
		url = defaultLangGraphAPIURL
	}
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = langGraphAPIKeyFromEnv()
	}
	return &LangSmithDeploymentTransport{
		url:        url,
		apiKey:     apiKey,
		graphID:    graphID,
		httpClient: client,
		headers:    options.Headers,
		stream:     options.Stream,
	}
}

// defaultLangGraphAPIURL is the langgraph-sdk Client default API URL.
const defaultLangGraphAPIURL = "http://localhost:8123"

// langGraphEnvQuote strips one leading and one trailing quote.
var langGraphEnvQuote = regexp.MustCompile(`^["']|["']$`)

// langGraphAPIKeyFromEnv mirrors langgraph-sdk getApiKey without an explicit
// key: the first non-empty LANGGRAPH_API_KEY, LANGSMITH_API_KEY or
// LANGCHAIN_API_KEY, trimmed, with one leading and one trailing quote
// removed (client/base.js getApiKey).
func langGraphAPIKeyFromEnv() string {
	for _, prefix := range []string{"LANGGRAPH", "LANGSMITH", "LANGCHAIN"} {
		if v := os.Getenv(prefix + "_API_KEY"); v != "" {
			return langGraphEnvQuote.ReplaceAllString(strings.TrimSpace(v), "")
		}
	}
	return ""
}

// langGraphMaxRetries is the langgraph-sdk AsyncCaller retry count.
const langGraphMaxRetries = 4

// langGraphRetryDelay is p-retry's randomized exponential backoff
// (minTimeout 1000ms, factor 2, randomize: a factor in [1, 2)) before
// retry attempt n (1-based). A variable so tests can shorten it.
var langGraphRetryDelay = func(n int) time.Duration {
	base := float64(time.Second) * float64(int64(1)<<(n-1))
	return time.Duration((rand.Float64() + 1) * base) //nolint:gosec // backoff jitter, not security
}

// LangGraphConnectionError is the langgraph-sdk ConnectionError: the run
// stream request failed on the network (after the connection retries).
// Unwrap returns the transport error.
type LangGraphConnectionError struct {
	Err error
}

// Error returns the TS message; undici reports every network failure as
// "fetch failed".
func (e *LangGraphConnectionError) Error() string {
	return "Unable to connect to LangGraph server. Please ensure the server is running and accessible. Original error: fetch failed"
}

// ErrorName is the TS error name.
func (e *LangGraphConnectionError) ErrorName() string { return "ConnectionError" }

func (e *LangGraphConnectionError) Unwrap() error { return e.Err }

// LangGraphHTTPError is the langgraph-sdk HTTPError for a non-2xx response:
// "HTTP <status>: <body text>".
type LangGraphHTTPError struct {
	Status int
	Text   string
}

func (e *LangGraphHTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Text) }

// ErrorName is the TS error name.
func (e *LangGraphHTTPError) ErrorName() string { return "HTTPError" }

// isLangGraphConnectError mirrors async_caller isConnectError: the
// connection could not be opened (refused, DNS failure, unreachable host or
// network, connect timeout).
func isLangGraphConnectError(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// maxLangGraphErrorBody bounds the HTTP error body kept in the error text.
const maxLangGraphErrorBody = 1 << 20

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
	client := internalhttp.WithFetchRedirects(t.httpClient)
	// langgraph-sdk AsyncCaller.fetch for the POST: up to 4 retries of a
	// connection failure (a POST is not idempotent, so other network errors
	// and HTTP errors are not retried); a network failure that is not
	// retried is a ConnectionError, a non-2xx status an HTTPError.
	for attempt := 0; ; attempt++ {
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
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			if attempt < langGraphMaxRetries && isLangGraphConnectError(err) {
				timer := time.NewTimer(langGraphRetryDelay(attempt + 1))
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return nil, &LangGraphConnectionError{Err: err}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			defer func() { _ = resp.Body.Close() }()
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxLangGraphErrorBody))
			text := string(data)
			if readErr != nil {
				// HTTPError.fromResponse falls back to statusText.
				text = strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprint(resp.StatusCode)))
			}
			return nil, &LangGraphHTTPError{Status: resp.StatusCode, Text: text}
		}
		// streamWithRetry: a response that is not an event stream fails.
		if contentType, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";"); contentType != "" && !strings.Contains(contentType, "text/event-stream") {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("Expected response header Content-Type to contain 'text/event-stream', got '%s'", contentType) //nolint:staticcheck // exact TS text
		}
		return readRemoteGraphEvents(ctx, resp.Body), nil
	}
}

// maxRemoteGraphLine bounds one SSE line (a "values" event carries the whole
// graph state).
const maxRemoteGraphLine = 64 << 20

// readRemoteGraphEvents decodes the server-sent events of a run stream and
// maps them like RemoteGraph._streamIterator: "values" and "messages" events
// become [mode, data] events, an "error" event fails the stream, and every
// other event ("metadata", ...) is dropped. An "updates" event that carries
// __interrupt__ fails the stream with a *GraphInterruptError (GraphInterrupt).
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
			case "updates":
				// RemoteGraph._streamIterator throws GraphInterrupt for an
				// updates event that carries __interrupt__, null included
				// (`data?.__interrupt__ !== undefined`, remote.js:356).
				if m, ok := payload.(map[string]interface{}); ok {
					if interrupts, found := m["__interrupt__"]; found {
						sendStreamEvent(ctx, out, StreamEvent{Err: newGraphInterrupt(interrupts)})
						return false
					}
				}
				return true
			case "error":
				text, ok := payload.(string)
				if !ok {
					encoded, _ := jsonstr.Marshal(payload)
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

// GraphInterruptError is the TS GraphInterrupt thrown by RemoteGraph when an
// "updates" stream event carries __interrupt__. Interrupts holds the raw value.
type GraphInterruptError struct {
	Interrupts interface{}
	message    string
}

func newGraphInterrupt(interrupts interface{}) *GraphInterruptError {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	message := ""
	if err := enc.Encode(interrupts); err == nil {
		message = strings.TrimSuffix(buf.String(), "\n")
	}
	return &GraphInterruptError{Interrupts: interrupts, message: message}
}

// Error returns JSON.stringify(interrupts, null, 2), as the TS message.
func (e *GraphInterruptError) Error() string { return e.message }
