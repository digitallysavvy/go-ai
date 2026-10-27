package libfx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tetratelabs/wazero/api"
)

// pollInterval is how long a stream import waits before answering "not
// ready", so the core regains control to check cancellation and steering
// (fx-sdk.js raceWithTimeout(..., 50, ...)).
const pollInterval = 50 * time.Millisecond

const streamChunkBytes = 32 << 10

// DefaultAllowedOrigins are the origins the default fetch lets the core
// reach: the AI Gateway and the codex and grok subscription endpoints. TS
// uses plain fetch with no allowlist; this is a Go hardening.
var DefaultAllowedOrigins = []string{
	"https://ai-gateway.vercel.sh",
	"https://chatgpt.com",
	"https://cli-chat-proxy.grok.com",
	"https://api.x.ai",
	"https://auth.openai.com",
	"https://auth.x.ai",
}

// AllowlistFetch returns a FetchFunc that sends requests whose origin is in
// origins through client and refuses every other origin.
func AllowlistFetch(client *http.Client, origins []string) FetchFunc {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(req *http.Request) (*http.Response, error) {
		origin := req.URL.Scheme + "://" + req.URL.Host
		if !allowed[origin] {
			return nil, fmt.Errorf("fx runtime: request to %s is not allowed", origin)
		}
		return client.Do(req)
	}
}

type chunkResult struct {
	data []byte
	err  error
}

// stream is one fx_http_stream_open fetch. The fetch and body pump run on
// their own goroutine; the guest polls with status and next.
type stream struct {
	ctx     context.Context
	release func()

	ready chan struct{} // closed once resp or err is set
	resp  *http.Response
	err   error

	chunks chan chunkResult // body chunks, produced on demand

	// Guest-goroutine state.
	leftover []byte
	readErr  error
	eof      bool
}

type httpHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// buildRequest decodes the guest's method, URL, JSON header list and body.
func buildRequest(ctx context.Context, method, rawURL string, headersJSON, body []byte) (*http.Request, error) {
	var headers []httpHeader
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &headers); err != nil {
			return nil, fmt.Errorf("fx runtime: headers: %w", err)
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("fx runtime: invalid request URL %q", rawURL)
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	for _, h := range headers {
		req.Header.Add(h.Name, h.Value)
	}
	return req, nil
}

// readRequestArgs copies four (ptr,len) pairs: method, url, headers, body.
func readRequestArgs(mod api.Module, s []uint64) (method, rawURL string, headers, body []byte, ok bool) {
	var m, u []byte
	if m, ok = memCopy(mod, u32(s[0]), u32(s[1])); !ok {
		return
	}
	if u, ok = memCopy(mod, u32(s[2]), u32(s[3])); !ok {
		return
	}
	if headers, ok = memCopy(mod, u32(s[4]), u32(s[5])); !ok {
		return
	}
	if body, ok = memCopy(mod, u32(s[6]), u32(s[7])); !ok {
		return
	}
	return string(m), string(u), headers, body, true
}

func streamOpen(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	method, rawURL, headers, body, ok := readRequestArgs(mod, s)
	ectx, release := inst.newEffect()
	st := &stream{ctx: ectx, release: release, ready: make(chan struct{}), chunks: make(chan chunkResult, 1)}
	inst.mu.Lock()
	inst.nextHandle++
	handle := inst.nextHandle
	inst.streams[handle] = st
	inst.mu.Unlock()
	go st.run(inst.fetch, method, rawURL, headers, body, ok)
	ret(s, handle)
}

func (st *stream) run(fetch FetchFunc, method, rawURL string, headers, body []byte, argsOK bool) {
	var resp *http.Response
	var err error
	if !argsOK {
		err = errors.New("fx runtime: request arguments are out of bounds")
	} else {
		var req *http.Request
		req, err = buildRequest(st.ctx, method, rawURL, headers, body)
		if err == nil {
			resp, err = fetch(req)
		}
	}
	if err == nil && st.ctx.Err() != nil {
		resp.Body.Close() //nolint:errcheck
		resp, err = nil, st.ctx.Err()
	}
	st.resp, st.err = resp, err
	close(st.ready)
	if resp == nil {
		return
	}
	defer resp.Body.Close() //nolint:errcheck
	for {
		buf := make([]byte, streamChunkBytes)
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			select {
			case st.chunks <- chunkResult{data: buf[:n]}:
			case <-st.ctx.Done():
				return
			}
		}
		if rerr != nil {
			if st.ctx.Err() != nil {
				rerr = st.ctx.Err()
			}
			select {
			case st.chunks <- chunkResult{err: rerr}:
			case <-st.ctx.Done():
			}
			return
		}
	}
}

