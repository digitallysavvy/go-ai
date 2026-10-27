package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Runtime selects how CreateFx runs fx.
type Runtime string

const (
	// RuntimeSandboxACP installs the fx CLI in the sandbox and drives
	// `fx acp` through the shared ACP bridge. It is the go-ai v0.5.x
	// behavior and stays the default: its built-in tools, network path and
	// lifecycle state differ from the libfx runtime's.
	RuntimeSandboxACP Runtime = "sandbox-acp"
	// RuntimeLibfx runs the libfx core (fx-core.wasm) in the host process,
	// the runtime TS @ai-sdk/harness-fx uses. Built-in tools run as
	// commands in the session sandbox; the host makes the model requests.
	// It supports AI Gateway authentication; codex and grok subscriptions
	// and MCP servers are not supported yet.
	RuntimeLibfx Runtime = "libfx"
)

// Service tiers (TS FxHarnessSettings serviceTier).
const (
	ServiceTierDefault   = "default"
	ServiceTierFast      = "fast"
	ServiceTierUltrafast = "ultrafast"
)

// libfxBuiltinToolNames is TS FX_BUILTIN_TOOLS' declaration order.
var libfxBuiltinToolNames = []string{"read", "write", "edit", "bash", "grep", "glob", "ls", "askUserQuestions"}

// zodObject is the JSON Schema zod v4 emits for z.object(...).
func zodObject(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func num() map[string]any { return map[string]any{"type": "number"} }

// LibfxBuiltinTools mirrors TS FX_BUILTIN_TOOLS: the built-in tools of the
// libfx runtime. They run as commands in the session sandbox, confined to
// the session workspace. RuntimeSandboxACP uses BuiltinTools instead.
var LibfxBuiltinTools = map[string]harness.BuiltinTool{
	"read": harness.CommonTool(harness.BuiltinToolRead, harness.CommonToolOptions{
		NativeName: "read", ToolUseKind: harness.BuiltinToolUseKindReadonly,
		Description: "Read a file in the sandbox workspace.",
		InputSchema: zodObject([]string{"file_path"}, props("file_path", str())),
	}),
	"write": harness.CommonTool(harness.BuiltinToolWrite, harness.CommonToolOptions{
		NativeName: "write", ToolUseKind: harness.BuiltinToolUseKindEdit,
		Description: "Create or overwrite a file in the sandbox workspace.",
		InputSchema: zodObject([]string{"file_path", "content"}, props("file_path", str(), "content", str())),
	}),
	"edit": harness.CommonTool(harness.BuiltinToolEdit, harness.CommonToolOptions{
		NativeName: "edit", ToolUseKind: harness.BuiltinToolUseKindEdit,
		Description: "Replace an exact, unique string in a sandbox workspace file.",
		InputSchema: zodObject([]string{"file_path", "old_string", "new_string"}, props("file_path", str(), "old_string", str(), "new_string", str())),
	}),
	"bash": harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
		NativeName: "bash", ToolUseKind: harness.BuiltinToolUseKindBash,
		Description: "Execute a shell command in the sandbox workspace. Timeout is in seconds.",
		InputSchema: zodObject([]string{"command"}, props("command", str(), "timeout", num())),
	}),
	"grep": harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName: "grep", ToolUseKind: harness.BuiltinToolUseKindReadonly,
		Description: "Search sandbox file contents.",
		InputSchema: zodObject([]string{"pattern"}, props(
			"pattern", str(), "path", str(), "glob", str(), "ignoreCase", boolean(),
			"literal", boolean(), "context", num(), "limit", num(),
		)),
	}),
	"glob": harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
		NativeName: "glob", ToolUseKind: harness.BuiltinToolUseKindReadonly,
		Description: "Find files in the sandbox workspace.",
		InputSchema: zodObject([]string{"pattern"}, props("pattern", str(), "path", str(), "limit", num())),
	}),
	"ls": {
		Tool: types.Tool{
			Name:         "ls",
			Description:  "List a sandbox workspace directory.",
			Parameters:   zodObject(nil, props("path", str(), "limit", num())),
			OutputSchema: map[string]any{"$schema": "http://json-schema.org/draft-07/schema#"},
		},
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
	},
	"askUserQuestions": func() harness.BuiltinTool {
		std := harness.StandardBuiltinTools()[harness.BuiltinToolAskUserQuestions]
		return harness.BuiltinTool{Tool: std, CommonName: harness.BuiltinToolAskUserQuestions, ToolUseKind: harness.BuiltinToolUseKindReadonly}
	}(),
}

