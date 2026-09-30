package vercel

// Wire (HTTP) request/response shapes for the Vercel Sandbox API. Ported from
// the response validators in
// node_modules/@vercel/sandbox/dist/api-client/validators.js. Only the fields
// this package actually reads or writes are modeled; the API accepts and
// returns more.

// SnapshotSource selects how a sandbox is created: from a git/tarball source
// (omitted here; unused) or from a snapshot.
type SnapshotSource struct {
	Type       string `json:"type"`
	SnapshotID string `json:"snapshotId,omitempty"`
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
	ProjectID          string                   `json:"projectId,omitempty"`
	Ports              []int                    `json:"ports,omitempty"`
	Source             *SnapshotSource          `json:"source,omitempty"`
	Timeout            int64                    `json:"timeout,omitempty"`
	Resources          *ResourcesParams         `json:"resources,omitempty"`
	Runtime            string                   `json:"runtime,omitempty"`
	Image              string                   `json:"image,omitempty"`
	Name               string                   `json:"name,omitempty"`
	Persistent         *bool                    `json:"persistent,omitempty"`
	NetworkPolicy      *NetworkPolicy           `json:"networkPolicy,omitempty"`
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
	Persistent         *bool          `json:"persistent,omitempty"`
	Timeout            int64          `json:"timeout,omitempty"`
	NetworkPolicy      *NetworkPolicy `json:"networkPolicy,omitempty"`
	Ports              []int          `json:"ports,omitempty"`
	SnapshotExpiration *int64         `json:"snapshotExpiration,omitempty"`
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
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	CWD     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Sudo    bool              `json:"sudo,omitempty"`
	Wait    bool              `json:"wait,omitempty"`
	Logs    bool              `json:"logs,omitempty"`
	Timeout int64             `json:"timeout,omitempty"`
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
