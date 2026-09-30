// Package vercel is a harness.SandboxProvider backed by Vercel Sandbox
// (https://vercel.com/docs/vercel-sandbox), talking its HTTP API directly.
//
// It ports packages/sandbox-vercel from the TypeScript AI SDK, which wraps
// the @vercel/sandbox npm client. Go has no such client, so this package
// re-implements the subset of its HTTP surface the harness adapter needs:
// create/get/fork/update/delete a sandbox, run/spawn commands with streaming
// logs, read/write files, and manage the network policy. See client.go for
// the endpoint-by-endpoint port of node_modules/@vercel/sandbox's
// api-client.js.
//
// Capability gaps vs the TS package (documented, not silently dropped):
//   - No `@vercel/oidc` token refresh loop or local-dev credential prompting
//     (`vercel link` / `vercel env pull`). VERCEL_OIDC_TOKEN is read fresh on
//     each top-level operation; explicit Token+TeamID+ProjectID work exactly
//     as in TS.
//   - No snapshot listing/tree, session listing, user/group management, or
//     `@workflow/serde` (de)serialization hooks — these are native-SDK
//     features unrelated to the harness sandbox surface and are not exposed
//     by packages/sandbox-vercel either.
package vercel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultSandboxTimeoutMs is 30 minutes: the @vercel/sandbox SDK defaults to
// 5 minutes, which is too short for multi-step harness workflows.
const DefaultSandboxTimeoutMs int64 = 30 * 60 * 1000

// DefaultSandboxRuntime is the runtime used when no runtime/image/snapshot
// source is given.
const DefaultSandboxRuntime = "node24"

const vercelAuthMessage = "Vercel Sandbox authentication failed. Set VERCEL_OIDC_TOKEN, or pass token, teamId, and projectId to createVercelSandbox(), then verify that they can access Vercel Sandbox."

func hasExplicitSandboxEnvironment(p CreateParams) bool {
	return p.Runtime != "" || p.Image != "" || (p.Source != nil && p.Source.Type == "snapshot")
}

// withDefaultSandboxSettings fills in the runtime/timeout defaults. Mirrors
// TS `withDefaultSandboxSettings`.
func withDefaultSandboxSettings(p CreateParams) CreateParams {
	if !hasExplicitSandboxEnvironment(p) {
		p.Runtime = DefaultSandboxRuntime
	}
	if p.TimeoutMs == 0 {
		p.TimeoutMs = DefaultSandboxTimeoutMs
	}
	return p
}

// withAuthError wraps err as a harness.SandboxAuthenticationError when it is
// a credential-resolution failure or a 401/403 API response. Other errors
// pass through unchanged. Mirrors TS `withVercelSandboxAuthenticationError`,
// simplified: this port does not reproduce the @vercel/oidc error-name/opaque
// message heuristics, since it never delegates to that library (see package
// doc).
func withAuthError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNoCredentials) {
		return harness.NewSandboxAuthenticationError(vercelAuthMessage, vercelProviderID, err)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && isAuthErrorStatus(apiErr.StatusCode) {
		return harness.NewSandboxAuthenticationError(vercelAuthMessage, vercelProviderID, err)
	}
	return err
}

// Template describes a one-time sandbox preparation recipe: `Identity`
// selects a stable, cached snapshot; `Prepare` runs against a fresh sandbox
// the first time that identity is seen. Mirrors TS
// `HarnessV1SandboxSessionCreateOptions.template`.
type Template struct {
	Identity string
	Prepare  func(ctx context.Context, session providerutils.SandboxSession) error
}

// CreateSessionOptions is the input of CreateNetworkSandboxSession. Mirrors
// TS `VercelNetworkSandboxSessionCreateOptions`.
type CreateSessionOptions struct {
	Credentials Credentials
	// BaseURL overrides the Vercel API base URL (tests only).
	BaseURL string

	SandboxID string
	Name      string

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

	Template *Template
}