// clientApp is the User-Agent and x-client-app the libfx runtime sends
// (TS FX_CLIENT_APP).
const clientApp = ClientAppName + "/" + ClientAppVersion

// runtimeOptions is what a turn hands the engine (TS FxRuntimeOptions).
type runtimeOptions struct {
	APIKey       string
	Provider     string
	Model        string
	Effort       string
	Fast         *bool
	Ultrafast    *bool
	Instructions string
	Tools        []libfx.Tool
	Checkpoint   []byte
	Fetch        libfx.FetchFunc
	OnHook       func(libfx.HookEvent)
	OnReceive    func(method string, params json.RawMessage)
	// Subscription carries a codex or grok session's credentials.
	Subscription *libfx.SubscriptionAuth
}

// fxEngine is the narrow runtime boundary (TS FxEngine); tests replace it.
type fxEngine interface {
	Prompt(ctx context.Context, blocks []libfx.PromptBlock) (fxTurn, error)
	Checkpoint(ctx context.Context) ([]byte, error)
	Close(ctx context.Context) error
}

// fxTurn is one engine turn (TS FxTurn).
type fxTurn interface {
	Next(ctx context.Context) (libfx.Event, error)
	Wait(ctx context.Context) (libfx.TurnResult, error)
	Steer(text string) (string, error)
	Cancel()
}

type engineFactory func(ctx context.Context, opts runtimeOptions) (fxEngine, error)

type agentEngine struct{ *libfx.Agent }

func (a agentEngine) Prompt(ctx context.Context, blocks []libfx.PromptBlock) (fxTurn, error) {
	t, err := a.Agent.Prompt(ctx, blocks)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (h *libfxHarness) startAgent(ctx context.Context, o runtimeOptions) (fxEngine, error) {
	var src libfx.Source
	if h.settings.Wasm != nil {
		src = h.settings.Wasm.internal()
	}
	pages := uint32(0)
	if h.settings.MemoryLimitBytes > 0 {
		pages = uint32((h.settings.MemoryLimitBytes + 65535) / 65536)
	}
	a, err := libfx.StartAgent(ctx, libfx.AgentOptions{
		Wasm:             src,
		MemoryLimitPages: pages,
		APIKey:           o.APIKey,
		Provider:         o.Provider,
		Model:            o.Model,
		Effort:           o.Effort,
		Fast:             o.Fast,
		Ultrafast:        o.Ultrafast,
		Instructions:     o.Instructions,
		Tools:            o.Tools,
		Checkpoint:       o.Checkpoint,
		Fetch:            o.Fetch,
		OnHook:           o.OnHook,
		OnReceive:        o.OnReceive,
		Subscription:     o.Subscription,
		Stderr:           io.Discard,
	})
	if err != nil {
		var newer *libfx.NewerCheckpointError
		if errors.As(err, &newer) {
			return nil, &IncompatibleStateError{Message: newer.Message, Reason: "checkpoint created by a newer libfx"}
		}
		return nil, err
	}
	return agentEngine{a}, nil
}

// fxAuthentication is the resolved credential (TS FxResolvedAuthentication):
// a Gateway key, or a codex or grok subscription store.
type fxAuthentication struct {
	Provider     string
	APIKey       string
	BaseURL      string
	Subscription *fxSubscription
}

const errNoGatewayCredential = "fx requires AI_GATEWAY_API_KEY or VERCEL_OIDC_TOKEN in the host environment or an explicit authentication environment. No subscription credentials were available."

// libfxHarness is CreateFx under RuntimeLibfx (TS createFx).
type libfxHarness struct {
	settings  Settings
	newEngine engineFactory
	resolve   func(ctx context.Context, auth AuthenticationMode, requiredProvider string) (fxAuthentication, error)
	network   libfx.FetchFunc
}

func newLibfxHarness(s Settings) (*libfxHarness, error) {
	switch s.Backend {
	case "", "auto", "wasm":
	case "native":
		return nil, harness.NewCapabilityUnsupportedError("The Go fx harness runs libfx only on its wasm backend; backend \"native\" needs the libfx Node addon.", HarnessID, nil)
	default:
		return nil, fmt.Errorf("fx: unknown backend %q", s.Backend)
	}
	switch s.ServiceTier {
	case "", ServiceTierDefault, ServiceTierFast, ServiceTierUltrafast:
	default:
		return nil, fmt.Errorf("fx: unknown serviceTier %q", s.ServiceTier)
	}
	if len(s.MCPServers) > 0 {
		return nil, harness.NewCapabilityUnsupportedError("MCP servers are not supported by the Go fx libfx runtime yet; use fx.RuntimeSandboxACP.", HarnessID, nil)
	}
	h := &libfxHarness{settings: s}
	h.newEngine = h.startAgent
	h.resolve = func(ctx context.Context, auth AuthenticationMode, required string) (fxAuthentication, error) {
		return resolveFxAuthentication(ctx, resolveAuthOptions{Auth: auth, Env: harnessutil.ProcessEnv(), RequiredProvider: required})
	}
	return h, nil
}

func (h *libfxHarness) SpecificationVersion() string { return harness.SpecificationVersion }
func (h *libfxHarness) HarnessID() string            { return HarnessID }

func (h *libfxHarness) BuiltinTools() map[string]harness.BuiltinTool { return LibfxBuiltinTools }

func (h *libfxHarness) SupportsBuiltinToolApprovals() bool { return true }
func (h *libfxHarness) SupportsBuiltinToolFiltering() bool { return true }

// ValidateLifecycleStateData mirrors TS fxResumeStateSchema.
func (h *libfxHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	_, err := parseLibfxResumeData(data)
	return err
}

func (h *libfxHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	return startLibfxSession(ctx, h, opts)
}

// networkFetch is the fetch model requests leave the host through: the
// caller's client or an SSRF-safe one, limited to the Gateway base origin
// and libfx's default origins.
func (h *libfxHarness) networkFetch(baseURL string) (libfx.FetchFunc, error) {
	if h.network != nil {
		return h.network, nil
	}
	origins := append([]string(nil), libfx.DefaultAllowedOrigins...)
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid AI_GATEWAY_BASE_URL %q", baseURL)
		}
		origins = append(origins, strings.ToLower(u.Scheme)+"://"+strings.ToLower(u.Host))
	}
	if h.settings.HTTPClient != nil {
		return libfx.AllowlistFetch(h.settings.HTTPClient, origins), nil
	}
	return libfx.AllowlistFetch(libfx.DefaultFetchClient(), origins), nil
}

