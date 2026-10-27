package fx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// fakeSandbox is an in-memory sandbox: files, and a command handler with
// the HOME and mv commands the fx state store uses built in.
type fakeSandbox struct {
	mu       sync.Mutex
	files    map[string]string
	commands []providerutils.SandboxProcessOptions
	home     string
	// handle answers other commands; nil returns exit 0 with no output.
	handle func(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error)
	writes []providerutils.SandboxWriteTextFileOptions
	reads  []string
}

func newFakeSandbox() *fakeSandbox {
	return &fakeSandbox{files: map[string]string{}, home: "/home/sandbox"}
}

var quotedArg = regexp.MustCompile(`'([^']+)'`)

func (f *fakeSandbox) Description() string { return "fake sandbox" }

func (f *fakeSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	f.mu.Lock()
	f.commands = append(f.commands, opts)
	handle := f.handle
	f.mu.Unlock()
	switch {
	case strings.Contains(opts.Command, `"$HOME"`):
		return providerutils.SandboxRunResult{Stdout: base64.StdEncoding.EncodeToString([]byte(f.home))}, nil
	case strings.HasPrefix(opts.Command, "mv --"):
		m := quotedArg.FindAllStringSubmatch(opts.Command, -1)
		f.mu.Lock()
		f.files[m[1][1]] = f.files[m[0][1]]
		delete(f.files, m[0][1])
		f.mu.Unlock()
		return providerutils.SandboxRunResult{}, nil
	case strings.HasPrefix(opts.Command, "mkdir -p --"):
		return providerutils.SandboxRunResult{}, nil
	case opts.Command == "pwd":
		return providerutils.SandboxRunResult{Stdout: "/workspace"}, nil
	}
	if handle != nil {
		return handle(ctx, opts)
	}
	return providerutils.SandboxRunResult{}, nil
}

func (f *fakeSandbox) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("spawn is not supported")
}

func (f *fakeSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not supported")
}

func (f *fakeSandbox) ReadBinaryFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("not supported")
}

func (f *fakeSandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, opts.Path)
	v, ok := f.files[opts.Path]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (f *fakeSandbox) WriteFile(context.Context, string, io.Reader) error {
	return errors.New("not supported")
}

func (f *fakeSandbox) WriteBinaryFile(context.Context, string, []byte) error {
	return errors.New("not supported")
}

func (f *fakeSandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, opts)
	f.files[opts.Path] = opts.Content
	return nil
}

func (f *fakeSandbox) allFiles() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, v := range f.files {
		b.WriteString(v)
		b.WriteString("\n")
	}
	return b.String()
}

func (f *fakeSandbox) stateFile(t *testing.T) (string, string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range f.files {
		if strings.HasSuffix(k, "/state.json") {
			return k, v
		}
	}
	t.Fatal("no state.json")
	return "", ""
}

func (f *fakeSandbox) commandList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.commands))
	for i, c := range f.commands {
		out[i] = c.Command
	}
	return out
}

// partLog collects emitted parts.
type partLog struct {
	mu    sync.Mutex
	parts []harness.StreamPart
	on    func(harness.StreamPart)
}

func (l *partLog) emit(p harness.StreamPart) {
	l.mu.Lock()
	l.parts = append(l.parts, p)
	on := l.on
	l.mu.Unlock()
	if on != nil {
		on(p)
	}
}

func (l *partLog) snapshot() []harness.StreamPart {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]harness.StreamPart(nil), l.parts...)
}

func (l *partLog) ofType(typ string) []harness.StreamPart {
	var out []harness.StreamPart
	for _, p := range l.snapshot() {
		if p.PartType() == typ {
			out = append(out, p)
		}
	}
	return out
}

// waitFor waits until at least n parts of typ have been emitted.
func (l *partLog) waitFor(t *testing.T, typ string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(l.ofType(typ)) < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d %s parts", n, typ)
		}
		time.Sleep(time.Millisecond)
	}
}

func (l *partLog) text() string {
	var b strings.Builder
	for _, p := range l.ofType(harness.PartTypeTextDelta) {
		b.WriteString(p.(*harness.TextDeltaPart).Delta)
	}
	return b.String()
}

func waitDone(t *testing.T, c harness.PromptControl) error {
	t.Helper()
	select {
	case <-c.Done():
		return c.Err()
	case <-time.After(60 * time.Second):
		t.Fatal("turn did not finish")
		return nil
	}
}

