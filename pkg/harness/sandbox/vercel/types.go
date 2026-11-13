package vercel

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// Wire (HTTP) request/response shapes for the Vercel Sandbox API. Ported from
// the response validators in
// node_modules/@vercel/sandbox/dist/api-client/validators.js. Only the fields
// this package actually reads or writes are modeled; the API accepts and
// returns more.

// SnapshotSource selects how a sandbox is created (the @vercel/sandbox
// 3.9.0 `source` union): a snapshot ({Type: "snapshot", SnapshotID}), a git
// repository ({Type: "git", URL, optional Depth / Revision, and Username +
// Password for a private one}) or a tarball ({Type: "tarball", URL}). It is
// sent as given and, for a template, hashed into the template name
// (harness-02). JSON keys follow the field order.
type SnapshotSource struct {
	Type       string `json:"type"`
	SnapshotID string `json:"snapshotId,omitempty"`
	// URL is the git repository or tarball URL.
	URL string `json:"url,omitempty"`
	// Username and Password authenticate a private git repository.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Depth makes a shallow git clone (minimum 1).
	Depth *int `json:"depth,omitempty"`
	// Revision is the git commit, branch or tag to check out.
	Revision string `json:"revision,omitempty"`
}

// ResourcesParams selects vCPU allocation for the sandbox. Mirrors TS
// `resources?: { vcpus: number }` on `BaseCreateSandboxParams`.
type ResourcesParams struct {
	Vcpus int `json:"vcpus"`
}

// KeepLastSnapshotsParams configures snapshot retention. Mirrors TS
// `keepLastSnapshots?: { count; expiration?; deleteEvicted? }`.
type KeepLastSnapshotsParams struct {
	Count         int    `json:"count"`
	Expiration    *int64 `json:"expiration,omitempty"`
	DeleteEvicted *bool  `json:"deleteEvicted,omitempty"`
}

// createSandboxRequest is the body of POST /v2|v3/sandboxes.
type createSandboxRequest struct {
	// ProjectID is always sent: 3.9.0 Sandbox.create passes
	// `projectId: credentials.projectId`, a string ("" from a token whose
	// project_id claim is empty).
	ProjectID string `json:"projectId"`
	// Ports is always sent: 3.9.0 Sandbox.create passes `ports ?? []`.
	Ports              []int                    `json:"ports"`
	Source             *SnapshotSource          `json:"source,omitempty"`
	Timeout            int64                    `json:"timeout,omitempty"`
	Resources          *ResourcesParams         `json:"resources,omitempty"`
	Runtime            string                   `json:"runtime,omitempty"`
	Image              string                   `json:"image,omitempty"`
	Name               string                   `json:"name,omitempty"`
	Persistent         *bool                    `json:"persistent,omitempty"`
	NetworkPolicy      *apiNetworkPolicy        `json:"networkPolicy,omitempty"`
	Env                map[string]string        `json:"env,omitempty"`
	Tags               map[string]string        `json:"tags,omitempty"`
	SnapshotExpiration *int64                   `json:"snapshotExpiration,omitempty"`
	KeepLastSnapshots  *KeepLastSnapshotsParams `json:"keepLastSnapshots,omitempty"`
	Region             string                   `json:"region,omitempty"`
	FailoverRegions    []string                 `json:"failoverRegions,omitempty"`
}

type sandboxRouteWire struct {
	URL       string `json:"url"`
	Subdomain string `json:"subdomain"`
	Port      int    `json:"port"`
}

