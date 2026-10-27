package libfx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// EnvWasmTest opts the real-wasm tests into downloading fx-core.wasm.
// Without it they run only when the pinned module is already cached.
const EnvWasmTest = "GOAI_FX_WASM_TEST"

// requireWasm returns a Source for the real pinned module or skips.
func requireWasm(t *testing.T) Source {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if os.Getenv(EnvWasmTest) == "1" {
		if _, err := Resolve(ctx, Source{}); err != nil {
			t.Fatalf("%s=1 but fx-core.wasm could not be resolved: %v", EnvWasmTest, err)
		}
		return Source{Offline: true}
	}
	if _, err := Resolve(ctx, Source{Offline: true}); err != nil {
		if errors.Is(err, ErrWasmUnavailable) {
			t.Skipf("fx-core.wasm %s is not cached; set %s=1 to download it: %v", Version, EnvWasmTest, err)
		}
		t.Fatal(err)
	}
	return Source{Offline: true}
}

func TestWasmBootInitialize(t *testing.T) {
	src := requireWasm(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e, err := Start(ctx, Options{Wasm: src, APIKey: "test-key", Model: "anthropic/claude-haiku-4.5", Stderr: testWriter{t}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := e.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	res, err := e.Request(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Logf("initialize result: %s", res)
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err := json.Unmarshal(res, &init); err != nil || init.ProtocolVersion != 1 {
		t.Fatalf("initialize result %s: %v", res, err)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("fx-core stderr: %s", p)
	return len(p), nil
}

type recorder struct {
	mu      sync.Mutex
	updates []json.RawMessage
	methods []string
}

func (r *recorder) on(method string, params json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, method)
	if method == "session/update" {
		r.updates = append(r.updates, append(json.RawMessage(nil), params...))
	}
}

func (r *recorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, u := range r.updates {
		var p struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(u, &p) == nil && p.Update.SessionUpdate == "agent_message_chunk" {
			b.WriteString(p.Update.Content.Text)
		}
	}
	return b.String()
}

// fixtureFetch serves the Gateway model catalog for GETs and the SSE
// fixtures, in order, for model POSTs. It never touches the network.
type fixtureFetch struct {
	t        *testing.T
	mu       sync.Mutex
	fixtures []string
	posts    []string
	gets     int
}

func (f *fixtureFetch) fetch(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.URL.Host != "ai-gateway.vercel.sh" {
		f.t.Errorf("unexpected request to %s", req.URL)
		return nil, errors.New("unexpected host")
	}
	if req.Method == http.MethodGet {
		f.gets++
		return fixtureResponse(f.t, "gateway-models.json"), nil
	}
	body, _ := io.ReadAll(req.Body)
	f.posts = append(f.posts, string(body))
	if len(f.posts) > len(f.fixtures) {
		return nil, errors.New("unexpected model request")
	}
	return fixtureResponse(f.t, f.fixtures[len(f.posts)-1]), nil
}

func fixtureResponse(t *testing.T, name string) *http.Response {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data))}
}

var bashTool = map[string]any{
	"name":        "bash",
	"description": "test",
	"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
}

func startCore(t *testing.T, ctx context.Context, src Source, f *fixtureFetch, rec *recorder, exec ToolExecutor) *Engine {
	t.Helper()
	e, err := Start(ctx, Options{
		Wasm:           src,
		APIKey:         "test-key",
		Model:          "anthropic/claude-haiku-4.5",
		Fetch:          f.fetch,
		ExecuteTool:    exec,
		OnNotification: rec.on,
		Stderr:         testWriter{t},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if _, err := e.Request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"libfx": map[string]any{"tools": []any{bashTool}}},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return e
}

func newSession(t *testing.T, ctx context.Context, e *Engine) string {
	t.Helper()
	res, err := e.Request(ctx, "libfx/new", map[string]any{"hooks": map[string]any{"captureContent": false}})
	if err != nil {
		t.Fatalf("libfx/new: %v", err)
	}
	var s struct {
		SessionID string `json:"sessionId"`
		Meta      struct {
			Fx struct {
				HooksVersion int `json:"hooksVersion"`
			} `json:"fx"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(res, &s); err != nil || s.SessionID == "" {
		t.Fatalf("libfx/new result %s: %v", res, err)
	}
	if s.Meta.Fx.HooksVersion != 1 {
		t.Errorf("hooksVersion = %d, want 1", s.Meta.Fx.HooksVersion)
	}
	return s.SessionID
}

// TestWasmGatewayPromptCheckpointRestore drives the real core through a
// Gateway turn that calls a host tool, then checkpoints the session and
// restores it in a fresh core.
func TestWasmGatewayPromptCheckpointRestore(t *testing.T) {
	src := requireWasm(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var toolMu sync.Mutex
	var toolCalls []string
	exec := func(_ context.Context, name string, input json.RawMessage, callID string) (ToolResult, error) {
		toolMu.Lock()
		defer toolMu.Unlock()
		toolCalls = append(toolCalls, name+" "+string(input)+" "+callID)
		return ToolResult{Content: "LIBFX_FIXTURE"}, nil
	}
	f := &fixtureFetch{t: t, fixtures: []string{"gateway-0.sse.txt", "gateway-1.sse.txt"}}
	rec := &recorder{}
	e := startCore(t, ctx, src, f, rec, exec)
	sessionID := newSession(t, ctx, e)

	e.OpenSteering()
	res, err := e.Request(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "test"}},
	})
	e.CloseSteering()
	if err != nil {
		t.Fatalf("session/prompt: %v", err)
	}
	var turn struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(res, &turn); err != nil || turn.StopReason != "end_turn" {
		t.Fatalf("session/prompt result %s: %v", res, err)
	}
	if got := rec.text(); got != "READY" {
		t.Errorf("agent text = %q, want READY", got)
	}
	toolMu.Lock()
	if len(toolCalls) != 1 || !strings.HasPrefix(toolCalls[0], `bash {"command":"printf LIBFX_FIXTURE"}`) && !strings.HasPrefix(toolCalls[0], `bash {"command": "printf LIBFX_FIXTURE"}`) {
		t.Errorf("tool calls = %q", toolCalls)
	}
	toolMu.Unlock()
	f.mu.Lock()
	if len(f.posts) != 2 {
		t.Errorf("model requests = %d posts, %d gets", len(f.posts), f.gets)
	} else if !strings.Contains(f.posts[1], "LIBFX_FIXTURE") {
		t.Errorf("second model request does not carry the tool result: %.300s", f.posts[1])
	}
	f.mu.Unlock()

	res, err = e.Request(ctx, "libfx/checkpoint", map[string]any{"sessionId": sessionID, "checkpointMeta": map[string]any{"libfxVersion": "2", "toolSchemaHash": "x", "model": "anthropic/claude-haiku-4.5"}})
	if err != nil {
		t.Fatalf("libfx/checkpoint: %v", err)
	}
	var cp struct {
		CheckpointAttachment int32 `json:"checkpointAttachment"`
	}
	if err := json.Unmarshal(res, &cp); err != nil || cp.CheckpointAttachment <= 0 {
		t.Fatalf("libfx/checkpoint result %s: %v", res, err)
	}
	checkpoint := e.TakeAttachment(cp.CheckpointAttachment)
	if len(checkpoint) == 0 || len(checkpoint) > MaxCheckpointBytes {
		t.Fatalf("checkpoint is %d bytes", len(checkpoint))
	}

	// Restore into a fresh core.
	rec2 := &recorder{}
	e2 := startCore(t, ctx, src, &fixtureFetch{t: t}, rec2, exec)
	sessionID2 := newSession(t, ctx, e2)
	if err := e2.WriteAttachment(1, checkpoint); err != nil {
		t.Fatal(err)
	}
	res, err = e2.Request(ctx, "libfx/restore", map[string]any{"sessionId": sessionID2, "checkpointAttachment": 1})
	if err != nil {
		t.Fatalf("libfx/restore: %v", err)
	}
	var restored struct {
		CheckpointMeta map[string]any `json:"checkpointMeta"`
	}
	if err := json.Unmarshal(res, &restored); err != nil || restored.CheckpointMeta["model"] != "anthropic/claude-haiku-4.5" {
		t.Errorf("libfx/restore result %s: %v", res, err)
	}
}
