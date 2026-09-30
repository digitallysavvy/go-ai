package vercel

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// NetworkSession is a harness.NetworkSandboxSession backed by a Vercel
// Sandbox. Ports
// packages/sandbox-vercel/src/vercel-network-sandbox-session.ts
// (VercelNetworkSandboxSession). Explicit Stop/Destroy calls delegate to the
// native sandbox unless the session was wrapped through the legacy
// compatibility provider's existing-sandbox path, where those calls are
// intentionally no-ops (ownsLifecycle=false).
type NetworkSession struct {
	*Session
	sandbox       *Sandbox
	ownsLifecycle bool
	policyManager *PolicyManager
}

var (
	_ harness.NetworkSandboxSession       = (*NetworkSession)(nil)
	_ harness.NetworkPolicySetter         = (*NetworkSession)(nil)
	_ harness.RequestTransformationSetter = (*NetworkSession)(nil)
	_ harness.RequestTransformationAdder  = (*NetworkSession)(nil)
	_ harness.PortsSetter                 = (*NetworkSession)(nil)
)

// NewNetworkSession wraps sandbox as a network sandbox session.
// ownsLifecycle controls whether Stop/Destroy act on the underlying sandbox
// (true) or are no-ops (false, for a caller-supplied sandbox).
func NewNetworkSession(sandbox *Sandbox, ownsLifecycle bool) *NetworkSession {
	return &NetworkSession{
		Session:       NewSession(sandbox),
		sandbox:       sandbox,
		ownsLifecycle: ownsLifecycle,
		policyManager: NewPolicyManager(sandbox),
	}
}

// ID is the sandbox's name.
func (n *NetworkSession) ID() string { return n.sandbox.Name() }

// DefaultWorkingDirectory is the running session's cwd.
func (n *NetworkSession) DefaultWorkingDirectory() string { return n.sandbox.CWD() }

// Ports lists the exposed port numbers.
func (n *NetworkSession) Ports() []int { return n.sandbox.Ports() }

// GetPortEndpoint resolves a port's public endpoint. Mirrors TS
// `getPortEndpoint`.
func (n *NetworkSession) GetPortEndpoint(_ context.Context, opts harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	exposed := n.sandbox.Ports()
	found := false
	for _, p := range exposed {
		if p == opts.Port {
			found = true
			break
		}
	}
	if !found {
		portsStr := make([]string, len(exposed))
		for i, p := range exposed {
			portsStr[i] = strconv.Itoa(p)
		}
		return harness.PortEndpoint{}, harness.NewCapabilityUnsupportedError(
			"Port "+strconv.Itoa(opts.Port)+" is not exposed on this sandbox. Exposed ports: ["+strings.Join(portsStr, ", ")+"].",
			vercelProviderID, nil,
		)
	}
	domain, err := n.sandbox.Domain(opts.Port)
	if err != nil {
		return harness.PortEndpoint{}, err
	}
	u, err := url.Parse(domain)
	if err != nil {
		return harness.PortEndpoint{}, err
	}
	isSecure := u.Scheme == "https"
	protocol := opts.Protocol
	if protocol == "" {
		protocol = harness.PortProtocolHTTPS
	}
	switch protocol {
	case harness.PortProtocolHTTP:
		if isSecure {
			u.Scheme = "https"
		} else {
			u.Scheme = "http"
		}
	case harness.PortProtocolHTTPS:
		u.Scheme = "https"
	case harness.PortProtocolWS:
		if isSecure {
			u.Scheme = "wss"
		} else {
			u.Scheme = "ws"
		}
	}
	if u.Path == "" {
		// JavaScript's URL#toString always includes a "/" path when the URL
		// has an authority and no explicit path; Go's net/url omits it. Add
		// it back so the formatted endpoint matches TS byte-for-byte.
		u.Path = "/"
	}
	return harness.PortEndpoint{URL: u.String()}, nil
}

// GetPortURL is a deprecated compatibility wrapper over GetPortEndpoint.
func (n *NetworkSession) GetPortURL(ctx context.Context, opts harness.PortEndpointOptions) (string, error) {
	endpoint, err := n.GetPortEndpoint(ctx, opts)
	return endpoint.URL, err
}

// SetNetworkPolicy delegates to the policy manager.
func (n *NetworkSession) SetNetworkPolicy(ctx context.Context, policy harness.NetworkPolicy) error {
	return n.policyManager.SetNetworkPolicy(ctx, policy)
}

// SetRequestTransformations delegates to the policy manager.
func (n *NetworkSession) SetRequestTransformations(ctx context.Context, transformations []harness.RequestTransformation) error {
	return n.policyManager.SetRequestTransformations(ctx, transformations)
}

// AddRequestTransformations delegates to the policy manager.
func (n *NetworkSession) AddRequestTransformations(ctx context.Context, transformations []harness.RequestTransformation) error {
	return n.policyManager.AddRequestTransformations(ctx, transformations)
}

// SetPorts replaces the exposed port list.
func (n *NetworkSession) SetPorts(ctx context.Context, ports []int) error {
	return n.sandbox.SetPorts(ctx, ports)
}

// Stop stops the sandbox. No-op when this session does not own the
// sandbox's lifecycle.
func (n *NetworkSession) Stop(ctx context.Context) error {
	if !n.ownsLifecycle {
		return nil
	}
	_, err := n.sandbox.Stop(ctx)
	return err
}

// Destroy stops then deletes the sandbox. No-op when this session does not
// own the sandbox's lifecycle. Must handle an already-stopped sandbox.
func (n *NetworkSession) Destroy(ctx context.Context) error {
	if !n.ownsLifecycle {
		return nil
	}
	_, _ = n.sandbox.Stop(ctx) // best-effort, matches TS `.catch(() => {})`
	return n.sandbox.Delete(ctx)
}

// Restricted returns a file-I/O/process-only view of the same sandbox.
func (n *NetworkSession) Restricted() providerutils.SandboxSession {
	return NewSession(n.sandbox)
}
