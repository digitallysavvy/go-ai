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

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
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
}

// NewAPIClient builds a client authenticated with credentials, talking to
// baseURL (DefaultBaseURL in production; overridden by tests).
func NewAPIClient(baseURL string, credentials Credentials) *APIClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &APIClient{
		http:        httpinternal.NewClient(httpinternal.Config{BaseURL: baseURL}),
		credentials: credentials,
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

func (c *APIClient) query(extra map[string]string) map[string]string {
	q := map[string]string{}
	if c.credentials.TeamID != "" {
		q["teamId"] = c.credentials.TeamID
	}
	for k, v := range extra {
		if v != "" {
			q[k] = v
		}
	}
	return q
}

func toAPIError(err error) error {
	statusErr, ok := err.(*httpinternal.HTTPStatusError)
	if !ok {
		return err
	}
	apiErr := &APIError{StatusCode: statusErr.StatusCode}
	var body errorResponseBody
	if json.Unmarshal(statusErr.Body, &body) == nil {
		apiErr.Message = body.Error.Message
		apiErr.Code = body.Error.Code
	}
	return apiErr
}

// CreateSandbox posts to /v3/sandboxes (no explicit runtime) or
// /v2/sandboxes (runtime set), matching TS `createSandbox`.
func (c *APIClient) CreateSandbox(ctx context.Context, req createSandboxRequest) (*sandboxAndSessionResponse, error) {
	req.ProjectID = c.credentials.ProjectID
	path := "/v3/sandboxes"
	if req.Runtime != "" {
		path = "/v2/sandboxes"
	}
	var out sandboxAndSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: path, Headers: c.headers(nil), Query: c.query(nil), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// GetSandbox fetches GET /v2/sandboxes/{name}, optionally resuming its
// session.
func (c *APIClient) GetSandbox(ctx context.Context, name string, resume *bool) (*sandboxAndSessionResponse, error) {
	q := map[string]string{"projectId": c.credentials.ProjectID}
	if resume != nil {
		q["resume"] = strconv.FormatBool(*resume)
	}
	var out sandboxAndSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: "/v2/sandboxes/" + pathEscape(name), Headers: c.headers(nil), Query: c.query(q),
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// UpdateSandbox patches PATCH /v2/sandboxes/{name}.
func (c *APIClient) UpdateSandbox(ctx context.Context, name string, req updateSandboxRequest) (*updateSandboxResponse, error) {
	var out updateSandboxResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPatch, Path: "/v2/sandboxes/" + pathEscape(name), Headers: c.headers(nil), Query: c.query(map[string]string{"projectId": c.credentials.ProjectID}), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out, nil
}

// DeleteSandbox deletes DELETE /v2/sandboxes/{name}.
func (c *APIClient) DeleteSandbox(ctx context.Context, name string) error {
	var out updateSandboxResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodDelete, Path: "/v2/sandboxes/" + pathEscape(name), Headers: c.headers(nil), Query: c.query(map[string]string{"projectId": c.credentials.ProjectID}),
	}, &out); err != nil {
		return toAPIError(err)
	}
	return nil
}

// StopSession posts POST /v2/sandboxes/sessions/{id}/stop.
func (c *APIClient) StopSession(ctx context.Context, sessionID string) (*stopSessionResponse, error) {
	var out stopSessionResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/stop", Headers: c.headers(nil), Query: c.query(nil),
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
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/network-policy", Headers: c.headers(nil), Query: c.query(nil), Body: policy,
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

// RunCommandWait posts POST /v2/sandboxes/sessions/{id}/cmd with wait=true,
// logs=true and consumes the ndjson stream to completion, accumulating
// stdout/stderr client-side (mirrors TS `Session.runCommand`'s non-detached
// path).
//
// On a stream failure (an early end, an oversized line, or a sandbox-
// reported "error" line) this still returns whatever stdout/stderr had
// already been accumulated, alongside the error — never a nil result — so a
// caller does not lose output the command had already produced just because
// the stream failed before the command's own finish line arrived. This
// mirrors how TS's `@vercel/sandbox` delivers stdout/stderr as they arrive,
// over independent ReadableStreams that keep whatever was already pushed to
// a reader even once the stream later errors.
func (c *APIClient) RunCommandWait(ctx context.Context, sessionID string, req runCommandRequest) (*RunCommandWaitResult, error) {
	req.Wait = true
	req.Logs = true
	resp, err := c.http.DoStream(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd", Headers: c.headers(nil), Query: c.query(nil), Body: req,
	})
	if err != nil {
		return nil, toAPIError(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), vercelNDJSONMaxLine)

	result := &RunCommandWaitResult{}

	if !scanner.Scan() {
		return result, streamEndedEarly(sessionID, wrapScanError(scanner.Err(), sessionID))
	}
	var first ndjsonLine
	if err := json.Unmarshal(scanner.Bytes(), &first); err != nil {
		return result, err
	}

	for scanner.Scan() {
		var line ndjsonLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return result, err
		}
		if line.Command != nil {
			result.Command = *line.Command
			return result, nil
		}
		switch line.Stream {
		case "error":
			return result, streamErrorFromData(line.Data, sessionID)
		case "stdout":
			result.Stdout += dataString(line.Data)
		case "stderr":
			result.Stderr += dataString(line.Data)
		}
	}
	return result, streamEndedEarly(sessionID, wrapScanError(scanner.Err(), sessionID))
}

