package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SandboxCreator creates the sandbox session for Agent.CreateSession when it
// is needed instead of upfront. Mirrors TS `HarnessAgentSandboxCreator`
// (TS 8a20b57d41), which TS accepts in place of `sandboxSession`; Go takes it
// as the separate CreateSessionOptions.SandboxCreator field.
//
// For a harness without a bootstrap recipe (no BootstrapProvider),
// CreateSandboxSession runs on the first file, exec or spawn operation, so a
// session that never touches the sandbox never creates it; a failed call is
// retried on the next operation. For any other harness it runs in
// CreateSession. Either way the caller owns the sandbox lifecycle.
type SandboxCreator struct {
	// DefaultWorkingDirectory is the working directory of the sandbox that
	// CreateSandboxSession returns. The session work directory is derived
	// from it before the sandbox exists.
	DefaultWorkingDirectory string
	// CreateSandboxSession creates the sandbox session. It may return a
	// NetworkSandboxSession.
	CreateSandboxSession func(ctx context.Context) (providerutils.SandboxSession, error)
}

// sandboxSetupGate runs a setup once, on the first operation that needs it.
// Concurrent first operations share one run; a failed run is reported to the
// operations that waited on it and retried by the next one. Mirrors TS
// `ensureSetup` (`setupCompleted ??= setup().catch(...)`).
type sandboxSetupGate struct {
	setup func(ctx context.Context) error

	mu       sync.Mutex
	done     bool
	inflight *sandboxSetupRun
}

type sandboxSetupRun struct {
	finished chan struct{}
	err      error
}

// ensure runs the setup unless it already succeeded. The setup does not stop
// when the triggering operation's ctx is cancelled (TS passes no abort signal
// to a deferred setup); the operation itself still returns on cancellation.
func (g *sandboxSetupGate) ensure(ctx context.Context) error {
	g.mu.Lock()
	if g.done {
		g.mu.Unlock()
		return nil
	}
	run := g.inflight
	if run == nil {
		run = &sandboxSetupRun{finished: make(chan struct{})}
		g.inflight = run
		go func(setupCtx context.Context) {
			err := g.setup(setupCtx)
			g.mu.Lock()
			run.err = err
			g.inflight = nil
			if err == nil {
				g.done = true
			}
			g.mu.Unlock()
			close(run.finished)
		}(context.WithoutCancel(ctx))
	}
	g.mu.Unlock()
	select {
	case <-run.finished:
		return run.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// gatedSandboxSession delegates every file, exec and spawn operation to the
// session resolve returns. Mirrors TS `delegateSandboxOperations`.
type gatedSandboxSession struct {
	description func() string
	resolve     func(ctx context.Context) (providerutils.SandboxSession, error)
}

var _ providerutils.SandboxSession = (*gatedSandboxSession)(nil)

func (s *gatedSandboxSession) Description() string { return s.description() }

func (s *gatedSandboxSession) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return providerutils.SandboxRunResult{}, err
	}
	return target.Run(ctx, opts)
}

func (s *gatedSandboxSession) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.Spawn(ctx, opts)
}

func (s *gatedSandboxSession) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.ReadFile(ctx, path)
}

func (s *gatedSandboxSession) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.ReadBinaryFile(ctx, path)
}

func (s *gatedSandboxSession) ReadTextFile(ctx context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return target.ReadTextFile(ctx, opts)
}

func (s *gatedSandboxSession) WriteFile(ctx context.Context, path string, content io.Reader) error {
	target, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return target.WriteFile(ctx, path, content)
}

func (s *gatedSandboxSession) WriteBinaryFile(ctx context.Context, path string, content []byte) error {
	target, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return target.WriteBinaryFile(ctx, path, content)
}

func (s *gatedSandboxSession) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	target, err := s.resolve(ctx)
	if err != nil {
		return err
	}
	return target.WriteTextFile(ctx, opts)
}

// Stat, ListDirectory and DeleteFile are required TS SandboxSession methods
// that TS delegateSandboxOperations forwards; in Go they are the optional
// providerutils.SandboxStatter, SandboxDirectoryLister and
// SandboxFileDeleter. The gated session always has them, waits for the
// setup and forwards to the target, or reports errors.ErrUnsupported when
// the target does not implement the method.
var (
	_ providerutils.SandboxStatter         = (*gatedSandboxSession)(nil)
	_ providerutils.SandboxDirectoryLister = (*gatedSandboxSession)(nil)
	_ providerutils.SandboxFileDeleter     = (*gatedSandboxSession)(nil)
)

func unsupportedSandboxOperation(method string) error {
	return fmt.Errorf("harness: the sandbox session does not implement %s: %w", method, errors.ErrUnsupported)
}

