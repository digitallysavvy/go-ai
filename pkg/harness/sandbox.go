package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SandboxSpecificationVersion is the spec version of SandboxProvider.
const SandboxSpecificationVersion = "harness-sandbox-v1"

// StateDirectoryName is the fixed directory, relative to the sandbox HOME,
// that holds every piece of harness-generated state.
const StateDirectoryName = ".ai-sdk-harness"

// StateDirectoryPath returns the fixed path for harness-generated state
// (bootstrap files, markers and `.agent-runs`) under the sandbox's own HOME,
// never under the working directory. Called with the symbolic "$HOME" when
// hashing bootstrap recipes. Mirrors TS `harnessStateDirectoryPath` (9c8c0c1).
func StateDirectoryPath(sandboxHomeDir string) string {
	return posixpath.Join(sandboxHomeDir, StateDirectoryName)
}

// EncodePathSegment encodes value as one path segment (encodeURIComponent plus
// escaping of "", "." and ".."). Mirrors TS `encodeHarnessPathSegment`.
func EncodePathSegment(value string) string {
	encoded := encodeURIComponent(value)
	switch encoded {
	case "":
		return "%"
	case ".":
		return "%2E"
	case "..":
		return "%2E%2E"
	}
	return encoded
}

// encodeURIComponent mirrors JavaScript's encodeURIComponent: every byte of
// the UTF-8 encoding is percent-escaped except A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// SessionDataDirectoryPath returns the per-session harness state path under
// `.agent-runs`. Session IDs occupy one encoded path segment. Mirrors TS
// `harnessSessionDataDirectoryPath`.
func SessionDataDirectoryPath(stateDirectory, sessionID string) string {
	return posixpath.Join(stateDirectory, ".agent-runs", EncodePathSegment(sessionID))
}

// PortEndpoint is the connection details for a sandbox-exposed port. Headers
// must be included when opening the connection.
type PortEndpoint struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// PortProtocol selects the URL scheme of a port endpoint.
type PortProtocol string

const (
	PortProtocolHTTP  PortProtocol = "http"
	PortProtocolHTTPS PortProtocol = "https"
	PortProtocolWS    PortProtocol = "ws"
)

// PortEndpointOptions is the input of GetPortEndpoint / GetPortURL.
type PortEndpointOptions struct {
	Port     int
	Protocol PortProtocol
}

// NetworkSandboxSession is the sandbox session returned by
// SandboxProvider.CreateSession: a providerutils.SandboxSession (file I/O,
// exec, spawn) plus the infra surface (ports, lifecycle, network policy).
// Mirrors TS `HarnessV1NetworkSandboxSession`.
//
// Optional TS members are optional interfaces: NetworkPolicySetter,
// RequestTransformationSetter, RequestTransformationAdder and PortsSetter.
type NetworkSandboxSession interface {
	providerutils.SandboxSession

	// ID is the stable identifier of the underlying sandbox resource.
	ID() string

	// DefaultWorkingDirectory is the absolute path relative commands resolve
	// against.
	DefaultWorkingDirectory() string

	// Ports lists the ports the sandbox exposes.
	Ports() []int

	// GetPortEndpoint resolves the connection details for an exposed port.
	GetPortEndpoint(ctx context.Context, opts PortEndpointOptions) (PortEndpoint, error)

	// GetPortURL resolves a publicly-reachable URL for an exposed port.
	//
	// Deprecated: use GetPortEndpoint.
	GetPortURL(ctx context.Context, opts PortEndpointOptions) (string, error)

	// Stop stops the sandbox. Idempotent.
	Stop(ctx context.Context) error

	// Destroy stops the sandbox, then performs any additional cleanup. Must
	// handle both running and previously stopped sandboxes (mandatory since
	// 7f50d28).
	Destroy(ctx context.Context) error

	// Restricted returns a reduced view over the same sandbox that only
	// exposes file I/O and process APIs.
	Restricted() providerutils.SandboxSession
}

