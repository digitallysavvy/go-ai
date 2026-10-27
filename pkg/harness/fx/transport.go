package fx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// Port of TS fx-transport.ts: the fetch the core's model requests go
// through. It rewrites the Gateway origin to the configured base URL, adds
// the Gateway headers, records each model response in the turn state before
// the core may act on it, replays recorded responses on a cold
// continuation, holds tool and terminal frames until the tool batch is
// published, and applies structured output.
//
// Go hardening beyond TS: the Gateway credential never enters the core. The
// core sends a per-turn placeholder, which the transport replaces with the
// real credential on Gateway requests only, and the real credential is
// redacted from every response body before the core or the state file sees
// it.

const defaultGatewayOrigin = "https://ai-gateway.vercel.sh"

var errTurnSuspended = errors.New("fx turn is suspended.") //nolint:staticcheck // matches TS SDK's exact error text

// gatewayPart is one parsed stream event.
type gatewayPart map[string]any

func (p gatewayPart) typ() string {
	s, _ := p["type"].(string)
	return s
}

// parseFxEvent mirrors TS parseFxEvent: the joined data lines of an SSE
// frame, parsed as JSON; nil for an empty frame or [DONE].
func parseFxEvent(frame string) (gatewayPart, error) {
	var lines []string
	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimLeftFunc(line[5:], unicode.IsSpace))
		}
	}
	data := strings.Join(lines, "\n")
	if data == "" || data == "[DONE]" {
		return nil, nil
	}
	if err := securejson.Check([]byte(data)); err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return nil, err
	}
	part, _ := v.(map[string]any)
	if part == nil {
		return gatewayPart{}, nil
	}
	return part, nil
}

