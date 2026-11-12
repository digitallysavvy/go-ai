package vercel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// CreateParams mirrors the subset of `@vercel/sandbox`'s `Sandbox.create`
// parameters this package uses (TS `BaseCreateSandboxParams`).
type CreateParams struct {
	Runtime            string
	Image              string
	Source             *SnapshotSource
	TimeoutMs          int64
	Ports              []int
	Name               string
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

func (p CreateParams) toRequest() createSandboxRequest {
	return createSandboxRequest{
		Ports:              p.Ports,
		Source:             p.Source,
		Timeout:            p.TimeoutMs,
		Runtime:            p.Runtime,
		Image:              p.Image,
		Name:               p.Name,
		Persistent:         p.Persistent,
		NetworkPolicy:      toAPINetworkPolicy(p.NetworkPolicy),
		SnapshotExpiration: p.SnapshotExpiration,
		Resources:          p.Resources,
		Env:                p.Env,
		Tags:               p.Tags,
		Region:             p.Region,
		FailoverRegions:    p.FailoverRegions,
		KeepLastSnapshots:  p.KeepLastSnapshots,
	}
}

// UpdateParams mirrors TS `Sandbox.update` parameters.
type UpdateParams struct {
	Ports              []int
	NetworkPolicy      *NetworkPolicy
	Persistent         *bool
	TimeoutMs          *int64
	SnapshotExpiration *int64
}

// Sandbox is a native-SDK-shaped handle on one Vercel Sandbox + its current
// session, backed directly by HTTP calls. It plays the role of
// `@vercel/sandbox`'s `Sandbox` class for the rest of this package.
type Sandbox struct {
	client *APIClient

	name              string
	persistent        bool
	currentSnapshotID string

	sessionID string
	// sessionStatus is the current session's status as the API last
	// reported it (TS `this.session.status`).
	sessionStatus string
	// sessionTimeout is the current session's timeout in milliseconds as
	// the API last reported it (TS `this.session.timeout`).
	sessionTimeout int64
	cwd            string
	routes         []sandboxRouteWire
	networkPolicy  NetworkPolicy // current session's network policy (zero value = unset)
}

func newSandboxFromResponse(client *APIClient, resp *sandboxAndSessionResponse) *Sandbox {
	s := &Sandbox{
		client:            client,
		name:              resp.Sandbox.Name,
		persistent:        resp.Sandbox.Persistent,
		currentSnapshotID: resp.Sandbox.CurrentSnapshotID,
		sessionID:         resp.Session.ID,
		sessionStatus:     resp.Session.Status,
		sessionTimeout:    resp.Session.Timeout,
		cwd:               resp.Session.CWD,
		routes:            resp.Routes,
	}
	if resp.Session.NetworkPolicy != nil {
		s.networkPolicy = *resp.Session.NetworkPolicy
	}
	return s
}

// CreateSandbox posts a create request and wraps the result.
func CreateSandbox(ctx context.Context, client *APIClient, params CreateParams) (*Sandbox, error) {
	if err := checkRuntimeOrImage(params); err != nil {
		return nil, err
	}
	resp, err := client.CreateSandbox(ctx, params.toRequest())
	if err != nil {
		return nil, err
	}
	return newSandboxFromResponse(client, resp), nil
}

// checkRuntimeOrImage is 3.9.0 Sandbox.create / getOrCreate's TypeError for
// a runtime and an image given together, raised before any request (extra,
// step 5).
func checkRuntimeOrImage(params CreateParams) error {
	if params.Runtime != "" && params.Image != "" {
		return errors.New("`runtime` and `image` cannot be used together.") //nolint:staticcheck // matches @vercel/sandbox TypeError text
	}
	return nil
}

// GetSandbox fetches an existing sandbox by name, optionally resuming.
func GetSandbox(ctx context.Context, client *APIClient, name string, resume *bool) (*Sandbox, error) {
	resp, err := client.GetSandbox(ctx, name, resume)
	if err != nil {
		return nil, err
	}
	return newSandboxFromResponse(client, resp), nil
}

// GetOrCreateSandbox mirrors TS `Sandbox.getOrCreate`: with no Name, it
// always creates (calling onCreate). With a Name, it tries Get first; on
// not_found it creates; on snapshot_not_found it deletes the stale named
// sandbox first, then creates. onCreate runs exactly once, only on a fresh
// create.
func GetOrCreateSandbox(ctx context.Context, client *APIClient, params CreateParams, onCreate func(context.Context, *Sandbox) error) (*Sandbox, error) {
	if err := checkRuntimeOrImage(params); err != nil {
		return nil, err
	}
	create := func() (*Sandbox, error) {
		sbx, err := CreateSandbox(ctx, client, params)
		if err != nil {
			return nil, err
		}
		if onCreate != nil {
			if err := onCreate(ctx, sbx); err != nil {
				return nil, err
			}
		}
		return sbx, nil
	}
	if params.Name == "" {
		return create()
	}
	sbx, err := GetSandbox(ctx, client, params.Name, nil)
	if err == nil {
		return sbx, nil
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		return nil, err
	}
	if apiErr.IsNotFound() {
		return create()
	}
	if apiErr.IsSnapshotNotFound() {
		if delErr := client.DeleteSandbox(ctx, params.Name); delErr != nil {
			if de, ok := delErr.(*APIError); !ok || !de.IsNotFound() {
				return nil, delErr
			}
		}
		return create()
	}
	return nil, err
}

// Name is the sandbox's name.
func (s *Sandbox) Name() string { return s.name }

// CWD is the running session's default working directory.
func (s *Sandbox) CWD() string { return s.cwd }

// Persistent reports whether the sandbox persists state.
func (s *Sandbox) Persistent() bool { return s.persistent }

// CurrentSnapshotID is the sandbox's current snapshot ID, if any.
func (s *Sandbox) CurrentSnapshotID() string { return s.currentSnapshotID }

// Routes are the port -> subdomain mappings currently exposed.
func (s *Sandbox) Routes() []sandboxRouteWire { return s.routes }

// Ports returns the exposed port numbers (TS `Sandbox.routes.map(r =>
// r.port)`).
func (s *Sandbox) Ports() []int {
	ports := make([]int, len(s.routes))
	for i, r := range s.routes {
		ports[i] = r.Port
	}
	return ports
}

// Domain resolves the public domain for port p (TS `Sandbox.domain`).
func (s *Sandbox) Domain(port int) (string, error) {
	for _, r := range s.routes {
		if r.Port == port {
			return "https://" + r.Subdomain + ".vercel.run", nil
		}
	}
	return "", fmt.Errorf("No route for port %d", port) //nolint:staticcheck // matches TS SDK's exact error text
}

// CurrentNetworkPolicy implements PolicySandbox: the running session's
// network policy, defaulting to allow-all when unset.
func (s *Sandbox) CurrentNetworkPolicy() NetworkPolicy {
	if s.networkPolicy.IsZero() {
		return AllowAllNetworkPolicy()
	}
	return s.networkPolicy
}

// CurrentSessionNetworkPolicyMetadata implements PolicySessionInspector like
// TS VercelNetworkPolicyManager #inspectPolicy: it lists the sandbox's
// sessions (`sandbox.listSessions()`, every page until the current one is
// found) and returns the current session's networkPolicy metadata as the API
// sent it, or nil when the session is not listed or carries none.
func (s *Sandbox) CurrentSessionNetworkPolicyMetadata(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.client.eachSession(ctx, s.name, func(w sessionWire) bool {
		if w.ID != s.sessionID {
			return false
		}
		raw = w.RawNetworkPolicy
		return true
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// UpdateNetworkPolicy implements PolicySandbox via the full Sandbox.update
// path (matches TS `VercelNetworkPolicyManager` calling `sandbox.update`, not
// the session-only endpoint).
func (s *Sandbox) UpdateNetworkPolicy(ctx context.Context, policy NetworkPolicy) error {
	return s.Update(ctx, UpdateParams{NetworkPolicy: &policy})
}

// Update applies params. Mirrors @vercel/sandbox 3.9.0 `Sandbox.update`: a
// timeout larger than the running session's current timeout first extends
// that session by the difference (POST .../extend-timeout; a 410 or a 422
// sandbox_stopping response is ignored, as the session is gone), then a
// network policy is applied to the current session (POST .../network-policy)
// unless that session is stopped, stopping, aborted or failed, and only then
// is the sandbox PATCHed, so a policy the session rejects is not persisted
// for later sessions.
func (s *Sandbox) Update(ctx context.Context, params UpdateParams) error {
	if params.TimeoutMs != nil && s.sessionID != "" && s.sessionStatus == "running" {
		if increment := *params.TimeoutMs - s.sessionTimeout; increment > 0 {
			sessResp, err := s.client.ExtendSessionTimeout(ctx, s.sessionID, increment)
			if err != nil {
				if !isSandboxStoppedOrStopping(err) {
					return err
				}
			} else {
				s.applySession(sessResp.Session)
			}
		}
	}
	if params.NetworkPolicy != nil && s.sessionID != "" && sessionAcceptsUpdates(s.sessionStatus) {
		sessResp, err := s.client.UpdateSessionNetworkPolicy(ctx, s.sessionID, *params.NetworkPolicy)
		if err != nil {
			return err
		}
		s.sessionStatus = sessResp.Session.Status
		s.sessionTimeout = sessResp.Session.Timeout
		if sessResp.Session.NetworkPolicy != nil {
			s.networkPolicy = *sessResp.Session.NetworkPolicy
		} else {
			s.networkPolicy = *params.NetworkPolicy
		}
	}
	req := updateSandboxRequest{
		Persistent:         params.Persistent,
		NetworkPolicy:      toAPINetworkPolicy(params.NetworkPolicy),
		Ports:              params.Ports,
		SnapshotExpiration: params.SnapshotExpiration,
	}
	if params.TimeoutMs != nil {
		req.Timeout = *params.TimeoutMs
	}
	resp, err := s.client.UpdateSandbox(ctx, s.name, req)
	if err != nil {
		return err
	}
	s.persistent = resp.Sandbox.Persistent
	s.currentSnapshotID = resp.Sandbox.CurrentSnapshotID
	if params.Ports != nil && resp.Routes != nil {
		s.routes = resp.Routes
	}
	return nil
}

// applySession replaces the tracked session state with a session the API
// returned (TS `this.session = toSandboxSnapshot(response.json.session)`).
func (s *Sandbox) applySession(w sessionWire) {
	s.sessionStatus = w.Status
	s.sessionTimeout = w.Timeout
	if w.NetworkPolicy != nil {
		s.networkPolicy = *w.NetworkPolicy
	} else {
		s.networkPolicy = NetworkPolicy{}
	}
}

// isSandboxStoppedOrStopping is 3.9.0 `isSandboxStoppedError(err) ||
// isSandboxStoppingError(err)`: a 410, or a 422 with code sandbox_stopping.
func isSandboxStoppedOrStopping(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == 410 || (apiErr.StatusCode == 422 && apiErr.Code == "sandbox_stopping")
}

// sessionAcceptsUpdates is the 3.9.0 `Sandbox.update` session check.
func sessionAcceptsUpdates(status string) bool {
	switch status {
	case "stopped", "stopping", "aborted", "failed":
		return false
	}
	return true
}

// SetPorts replaces the exposed port list (a thin Update wrapper matching
// `VercelNetworkSandboxSession.setPorts`).
func (s *Sandbox) SetPorts(ctx context.Context, ports []int) error {
	return s.Update(ctx, UpdateParams{Ports: ports})
}

// Stop stops the running session, returning the resulting snapshot ID if the
// stop produced one.
func (s *Sandbox) Stop(ctx context.Context) (snapshotID string, err error) {
	resp, err := s.client.StopSession(ctx, s.sessionID)
	if err != nil {
		return "", err
	}
	s.sessionStatus = resp.Session.Status
	if resp.Snapshot != nil {
		snapshotID = resp.Snapshot.ID
	}
	if resp.Sandbox != nil {
		s.currentSnapshotID = resp.Sandbox.CurrentSnapshotID
	}
	return snapshotID, nil
}

// Snapshot snapshots the running session and returns the snapshot ID (TS
// `Sandbox.snapshot`). The session is stopped by the API once the snapshot
// is taken.
func (s *Sandbox) Snapshot(ctx context.Context, expiration *int64) (string, error) {
	resp, err := s.client.CreateSnapshot(ctx, s.sessionID, expiration)
	if err != nil {
		return "", err
	}
	s.sessionStatus = resp.Session.Status
	return resp.Snapshot.ID, nil
}

// Delete deletes the sandbox. After this the instance should not be used.
func (s *Sandbox) Delete(ctx context.Context) error {
	return s.client.DeleteSandbox(ctx, s.name)
}