func (inst *instance) stream(handle int32) *stream {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.streams[handle]
}

// aborted reports whether a failure came from aborting the effect.
func (st *stream) aborted() bool { return st.ctx.Err() != nil }

func (st *stream) failureCode() int32 {
	if st.aborted() {
		return -2
	}
	return -1
}

// streamStatus: 1 with the status written, 0 not ready after up to 50 ms,
// -1 error or unknown handle, -2 aborted.
func streamStatus(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	st := inst.stream(api.DecodeI32(s[0]))
	if st == nil {
		ret(s, -1)
		return
	}
	settled := func() int32 {
		select {
		case <-st.ready:
		default:
			return 0
		}
		if st.err != nil {
			return st.failureCode()
		}
		if !memWriteU16(mod, u32(s[1]), uint16(st.resp.StatusCode)) {
			return -1
		}
		return 1
	}
	if code := settled(); code != 0 {
		ret(s, code)
		return
	}
	t := time.NewTimer(pollInterval)
	defer t.Stop()
	select {
	case <-st.ready:
	case <-t.C:
	case <-inst.runCtx.Done():
	}
	ret(s, settled())
}

// streamNext: >0 bytes copied (leftovers kept), 0 EOF, -1 error, -2
// aborted, -3 no chunk within 50 ms.
func streamNext(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	st := inst.stream(api.DecodeI32(s[0]))
	if st == nil {
		ret(s, -1)
		return
	}
	outPtr, outCap := u32(s[1]), u32(s[2])
	copyOut := func(chunk []byte) int32 {
		n := len(chunk)
		if uint32(n) > outCap {
			n = int(outCap)
		}
		if !memWrite(mod, outPtr, chunk[:n]) {
			return -1
		}
		st.leftover = chunk[n:]
		return int32(n)
	}
	consume := func(r *chunkResult) (int32, bool) {
		if len(st.leftover) > 0 {
			return copyOut(st.leftover), true
		}
		if st.readErr != nil {
			return st.failureCode(), true
		}
		if st.eof {
			return 0, true
		}
		if r == nil {
			return 0, false
		}
		if r.err != nil {
			if errors.Is(r.err, io.EOF) {
				st.eof = true
				return 0, true
			}
			st.readErr = r.err
			return st.failureCode(), true
		}
		return copyOut(r.data), true
	}
	if code, done := consume(nil); done {
		ret(s, code)
		return
	}
	select {
	case <-st.ready:
	default:
		ret(s, 0)
		return
	}
	if st.resp == nil {
		ret(s, st.failureCode())
		return
	}
	t := time.NewTimer(pollInterval)
	defer t.Stop()
	select {
	case r := <-st.chunks:
		code, _ := consume(&r)
		ret(s, code)
	case <-st.ctx.Done():
		ret(s, -2)
	case <-t.C:
		ret(s, -3)
	}
}

func streamClose(ctx context.Context, _ api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	handle := api.DecodeI32(s[0])
	inst.mu.Lock()
	st := inst.streams[handle]
	delete(inst.streams, handle)
	inst.mu.Unlock()
	if st != nil {
		st.release()
	}
}

// httpRequest is the buffered fetch (model catalog, OAuth). It returns the
// body length after writing the u16 status; -2 when the body exceeds the
// guest's buffer; -1 on error or abort.
func httpRequest(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	method, rawURL, headers, body, ok := readRequestArgs(mod, s)
	if !ok {
		ret(s, -1)
		return
	}
	statusOut, respPtr, respCap := u32(s[8]), u32(s[9]), u32(s[10])
	ectx, release := inst.newEffect()
	defer release()
	type result struct {
		status int
		body   []byte
		tooBig bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, err := buildRequest(ectx, method, rawURL, headers, body)
		if err != nil {
			done <- result{err: err}
			return
		}
		resp, err := inst.fetch(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close() //nolint:errcheck
		data, err := io.ReadAll(io.LimitReader(resp.Body, int64(respCap)+1))
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{status: resp.StatusCode, body: data, tooBig: int64(len(data)) > int64(respCap)}
	}()
	var r result
	select {
	case r = <-done:
	case <-ectx.Done():
		ret(s, -1)
		return
	}
	if r.err != nil || ectx.Err() != nil {
		ret(s, -1)
		return
	}
	if !memWriteU16(mod, statusOut, uint16(r.status)) {
		ret(s, -1)
		return
	}
	if r.tooBig {
		ret(s, -2)
		return
	}
	if !memWrite(mod, respPtr, r.body) {
		ret(s, -1)
		return
	}
	ret(s, int32(len(r.body)))
}