// legacyHarness is the v0.5.x sandbox-ACP adapter with one addition: libfx
// lifecycle state gets a clear error instead of a schema failure.
type legacyHarness struct {
	harness.Harness
}

func (l legacyHarness) SupportsBuiltinToolApprovals() bool {
	return harness.SupportsBuiltinToolApprovals(l.Harness)
}

func (l legacyHarness) SupportsBuiltinToolFiltering() bool {
	return harness.SupportsBuiltinToolFiltering(l.Harness)
}

func (l legacyHarness) GetBootstrap(ctx context.Context) (*harness.Bootstrap, error) {
	return harness.GetBootstrap(ctx, l.Harness)
}

func (l legacyHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	if isLibfxData(data) {
		return &IncompatibleStateError{Message: errLibfxStateUnderLegacy, Reason: "libfx lifecycle data"}
	}
	if v, ok := l.Harness.(harness.LifecycleStateValidator); ok {
		return v.ValidateLifecycleStateData(data)
	}
	return nil
}

func (l legacyHarness) DoStart(ctx context.Context, opts harness.StartOptions) (harness.Session, error) {
	for _, data := range lifecycleDataOf(opts) {
		if isLibfxData(data) {
			return nil, &IncompatibleStateError{Message: errLibfxStateUnderLegacy, Reason: "libfx lifecycle data"}
		}
	}
	return l.Harness.DoStart(ctx, opts)
}

func lifecycleDataOf(opts harness.StartOptions) []json.RawMessage {
	var out []json.RawMessage
	if opts.ContinueFrom != nil {
		out = append(out, opts.ContinueFrom.Data)
	}
	if opts.ResumeFrom != nil {
		out = append(out, opts.ResumeFrom.Data)
		if opts.ResumeFrom.ContinueFrom != nil {
			out = append(out, opts.ResumeFrom.ContinueFrom.Data)
		}
	}
	return out
}