// CreateNetworkSandboxSession creates a fresh sandbox (or forks one from a
// template snapshot) and returns it as a harness.NetworkSandboxSession.
// Mirrors TS `createVercelNetworkSandboxSession`.
func CreateNetworkSandboxSession(ctx context.Context, opts CreateSessionOptions) (harness.NetworkSandboxSession, error) {
	if opts.SandboxID != "" && opts.Name != "" && opts.SandboxID != opts.Name {
		return nil, errors.New("createVercelNetworkSandboxSession: sandboxId and name must match when both are provided.")
	}
	liveName := opts.SandboxID
	if liveName == "" {
		liveName = opts.Name
	}
	baseParams := withDefaultSandboxSettings(CreateParams{
		Runtime: opts.Runtime, Image: opts.Image, Source: opts.Source, TimeoutMs: opts.TimeoutMs,
		Ports: opts.Ports, Persistent: opts.Persistent, NetworkPolicy: opts.NetworkPolicy,
		SnapshotExpiration: opts.SnapshotExpiration,
		Resources:          opts.Resources,
		Env:                opts.Env,
		Tags:               opts.Tags,
		Region:             opts.Region,
		FailoverRegions:    opts.FailoverRegions,
		KeepLastSnapshots:  opts.KeepLastSnapshots,
	})

	creds, err := ResolveCredentials(opts.Credentials)
	if err != nil {
		return nil, withAuthError(err)
	}
	client := NewAPIClient(opts.BaseURL, creds)

	if opts.Template == nil {
		params := baseParams
		if liveName != "" {
			params.Name = liveName
		}
		sbx, err := CreateSandbox(ctx, client, params)
		if err != nil {
			return nil, withAuthError(err)
		}
		return NewNetworkSession(sbx, true), nil
	}

	templateName := deriveTemplateName(opts.Template.Identity, baseParams)
	snapshotID, err := ensureTemplateSnapshot(ctx, client, baseParams, templateName, nil, func(ctx context.Context, sbx *Sandbox) error {
		return opts.Template.Prepare(ctx, NewSession(sbx))
	})
	if err != nil {
		return nil, withAuthError(err)
	}
	liveSandbox, err := createLiveSandboxFromSnapshot(ctx, client, baseParams, snapshotID, liveName)
	if err != nil {
		return nil, withAuthError(err)
	}
	return NewNetworkSession(liveSandbox, true), nil
}

// ResumeSessionOptions is the input of ResumeNetworkSandboxSession.
type ResumeSessionOptions struct {
	Credentials Credentials
	BaseURL     string
	SandboxID   string
}

// ResumeNetworkSandboxSession resumes an existing named sandbox. Mirrors TS
// `resumeVercelNetworkSandboxSession`.
func ResumeNetworkSandboxSession(ctx context.Context, opts ResumeSessionOptions) (harness.NetworkSandboxSession, error) {
	creds, err := ResolveCredentials(opts.Credentials)
	if err != nil {
		return nil, withAuthError(err)
	}
	client := NewAPIClient(opts.BaseURL, creds)
	resume := true
	sbx, err := GetSandbox(ctx, client, opts.SandboxID, &resume)
	if err != nil {
		return nil, withAuthError(err)
	}
	return NewNetworkSession(sbx, true), nil
}

// NetworkSessionFromNativeSandbox wraps an already-created *Sandbox as a
// network session whose lifecycle this call owns. Mirrors TS
// `createVercelNetworkSandboxSessionFromNativeSandbox`.
func NetworkSessionFromNativeSandbox(sandbox *Sandbox) harness.NetworkSandboxSession {
	return NewNetworkSession(sandbox, true)
}

// SessionFromNativeSandbox wraps an already-created *Sandbox as a restricted
// (file I/O + exec only) session. Mirrors TS
// `createVercelSandboxSessionFromNativeSandbox`.
func SessionFromNativeSandbox(sandbox *Sandbox) providerutils.SandboxSession {
	return NewSession(sandbox)
}