// splitLines splits on /\r?\n/.
func splitLines(s string) []string {
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

// frameBoundary finds the first /\r?\n\r?\n/ in b: its start and end.
func frameBoundary(b []byte) (int, int) {
	for i := 0; i < len(b); i++ {
		if b[i] != '\n' {
			continue
		}
		start := i
		if i > 0 && b[i-1] == '\r' {
			start = i - 1
		}
		if i+1 < len(b) && b[i+1] == '\n' {
			return start, i + 2
		}
		if i+2 < len(b) && b[i+1] == '\r' && b[i+2] == '\n' {
			return start, i + 3
		}
	}
	return -1, -1
}

// splitFrames mirrors body.split(/\r?\n\r?\n/).
func splitFrames(body string) []string {
	var frames []string
	b := []byte(body)
	for {
		start, end := frameBoundary(b)
		if start < 0 {
			return append(frames, string(b))
		}
		frames = append(frames, string(b[:start]))
		b = b[end:]
	}
}

// transportProtocol is the provider-specific part of the transport. Only the
// Gateway protocol exists yet; codex and grok subscriptions add theirs.
type transportProtocol interface {
	label() string
	isGatewayRequest(u *url.URL) bool
	rewriteURL(u *url.URL, baseURL string) (*url.URL, error)
	isModelRequest(u *url.URL, gateway bool) bool
	prepareBody(body []byte, responseFormat json.RawMessage) ([]byte, error)
	modelID(h http.Header, body []byte) *string
	fingerprint(u *url.URL, h http.Header, body []byte) any
	isTerminal(p gatewayPart) bool
	shouldHold(p gatewayPart, terminal bool) bool
}

type gatewayProtocol struct{}

func originOf(u *url.URL) string { return u.Scheme + "://" + u.Host }

func (gatewayProtocol) label() string { return "Gateway" }

func (gatewayProtocol) isGatewayRequest(u *url.URL) bool { return originOf(u) == defaultGatewayOrigin }

func (gatewayProtocol) rewriteURL(u *url.URL, baseURL string) (*url.URL, error) {
	if originOf(u) != defaultGatewayOrigin {
		return u, nil
	}
	if baseURL == "" {
		baseURL = defaultGatewayOrigin
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid AI_GATEWAY_BASE_URL %q", baseURL)
	}
	// "./" keeps a path such as "/https://host/x" or "///host/x" a path:
	// resolved on its own it would be another origin, and the transport
	// adds the real credential to Gateway requests.
	ref, err := url.Parse("./" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if err != nil {
		return nil, err
	}
	ref.RawQuery = u.RawQuery
	target := base.ResolveReference(ref)
	if originOf(target) != originOf(base) {
		return nil, fmt.Errorf("fx Gateway request path %q leaves AI_GATEWAY_BASE_URL", u.EscapedPath())
	}
	return target, nil
}

func (gatewayProtocol) isModelRequest(u *url.URL, gateway bool) bool {
	return gateway && strings.HasSuffix(u.Path, "/ai/language-model")
}

func (gatewayProtocol) prepareBody(body []byte, responseFormat json.RawMessage) ([]byte, error) {
	var rf struct {
		Type string `json:"type"`
	}
	if len(responseFormat) == 0 || json.Unmarshal(responseFormat, &rf) != nil || rf.Type != "json" {
		return body, nil
	}
	if err := securejson.Check(body); err != nil {
		return nil, err
	}
	parsed, ok := jsonorder.Parse(body)
	obj, isObj := parsed.(*jsonorder.Object)
	if !ok || !isObj {
		return nil, errors.New("fx model request body is not a JSON object")
	}
	format, _ := jsonorder.Parse(responseFormat)
	obj.Set("responseFormat", format)
	return []byte(jsonorder.Stringify(obj)), nil
}

func (gatewayProtocol) modelID(h http.Header, _ []byte) *string {
	if v := h.Values("ai-language-model-id"); len(v) > 0 {
		s := strings.Join(v, ", ")
		return &s
	}
	return nil
}

func (p gatewayProtocol) fingerprint(u *url.URL, h http.Header, body []byte) any {
	return struct {
		Path  string  `json:"path"`
		Model *string `json:"model"`
		Body  string  `json:"body"`
	}{u.EscapedPath(), p.modelID(h, body), string(body)}
}

func (gatewayProtocol) isTerminal(p gatewayPart) bool {
	return p != nil && (p.typ() == "finish" || p.typ() == "error")
}

func (gatewayProtocol) shouldHold(p gatewayPart, terminal bool) bool {
	return p != nil && (strings.HasPrefix(p.typ(), "tool-") || terminal)
}

type fxToolCall struct {
	ToolCallID string
	ToolName   string
	Input      any
}

type rejectedResponse struct {
	modelID *string
	status  int
}

// credentialBroker swaps the core's placeholder credential for the real one
// on Gateway requests and redacts the real one from responses.
type credentialBroker struct {
	placeholder string
	real        string
}

func (b *credentialBroker) active() bool { return b != nil && b.real != "" && b.placeholder != "" }

func (b *credentialBroker) authorize(h http.Header) {
	if !b.active() {
		return
	}
	for name, values := range h {
		for i, v := range values {
			if strings.Contains(v, b.placeholder) {
				values[i] = strings.ReplaceAll(v, b.placeholder, b.real)
			}
		}
		h[name] = values
	}
}

// strip removes the placeholder from a request that leaves the Gateway, so
// nothing else learns it either.
func (b *credentialBroker) strip(h http.Header) {
	if !b.active() {
		return
	}
	for name, values := range h {
		for _, v := range values {
			if strings.Contains(v, b.placeholder) || strings.Contains(v, b.real) {
				h.Del(name)
				break
			}
		}
	}
}

func (b *credentialBroker) redact(data []byte) []byte {
	if !b.active() || !bytes.Contains(data, []byte(b.real)) {
		return data
	}
	return bytes.ReplaceAll(data, []byte(b.real), []byte(b.placeholder))
}

// redactingReader redacts the real credential from a streamed body,
// holding back a tail that could start a match split across reads.
type redactingReader struct {
	src     io.ReadCloser
	broker  *credentialBroker
	pending []byte
	out     []byte
	eof     bool
}

func (r *redactingReader) Read(p []byte) (int, error) {
	for len(r.out) == 0 {
		if r.eof {
			if len(r.pending) == 0 {
				return 0, io.EOF
			}
			r.out, r.pending = r.broker.redact(r.pending), nil
			break
		}
		buf := make([]byte, 32<<10)
		n, err := r.src.Read(buf)
		r.pending = append(r.pending, buf[:n]...)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return 0, err
			}
			r.eof = true
			continue
		}
		red := r.broker.redact(r.pending)
		keep := len(r.broker.real) - 1
		if keep > len(red) {
			keep = len(red)
		}
		r.out, r.pending = red[:len(red)-keep], append([]byte(nil), red[len(red)-keep:]...)
	}
	n := copy(p, r.out)
	r.out = r.out[n:]
	return n, nil
}

func (r *redactingReader) Close() error { return r.src.Close() }

