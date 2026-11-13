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
	"sort"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"

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
	streamEvents, err := t.openStream(ctx, baseMessages, langGraphRawToolArgs(messages))
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
func (t *LangSmithDeploymentTransport) openStream(ctx context.Context, messages []LangChainMessage, rawArgs map[string]interface{}) (<-chan StreamEvent, error) {
	if t.stream != nil {
		return t.stream(ctx, messages)
	}
	body, err := langGraphRunStreamBody(messages, t.graphID, rawArgs)
	if err != nil {
		return nil, err
	}
	// langgraph-sdk prepareFetchOptions: new URL(`${apiUrl}${path}`), so the
	// request goes to the WHATWG serialization and a URL that does not parse
	// fails with the URL constructor's TypeError.
	endpoint, err := urlnorm.Href(t.url + "/runs/stream")
	if err != nil {
		return nil, errors.New("Invalid URL") //nolint:staticcheck // exact TS text (TypeError: Invalid URL)
	}
	client := internalhttp.WithFetchRedirects(t.httpClient)
	// langgraph-sdk AsyncCaller.fetch for the POST: up to 4 retries of a
	// connection failure (a POST is not idempotent, so other network errors
	// and HTTP errors are not retried); a network failure that is not
	// retried is a ConnectionError, a non-2xx status an HTTPError.
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		// langgraph-sdk sets no Accept header, so a configured one is sent
		// as given; content-type and x-api-key win over configured headers
		// (prepareFetchOptions / BaseClient mergeHeaders).
		req.Header.Set("Accept", "text/event-stream")
		for name, value := range t.headers {
			req.Header.Set(name, value)
		}
		req.Header.Set("Content-Type", "application/json")
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
		// streamWithRetry makeRequest: a response without a body (fetch gives
		// a null body for 204 and 205) fails before the content type check.
		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusResetContent {
			_ = resp.Body.Close()
			return nil, errors.New("Expected response body from stream endpoint") //nolint:staticcheck // exact TS text
		}
		// streamWithRetry: a response that is not an event stream fails.
		if contentType, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";"); contentType != "" && !strings.Contains(contentType, "text/event-stream") {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("Expected response header Content-Type to contain 'text/event-stream', got '%s'", contentType) //nolint:staticcheck // exact TS text
		}
		return readRemoteGraphEvents(ctx, resp.Body), nil
	}
}

// langGraphDefaultRecursionLimit is @langchain/core ensureConfig's
// recursionLimit, which Runnable.stream sets before RemoteGraph sends it as
// config.recursion_limit.
const langGraphDefaultRecursionLimit = 25

// langGraphRunConfig is RemoteGraph._sanitizeConfig's result for the
// transport's call (no configured tags, metadata or configurable).
type langGraphRunConfig struct {
	Tags           []interface{}          `json:"tags"`
	Metadata       map[string]interface{} `json:"metadata"`
	Configurable   map[string]interface{} `json:"configurable"`
	RecursionLimit int                    `json:"recursion_limit"`
}

// langGraphRunStreamRequest is the langgraph-sdk RunsClient.stream JSON body
// in its key order (undefined fields dropped).
type langGraphRunStreamRequest struct {
	Input struct {
		Messages []*jsonorder.Object `json:"messages"`
	} `json:"input"`
	Config          langGraphRunConfig `json:"config"`
	StreamMode      []string           `json:"stream_mode"`
	StreamSubgraphs bool               `json:"stream_subgraphs"`
	AssistantID     string             `json:"assistant_id"`
	IfNotExists     string             `json:"if_not_exists"`
}

// langGraphRunStreamBody builds the run stream body TS RemoteGraph sends for
// LangSmithDeploymentTransport.sendMessages: streamMode ["values",
// "messages"] becomes ["values", "messages-tuple", "updates"], and each
// message is RemoteGraph's _serializeInputs form.
func langGraphRunStreamBody(messages []LangChainMessage, graphID string, rawArgs map[string]interface{}) ([]byte, error) {
	var req langGraphRunStreamRequest
	req.Input.Messages = make([]*jsonorder.Object, 0, len(messages))
	for _, msg := range messages {
		req.Input.Messages = append(req.Input.Messages, langGraphWireMessage(msg, rawArgs))
	}
	req.Config = langGraphRunConfig{
		Tags:           []interface{}{},
		Metadata:       map[string]interface{}{},
		Configurable:   map[string]interface{}{},
		RecursionLimit: langGraphDefaultRecursionLimit,
	}
	req.StreamMode = []string{"values", "messages-tuple", "updates"}
	req.AssistantID = graphID
	req.IfNotExists = "create"
	return jsonstr.Marshal(req)
}

