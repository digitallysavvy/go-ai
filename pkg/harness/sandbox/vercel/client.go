package vercel

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultBaseURL is the production Vercel API base URL (TS `baseUrl:
// 'https://vercel.com/api'`).
const DefaultBaseURL = "https://vercel.com/api"

// vercelNDJSONMaxLine bounds a single ndjson line read from a command/logs
// stream (both RunCommandWait and GetLogs). The TS client has no such cap —
// it hands the response body to the `jsonlines` package, which parses
// incrementally rather than buffering whole lines — but bufio.Scanner, which
// this client uses instead, requires a fixed maximum token size. 16MB
// comfortably covers any realistic single log line (e.g. a base64-encoded
// file dump) while still bounding per-command memory use; wrapScanError
// below turns a line that exceeds it into a clearly-labeled error instead of
// letting it read as an unexplained stream failure.
const vercelNDJSONMaxLine = 16 * 1024 * 1024

// wrapScanError clarifies a bufio.Scanner failure reading an ndjson stream —
// most commonly bufio.ErrTooLong from a single line exceeding
// vercelNDJSONMaxLine — instead of letting it propagate as an opaque error
// indistinguishable from the stream simply ending. It wraps the original
// error with %w so callers can still errors.Is(err, bufio.ErrTooLong) (or
// unwrap any other underlying cause, such as a read timeout or a connection
// reset) rather than losing it.
func wrapScanError(err error, sessionID string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return fmt.Errorf("vercel sandbox session %q: a single ndjson stream line exceeded the %d-byte limit: %w", sessionID, vercelNDJSONMaxLine, err)
	}
	return fmt.Errorf("vercel sandbox session %q: ndjson stream read failed: %w", sessionID, err)
}

// linuxSignalMapping mirrors TS `resolveSignal`'s table.
var linuxSignalMapping = map[string]int{
	"SIGHUP":  1,
	"SIGINT":  2,
	"SIGQUIT": 3,
	"SIGKILL": 9,
	"SIGTERM": 15,
	"SIGCONT": 18,
	"SIGSTOP": 19,
}

// APIClient is a thin HTTP client for the Vercel Sandbox API. Ported from
// node_modules/@vercel/sandbox/dist/api-client/api-client.js — only the
// endpoints packages/sandbox-vercel actually calls are implemented.
type APIClient struct {
	http        *httpinternal.Client
	credentials Credentials
	// team is the `teamId` query of every request (see teamQuery).
	team teamQuery
}

// NewAPIClient builds a client authenticated with credentials, talking to
// baseURL (DefaultBaseURL in production; overridden by tests).
func NewAPIClient(baseURL string, credentials Credentials) *APIClient {
	return newAPIClient(baseURL, credentials, nil)
}

// newAPIClient is NewAPIClient with an optional OIDC refresh. Like the 3.9.0
// APIClient constructor, a JWT token whose payload has an owner_id sets the
// team (owner_id, the `teamId` query of every request) from its claims. The
// claims' project_id only feeds the refresh: request bodies and queries keep
// the credentials' ProjectID, which TS passes from getCredentials
// (`projectId: credentials.projectId`). With refresh set, such a token is
// checked before every request (ensureValidToken).
func newAPIClient(baseURL string, credentials Credentials, refresh *OIDCRefresh) *APIClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	base := httpinternal.DefaultHTTPClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	var transport http.RoundTripper = &retryTransport{base: base}
	team := teamQuery{values: []string{credentials.TeamID}}
	if owner, project, ok := unverifiedTokenOwner(credentials.Token); ok {
		credentials.TeamID = providerutils.JSString(owner)
		team = teamQueryOf(owner)
		if refresh != nil {
			transport = &oidcTokenTransport{base: transport, refresh: refresh,
				token: credentials.Token, teamID: credentials.TeamID, team: team, projectID: project}
		} else if team.err != nil {
			transport = teamErrorTransport{err: team.err}
		}
	}
	return &APIClient{
		http: httpinternal.NewClient(httpinternal.Config{
			BaseURL:    baseURL,
			HTTPClient: &http.Client{Transport: transport},
		}),
		credentials: credentials,
		team:        team,
	}
}