// transportOptions configures newTransport.
type transportOptions struct {
	protocol transportProtocol
	baseURL  string
	// headers are set on Gateway requests.
	headers        map[string]string
	responseFormat json.RawMessage
	// mu guards turn; save must be called without holding it.
	mu            *sync.Mutex
	turn          *fxTurnState
	save          func(ctx context.Context) error
	onRequest     func(requestIndex int, modelID *string)
	beforeRelease func(ctx context.Context, requestIndex int, replay bool) error
	network       libfx.FetchFunc
	broker        *credentialBroker
}

type transport struct {
	o transportOptions

	// Guarded by o.mu.
	requestIndex int
	active       bool
	rejected     map[int]rejectedResponse
	toolCalls    map[int][]fxToolCall

	tasks sync.WaitGroup
}

func newTransport(o transportOptions) *transport {
	if o.protocol == nil {
		o.protocol = gatewayProtocol{}
	}
	return &transport{o: o, requestIndex: -1, active: true, rejected: map[int]rejectedResponse{}, toolCalls: map[int][]fxToolCall{}}
}

// index is the current request index (TS transport.requestIndex).
func (t *transport) index() int {
	t.o.mu.Lock()
	defer t.o.mu.Unlock()
	return t.requestIndex
}

func (t *transport) isActive() bool {
	t.o.mu.Lock()
	defer t.o.mu.Unlock()
	return t.active
}

// stop deactivates the transport and waits for its stream pumps.
func (t *transport) stop() {
	t.o.mu.Lock()
	t.active = false
	t.o.mu.Unlock()
	t.tasks.Wait()
}

// collectToolCalls reads a recorded response's complete tool calls; caller
// holds o.mu.
func (t *transport) collectToolCallsLocked(record *fxModelRecord, index int) {
	var calls []fxToolCall
	pos := map[string]int{}
	for _, frame := range splitFrames(record.Body) {
		part, err := parseFxEvent(frame)
		if err != nil || part == nil {
			continue
		}
		id, idOK := part["toolCallId"].(string)
		name, nameOK := part["toolName"].(string)
		if part.typ() == "tool-call" && part["providerExecuted"] != true && idOK && nameOK {
			call := fxToolCall{ToolCallID: id, ToolName: name, Input: part["input"]}
			if i, ok := pos[id]; ok {
				calls[i] = call
			} else {
				pos[id] = len(calls)
				calls = append(calls, call)
			}
		}
	}
	t.toolCalls[index] = calls
}

func (t *transport) rejectedAt(index int) (rejectedResponse, bool) {
	t.o.mu.Lock()
	defer t.o.mu.Unlock()
	r, ok := t.rejected[index]
	return r, ok
}

func (t *transport) toolCallsAt(index int) []fxToolCall {
	t.o.mu.Lock()
	defer t.o.mu.Unlock()
	return append([]fxToolCall(nil), t.toolCalls[index]...)
}