func (s *gatedSandboxSession) Stat(ctx context.Context, path string) (*providerutils.SandboxFileStat, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	statter, ok := target.(providerutils.SandboxStatter)
	if !ok {
		return nil, unsupportedSandboxOperation("Stat")
	}
	return statter.Stat(ctx, path)
}

func (s *gatedSandboxSession) ListDirectory(ctx context.Context, path string) ([]providerutils.SandboxDirectoryEntry, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return nil, err
	}
	lister, ok := target.(providerutils.SandboxDirectoryLister)
	if !ok {
		return nil, unsupportedSandboxOperation("ListDirectory")
	}
	return lister.ListDirectory(ctx, path)
}

func (s *gatedSandboxSession) DeleteFile(ctx context.Context, path string) (bool, error) {
	target, err := s.resolve(ctx)
	if err != nil {
		return false, err
	}
	deleter, ok := target.(providerutils.SandboxFileDeleter)
	if !ok {
		return false, unsupportedSandboxOperation("DeleteFile")
	}
	return deleter.DeleteFile(ctx, path)
}

// gateSandboxSession wraps target so every operation waits for gate.
func gateSandboxSession(target providerutils.SandboxSession, gate *sandboxSetupGate) *gatedSandboxSession {
	return &gatedSandboxSession{
		description: target.Description,
		resolve: func(ctx context.Context) (providerutils.SandboxSession, error) {
			if err := gate.ensure(ctx); err != nil {
				return nil, err
			}
			return target, nil
		},
	}
}

// deferredNetworkSandboxSession is a NetworkSandboxSession whose file, exec
// and spawn operations, on itself and on its restricted view, wait for the
// session setup. Identity, port, lifecycle and network methods pass straight
// through. Mirrors the `restricted` branch of TS `deferSandboxSetup`.
type deferredNetworkSandboxSession struct {
	*gatedSandboxSession
	original NetworkSandboxSession
	gate     *sandboxSetupGate

	restrictedOnce sync.Once
	restricted     providerutils.SandboxSession
}

var _ NetworkSandboxSession = (*deferredNetworkSandboxSession)(nil)

func (s *deferredNetworkSandboxSession) ID() string { return s.original.ID() }
func (s *deferredNetworkSandboxSession) DefaultWorkingDirectory() string {
	return s.original.DefaultWorkingDirectory()
}
func (s *deferredNetworkSandboxSession) Ports() []int { return s.original.Ports() }
func (s *deferredNetworkSandboxSession) GetPortEndpoint(ctx context.Context, opts PortEndpointOptions) (PortEndpoint, error) {
	return s.original.GetPortEndpoint(ctx, opts)
}
func (s *deferredNetworkSandboxSession) GetPortURL(ctx context.Context, opts PortEndpointOptions) (string, error) {
	return s.original.GetPortURL(ctx, opts) //nolint:staticcheck // forwards the deprecated method for callers that still use it
}
func (s *deferredNetworkSandboxSession) Stop(ctx context.Context) error { return s.original.Stop(ctx) }
func (s *deferredNetworkSandboxSession) Destroy(ctx context.Context) error {
	return s.original.Destroy(ctx)
}

// Restricted returns the gated restricted view (TS `restricted ??=
// gate(original.restricted())`).
func (s *deferredNetworkSandboxSession) Restricted() providerutils.SandboxSession {
	s.restrictedOnce.Do(func() { s.restricted = gateSandboxSession(s.original.Restricted(), s.gate) })
	return s.restricted
}

// The optional network methods are forwarded only when the original session
// has them (TS spreads `setNetworkPolicy` and the others only when defined),
// so a type assertion on the deferred session answers as it would on the
// original.
type deferredPolicy struct{ s NetworkPolicySetter }

func (d deferredPolicy) SetNetworkPolicy(ctx context.Context, policy NetworkPolicy) error {
	return d.s.SetNetworkPolicy(ctx, policy)
}

type deferredTransformSetter struct{ s RequestTransformationSetter }

func (d deferredTransformSetter) SetRequestTransformations(ctx context.Context, transformations []RequestTransformation) error {
	return d.s.SetRequestTransformations(ctx, transformations)
}

type deferredTransformAdder struct{ s RequestTransformationAdder }

func (d deferredTransformAdder) AddRequestTransformations(ctx context.Context, transformations []RequestTransformation) error {
	return d.s.AddRequestTransformations(ctx, transformations)
}

type deferredPorts struct{ s PortsSetter }

func (d deferredPorts) SetPorts(ctx context.Context, ports []int) error {
	return d.s.SetPorts(ctx, ports)
}

