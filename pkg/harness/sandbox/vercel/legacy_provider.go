package vercel

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// LegacySettings configures LegacyProvider. Two mutually-exclusive shapes,
// mirroring TS `VercelSandboxSettings`:
//   - Sandbox set: wrap an already-created sandbox. The caller owns its
//     lifecycle; Stop/Destroy are no-ops.
//   - Sandbox unset: the provider creates the underlying sandbox per
//     session, optionally forking from a cached, prepared template snapshot
//     keyed by CreateSession's Identity.
//
// Deprecated: use CreateNetworkSandboxSession / ResumeNetworkSandboxSession
// for new code. Mirrors TS `VercelSandboxSettings` / `VercelSandboxProvider`
// (both deprecated there too).
type LegacySettings struct {
	Sandbox *Sandbox

	Credentials Credentials
	BaseURL     string
	// Name overrides the auto-derived template snapshot name
	// ("ai-sdk-harness-<identity>").
	Name string

	Runtime            string
	Image              string
	Source             *SnapshotSource
	TimeoutMs          int64
	Ports              []int
	Persistent         *bool
	NetworkPolicy      *NetworkPolicy
	SnapshotExpiration *int64
	Resources          *ResourcesParams
	Env                map[string]string
	Tags               map[string]string
	Region             string
	FailoverRegions    []string
	KeepLastSnapshots  *KeepLastSnapshotsParams
}

func (s LegacySettings) createParams() CreateParams {
	return CreateParams{
		Runtime: s.Runtime, Image: s.Image, Source: s.Source, TimeoutMs: s.TimeoutMs,
		Ports: s.Ports, Persistent: s.Persistent, NetworkPolicy: s.NetworkPolicy,
		SnapshotExpiration: s.SnapshotExpiration,
		Resources:          s.Resources,
		Env:                s.Env,
		Tags:               s.Tags,
		Region:             s.Region,
		FailoverRegions:    s.FailoverRegions,
		KeepLastSnapshots:  s.KeepLastSnapshots,
	}
}

// legacySnapshotCache is a process-wide identity -> snapshotID cache, mirroring
// TS's `Symbol.for('ai-sdk.harness.vercel-template-snapshots')` global Map:
// every LegacyProvider instance in the process shares it.
var legacySnapshotCache = newSnapshotCache()

// ResetLegacySnapshotCache clears the process-wide legacy template snapshot
// cache. Exposed for tests, mirroring the TS test suite's
// `globalThis[symbol]?.clear()` between cases.
func ResetLegacySnapshotCache() { legacySnapshotCache = newSnapshotCache() }

const legacySessionNamePrefix = "ai-sdk-harness-session"

func legacySessionSandboxName(sessionID string) string {
	return legacySessionNamePrefix + "-" + sessionID
}

// LegacyProvider implements harness.SandboxProvider and
// harness.SandboxSessionResumer. Construct via NewLegacyProvider.
//
// Deprecated: use CreateNetworkSandboxSession instead.
type LegacyProvider struct {
	settings LegacySettings
}

var (
	_ harness.SandboxProvider       = (*LegacyProvider)(nil)
	_ harness.SandboxSessionResumer = (*LegacyProvider)(nil)
)

// NewLegacyProvider constructs a deprecated sandbox provider backed by
// settings. Mirrors TS `createVercelSandbox`/`VercelSandboxProvider`.
func NewLegacyProvider(settings LegacySettings) *LegacyProvider {
	return &LegacyProvider{settings: settings}
}

// SpecificationVersion returns "harness-sandbox-v1".
func (p *LegacyProvider) SpecificationVersion() string { return harness.SandboxSpecificationVersion }

// ProviderID returns "vercel-sandbox".
func (p *LegacyProvider) ProviderID() string { return vercelProviderID }

// CreateSession creates (or wraps, or forks from a prepared template
// snapshot) a network sandbox session. Mirrors TS
// `VercelSandboxProvider.createSession`.
func (p *LegacyProvider) CreateSession(ctx context.Context, opts harness.CreateSandboxSessionOptions) (harness.NetworkSandboxSession, error) {
	if p.settings.Sandbox != nil {
		return NewNetworkSession(p.settings.Sandbox, false), nil
	}

	baseParams := withDefaultSandboxSettings(p.settings.createParams())

	creds, err := ResolveCredentials(p.settings.Credentials)
	if err != nil {
		return nil, withAuthError(err)
	}
	client := NewAPIClient(p.settings.BaseURL, creds)

	var sessionNameOverride string
	if opts.SessionID != "" {
		sessionNameOverride = legacySessionSandboxName(opts.SessionID)
	}

	if opts.Identity == "" || opts.OnFirstCreate == nil {
		params := baseParams
		if sessionNameOverride != "" {
			params.Name = sessionNameOverride
		}
		sbx, err := CreateSandbox(ctx, client, params)
		if err != nil {
			return nil, withAuthError(err)
		}
		return NewNetworkSession(sbx, true), nil
	}

	templateName := p.settings.Name
	if templateName == "" {
		templateName = "ai-sdk-harness-" + opts.Identity
	}
	snapshotID, err := ensureTemplateSnapshot(ctx, client, baseParams, templateName, legacySnapshotCache, func(ctx context.Context, sbx *Sandbox) error {
		return opts.OnFirstCreate(ctx, NewSession(sbx))
	})
	if err != nil {
		return nil, withAuthError(err)
	}

	fork, err := createLiveSandboxFromSnapshot(ctx, client, baseParams, snapshotID, sessionNameOverride)
	if err != nil {
		return nil, withAuthError(err)
	}
	return NewNetworkSession(fork, true), nil
}

// ResumeSession reattaches to the named session sandbox. Mirrors TS
// `VercelSandboxProvider.resumeSession`.
func (p *LegacyProvider) ResumeSession(ctx context.Context, sessionID string) (harness.NetworkSandboxSession, error) {
	if p.settings.Sandbox != nil {
		return NewNetworkSession(p.settings.Sandbox, false), nil
	}
	creds, err := ResolveCredentials(p.settings.Credentials)
	if err != nil {
		return nil, withAuthError(err)
	}
	client := NewAPIClient(p.settings.BaseURL, creds)
	sbx, err := GetSandbox(ctx, client, legacySessionSandboxName(sessionID), nil)
	if err != nil {
		return nil, withAuthError(err)
	}
	return NewNetworkSession(sbx, true), nil
}