// deriveTemplateName computes the stable, content-addressed template
// snapshot name for (identity, environment selection). Mirrors TS's
// `ai-sdk-harness-v2-${sha256(material).slice(0,12).hex}` derivation in
// `createVercelNetworkSandboxSession`. The exact hash algorithm need not
// match TS byte-for-byte (each runs in its own process and only compares
// against its own prior calls); determinism and injectivity in (identity,
// selection) are what matters.
func deriveTemplateName(identity string, baseParams CreateParams) string {
	var selection interface{}
	switch {
	case baseParams.Source != nil:
		selection = baseParams.Source
	case baseParams.Image != "":
		selection = map[string]string{"image": baseParams.Image}
	default:
		selection = map[string]string{"runtime": baseParams.Runtime}
	}
	material, _ := json.Marshal([]interface{}{2, identity, selection})
	digest := sha256.Sum256(material)
	return "ai-sdk-harness-v2-" + hex.EncodeToString(digest[:12])
}

// snapshotCache is a small in-process identity -> snapshotID cache, mirroring
// TS's `getSnapshotCache()` global Map (used only by the legacy provider, to
// skip a Get round-trip for identities it already resolved in this process).
type snapshotCache struct {
	mu sync.Mutex
	m  map[string]string
}

func newSnapshotCache() *snapshotCache { return &snapshotCache{m: map[string]string{}} }

func (c *snapshotCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *snapshotCache) set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = value
}

// ensureTemplateSnapshot gets-or-creates a persistent named template sandbox,
// preparing it via onCreate exactly once, then returns a snapshot ID for it
// (stopping it to produce one if it doesn't already have one). Mirrors TS
// `ensureTemplateSnapshot`.
func ensureTemplateSnapshot(ctx context.Context, client *APIClient, baseParams CreateParams, templateName string, cache *snapshotCache, onCreate func(context.Context, *Sandbox) error) (string, error) {
	if cache != nil {
		if id, ok := cache.get(templateName); ok {
			return id, nil
		}
	}

	templateParams := baseParams
	templateParams.Name = templateName
	persistent := true
	templateParams.Persistent = &persistent
	if templateParams.SnapshotExpiration == nil {
		zero := int64(0)
		templateParams.SnapshotExpiration = &zero
	}

	prepared, err := GetOrCreateSandbox(ctx, client, templateParams, onCreate)
	if err != nil {
		return "", err
	}

	snapshotID := prepared.CurrentSnapshotID()
	if snapshotID == "" {
		stoppedSnapshotID, err := prepared.Stop(ctx)
		if err != nil {
			return "", err
		}
		snapshotID = stoppedSnapshotID
		if snapshotID == "" {
			snapshotID, err = pollForTemplateSnapshot(ctx, client, templateName)
			if err != nil {
				return "", err
			}
		}
	}

	if cache != nil {
		cache.set(templateName, snapshotID)
	}
	return snapshotID, nil
}

const (
	snapshotPollInterval = 500 * time.Millisecond
	snapshotPollTimeout  = 30 * time.Second
)

// pollForTemplateSnapshot polls a named sandbox until it reports a snapshot
// ID. Mirrors TS `pollForTemplateSnapshot`.
func pollForTemplateSnapshot(ctx context.Context, client *APIClient, name string) (string, error) {
	deadline := time.Now().Add(snapshotPollTimeout)
	resumeFalse := false
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sbx, err := GetSandbox(ctx, client, name, &resumeFalse)
		if err != nil {
			return "", err
		}
		if sbx.CurrentSnapshotID() != "" {
			return sbx.CurrentSnapshotID(), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(snapshotPollInterval):
		}
	}
	return "", fmt.Errorf("Timed out waiting for snapshot of template %q to publish.", name)
}

// createLiveSandboxFromSnapshot forks a live sandbox from a prepared
// snapshot. Mirrors TS `createLiveSandboxFromSnapshot`: runtime, image,
// source, and persistent are stripped from baseParams (the snapshot's own
// values apply), and liveName overrides the sandbox name when given.
func createLiveSandboxFromSnapshot(ctx context.Context, client *APIClient, baseParams CreateParams, snapshotID, liveName string) (*Sandbox, error) {
	forkParams := baseParams
	forkParams.Runtime = ""
	forkParams.Image = ""
	forkParams.Persistent = nil
	forkParams.Source = &SnapshotSource{Type: "snapshot", SnapshotID: snapshotID}
	if liveName != "" {
		forkParams.Name = liveName
	} else {
		forkParams.Name = ""
	}
	return CreateSandbox(ctx, client, forkParams)
}