// langGraphRawToolArgs maps each tool call id to the ordered tree of its raw
// input. TS args is the part's input object, whose keys keep the order of the
// JSON it came from; the converted args are a Go map, so the raw input gives
// the order back.
func langGraphRawToolArgs(messages []types.Message) map[string]interface{} {
	out := map[string]interface{}{}
	for _, message := range messages {
		if message.Role != types.RoleAssistant {
			continue
		}
		for _, part := range message.Content {
			if call, ok := part.(types.ToolCallContent); ok && call.Input != "" {
				if tree, ok := jsonorder.Parse([]byte(call.Input)); ok {
					out[call.ToolCallID] = tree
				}
			}
		}
	}
	return out
}

// langGraphWireMessage is RemoteGraph _serializeInputs for a message:
// {...message.toDict().data, role: message.getType()}. toDict().data has no
// "type" key (the message type travels as role), and its keys follow the
// message constructor: tool_call_id first for a tool message, then content,
// the AI message's tool_calls and invalid_tool_calls, additional_kwargs and
// response_metadata, then role.
func langGraphWireMessage(msg LangChainMessage, rawArgs map[string]interface{}) *jsonorder.Object {
	lead := []string{"tool_call_id", "content", "tool_calls", "invalid_tool_calls"}
	tail := []string{"additional_kwargs", "response_metadata", "role"}
	out := langGraphOrdered(msg, lead, tail, "type")
	if _, ok := msg["role"]; !ok {
		out.Set("role", msg["type"])
	}
	if content, ok := msg["content"].([]interface{}); ok {
		out.Set("content", langGraphOrderedList(content, []string{"type", "text", "url", "data", "mimeType", "filename"}))
	}
	if calls, ok := msg["tool_calls"].([]interface{}); ok {
		ordered := langGraphOrderedList(calls, []string{"id", "name", "args"})
		for _, call := range ordered {
			o, ok := call.(*jsonorder.Object)
			if !ok {
				continue
			}
			id, _ := o.Get("id")
			idText, _ := id.(string)
			if hint, found := rawArgs[idText]; found {
				if args, has := o.Get("args"); has {
					o.Set("args", jsonorder.Hinted{V: args, Hint: hint})
				}
			}
		}
		out.Set("tool_calls", ordered)
	}
	return out
}

// langGraphOrdered copies m in key order: lead keys, the other keys sorted,
// then tail keys; drop is left out.
func langGraphOrdered(m map[string]interface{}, lead, tail []string, drop string) *jsonorder.Object {
	out := jsonorder.New()
	placed := map[string]bool{}
	if drop != "" {
		placed[drop] = true
	}
	for _, k := range append(append([]string(nil), lead...), tail...) {
		placed[k] = true
	}
	for _, k := range lead {
		if v, ok := m[k]; ok {
			out.Set(k, v)
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !placed[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		out.Set(k, m[k])
	}
	for _, k := range tail {
		if v, ok := m[k]; ok {
			out.Set(k, v)
		}
	}
	return out
}

// langGraphOrderedList orders the object entries of a list (content blocks,
// tool calls) by the TS literal key order.
func langGraphOrderedList(list []interface{}, lead []string) []interface{} {
	out := make([]interface{}, len(list))
	for i, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			out[i] = langGraphOrdered(m, lead, nil, "")
			continue
		}
		out[i] = item
	}
	return out
}

// maxRemoteGraphLine bounds one SSE line (a "values" event carries the whole
// graph state).
const maxRemoteGraphLine = 64 << 20