// NetworkPolicySetter updates the sandbox's outbound network policy.
type NetworkPolicySetter interface {
	SetNetworkPolicy(ctx context.Context, policy NetworkPolicy) error
}

// RequestTransformationSetter replaces the complete request-transformation set.
type RequestTransformationSetter interface {
	SetRequestTransformations(ctx context.Context, transformations []RequestTransformation) error
}

// RequestTransformationAdder adds request-transformation rules without
// replacing existing ones.
type RequestTransformationAdder interface {
	AddRequestTransformations(ctx context.Context, transformations []RequestTransformation) error
}

// PortsSetter replaces the set of exposed ports (full-replacement semantics).
type PortsSetter interface {
	SetPorts(ctx context.Context, ports []int) error
}

// AsNetworkSandboxSession returns the network view of s when available.
func AsNetworkSandboxSession(s providerutils.SandboxSession) (NetworkSandboxSession, bool) {
	n, ok := s.(NetworkSandboxSession)
	return n, ok
}

// GetRestrictedSandboxSession returns the restricted view of a network
// session, or the session itself. Mirrors TS `getRestrictedSandboxSession`.
func GetRestrictedSandboxSession(s providerutils.SandboxSession) providerutils.SandboxSession {
	if n, ok := s.(NetworkSandboxSession); ok {
		return n.Restricted()
	}
	return s
}

// Network policy modes.
const (
	NetworkPolicyAllowAll = "allow-all"
	NetworkPolicyDenyAll  = "deny-all"
	NetworkPolicyCustom   = "custom"
)

// NetworkPolicy is the outbound network policy. Mirrors TS
// `HarnessV1NetworkPolicy`. For "custom", at least one of AllowedHosts or
// AllowedCIDRs is required; DeniedCIDRs takes precedence over both.
type NetworkPolicy struct {
	Mode         string   `json:"mode"`
	AllowedHosts []string `json:"allowedHosts,omitempty"`
	AllowedCIDRs []string `json:"allowedCIDRs,omitempty"`
	DeniedCIDRs  []string `json:"deniedCIDRs,omitempty"`
}

// Validate enforces the TS compile-time constraints at runtime.
func (p NetworkPolicy) Validate() error {
	switch p.Mode {
	case NetworkPolicyAllowAll, NetworkPolicyDenyAll:
		return nil
	case NetworkPolicyCustom:
		if p.AllowedHosts == nil && p.AllowedCIDRs == nil {
			return errors.New("harness: custom network policy requires allowedHosts or allowedCIDRs")
		}
		return nil
	}
	return fmt.Errorf("harness: invalid network policy mode %q", p.Mode)
}

// MarshalJSON keeps the TS shape per mode (custom always carries its allow
// field(s) as arrays).
func (p NetworkPolicy) MarshalJSON() ([]byte, error) {
	if p.Mode != NetworkPolicyCustom {
		return json.Marshal(struct {
			Mode string `json:"mode"`
		}{p.Mode})
	}
	type alias NetworkPolicy
	return json.Marshal(alias(p))
}

// StringMatcher matches a string exactly, by prefix, or by regex. Exactly one
// field should be set.
type StringMatcher struct {
	Exact      string `json:"exact,omitempty"`
	StartsWith string `json:"startsWith,omitempty"`
	Regex      string `json:"regex,omitempty"`
}

// KeyValueMatcher matches a header or query parameter.
type KeyValueMatcher struct {
	Key   *StringMatcher `json:"key,omitempty"`
	Value *StringMatcher `json:"value,omitempty"`
}

// RequestTransformationMatch selects outbound requests.
type RequestTransformationMatch struct {
	Host        string            `json:"host"`
	Path        *StringMatcher    `json:"path,omitempty"`
	Method      []string          `json:"method,omitempty"`
	QueryString []KeyValueMatcher `json:"queryString,omitempty"`
	Headers     []KeyValueMatcher `json:"headers,omitempty"`
}