type sessionWire struct {
	ID            string         `json:"id"`
	CWD           string         `json:"cwd"`
	NetworkPolicy *NetworkPolicy `json:"networkPolicy,omitempty"`
	Status        string         `json:"status"`
	Timeout       int64          `json:"timeout"`
	// RawNetworkPolicy is the session's networkPolicy exactly as the API
	// returned it: in a listSessions() page, the session metadata TS reads
	// to find response rules the decoded policy does not carry (44a4261e52).
	RawNetworkPolicy json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the session and keeps its raw networkPolicy.
func (w *sessionWire) UnmarshalJSON(data []byte) error {
	type plain sessionWire
	var p plain
	if err := jsonexact.UnmarshalPlain(data, &p); err != nil { // base-client.js parse(): plain JSON.parse
		return err
	}
	var raw struct {
		NetworkPolicy json.RawMessage `json:"networkPolicy"`
	}
	if err := jsonexact.UnmarshalPlain(data, &raw); err != nil {
		return err
	}
	p.RawNetworkPolicy = raw.NetworkPolicy
	*w = sessionWire(p)
	return nil
}

type sandboxWire struct {
	Name               string         `json:"name"`
	Persistent         bool           `json:"persistent"`
	CurrentSnapshotID  string         `json:"currentSnapshotId,omitempty"`
	Timeout            int64          `json:"timeout,omitempty"`
	NetworkPolicy      *NetworkPolicy `json:"networkPolicy,omitempty"`
	SnapshotExpiration *int64         `json:"snapshotExpiration,omitempty"`
	CurrentSessionID   string         `json:"currentSessionId,omitempty"`
}

// sandboxAndSessionResponse is returned by create/fork/get sandbox calls.
type sandboxAndSessionResponse struct {
	Sandbox sandboxWire        `json:"sandbox"`
	Session sessionWire        `json:"session"`
	Routes  []sandboxRouteWire `json:"routes"`
	Resumed *bool              `json:"resumed,omitempty"`
}

type updateSandboxRequest struct {
	Persistent         *bool             `json:"persistent,omitempty"`
	Timeout            int64             `json:"timeout,omitempty"`
	NetworkPolicy      *apiNetworkPolicy `json:"networkPolicy,omitempty"`
	Ports              []int             `json:"ports,omitempty"`
	SnapshotExpiration *int64            `json:"snapshotExpiration,omitempty"`
}

type updateSandboxResponse struct {
	Sandbox sandboxWire        `json:"sandbox"`
	Routes  []sandboxRouteWire `json:"routes,omitempty"`
}

type stopSessionResponse struct {
	Session  sessionWire  `json:"session"`
	Sandbox  *sandboxWire `json:"sandbox,omitempty"`
	Snapshot *struct {
		ID string `json:"id"`
	} `json:"snapshot,omitempty"`
}

// snapshotWire is one entry of the snapshot list / create responses.
type snapshotWire struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	CreationMethod string `json:"creationMethod"`
}

type createSnapshotRequest struct {
	Expiration *int64 `json:"expiration,omitempty"`
}

type createSnapshotResponse struct {
	Snapshot snapshotWire `json:"snapshot"`
	Session  sessionWire  `json:"session"`
}

type listSnapshotsResponse struct {
	Snapshots  []snapshotWire `json:"snapshots"`
	Pagination struct {
		Next *string `json:"next"`
	} `json:"pagination"`
}

// listSessionsResponse is the 3.9.0 SessionsResponse (GET
// /v2/sandboxes/sessions).
type listSessionsResponse struct {
	Sessions   []sessionWire `json:"sessions"`
	Pagination struct {
		Next *string `json:"next"`
	} `json:"pagination"`
}

type sessionResponse struct {
	Session sessionWire `json:"session"`
}

type commandWire struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Args       []string `json:"args"`
	CWD        string   `json:"cwd"`
	SessionID  string   `json:"sessionId"`
	ExitCode   *int     `json:"exitCode"`
	DurationMs *int64   `json:"durationMs,omitempty"`
	StartedAt  int64    `json:"startedAt"`
}

type commandResponse struct {
	Command commandWire `json:"command"`
}

// ndjsonLine is the generic shape of one line from the streaming command/log
// endpoints: either a {command} line (initial ack or final-finished), or a
// {stream, data} log line (stdout/stderr/error).
type ndjsonLine struct {
	Command *commandWire `json:"command,omitempty"`
	Stream  string       `json:"stream,omitempty"`
	Data    interface{}  `json:"data,omitempty"`
}

type runCommandRequest struct {
	Command string `json:"command"`
	// Args, Env and Sudo are always sent: 3.9.0 Session.runCommand passes
	// `args ?? []`, `env ?? {}` and `sudo ?? false` (see withDefaults).
	Args []string          `json:"args"`
	CWD  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env"`
	Sudo bool              `json:"sudo"`
	Wait bool              `json:"wait,omitempty"`
	// AttachStdin is TS `attachStdin: params.attachStdin || undefined`.
	AttachStdin bool  `json:"attachStdin,omitempty"`
	Logs        bool  `json:"logs,omitempty"`
	Timeout     int64 `json:"timeout,omitempty"`
}

// withDefaults fills the 3.9.0 Session.runCommand defaults.
func (r runCommandRequest) withDefaults() runCommandRequest {
	if r.Args == nil {
		r.Args = []string{}
	}
	if r.Env == nil {
		r.Env = map[string]string{}
	}
	return r
}

type mkdirRequest struct {
	Path string `json:"path"`
	CWD  string `json:"cwd,omitempty"`
}

type readFileRequest struct {
	Path string `json:"path"`
	CWD  string `json:"cwd,omitempty"`
}

type killCommandRequest struct {
	Signal int `json:"signal"`
}

// errorResponseBody is the shape the Sandbox API returns on non-2xx
// responses: {"error": {"message": "..."}}.
type errorResponseBody struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}