// signal is a one-shot event for emit callbacks.
type signal struct {
	once sync.Once
	ch   chan struct{}
}

func newSignal() *signal { return &signal{ch: make(chan struct{})} }
func (s *signal) fire()  { s.once.Do(func() { close(s.ch) }) }
func (s *signal) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.ch:
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting")
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// gatewayServer answers the core's model catalog GET and serves model
// fixtures in order for POSTs, recording every request it receives.
type gatewayServer struct {
	t        *testing.T
	mu       sync.Mutex
	fixtures []string
	posts    []*http.Request
	bodies   []string
	gets     int
	respond  func(req *http.Request, body string, index int) (*http.Response, error)
}

func (g *gatewayServer) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	g.mu.Lock()
	if req.Method == http.MethodGet {
		g.gets++
		g.mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(readFixture(g.t, "gateway-models.json"))), Request: req}, nil
	}
	g.posts = append(g.posts, req)
	g.bodies = append(g.bodies, string(body))
	index := len(g.posts) - 1
	respond := g.respond
	var fixture string
	if index < len(g.fixtures) {
		fixture = g.fixtures[index]
	}
	g.mu.Unlock()
	if respond != nil {
		return respond(req, string(body), index)
	}
	if fixture == "" {
		return nil, errors.New("unexpected model request")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader([]byte(readFixture(g.t, fixture)))), Request: req}, nil
}

func (g *gatewayServer) postCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.posts)
}

func (g *gatewayServer) setFixtures(names ...string) {
	g.mu.Lock()
	g.fixtures = append(g.fixtures[:len(g.posts):len(g.posts)], names...)
	g.mu.Unlock()
}

func (g *gatewayServer) body(i int) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i >= len(g.bodies) {
		return ""
	}
	return g.bodies[i]
}

// requireLibfxWasm skips unless the pinned fx-core.wasm is cached (or may
// be downloaded with GOAI_FX_WASM_TEST=1).
func requireLibfxWasm(t *testing.T) *WasmSource {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if os.Getenv("GOAI_FX_WASM_TEST") == "1" {
		if err := PrepareWasm(ctx, nil); err != nil {
			t.Fatalf("GOAI_FX_WASM_TEST=1 but fx-core.wasm could not be prepared: %v", err)
		}
		return &WasmSource{Offline: true}
	}
	if err := PrepareWasm(ctx, &WasmSource{Offline: true}); err != nil {
		if errors.Is(err, ErrWasmUnavailable) {
			t.Skipf("fx-core.wasm %s is not cached; set GOAI_FX_WASM_TEST=1 to download it: %v", LibfxVersion, err)
		}
		t.Fatal(err)
	}
	return &WasmSource{Offline: true}
}

// newRealHarness builds the libfx harness on the real core with gw as the
// network.
func newRealHarness(t *testing.T, gw *gatewayServer, s Settings) *libfxHarness {
	t.Helper()
	s.Runtime = RuntimeLibfx
	s.Wasm = requireLibfxWasm(t)
	if s.Auth.IsZero() {
		s.Auth = harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "test-key"})
	}
	s.HTTPClient = &http.Client{Transport: gw}
	h, err := CreateFx(s)
	if err != nil {
		t.Fatal(err)
	}
	return h.(*libfxHarness)
}

func startOpts(sb *fakeSandbox, sessionID string) harness.StartOptions {
	return harness.StartOptions{SandboxSession: sb, SessionID: sessionID, SessionWorkDir: "/workspace"}
}

func textOutput(v string) types.ToolResultOutput {
	return types.ToolResultOutput{Type: types.ToolResultOutputText, Value: v}
}

// suspendFromEmit starts DoSuspendTurn from inside an Emit callback and
// returns once the turn is suspending, the way TS's synchronous
// `suspending = true` in doSuspendTurn() behaves for an emit handler. The
// continuation arrives on the returned channel.
func suspendFromEmit(t *testing.T, session harness.Session) <-chan *harness.ContinueTurnState {
	out := make(chan *harness.ContinueTurnState, 1)
	s := session.(*libfxSession)
	s.mu.Lock()
	a := s.active
	s.mu.Unlock()
	go func() {
		st, err := session.DoSuspendTurn(context.Background())
		if err != nil {
			t.Errorf("suspend: %v", err)
		}
		out <- st
	}()
	if a == nil {
		return out
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		suspending := a.suspending
		s.mu.Unlock()
		if suspending {
			return out
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("the turn never started suspending")
	return out
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