// RequestTransformationTransform is applied to matching requests.
type RequestTransformationTransform struct {
	Headers map[string]string `json:"headers"`
}

// RequestTransformation is an outbound HTTPS request transformation applied
// outside the sandbox security boundary (credential brokering). Mirrors TS
// `HarnessV1RequestTransformation` (69bb613).
type RequestTransformation struct {
	Match     RequestTransformationMatch     `json:"match"`
	Transform RequestTransformationTransform `json:"transform"`
}

// RequestTransformationSources are the inputs adapters use to build
// credential request transformations (TS
// `HarnessV1RequestTransformationSources`).
type RequestTransformationSources struct {
	Env        map[string]string
	SandboxEnv map[string]string
	Auth       string
}

// OnFirstCreateFunc runs once per identity on fresh sandbox creation.
type OnFirstCreateFunc func(ctx context.Context, session providerutils.SandboxSession) error

// CreateSandboxSessionOptions is the input of SandboxProvider.CreateSession.
// Cancellation (TS abortSignal) is carried by the context.
type CreateSandboxSessionOptions struct {
	// SessionID names the underlying resource deterministically so a future
	// ResumeSession can find it. Empty for prewarm paths.
	SessionID string
	// Identity is the stable identity for snapshot-based reuse.
	Identity string
	// OnFirstCreate is called exactly once per identity, on fresh creation.
	OnFirstCreate OnFirstCreateFunc
}

// SandboxProvider produces network sandbox sessions for harness sessions.
// Mirrors TS `HarnessV1SandboxProvider`. Providers should return a
// SandboxAuthenticationError when credentials are missing or invalid.
// Providers that can reattach by id also implement SandboxSessionResumer.
type SandboxProvider interface {
	SpecificationVersion() string // "harness-sandbox-v1"
	ProviderID() string
	CreateSession(ctx context.Context, opts CreateSandboxSessionOptions) (NetworkSandboxSession, error)
}

// SandboxSessionResumer is the optional TS `resumeSession`.
type SandboxSessionResumer interface {
	ResumeSession(ctx context.Context, sessionID string) (NetworkSandboxSession, error)
}

// ResolveSandboxHomeDir resolves the sandbox HOME via `printf "%s" "$HOME"`.
// Mirrors TS `resolveSandboxHomeDir`.
func ResolveSandboxHomeDir(ctx context.Context, sandbox providerutils.SandboxSession) (string, error) {
	result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: `printf "%s" "$HOME"`})
	if err != nil {
		return "", err
	}
	homeDir := strings.TrimSpace(result.Stdout)
	if result.ExitCode != 0 || homeDir == "" || !posixpath.IsAbs(homeDir) {
		return "", fmt.Errorf("Unable to resolve sandbox HOME directory: %s", orString(result.Stderr, result.Stdout))
	}
	return homeDir, nil
}

// ResolveSandboxDefaultWorkingDirectory returns DefaultWorkingDirectory for
// network sessions, otherwise runs `pwd`. Mirrors TS
// `resolveSandboxDefaultWorkingDirectory`.
func ResolveSandboxDefaultWorkingDirectory(ctx context.Context, sandbox providerutils.SandboxSession) (string, error) {
	if n, ok := sandbox.(NetworkSandboxSession); ok {
		return n.DefaultWorkingDirectory(), nil
	}
	result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: "pwd"})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("Failed to resolve sandbox default working directory (exit %d): %s", result.ExitCode, orString(result.Stderr, result.Stdout))
	}
	cwd := strings.TrimSpace(result.Stdout)
	if !posixpath.IsAbs(cwd) {
		quoted, _ := json.Marshal(cwd)
		return "", fmt.Errorf("Failed to resolve sandbox default working directory: expected an absolute path, got %s.", quoted)
	}
	if cwd == "/" {
		return cwd, nil
	}
	return strings.TrimRight(cwd, "/"), nil
}

func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