// RunCommandDetached posts POST .../cmd without wait, returning immediately
// with the created command (TS detached path).
func (c *APIClient) RunCommandDetached(ctx context.Context, sessionID string, req runCommandRequest) (*commandWire, error) {
	var out commandResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd", Headers: c.headers(nil), Query: c.query(nil), Body: req,
	}, &out); err != nil {
		return nil, toAPIError(err)
	}
	return &out.Command, nil
}

// GetCommand fetches GET .../cmd/{id}[?wait=true].
func (c *APIClient) GetCommand(ctx context.Context, sessionID, cmdID string, wait bool) (*commandWire, error) {
	q := map[string]string{}
	if wait {
		q["wait"] = "true"
	}
	var out commandResponse
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID), Headers: c.headers(nil), Query: c.query(q),
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
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID) + "/kill",
		Headers: c.headers(nil), Query: c.query(nil), Body: killCommandRequest{Signal: sig},
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
// the stream ends.
func (c *APIClient) GetLogs(ctx context.Context, sessionID, cmdID string, onLog func(LogEntry)) error {
	resp, err := c.http.DoStream(ctx, httpinternal.Request{
		Method: http.MethodGet, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID) + "/logs", Headers: c.headers(nil), Query: c.query(nil),
	})
	if err != nil {
		return toAPIError(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), vercelNDJSONMaxLine)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var line ndjsonLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return err
		}
		switch line.Stream {
		case "error":
			return streamErrorFromData(line.Data, sessionID)
		case "stdout", "stderr":
			onLog(LogEntry{Stream: line.Stream, Data: dataString(line.Data)})
		}
	}
	// wrapScanError clarifies a bufio.ErrTooLong (or other scan failure) the
	// same way RunCommandWait does, instead of returning it bare: see
	// vercelNDJSONMaxLine's doc comment — this cap and RunCommandWait's are
	// the same client-side limitation, not a sandbox-reported error.
	return wrapScanError(scanner.Err(), sessionID)
}

// MkDir posts POST .../fs/mkdir.
func (c *APIClient) MkDir(ctx context.Context, sessionID, path, cwd string) error {
	var out map[string]interface{}
	if err := c.http.DoJSON(ctx, httpinternal.Request{
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/mkdir", Headers: c.headers(nil), Query: c.query(nil),
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
		Path:    "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/write",
		Headers: c.headers(map[string]string{"Content-Type": "application/gzip", "X-Cwd": extractDir}),
		Query:   c.query(nil),
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
		Method: http.MethodPost, Path: "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/fs/read", Headers: c.headers(nil), Query: c.query(nil),
		Body: readFileRequest{Path: path, CWD: cwd},
	})
	if err != nil {
		if statusErr, ok := err.(*httpinternal.HTTPStatusError); ok && statusErr.StatusCode == 404 {
			return nil, nil
		}
		return nil, toAPIError(err)
	}
	return resp.Body, nil
}

func pathEscape(s string) string {
	// Sandbox names/session/command IDs are opaque identifiers; percent-encode
	// path-unsafe characters the same way encodeURIComponent would for the
	// characters these IDs can plausibly contain.
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func dataString(data interface{}) string {
	s, _ := data.(string)
	return s
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

func streamErrorFromData(data interface{}, sessionID string) error {
	m, _ := data.(map[string]interface{})
	code, _ := m["code"].(string)
	message, _ := m["message"].(string)
	return &StreamError{Code: code, Message: message, SessionID: sessionID}
}

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