// readRemoteGraphEvents decodes the server-sent events of a run stream the
// way langgraph-sdk does (utils/sse.js BytesLineDecoder + SSEDecoder: CR, LF
// and CRLF end a line, a leading BOM is dropped, the data lines are joined
// and parsed as JSON, empty or whitespace-only data is null, invalid JSON
// fails the stream) and maps them like RemoteGraph._streamIterator
// (pregel/remote.js:337-369): an "updates" event that carries __interrupt__
// fails the stream with a *GraphInterruptError (GraphInterrupt), an "error"
// event fails it, "values" and "messages" events become [mode, data] events
// (a namespaced "mode|ns" event only for "messages", since subgraphs are not
// requested), and every other event ("metadata", ...) is dropped.
func readRemoteGraphEvents(ctx context.Context, body io.ReadCloser) <-chan StreamEvent {
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer func() { _ = body.Close() }()
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64<<10), maxRemoteGraphLine)
		scanner.Split(scanSSELines)
		var event string
		var data []string
		firstLine := true
		dispatch := func() bool {
			defer func() { event, data = "", nil }()
			if event == "" && len(data) == 0 {
				return true
			}
			var payload interface{}
			if raw := strings.Join(data, "\n"); len(data) > 0 && !sseBlankData.MatchString(raw) {
				if err := jsonexact.Unmarshal([]byte(raw), &payload); err != nil {
					sendStreamEvent(ctx, out, StreamEvent{Err: fmt.Errorf("langchain: invalid event data: %w", err)})
					return false
				}
			}
			mode, _, namespaced := strings.Cut(event, "|")
			switch {
			case strings.HasPrefix(event, "updates"):
				// RemoteGraph throws GraphInterrupt for an updates event that
				// carries __interrupt__, null included
				// (`data?.__interrupt__ !== undefined`, remote.js:356).
				if m, ok := payload.(map[string]interface{}); ok {
					if interrupts, found := m["__interrupt__"]; found {
						interrupt := newGraphInterrupt(interrupts)
						// The message keeps the wire key order, as
						// JSON.stringify of the parsed data does.
						if tree, ok := jsonorder.Parse([]byte(strings.Join(data, "\n"))); ok {
							if ordered, ok := jsonorder.Get(tree, "__interrupt__"); ok {
								interrupt.message = langGraphIndent(jsonorder.Stringify(ordered))
							}
						}
						sendStreamEvent(ctx, out, StreamEvent{Err: interrupt})
						return false
					}
				}
				return true
			case strings.HasPrefix(event, "error"):
				// RemoteException: the string data, else JSON.stringify of
				// the parsed data (wire key order kept).
				text, ok := payload.(string)
				if !ok {
					encoded, _ := jsonstr.Marshal(payload)
					text = string(encoded)
					if tree, parsed := jsonorder.Parse([]byte(strings.Join(data, "\n"))); parsed {
						text = jsonorder.Stringify(tree)
					}
				}
				sendStreamEvent(ctx, out, StreamEvent{Err: errors.New(text)})
				return false
			case mode != "values" && mode != "messages":
				return true
			case namespaced && mode != "messages":
				return true
			}
			return sendStreamEvent(ctx, out, StreamEvent{Mode: mode, Data: payload})
		}
		for scanner.Scan() {
			line := scanner.Text()
			if firstLine {
				firstLine = false
				line = strings.TrimPrefix(line, "\ufeff")
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
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				err = errors.New("langchain: event stream line too long")
			}
			sendStreamEvent(ctx, out, StreamEvent{Err: err})
			return
		}
		dispatch()
	}()
	return out
}

// sseBlankData is langgraph-sdk decodeArraysToJson's null body
// (/^[\x09-\x0d\x20]*$/).
var sseBlankData = regexp.MustCompile(`^[\x09-\x0d\x20]*$`)

// scanSSELines splits at CR, LF or CRLF (langgraph-sdk BytesLineDecoder); a
// trailing unterminated line is a line.
func scanSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		switch b {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}
				return i + 1, data[:i], nil
			}
			if atEOF {
				return i + 1, data[:i], nil
			}
			// A CR at the end of the buffer may be the first half of CRLF.
			return 0, nil, nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
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
	// jsonstr.Marshal is JSON.stringify (a non-finite number is null), then
	// indented as JSON.stringify(value, null, 2).
	message := ""
	if compact, err := jsonstr.Marshal(interrupts); err == nil {
		message = langGraphIndent(string(compact))
	}
	return &GraphInterruptError{Interrupts: interrupts, message: message}
}

// langGraphIndent is JSON.stringify(value, null, 2) for compact JSON text.
func langGraphIndent(compact string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(compact), "", "  "); err != nil {
		return compact
	}
	return buf.String()
}

// Error returns JSON.stringify(interrupts, null, 2), as the TS message.
func (e *GraphInterruptError) Error() string { return e.message }