// withOptionalNetworkMethods returns base extended with exactly the optional
// network methods original implements.
func withOptionalNetworkMethods(base *deferredNetworkSandboxSession, original NetworkSandboxSession) NetworkSandboxSession {
	policy, hasPolicy := original.(NetworkPolicySetter)
	setter, hasSetter := original.(RequestTransformationSetter)
	adder, hasAdder := original.(RequestTransformationAdder)
	ports, hasPorts := original.(PortsSetter)
	p, ts, ta, ps := deferredPolicy{policy}, deferredTransformSetter{setter}, deferredTransformAdder{adder}, deferredPorts{ports}
	switch {
	case hasPolicy && hasSetter && hasAdder && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformSetter
			deferredTransformAdder
			deferredPorts
		}{base, p, ts, ta, ps}
	case hasPolicy && hasSetter && hasAdder:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformSetter
			deferredTransformAdder
		}{base, p, ts, ta}
	case hasPolicy && hasSetter && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformSetter
			deferredPorts
		}{base, p, ts, ps}
	case hasPolicy && hasAdder && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformAdder
			deferredPorts
		}{base, p, ta, ps}
	case hasSetter && hasAdder && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformSetter
			deferredTransformAdder
			deferredPorts
		}{base, ts, ta, ps}
	case hasPolicy && hasSetter:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformSetter
		}{base, p, ts}
	case hasPolicy && hasAdder:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredTransformAdder
		}{base, p, ta}
	case hasPolicy && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
			deferredPorts
		}{base, p, ps}
	case hasSetter && hasAdder:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformSetter
			deferredTransformAdder
		}{base, ts, ta}
	case hasSetter && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformSetter
			deferredPorts
		}{base, ts, ps}
	case hasAdder && hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformAdder
			deferredPorts
		}{base, ta, ps}
	case hasPolicy:
		return struct {
			*deferredNetworkSandboxSession
			deferredPolicy
		}{base, p}
	case hasSetter:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformSetter
		}{base, ts}
	case hasAdder:
		return struct {
			*deferredNetworkSandboxSession
			deferredTransformAdder
		}{base, ta}
	case hasPorts:
		return struct {
			*deferredNetworkSandboxSession
			deferredPorts
		}{base, ps}
	}
	return base
}

// deferSandboxSetup returns session with its file, exec and spawn operations
// gated on setup, which runs once on the first of them. Mirrors TS
// `deferSandboxSetup`.
func deferSandboxSetup(session providerutils.SandboxSession, setup func(ctx context.Context) error) providerutils.SandboxSession {
	gate := &sandboxSetupGate{setup: setup}
	original, ok := AsNetworkSandboxSession(session)
	if !ok {
		return gateSandboxSession(session, gate)
	}
	base := &deferredNetworkSandboxSession{gatedSandboxSession: gateSandboxSession(original, gate), original: original, gate: gate}
	return withOptionalNetworkMethods(base, original)
}

// deferSandboxSession returns a plain SandboxSession that creates the sandbox
// with createSandboxSession and sets it up on the first file, exec or spawn
// operation, then delegates to its restricted view. A failed creation or
// setup is retried on the next operation; a created sandbox is kept. Mirrors
// TS `deferSandboxSession`.
func deferSandboxSession(defaultWorkingDirectory string, createSandboxSession func(ctx context.Context) (providerutils.SandboxSession, error), setup func(ctx context.Context, session providerutils.SandboxSession) error) providerutils.SandboxSession {
	var mu sync.Mutex
	var created providerutils.SandboxSession
	var ready providerutils.SandboxSession
	gate := &sandboxSetupGate{}
	gate.setup = func(ctx context.Context) error {
		mu.Lock()
		session := created
		mu.Unlock()
		if session == nil {
			var err error
			if session, err = createSandboxSession(ctx); err != nil {
				return err
			}
			mu.Lock()
			created = session
			mu.Unlock()
		}
		if err := setup(ctx, session); err != nil {
			return err
		}
		mu.Lock()
		ready = GetRestrictedSandboxSession(session)
		mu.Unlock()
		return nil
	}
	return &gatedSandboxSession{
		description: func() string {
			mu.Lock()
			session := created
			mu.Unlock()
			if session != nil {
				return session.Description()
			}
			return "Sandbox with the working directory " + defaultWorkingDirectory + ". It starts on first use."
		},
		resolve: func(ctx context.Context) (providerutils.SandboxSession, error) {
			if err := gate.ensure(ctx); err != nil {
				return nil, err
			}
			mu.Lock()
			defer mu.Unlock()
			return ready, nil
		},
	}
}
