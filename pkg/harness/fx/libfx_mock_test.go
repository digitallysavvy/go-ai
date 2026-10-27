package fx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
)

// mockRuntime is the Go counterpart of TS fx-harness.test.ts's
// vi.mock('./fx-runtime'): it records runtime options and prompts and runs
// a scripted turn body.
type mockRuntime struct {
	mu       sync.Mutex
	options  []runtimeOptions
	prompts  [][]libfx.PromptBlock
	run      func(ctx context.Context, o runtimeOptions) error
	close    func() error
	startErr error
}

func (m *mockRuntime) factory(_ context.Context, o runtimeOptions) (fxEngine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		err := m.startErr
		m.startErr = nil
		return nil, err
	}
	m.options = append(m.options, o)
	return &mockEngine{m: m, o: o}, nil
}

func (m *mockRuntime) opts() []runtimeOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]runtimeOptions(nil), m.options...)
}

func (m *mockRuntime) setRun(f func(ctx context.Context, o runtimeOptions) error) {
	m.mu.Lock()
	m.run = f
	m.mu.Unlock()
}

type mockEngine struct {
	m *mockRuntime
	o runtimeOptions
}

func (e *mockEngine) Prompt(ctx context.Context, blocks []libfx.PromptBlock) (fxTurn, error) {
	e.m.mu.Lock()
	e.m.prompts = append(e.m.prompts, blocks)
	run := e.m.run
	e.m.mu.Unlock()
	t := &mockTurn{done: make(chan struct{})}
	go func() {
		var err error
		if run != nil {
			err = run(ctx, e.o)
		}
		if err == nil && ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		t.err = err
		close(t.done)
	}()
	return t, nil
}

func (e *mockEngine) Checkpoint(context.Context) ([]byte, error) { return []byte{1, 2, 3}, nil }

func (e *mockEngine) Close(context.Context) error {
	e.m.mu.Lock()
	c := e.m.close
	e.m.mu.Unlock()
	if c != nil {
		return c()
	}
	return nil
}

type mockTurn struct {
	done chan struct{}
	err  error
}

func (t *mockTurn) Next(ctx context.Context) (libfx.Event, error) {
	select {
	case <-t.done:
		return libfx.Event{}, io.EOF
	case <-ctx.Done():
		return libfx.Event{}, ctx.Err()
	}
}

func (t *mockTurn) Wait(ctx context.Context) (libfx.TurnResult, error) {
	select {
	case <-t.done:
	case <-ctx.Done():
		return libfx.TurnResult{}, ctx.Err()
	}
	if t.err != nil {
		return libfx.TurnResult{}, t.err
	}
	return libfx.TurnResult{StopReason: "end_turn"}, nil
}

func (t *mockTurn) Steer(string) (string, error) { return "in_1", nil }
func (t *mockTurn) Cancel()                      {}

// invoke mirrors TS invoke(): announce a tool_call, then run the tool.
func invoke(o runtimeOptions, name, id string, input any) (any, error) {
	update, _ := json.Marshal(map[string]any{"update": map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "name": name}})
	o.OnReceive("session/update", update)
	raw, _ := json.Marshal(input)
	for _, tool := range o.Tools {
		if tool.Name == name {
			return tool.Execute(context.Background(), raw)
		}
	}
	return nil, errors.New("no tool " + name)
}

// newMockHarness builds a libfx harness on the mock runtime with a Gateway
// key in an isolated authentication environment.
func newMockHarness(t *testing.T, s Settings) (*libfxHarness, *mockRuntime) {
	t.Helper()
	s.Runtime = RuntimeLibfx
	if s.Auth.IsZero() {
		s.Auth = harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "test-key"})
	}
	h, err := CreateFx(s)
	if err != nil {
		t.Fatal(err)
	}
	lh := h.(*libfxHarness)
	m := &mockRuntime{}
	lh.newEngine = m.factory
	return lh, m
}
