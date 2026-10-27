package fx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
)

// Ports of the Gateway cases of TS fx-transport.test.ts.

const modelURL = "https://ai-gateway.vercel.sh/v4/ai/language-model"

func newTurnState() *fxTurnState {
	return &fxTurnState{ID: "test", Signature: "test", Prompt: json.RawMessage(`"test"`), Models: []*fxModelRecord{}, Tools: []*fxToolRecord{}, EmittedKeys: []string{}, Steers: []fxSteer{}}
}

type transportHarness struct {
	mu      sync.Mutex
	turn    *fxTurnState
	saves   int
	fetches int
}

func (h *transportHarness) options(network libfx.FetchFunc) transportOptions {
	return transportOptions{
		baseURL: "https://ai-gateway.vercel.sh",
		headers: map[string]string{},
		mu:      &h.mu,
		turn:    h.turn,
		save: func(context.Context) error {
			h.mu.Lock()
			h.saves++
			h.mu.Unlock()
			return nil
		},
		onRequest:     func(int, *string) {},
		beforeRelease: func(context.Context, int, bool) error { return nil },
		network: func(req *http.Request) (*http.Response, error) {
			h.mu.Lock()
			h.fetches++
			h.mu.Unlock()
			return network(req)
		},
	}
}

func (h *transportHarness) fetchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fetches
}

func serve(body string) libfx.FetchFunc {
	return func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
}

func modelRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, modelURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("ai-language-model-id", "anthropic/claude-haiku-4-5")
	return req
}