func cloneHeader(h http.Header) http.Header {
	out := http.Header{}
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (t *transport) outbound(ctx context.Context, method string, u *url.URL, h http.Header, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header = h
	return req, nil
}

func sseResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

// fetch is the core-facing fetch.
func (t *transport) fetch(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if !t.isActive() {
		return nil, errTurnSuspended
	}
	original := req.URL
	p := t.o.protocol
	gateway := p.isGatewayRequest(original)
	target, err := p.rewriteURL(original, t.o.baseURL)
	if err != nil {
		return nil, err
	}
	headers := cloneHeader(req.Header)
	if gateway {
		for k, v := range t.o.headers {
			headers.Set(k, v)
		}
		t.o.broker.authorize(headers)
	} else {
		t.o.broker.strip(headers)
	}
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	if method != http.MethodPost || !p.isModelRequest(original, gateway) {
		out, err := t.outbound(ctx, method, target, headers, body)
		if err != nil {
			return nil, err
		}
		resp, err := t.o.network(out)
		if err != nil {
			return nil, err
		}
		if t.o.broker.active() && resp.Body != nil {
			resp.Body = &redactingReader{src: resp.Body, broker: t.o.broker}
			resp.ContentLength = -1
		}
		return resp, nil
	}

	t.o.mu.Lock()
	t.requestIndex++
	index := t.requestIndex
	delete(t.rejected, index)
	t.o.mu.Unlock()
	undo := func(previous *fxModelRecord) {
		t.o.mu.Lock()
		if previous != nil {
			t.o.turn.Models[index] = previous
		} else if index < len(t.o.turn.Models) {
			t.o.turn.Models = append(t.o.turn.Models[:index], t.o.turn.Models[index+1:]...)
		}
		t.requestIndex--
		t.o.mu.Unlock()
		_ = t.o.save(ctx)
	}

	prepared, err := p.prepareBody(body, t.o.responseFormat)
	if err != nil {
		return nil, err
	}
	modelID := p.modelID(headers, prepared)
	fpJSON, _ := jsstr.Marshal(p.fingerprint(original, headers, prepared))
	sum := sha256.Sum256(fpJSON)
	fingerprint := hex.EncodeToString(sum[:])
	t.o.onRequest(index, modelID)

	t.o.mu.Lock()
	previous := t.recordAtLocked(index)
	if previous != nil && previous.Fingerprint != fingerprint {
		t.o.mu.Unlock()
		return nil, fmt.Errorf("fx continuation diverged at model request %d. Refusing to replay different work.", index) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if previous != nil && previous.Complete {
		if previous.Status < 200 || previous.Status >= 300 {
			b := previous.Body
			t.o.mu.Unlock()
			return sseResponse(previous.Status, nil, b), nil
		}
		// Older saved turns track emitted wire frames rather than parsed
		// output lengths; convert that cursor once.
		if previous.EmittedOutput == nil {
			out := &fxModelOutput{}
			finished := false
			var nonEmpty []string
			for _, f := range splitFrames(previous.Body) {
				if f != "" {
					nonEmpty = append(nonEmpty, f)
				}
			}
			if previous.EmittedParts < len(nonEmpty) {
				nonEmpty = nonEmpty[:previous.EmittedParts]
			}
			for _, f := range nonEmpty {
				part, _ := parseFxEvent(f)
				switch part.typ() {
				case "text-delta":
					out.Text += utf16Len(jsString(part["delta"]))
				case "reasoning-delta":
					out.Reasoning += utf16Len(jsString(part["delta"]))
				case "finish":
					finished = true
				}
			}
			out.Finished = &finished
			previous.EmittedOutput = out
		}
		t.collectToolCallsLocked(previous, index)
		replayBody := previous.Body
		status := previous.Status
		t.o.mu.Unlock()
		if err := t.o.beforeRelease(ctx, index, true); err != nil {
			return nil, err
		}
		if err := t.o.save(ctx); err != nil {
			return nil, err
		}
		if !t.isActive() {
			return nil, errTurnSuspended
		}
		return sseResponse(status, http.Header{"Content-Type": {"text/event-stream"}}, replayBody), nil
	}
	record := &fxModelRecord{Fingerprint: fingerprint, Status: 200, EmittedOutput: &fxModelOutput{}}
	if previous != nil {
		record.Attempt = previous.Attempt + 1
	}
	if index < len(t.o.turn.Models) {
		t.o.turn.Models[index] = record
	} else {
		for len(t.o.turn.Models) < index {
			t.o.turn.Models = append(t.o.turn.Models, nil)
		}
		t.o.turn.Models = append(t.o.turn.Models, record)
	}
	t.o.mu.Unlock()
	if err := t.o.save(ctx); err != nil {
		return nil, err
	}

	out, err := t.outbound(ctx, method, target, headers, prepared)
	if err != nil {
		undo(previous)
		return nil, err
	}
	resp, err := t.o.network(out)
	if err == nil && resp == nil {
		err = errors.New("fx network fetch returned no response")
	}
	if err != nil {
		undo(previous)
		return nil, err
	}
	t.o.mu.Lock()
	record.Status = resp.StatusCode
	t.o.mu.Unlock()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil || resp.Body == http.NoBody {
		var data []byte
		if resp.Body != nil {
			data, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return nil, err
			}
		}
		data = t.o.broker.redact(data)
		t.o.mu.Lock()
		record.Body = string(data)
		record.Complete = true
		t.o.mu.Unlock()
		if err := t.o.save(ctx); err != nil {
			return nil, err
		}
		h := cloneHeader(resp.Header)
		h.Del("Content-Length")
		return sseResponse(resp.StatusCode, h, string(data)), nil
	}

	pipe := newFramePipe()
	t.tasks.Add(1)
	go func() {
		defer t.tasks.Done()
		defer resp.Body.Close() //nolint:errcheck
		// The core closing its body (an aborted stream) ends the pump.
		pipe.closeWrite(t.pump(ctx, index, record, resp.Body, pipe))
	}()
	h := cloneHeader(resp.Header)
	h.Del("Content-Length")
	return &http.Response{StatusCode: resp.StatusCode, Status: resp.Status, Header: h, Body: pipe, ContentLength: -1}, nil
}

func jsString(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (t *transport) recordAtLocked(index int) *fxModelRecord {
	if index < len(t.o.turn.Models) {
		return t.o.turn.Models[index]
	}
	return nil
}

// framePipe is the body the core reads a live response from. Writes never
// block on the reader, so the pump records a response as fast as it
// arrives, whatever the core's pace (a TS TransformStream likewise queues
// writes). The record already holds the whole response, so the buffer adds
// no new bound.
type framePipe struct {
	mu      sync.Mutex
	buf     []byte
	err     error // set by closeWrite; io.EOF for a clean end
	closed  bool  // the reader closed
	changed chan struct{}
}

func newFramePipe() *framePipe { return &framePipe{changed: make(chan struct{})} }

func (p *framePipe) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *framePipe) write(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return io.ErrClosedPipe
	}
	p.buf = append(p.buf, data...)
	p.signalLocked()
	return nil
}

