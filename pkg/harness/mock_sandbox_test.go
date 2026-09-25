package harness

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// mockSandbox mirrors the vi.fn()-based sandbox mocks of the TS tests: it
// records every run/read/write in call order.
type mockSandbox struct {
	mu     sync.Mutex
	files  map[string]string
	runs   []providerutils.SandboxProcessOptions
	reads  []string
	writes []providerutils.SandboxWriteTextFileOptions
	events []string // "run:<cmd>", "write:<path>", "read:<path>"

	runFn func(opts providerutils.SandboxProcessOptions) providerutils.SandboxRunResult
}

func newMockSandbox() *mockSandbox {
	return &mockSandbox{
		files: map[string]string{},
		runFn: func(opts providerutils.SandboxProcessOptions) providerutils.SandboxRunResult {
			switch opts.Command {
			case "pwd":
				return providerutils.SandboxRunResult{Stdout: "/work\n"}
			case `printf "%s" "$HOME"`:
				return providerutils.SandboxRunResult{Stdout: "/home/agent"}
			}
			return providerutils.SandboxRunResult{}
		},
	}
}

func (m *mockSandbox) Description() string { return "mock" }

func (m *mockSandbox) Run(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	m.mu.Lock()
	m.runs = append(m.runs, opts)
	m.events = append(m.events, "run:"+opts.Command)
	fn := m.runFn
	m.mu.Unlock()
	return fn(opts), nil
}

func (m *mockSandbox) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("not supported")
}

func (m *mockSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not supported")
}

func (m *mockSandbox) ReadBinaryFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("not supported")
}

func (m *mockSandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reads = append(m.reads, opts.Path)
	m.events = append(m.events, "read:"+opts.Path)
	if content, ok := m.files[opts.Path]; ok {
		return &content, nil
	}
	return nil, nil
}

func (m *mockSandbox) WriteFile(context.Context, string, io.Reader) error {
	return errors.New("not supported")
}

func (m *mockSandbox) WriteBinaryFile(context.Context, string, []byte) error {
	return errors.New("not supported")
}

func (m *mockSandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, opts)
	m.events = append(m.events, "write:"+opts.Path)
	m.files[opts.Path] = opts.Content
	return nil
}

func (m *mockSandbox) runCommands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.runs))
	for i, r := range m.runs {
		out[i] = r.Command
	}
	return out
}

func (m *mockSandbox) indexOfEvent(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// mockNetworkSandbox wraps mockSandbox with the network surface.
type mockNetworkSandbox struct {
	*mockSandbox
	stops int
}

func (m *mockNetworkSandbox) ID() string                      { return "mock-id" }
func (m *mockNetworkSandbox) DefaultWorkingDirectory() string { return "/work" }
func (m *mockNetworkSandbox) Ports() []int                    { return nil }
func (m *mockNetworkSandbox) GetPortEndpoint(context.Context, PortEndpointOptions) (PortEndpoint, error) {
	return PortEndpoint{}, errors.New("no ports")
}
func (m *mockNetworkSandbox) GetPortURL(context.Context, PortEndpointOptions) (string, error) {
	return "", errors.New("no ports")
}
func (m *mockNetworkSandbox) Stop(context.Context) error {
	m.mu.Lock()
	m.stops++
	m.mu.Unlock()
	return nil
}
func (m *mockNetworkSandbox) Destroy(ctx context.Context) error { return m.Stop(ctx) }
func (m *mockNetworkSandbox) Restricted() providerutils.SandboxSession {
	return m.mockSandbox
}

// fakeHarness is a minimal Harness with an optional recipe.
type fakeHarness struct {
	id     string
	recipe *Bootstrap
}

func (h *fakeHarness) SpecificationVersion() string         { return SpecificationVersion }
func (h *fakeHarness) HarnessID() string                    { return h.id }
func (h *fakeHarness) BuiltinTools() map[string]BuiltinTool { return map[string]BuiltinTool{} }
func (h *fakeHarness) DoStart(context.Context, StartOptions) (Session, error) {
	return nil, errors.New("not used")
}

type fakeBootstrapHarness struct{ fakeHarness }

func (h *fakeBootstrapHarness) GetBootstrap(context.Context) (*Bootstrap, error) {
	return h.recipe, nil
}

func makeHarness(id string, recipe *Bootstrap) Harness {
	if recipe == nil {
		return &fakeHarness{id: id}
	}
	return &fakeBootstrapHarness{fakeHarness{id: id, recipe: recipe}}
}