// APIError preserves the Vercel Sandbox API's structured error response.
// Mirrors TS `APIError`.
type APIError struct {
	StatusCode int
	Message    string
	Code       string
	SessionID  string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("vercel sandbox API: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("vercel sandbox API: status %d", e.StatusCode)
}

// IsNotFound reports a 404 response (TS `isNotFoundError`).
func (e *APIError) IsNotFound() bool { return e.StatusCode == 404 }

// IsSnapshotNotFound reports the 410 snapshot_not_found response (TS
// `isSnapshotNotFoundError`).
func (e *APIError) IsSnapshotNotFound() bool {
	return e.StatusCode == 410 && e.Code == "snapshot_not_found"
}

func (c *APIClient) headers(extra map[string]string) map[string]string {
	h := map[string]string{
		"Authorization": "Bearer " + c.credentials.Token,
		"Content-Type":  "application/json",
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// path is p with the 3.9.0 APIClient.request query `{teamId: this.teamId,
// ...params.query}`, serialized like base-client.js (url.searchParams.append
// in object order): the team comes first and is always sent (the client's
// teamId is a string, so an empty team is `teamId=`), then pairs (name,
// value, ...) in the TS call's key order, "" values included (base-client.js
// drops only undefined and null, which callers express by leaving the pair
// out).
func (c *APIClient) path(p string, pairs ...string) string {
	q := &httpinternal.SearchParams{}
	for _, team := range c.team.values {
		q.Append("teamId", team)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		q.Append(pairs[i], pairs[i+1])
	}
	return p + "?" + q.Encode()
}

func toAPIError(err error) error {
	statusErr, ok := err.(*httpinternal.HTTPStatusError)
	if !ok {
		return err
	}
	apiErr := &APIError{StatusCode: statusErr.StatusCode}
	// TS extractServerMessage needs only a string error.message; the code
	// is read on its own (pipe-stdin.js `error.json?.error?.code`), so one
	// mistyped member does not hide the other.
	var body struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if json.Unmarshal(statusErr.Body, &body) == nil {
		_ = json.Unmarshal(body.Error["message"], &apiErr.Message)
		_ = json.Unmarshal(body.Error["code"], &apiErr.Code)
	}
	return apiErr
}

// CreateSandbox posts to /v3/sandboxes (no explicit runtime) or
// /v2/sandboxes (runtime set), matching TS `createSandbox`.
func (c *APIClient) CreateSandbox(ctx context.Context, req createSandboxRequest) (*sandboxAndSessionResponse, error) {
	req.ProjectID = c.credentials.ProjectID
	if req.Ports == nil {
		req.Ports = []int{}
	}
	path := "/v3/sandboxes"
	if req.Runtime != "" {
		path = "/v2/sandboxes"
	}
	var out sandboxAndSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path(path), Headers: c.headers(nil), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// GetSandbox fetches GET /v2/sandboxes/{name}, optionally resuming its
// session.
func (c *APIClient) GetSandbox(ctx context.Context, name string, resume *bool) (*sandboxAndSessionResponse, error) {
	q := []string{"projectId", c.credentials.ProjectID}
	if resume != nil {
		q = append(q, "resume", strconv.FormatBool(*resume))
	}
	var out sandboxAndSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: c.path("/v2/sandboxes/"+pathEscape(name), q...), Headers: c.headers(nil),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// UpdateSandbox patches PATCH /v2/sandboxes/{name}.
func (c *APIClient) UpdateSandbox(ctx context.Context, name string, req updateSandboxRequest) (*updateSandboxResponse, error) {
	var out updateSandboxResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPatch, Path: c.path("/v2/sandboxes/"+pathEscape(name), "projectId", c.credentials.ProjectID), Headers: c.headers(nil), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// DeleteSandbox deletes DELETE /v2/sandboxes/{name}.
func (c *APIClient) DeleteSandbox(ctx context.Context, name string) error {
	var out updateSandboxResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodDelete, Path: c.path("/v2/sandboxes/"+pathEscape(name), "projectId", c.credentials.ProjectID), Headers: c.headers(nil),
	}, &out); err != nil {
		return toAPIError(err)
	}
	return nil
}

// StopSession posts POST /v2/sandboxes/sessions/{id}/stop.
func (c *APIClient) StopSession(ctx context.Context, sessionID string) (*stopSessionResponse, error) {
	var out stopSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/stop"), Headers: c.headers(nil),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// CreateSnapshot posts POST /v2/sandboxes/sessions/{id}/snapshot. Like TS
// `Sandbox.snapshot`, taking a snapshot stops the session.
func (c *APIClient) CreateSnapshot(ctx context.Context, sessionID string, expiration *int64) (*createSnapshotResponse, error) {
	var out createSnapshotResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/snapshot"), Headers: c.headers(nil), Body: createSnapshotBody(expiration),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// createSnapshotBody is TS `expiration === undefined ? undefined :
// JSON.stringify({expiration})`: no body without an expiration.
func createSnapshotBody(expiration *int64) interface{} {
	if expiration == nil {
		return nil
	}
	return createSnapshotRequest{Expiration: expiration}
}

// ListSnapshots lists the project's snapshots for a sandbox name, newest
// first, following pagination (TS `Snapshot.list({ name, sortOrder: 'desc' })`
// iterated with `for await`).
func (c *APIClient) ListSnapshots(ctx context.Context, name string) ([]snapshotWire, error) {
	var all []snapshotWire
	err := c.eachSnapshot(ctx, name, func(s snapshotWire) bool {
		all = append(all, s)
		return false
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// eachSnapshot is `for await (const snapshot of Snapshot.list({name,
// sortOrder: 'desc'}))`: the first page has no cursor, the next page is
// fetched while `pagination.next !== null` (an empty page or an empty
// cursor included), and the iteration stops as soon as yield returns true.
// Like attachPaginator, there is no page bound: ctx ends a server that never
// stops paginating.
func (c *APIClient) eachSnapshot(ctx context.Context, name string, yield func(snapshotWire) bool) error {
	var cursor *string
	for {
		// listSnapshots query order: project, name, (limit), cursor, sortOrder.
		q := []string{"project", c.credentials.ProjectID, "name", name}
		if cursor != nil {
			q = append(q, "cursor", *cursor)
		}
		q = append(q, "sortOrder", "desc")
		var out listSnapshotsResponse
		if err := c.http.DoJSON(ctx, httpinternal.Request{
			Method: http.MethodGet, Path: c.path("/v2/sandboxes/snapshots", q...), Headers: c.headers(nil),
		}, &out); err != nil {
			return toAPIError(err)
		}
		for _, s := range out.Snapshots {
			if yield(s) {
				return nil
			}
		}
		if out.Pagination.Next == nil {
			return nil
		}
		cursor = out.Pagination.Next
	}
}

// eachSession is `for await (const metadata of sandbox.listSessions())`
// (3.9.0 `Sandbox.listSessions` with no parameters, `APIClient.listSessions`
// and attachPaginator): the first page has no cursor, the next page is
// fetched while `pagination.next !== null`, and the iteration stops as soon
// as yield returns true. There is no page bound; ctx ends the iteration.
func (c *APIClient) eachSession(ctx context.Context, name string, yield func(sessionWire) bool) error {
	var cursor *string
	for {
		// listSessions query order: project, name, (limit), cursor, (sortOrder).
		q := []string{"project", c.credentials.ProjectID, "name", name}
		if cursor != nil {
			q = append(q, "cursor", *cursor)
		}
		var out listSessionsResponse
		if err := c.http.DoJSON(ctx, httpinternal.Request{
			Method: http.MethodGet, Path: c.path("/v2/sandboxes/sessions", q...), Headers: c.headers(nil),
		}, &out); err != nil {
			return toAPIError(err)
		}
		for _, s := range out.Sessions {
			if yield(s) {
				return nil
			}
		}
		if out.Pagination.Next == nil {
			return nil
		}
		cursor = out.Pagination.Next
	}
}

// extendTimeoutRequest is the 3.9.0 `extendTimeout` body.
type extendTimeoutRequest struct {
	Duration int64 `json:"duration"`
}

// ExtendSessionTimeout posts POST /v2/sandboxes/sessions/{id}/extend-timeout
// with `{"duration": ms}` (3.9.0 `APIClient.extendTimeout`).
func (c *APIClient) ExtendSessionTimeout(ctx context.Context, sessionID string, durationMs int64) (*sessionResponse, error) {
	var out sessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/extend-timeout"), Headers: c.headers(nil), Body: extendTimeoutRequest{Duration: durationMs},
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// UpdateSessionNetworkPolicy posts POST
// /v2/sandboxes/sessions/{id}/network-policy.
func (c *APIClient) UpdateSessionNetworkPolicy(ctx context.Context, sessionID string, policy NetworkPolicy) (*sessionResponse, error) {
	var out sessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/network-policy"), Headers: c.headers(nil), Body: apiNetworkPolicy(policy),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// RunCommandWaitResult is the accumulated result of a blocking (wait=true,
// logs=true) command run.
type RunCommandWaitResult struct {
	Command commandWire
	Stdout  string
	Stderr  string
}

// expectNDJSON is the 3.9.0 check `response.headers.get("content-type") !==
// "application/x-ndjson"` on the command and logs streams.
func expectNDJSON(resp *http.Response, sessionID, message string) error {
	if resp.Header.Get("Content-Type") != "application/x-ndjson" {
		return &APIError{StatusCode: resp.StatusCode, Message: message, SessionID: sessionID}
	}
	return nil
}

// RunCommandWait posts POST /v2/sandboxes/sessions/{id}/cmd with wait=true,
// logs=true and consumes the ndjson stream to completion, accumulating
// stdout/stderr client-side. Mirrors @vercel/sandbox 3.9.0
// `APIClient.runCommand` (wait) with `Session.runCommand`'s onLog: the
// request is not retried; the first record is the started command; when the
// stream is interrupted or ends before the finished command arrives (a
// connectionError), the remaining output is read from the logs endpoint,
// skipping what was already delivered, and the exit code from GET
// .../cmd/{id}?wait=true.
//
// On a stream failure this still returns whatever stdout/stderr had already
// been accumulated, alongside the error — never a nil result — so a caller
// does not lose output the command had already produced.
func (c *APIClient) RunCommandWait(ctx context.Context, sessionID string, req runCommandRequest) (*RunCommandWaitResult, error) {
	req = req.withDefaults()
	req.Wait = true
	req.Logs = true
	result := &RunCommandWaitResult{}
	resp, err := c.http.DoStream(withoutRetry(ctx), httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd"), Headers: c.headers(nil), Body: req,
	})
	if err != nil {
		return result, toAPIError(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := expectNDJSON(resp, sessionID, "Expected a stream of command data"); err != nil {
		return result, err
	}

	delivered := logDelivered{}
	var started *commandWire
	finished := false
	streamErr := readJSONLines(resp.Body, sessionID, func(raw []byte) error {
		if started == nil {
			var first commandResponse
			var probe struct {
				Command *commandWire `json:"command"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil || probe.Command == nil {
				return fmt.Errorf("vercel sandbox: invalid command response: %s", truncateForError(raw))
			}
			first.Command = *probe.Command
			started = &first.Command
			return nil
		}
		var probe struct {
			Command *commandWire `json:"command"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Command != nil {
			result.Command = *probe.Command
			finished = true
			return errStopJSONLines
		}
		line, err := parseLogLine(raw, sessionID)
		if err != nil {
			return err
		}
		if line.Err != nil {
			return line.Err
		}
		if line.Stream == "stdout" {
			result.Stdout += line.Data
		} else {
			result.Stderr += line.Data
		}
		delivered[line.Stream] += jsLength(line.Data)
		return nil
	})
	if started == nil {
		if streamErr == nil {
			return result, streamEndedEarly(sessionID, nil)
		}
		var connErr *connectionError
		if errors.As(streamErr, &connErr) || errors.Is(streamErr, bufio.ErrTooLong) {
			return result, streamEndedEarly(sessionID, streamErr)
		}
		return result, streamErr
	}
	if finished {
		return result, nil
	}
	if streamErr == nil {
		streamErr = &connectionError{message: "Stream ended before command finished"}
	}
	var connErr *connectionError
	if ctx.Err() != nil || !errors.As(streamErr, &connErr) {
		if errors.Is(streamErr, bufio.ErrTooLong) {
			return result, streamEndedEarly(sessionID, streamErr)
		}
		return result, streamErr
	}
	if err := c.getLogsStream(ctx, sessionID, started.ID, delivered, func(entry LogEntry) {
		if entry.Stream == "stdout" {
			result.Stdout += entry.Data
		} else {
			result.Stderr += entry.Data
		}
	}); err != nil {
		return result, err
	}
	command, err := c.GetCommand(ctx, sessionID, started.ID, true)
	if err != nil {
		return result, err
	}
	result.Command = *command
	return result, nil
}

// RunCommandDetached posts POST .../cmd without wait, returning immediately
// with the created command (TS detached path).
func (c *APIClient) RunCommandDetached(ctx context.Context, sessionID string, req runCommandRequest) (*commandWire, error) {
	req = req.withDefaults()
	var out commandResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd"), Headers: c.headers(nil), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out.Command, nil
}

// GetCommand fetches GET .../cmd/{id}[?wait=true].
func (c *APIClient) GetCommand(ctx context.Context, sessionID, cmdID string, wait bool) (*commandWire, error) {
	var q []string
	if wait {
		q = []string{"wait", "true"}
	}
	var out commandResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: c.path("/v2/sandboxes/sessions/"+pathEscape(sessionID)+"/cmd/"+pathEscape(cmdID), q...), Headers: c.headers(nil),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out.Command, nil
}

// KillCommand posts POST .../cmd/{id}/kill.
func (c *APIClient) KillCommand(ctx context.Context, sessionID, cmdID string, signal string) error {
	sig, ok := linuxSignalMapping[signal]
	if !ok {
		return fmt.Errorf("vercel sandbox: unknown signal name: %s", signal)
	}
	var out commandResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID) + "/kill"),
		Headers: c.headers(nil), Body: killCommandRequest{Signal: sig},
	}, &out); err != nil {
		return toAPIError(err)
	}
	return nil
}

// LogEntry is one line from GetLogs: Stream is "stdout" or "stderr".
type LogEntry struct {
	Stream string
	Data   string
}

// GetLogs streams GET .../cmd/{id}/logs, invoking onLog for every stdout/
// stderr line until the stream closes or ctx is cancelled. It blocks until
// the stream ends. Like 3.9.0 `getLogsStream`, an interrupted stream (a
// connectionError) is requested again up to twice, 200 ms and 400 ms later,
// skipping the output already delivered.
func (c *APIClient) GetLogs(ctx context.Context, sessionID, cmdID string, onLog func(LogEntry)) error {
	return c.getLogsStream(ctx, sessionID, cmdID, logDelivered{}, onLog)
}

func (c *APIClient) getLogsStream(ctx context.Context, sessionID, cmdID string, delivered logDelivered, onLog func(LogEntry)) error {
	for attempt := 0; ; attempt++ {
		err := c.getLogsOnce(ctx, sessionID, cmdID, delivered, onLog)
		var connErr *connectionError
		if err == nil || ctx.Err() != nil || !errors.As(err, &connErr) || attempt >= 2 {
			return err
		}
		if err := sleepCtx(ctx, logsReconnectDelay(attempt)); err != nil {
			return err
		}
	}
}

func (c *APIClient) getLogsOnce(ctx context.Context, sessionID, cmdID string, delivered logDelivered, onLog func(LogEntry)) error {
	resp, err := c.http.DoStream(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID) + "/logs"), Headers: c.headers(nil),
	})
	if err != nil {
		return toAPIError(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if err := expectNDJSON(resp, sessionID, "Expected a stream of logs"); err != nil {
		return err
	}
	skip := logDelivered{}
	for k, v := range delivered {
		skip[k] = v
	}
	return readJSONLines(resp.Body, sessionID, func(raw []byte) error {
		line, err := parseLogLine(raw, sessionID)
		if err != nil {
			return err
		}
		if line.Err != nil {
			return line.Err
		}
		length := jsLength(line.Data)
		skipped := skip[line.Stream]
		if skipped > length {
			skipped = length
		}
		skip[line.Stream] -= skipped
		data := jsSliceFrom(line.Data, skipped)
		if data != "" || length == 0 {
			delivered[line.Stream] += length - skipped
			onLog(LogEntry{Stream: line.Stream, Data: data})
		}
		return nil
	})
}

// MkDir posts POST .../fs/mkdir.
func (c *APIClient) MkDir(ctx context.Context, sessionID, path, cwd string) error {
	var out map[string]interface{}
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/mkdir"), Headers: c.headers(nil),
		Body: mkdirRequest{Path: path, CWD: cwd},
	}, &out); err != nil {
		return toAPIError(err)
	}
	return nil
}

// FileToWrite is one file in a WriteFiles call.
type FileToWrite struct {
	// Name is the tar entry name, already normalized relative to the
	// extraction root (see normalizePath).
	Name    string
	Content []byte
	Mode    int64
}

// WriteFiles gzip+tars files and POSTs them to POST .../fs/write with
// Content-Type: application/gzip and X-Cwd: extractDir. Mirrors TS
// `FileWriter` + `APIClient.writeFiles`.
func (c *APIClient) WriteFiles(ctx context.Context, sessionID, extractDir string, files []FileToWrite) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: f.Name, Size: int64(len(f.Content)), Mode: mode}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(f.Content); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}

	var out map[string]interface{}
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method:  http.MethodPost,
		Path:    c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/write"),
		Headers: c.headers(map[string]string{"Content-Type": "application/gzip", "X-Cwd": extractDir}),
		Body:    buf.Bytes(),
	}, &out); err != nil {
		return toAPIError(err)
	}
	return nil
}

// ReadFile posts POST .../fs/read and returns the raw file body, or nil,nil
// on a 404 (file not found). The caller must Close the returned reader.
func (c *APIClient) ReadFile(ctx context.Context, sessionID, path, cwd string) (io.ReadCloser, error) {
	resp, err := c.http.DoStream(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: c.path("/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/read"), Headers: c.headers(nil),
		Body: readFileRequest{Path: path, CWD: cwd},
	})
	// 3.9.0 readFile: 404 is a missing file; any other error status, or a
	// success that is not a byte stream, is "Unexpected response reading
	// file: status N, content-type T".
	unexpected := func(status int, header http.Header, apiErr *APIError) error {
		contentType := header.Get("Content-Type")
		if contentType == "" {
			contentType = "(none)"
		}
		if apiErr == nil {
			apiErr = &APIError{StatusCode: status}
		}
		apiErr.Message = "Unexpected response reading file: status " + strconv.Itoa(status) + ", content-type " + contentType
		apiErr.SessionID = sessionID
		return apiErr
	}
	if err != nil {
		statusErr, ok := err.(*httpinternal.HTTPStatusError)
		if !ok {
			return nil, err
		}
		if statusErr.StatusCode == 404 {
			return nil, nil
		}
		apiErr, _ := toAPIError(err).(*APIError)
		return nil, unexpected(statusErr.StatusCode, statusErr.Headers, apiErr)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/octet-stream") {
		_ = resp.Body.Close()
		return nil, unexpected(resp.StatusCode, resp.Header, nil)
	}
	return resp.Body, nil
}

// pathEscape is JS encodeURIComponent (3.9.0 encodes sandbox names with it):
// every byte but A-Z a-z 0-9 - _ . ! ~ * ' ( ) is percent-encoded.
func pathEscape(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		_, _ = fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// StreamError mirrors TS `StreamError`: a sandbox-reported error interleaved
// in an ndjson stream (typically the sandbox stopping mid-stream), or a
// client-side failure reading that stream (an early end, a read error, or an
// oversized line). Cause, when set, is the underlying client-side error
// (e.g. bufio.ErrTooLong or a network read error) — Unwrap exposes it so
// callers can errors.Is/errors.As through it instead of it being swallowed
// behind a generic "stream ended early" message.
type StreamError struct {
	Code      string
	Message   string
	SessionID string
	Cause     error
}

func (e *StreamError) Error() string { return e.Message }

func (e *StreamError) Unwrap() error { return e.Cause }

// streamEndedEarly reports that the ndjson command/log stream ended (or
// failed) before the expected data was received. cause, when non-nil (a real
// client-side scan error such as bufio.ErrTooLong, distinct from a clean EOF
// with no error), is folded into Message so the real failure is visible
// instead of being swallowed behind the generic message, and is also
// preserved as Cause/Unwrap for callers that want to match on it
// programmatically.
func streamEndedEarly(sessionID string, cause error) error {
	message := "Stream ended before command data was received"
	if cause != nil {
		message = fmt.Sprintf("%s: %v", message, cause)
	}
	return &StreamError{Code: "stream_ended_early", Message: message, SessionID: sessionID, Cause: cause}
}
