package bridge

import (
	"context"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ValidateBridgeSandboxSettings validates the bridge connection settings
// against the sandbox session before any bridge work starts. Mirrors TS
// `validateBridgeSandboxSettings`.
//
// Basic sandbox sessions cannot expose ports, so they need both an explicit
// port and portEndpoint. A custom MintBridgeToken callback needs a sandbox id
// to mint the token for. Errors are *harness.CapabilityUnsupportedError with
// the harness-neutral TS wording; harnessID is only structured context.
func ValidateBridgeSandboxSettings(sandbox providerutils.SandboxSession, sandboxID string, port int, portEndpoint *harness.PortEndpoint, mint harness.MintBridgeTokenCallback, harnessID string) error {
	if _, isNetwork := harness.AsNetworkSandboxSession(sandbox); !isNetwork {
		if port == 0 {
			return harness.NewCapabilityUnsupportedError("The harness requires an explicit `port` setting when using a basic sandbox session.", harnessID, nil)
		}
		if portEndpoint == nil {
			return missingPortEndpointError(harnessID)
		}
	}
	if mint != nil && sandboxID == "" {
		return harness.NewCapabilityUnsupportedError("The harness cannot use `mintBridgeToken` with a sandbox session that does not expose an id.", harnessID, nil)
	}
	return nil
}

// ResolveBridgePortOrUnsupported is ResolveBridgePort with the TS error: the
// port setting if present, otherwise the first port the sandbox exposes.
func ResolveBridgePortOrUnsupported(sandbox providerutils.SandboxSession, port int, harnessID string) (int, error) {
	resolved, err := ResolveBridgePort(sandbox, port)
	if errors.Is(err, ErrNoPort) {
		return 0, harness.NewCapabilityUnsupportedError(
			"The harness needs a TCP port exposed by the sandbox. Create the sandbox with `ports: [<port>]` or configure the harness `port` setting.",
			harnessID, err)
	}
	return resolved, err
}

// ResolveBridgeEndpointOrUnsupported resolves the host-reachable WebSocket
// endpoint for a bridge port, without a token. A portEndpoint setting takes
// precedence and is used as-is regardless of port. Mirrors TS
// `resolveBridgeEndpoint` (the caller adds the token with WithBridgeToken).
func ResolveBridgeEndpointOrUnsupported(ctx context.Context, sandbox providerutils.SandboxSession, override *harness.PortEndpoint, port int, harnessID string) (harness.PortEndpoint, error) {
	endpoint, err := ResolveBridgeEndpoint(ctx, sandbox, override, port)
	if errors.Is(err, ErrNoPortEndpoint) {
		return harness.PortEndpoint{}, missingPortEndpointError(harnessID)
	}
	return endpoint, err
}

func missingPortEndpointError(harnessID string) error {
	return harness.NewCapabilityUnsupportedError("The harness requires an explicit `portEndpoint` setting when using a basic sandbox session.", harnessID, nil)
}