func (p *framePipe) closeWrite(err error) {
	if err == nil {
		err = io.EOF
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil {
		p.err = err
	}
	p.signalLocked()
}

func (p *framePipe) Read(b []byte) (int, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		if len(p.buf) > 0 {
			n := copy(b, p.buf)
			p.buf = p.buf[n:]
			p.mu.Unlock()
			return n, nil
		}
		if p.err != nil {
			err := p.err
			p.mu.Unlock()
			return 0, err
		}
		wait := p.changed
		p.mu.Unlock()
		<-wait
	}
}

func (p *framePipe) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.buf = nil
	p.signalLocked()
	return nil
}

// pump copies the live stream to the core frame by frame, recording it and
// holding tool and terminal frames until the batch is published. A read
// that ends the stream is recorded, and the record completed and saved,
// before its frames are released, so the core cannot act on the end of a
// response its record does not hold yet.
func (t *transport) pump(ctx context.Context, index int, record *fxModelRecord, src io.Reader, out *framePipe) error {
	p := t.o.protocol
	var pending []byte
	holding, terminal := false, false
	var held []byte
	buf := make([]byte, 32<<10)
	for {
		n, rerr := src.Read(buf)
		if !t.isActive() {
			return errTurnSuspended
		}
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
		pending = append(pending, buf[:n]...)
		var release []byte
		for {
			start, end := frameBoundary(pending)
			if start < 0 {
				break
			}
			frame := t.o.broker.redact(append([]byte(nil), pending[:end]...))
			pending = pending[end:]
			t.o.mu.Lock()
			record.Body += string(frame)
			t.o.mu.Unlock()
			part, err := parseFxEvent(string(frame))
			if err != nil {
				return err
			}
			terminalPart := p.isTerminal(part)
			if terminalPart {
				terminal = true
			}
			if p.shouldHold(part, terminalPart) {
				holding = true
			}
			if holding {
				held = append(held, frame...)
			} else {
				release = append(release, frame...)
			}
		}
		if errors.Is(rerr, io.EOF) {
			if err := t.completeRecord(ctx, record, pending, terminal); err != nil {
				return err
			}
			if err := out.write(release); err != nil {
				return err
			}
			break
		}
		if len(release) > 0 {
			if err := out.write(release); err != nil {
				return err
			}
		}
	}
	t.o.mu.Lock()
	t.collectToolCallsLocked(record, index)
	t.o.mu.Unlock()
	if err := t.o.beforeRelease(ctx, index, false); err != nil {
		return err
	}
	if err := t.o.save(ctx); err != nil {
		return err
	}
	if !t.isActive() {
		return errTurnSuspended
	}
	if len(held) > 0 {
		if err := out.write(held); err != nil {
			return err
		}
	}
	return nil
}

// completeRecord validates the end of a live stream and marks its record
// complete.
func (t *transport) completeRecord(ctx context.Context, record *fxModelRecord, pending []byte, terminal bool) error {
	p := t.o.protocol
	if len(pending) > 0 {
		t.o.mu.Lock()
		record.Body += string(t.o.broker.redact(pending))
		t.o.mu.Unlock()
	}
	if strings.TrimSpace(string(pending)) != "" {
		return fmt.Errorf("%s stream ended with an incomplete event.", p.label()) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if !terminal {
		return fmt.Errorf("%s stream ended before a terminal event.", p.label()) //nolint:staticcheck // matches TS SDK's exact error text
	}
	t.o.mu.Lock()
	record.Complete = true
	t.o.mu.Unlock()
	return t.o.save(ctx)
}