func readAll(t *testing.T, tr *transport, req *http.Request) (string, error) {
	t.Helper()
	resp, err := tr.fetch(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(resp.Body)
	return string(data), err
}

func TestTransportConvertsSavedFrameCursor(t *testing.T) {
	fixture := readFixture(t, "gateway-1.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	tr := newTransport(h.options(serve(fixture)))
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	tr.stop()
	record := h.turn.Models[0]
	record.EmittedOutput = nil
	n := 0
	for _, f := range splitFrames(record.Body) {
		if f != "" {
			n++
		}
	}
	record.EmittedParts = n
	replay := newTransport(h.options(serve(fixture)))
	if _, err := readAll(t, replay, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	if o := record.EmittedOutput; o == nil || o.Text != 5 || o.Reasoning != 0 || o.Finished == nil || !*o.Finished {
		t.Errorf("emittedOutput = %+v", o)
	}
	if h.fetchCount() != 1 {
		t.Errorf("fetches = %d", h.fetchCount())
	}
	replay.stop()
}

func TestTransportRewritesPassthroughGatewayURL(t *testing.T) {
	h := &transportHarness{turn: newTurnState()}
	var forwarded *http.Request
	var forwardedBody string
	o := h.options(func(req *http.Request) (*http.Response, error) {
		forwarded = req
		b, _ := io.ReadAll(req.Body)
		forwardedBody = string(b)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	o.baseURL = "https://gateway.example/proxy"
	o.headers = map[string]string{"x-client-app": "ai-sdk-harness-fx/test"}
	tr := newTransport(o)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://ai-gateway.vercel.sh/v4/other?query=value", strings.NewReader("request body"))
	req.Header.Set("Authorization", "Bearer test-key")
	if _, err := readAll(t, tr, req); err != nil {
		t.Fatal(err)
	}
	if forwarded.URL.String() != "https://gateway.example/proxy/v4/other?query=value" || forwarded.Method != http.MethodPost || forwardedBody != "request body" {
		t.Errorf("forwarded %s %s %q", forwarded.Method, forwarded.URL, forwardedBody)
	}
	if forwarded.Header.Get("Authorization") != "Bearer test-key" || forwarded.Header.Get("x-client-app") != "ai-sdk-harness-fx/test" {
		t.Errorf("headers = %v", forwarded.Header)
	}
	if forwarded.Context().Err() != nil {
		t.Error("signal aborted early")
	}
	cancel()
	if forwarded.Context().Err() == nil {
		t.Error("abort did not reach the forwarded request")
	}
	if len(h.turn.Models) != 0 {
		t.Error("recorded a passthrough request")
	}
	tr.stop()
}

func TestTransportScopesGatewayHeadersToGateway(t *testing.T) {
	h := &transportHarness{turn: newTurnState()}
	var forwarded *http.Request
	o := h.options(func(req *http.Request) (*http.Response, error) {
		forwarded = req
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	o.headers = map[string]string{"x-client-app": "adapter"}
	tr := newTransport(o)
	req, _ := http.NewRequest(http.MethodPut, "https://other.example/resource", strings.NewReader("override"))
	req.Header.Set("x-override", "override")
	if _, err := readAll(t, tr, req); err != nil {
		t.Fatal(err)
	}
	if forwarded.URL.String() != "https://other.example/resource" || forwarded.Method != http.MethodPut || forwarded.Header.Get("x-client-app") != "" || forwarded.Header.Get("x-override") != "override" {
		t.Errorf("forwarded %s %s %v", forwarded.Method, forwarded.URL, forwarded.Header)
	}
}

func TestTransportAwaitsBatchPublicationBeforeRelease(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	for _, replay := range []bool{false, true} {
		publishing := make(chan struct{})
		release := make(chan struct{})
		var gotIndex int
		var gotReplay bool
		o := h.options(serve(fixture))
		o.beforeRelease = func(_ context.Context, index int, r bool) error {
			gotIndex, gotReplay = index, r
			close(publishing)
			<-release
			return nil
		}
		tr := newTransport(o)
		var released bool
		var mu sync.Mutex
		bodyCh := make(chan string, 1)
		go func() {
			body, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`))
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			released = true
			mu.Unlock()
			bodyCh <- body
		}()
		<-publishing
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		if released {
			t.Error("released before the batch was published")
		}
		mu.Unlock()
		if gotIndex != 0 || gotReplay != replay {
			t.Errorf("beforeRelease(%d, %v)", gotIndex, gotReplay)
		}
		calls := tr.toolCallsAt(0)
		if len(calls) != 1 || calls[0].ToolCallID != "toolu_01KswTwrpijqi88ffraLQVsY" || calls[0].ToolName != "bash" || !strings.Contains(calls[0].Input.(string), "printf LIBFX_FIXTURE") {
			t.Errorf("tool calls = %+v", calls)
		}
		close(release)
		if body := <-bodyCh; strings.TrimSpace(body) != strings.TrimSpace(fixture) {
			t.Errorf("body differs")
		}
		tr.stop()
	}
	if h.fetchCount() != 1 {
		t.Errorf("fetches = %d", h.fetchCount())
	}
}

func TestTransportKeepsRequestIndexOnRetriedFetch(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	failed := false
	network := func(req *http.Request) (*http.Response, error) {
		if !failed {
			failed = true
			return nil, errors.New("fetch failed")
		}
		return serve(fixture)(req)
	}
	tr := newTransport(h.options(network))
	if _, err := tr.fetch(modelRequest(t, `{"prompt":[]}`)); err == nil || err.Error() != "fetch failed" {
		t.Fatalf("first fetch = %v", err)
	}
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	if tr.index() != 0 || len(h.turn.Models) != 1 {
		t.Errorf("index %d, models %d", tr.index(), len(h.turn.Models))
	}
	replay := newTransport(h.options(network))
	if _, err := readAll(t, replay, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	if h.fetchCount() != 2 {
		t.Errorf("fetches = %d", h.fetchCount())
	}
	tr.stop()
	replay.stop()
}

func TestTransportRecordsBeforeExecutableFramesAndReplays(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	o := h.options(serve(fixture))
	o.beforeRelease = func(context.Context, int, bool) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if !h.turn.Models[0].Complete || h.saves == 0 {
			t.Error("released before the response was recorded and saved")
		}
		return nil
	}
	tr := newTransport(o)
	body, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`))
	if err != nil || strings.TrimSpace(body) != strings.TrimSpace(fixture) {
		t.Fatalf("body %q, %v", body, err)
	}
	tr.stop()
	if !h.turn.Models[0].Complete || len(tr.toolCallsAt(0)) != 1 {
		t.Fatalf("record %+v, calls %d", h.turn.Models[0].Complete, len(tr.toolCallsAt(0)))
	}
	replay := newTransport(o)
	resp, err := replay.fetch(modelRequest(t, `{"prompt":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != fixture || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("replayed body differs (%s)", resp.Header.Get("Content-Type"))
	}
	if h.fetchCount() != 1 {
		t.Errorf("fetches = %d", h.fetchCount())
	}
	if a, b := tr.toolCallsAt(0), replay.toolCallsAt(0); len(a) != len(b) || a[0] != b[0] {
		t.Errorf("replayed calls %+v != %+v", b, a)
	}
	replay.stop()
}

func TestTransportDoesNotPublishWithoutTerminalEvent(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	o := h.options(serve(fixture[:strings.Index(fixture, `data: {"type":"finish"`)]))
	called := false
	o.beforeRelease = func(context.Context, int, bool) error { called = true; return nil }
	tr := newTransport(o)
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err == nil || !strings.Contains(err.Error(), "before a terminal event") {
		t.Errorf("read = %v", err)
	}
	if called || len(tr.toolCallsAt(0)) != 0 {
		t.Error("published a truncated batch")
	}
	tr.stop()
}

func TestTransportForwardsStructuredOutputAndAttribution(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	var forwarded *http.Request
	var body string
	o := h.options(func(req *http.Request) (*http.Response, error) {
		forwarded = req
		b, _ := io.ReadAll(req.Body)
		body = string(b)
		return serve(fixture)(req)
	})
	o.baseURL = "https://gateway.example"
	o.headers = map[string]string{"x-client-app": "ai-sdk-harness-fx/test"}
	o.responseFormat = json.RawMessage(`{"type":"json","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}`)
	tr := newTransport(o)
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	if forwarded.URL.String() != "https://gateway.example/v4/ai/language-model" || forwarded.Header.Get("x-client-app") != "ai-sdk-harness-fx/test" {
		t.Errorf("forwarded %s %v", forwarded.URL, forwarded.Header)
	}
	if want := `{"prompt":[],"responseFormat":{"type":"json","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}`; body != want {
		t.Errorf("body = %s", body)
	}
	tr.stop()
}

func TestTransportRefusesDivergentReplay(t *testing.T) {
	fixture := readFixture(t, "gateway-0.sse.txt")
	h := &transportHarness{turn: newTurnState()}
	tr := newTransport(h.options(serve(fixture)))
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	replay := newTransport(h.options(serve(fixture)))
	if _, err := replay.fetch(modelRequest(t, `{"prompt":["changed"]}`)); err == nil || !strings.Contains(err.Error(), "continuation diverged") {
		t.Errorf("divergent replay = %v", err)
	}
	tr.stop()
	replay.stop()
}

func TestTransportRecomputesTruncatedResponseWithNewAttempt(t *testing.T) {
	fixture := readFixture(t, "gateway-1.sse.txt")
	partial := fixture[strings.Index(fixture, "data:"):strings.Index(fixture, `data: {"type":"text-end"`)]
	h := &transportHarness{turn: newTurnState()}
	tr := newTransport(h.options(serve(partial)))
	if _, err := readAll(t, tr, modelRequest(t, `{"prompt":[]}`)); err == nil || !strings.Contains(err.Error(), "before a terminal event") {
		t.Fatalf("read = %v", err)
	}
	tr.stop()
	if h.turn.Models[0].Complete {
		t.Fatal("truncated record marked complete")
	}
	replay := newTransport(h.options(serve(fixture)))
	if _, err := readAll(t, replay, modelRequest(t, `{"prompt":[]}`)); err != nil {
		t.Fatal(err)
	}
	r := h.turn.Models[0]
	if h.fetchCount() != 2 || r.Attempt != 1 || r.EmittedOutput == nil || r.EmittedOutput.Text != 0 || r.EmittedOutput.Reasoning != 0 {
		t.Errorf("fetches %d, record %+v", h.fetchCount(), r)
	}
	replay.stop()
}

func TestTransportReplaysRecordedErrorStatus(t *testing.T) {
	h := &transportHarness{turn: newTurnState()}
	network := func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"boom"}`))}, nil
	}
	tr := newTransport(h.options(network))
	resp, err := tr.fetch(modelRequest(t, `{"prompt":[]}`))
	if err != nil || resp.StatusCode != 500 {
		t.Fatalf("resp %v, %v", resp, err)
	}
	replay := newTransport(h.options(network))
	resp, err = replay.fetch(modelRequest(t, `{"prompt":[]}`))
	if err != nil || resp.StatusCode != 500 || h.fetchCount() != 1 {
		t.Errorf("replay %v %v fetches %d", resp, err, h.fetchCount())
	}
}

func TestCredentialBrokerSwapsAndRedacts(t *testing.T) {
	b := &credentialBroker{placeholder: "fx-brokered-ph", real: "sk-real-secret"}
	h := &transportHarness{turn: newTurnState()}
	var forwarded []*http.Request
	o := h.options(func(req *http.Request) (*http.Response, error) {
		forwarded = append(forwarded, req)
		// Echo the credential, split across reads.
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(&slowReader{data: []byte("invalid key " + req.Header.Get("Authorization"))})}, nil
	})
	o.broker = b
	tr := newTransport(o)
	for _, u := range []string{"https://ai-gateway.vercel.sh/v1/models", "https://chatgpt.com/backend-api/x"} {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer fx-brokered-ph")
		body, err := readAll(t, tr, req)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body, "sk-real-secret") {
			t.Errorf("%s: the core saw the real key: %q", u, body)
		}
	}
	if got := forwarded[0].Header.Get("Authorization"); got != "Bearer sk-real-secret" {
		t.Errorf("gateway Authorization = %q", got)
	}
	if got := forwarded[1].Header.Get("Authorization"); got != "" {
		t.Errorf("non-gateway Authorization = %q", got)
	}
	// A recorded model response is redacted too.
	model := modelRequest(t, `{"prompt":[]}`)
	model.Header.Set("Authorization", "Bearer fx-brokered-ph")
	resp, err := tr.fetch(model)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(data, []byte("fx-brokered-ph")) || bytes.Contains(data, []byte("sk-real-secret")) || strings.Contains(h.turn.Models[0].Body, "sk-real-secret") {
		t.Errorf("model error body kept the key: %q / %q", data, h.turn.Models[0].Body)
	}
}

// slowReader returns one byte per Read.
type slowReader struct{ data []byte }

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

// A Gateway request whose path is itself an absolute or scheme-relative URL
// must not be rewritten to another origin: the transport would add the real
// key to a request that leaves the Gateway.
func TestTransportRewriteStaysOnGatewayOrigin(t *testing.T) {
	b := &credentialBroker{placeholder: "fx-brokered-ph", real: "sk-real-secret"}
	for _, base := range []string{"", "https://gateway.example/proxy"} {
		for _, path := range []string{"/https://chatgpt.com/steal", "///chatgpt.com/steal", "/https:%2F%2Fchatgpt.com/steal"} {
			h := &transportHarness{turn: newTurnState()}
			var forwarded []*http.Request
			o := h.options(func(req *http.Request) (*http.Response, error) {
				forwarded = append(forwarded, req)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
			})
			o.baseURL = base
			o.broker = b
			tr := newTransport(o)
			req, err := http.NewRequest(http.MethodGet, "https://ai-gateway.vercel.sh"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer fx-brokered-ph")
			_, _ = readAll(t, tr, req)
			want := defaultGatewayOrigin
			if base != "" {
				want = "https://gateway.example"
			}
			for _, f := range forwarded {
				if originOf(f.URL) != want {
					t.Errorf("base %q path %q: forwarded to %s with Authorization %q", base, path, f.URL, f.Header.Get("Authorization"))
				}
			}
			tr.stop()
		}
	}
}

// The real key is redacted from response headers as well as bodies.
func TestCredentialBrokerRedactsResponseHeaders(t *testing.T) {
	b := &credentialBroker{placeholder: "fx-brokered-ph", real: "sk-real-secret"}
	h := &transportHarness{turn: newTurnState()}
	o := h.options(func(req *http.Request) (*http.Response, error) {
		hdr := http.Header{"X-Echo": {req.Header.Get("Authorization")}, "Content-Type": {"text/event-stream"}}
		status := 200
		if req.Method == http.MethodPost {
			status = 401
		}
		return &http.Response{StatusCode: status, Header: hdr, Body: io.NopCloser(strings.NewReader("x"))}, nil
	})
	o.broker = b
	tr := newTransport(o)
	pass, _ := http.NewRequest(http.MethodGet, "https://ai-gateway.vercel.sh/v1/models", nil)
	pass.Header.Set("Authorization", "Bearer fx-brokered-ph")
	model := modelRequest(t, `{"prompt":[]}`)
	model.Header.Set("Authorization", "Bearer fx-brokered-ph")
	for _, req := range []*http.Request{pass, model} {
		resp, err := tr.fetch(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		for name, values := range resp.Header {
			for _, v := range values {
				if strings.Contains(v, "sk-real-secret") {
					t.Errorf("%s %s: header %s = %q", req.Method, req.URL, name, v)
				}
			}
		}
	}
	tr.stop()
}
