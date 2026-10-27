package fx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Port of TS fx-resume-state.ts: the lifecycle data, the private state file
// schema and its sandbox store. Field names and order match TS, so a state
// file written by either host is read by the other.

var stateFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.json$`)

// supportedStateSDKVersions are the libfx versions whose saved state this
// release reads (TS storedStateSchema sdkVersion). Each pin bump appends the
// new version.
var supportedStateSDKVersions = []string{"0.0.13", "0.0.14"}

// libfxResumeData is the libfx runtime's lifecycle `data` (TS
// fxResumeStateSchema).
type libfxResumeData struct {
	Version       int    `json:"version"`
	StateFileName string `json:"stateFileName"`
}

// fxModelOutput is the parsed-output cursor of a recorded model response.
type fxModelOutput struct {
	Text      int   `json:"text"`
	Reasoning int   `json:"reasoning"`
	Finished  *bool `json:"finished,omitempty"`
}

// fxModelRecord is one recorded model response (TS FxModelRecord).
type fxModelRecord struct {
	Fingerprint   string         `json:"fingerprint"`
	Body          string         `json:"body"`
	Status        int            `json:"status"`
	Complete      bool           `json:"complete"`
	EmittedParts  int            `json:"emittedParts"`
	EmittedOutput *fxModelOutput `json:"emittedOutput,omitempty"`
	Attempt       int            `json:"attempt"`
}

type fxApproval struct {
	Approved bool    `json:"approved"`
	Reason   *string `json:"reason,omitempty"`
}

// fxToolResult is libfx's typed tool result (TS FxToolResult).
type fxToolResult struct {
	Type    string        `json:"type"`
	Text    string        `json:"text"`
	Images  []fxToolImage `json:"images"`
	IsError *bool         `json:"isError,omitempty"`
}

type fxToolImage struct {
	Type      string `json:"type"`
	MimeType  string `json:"mimeType"`
	Data      string `json:"data,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
}

// Tool record statuses.
const (
	toolWaiting   = "waiting"
	toolExecuting = "executing"
	toolCompleted = "completed"
)

// fxToolRecord is one tool call of the open turn (TS FxToolRecord).
type fxToolRecord struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
	Status     string          `json:"status"`
	ApprovalID string          `json:"approvalId,omitempty"`
	Approval   *fxApproval     `json:"approval,omitempty"`
	Result     *fxToolResult   `json:"result,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
}

type fxSteer struct {
	Text          string `json:"text"`
	BeforeRequest int    `json:"beforeRequest"`
}

// fxTurnState is the open turn (TS FxTurnState).
type fxTurnState struct {
	ID             string          `json:"id"`
	Prompt         json.RawMessage `json:"prompt"`
	Signature      string          `json:"signature"`
	ResponseFormat json.RawMessage `json:"responseFormat,omitempty"`
	Models         []*fxModelRecord `json:"models"`
	Tools          []*fxToolRecord  `json:"tools"`
	EmittedKeys    []string         `json:"emittedKeys"`
	Steers         []fxSteer        `json:"steers"`
}

// fxStoredState is the private state file (TS FxStoredState).
type fxStoredState struct {
	Version    int          `json:"version"`
	SDKVersion string       `json:"sdkVersion"`
	Provider   string       `json:"provider,omitempty"`
	Checkpoint string       `json:"checkpoint,omitempty"`
	Turn       *fxTurnState `json:"turn,omitempty"`
}

// errStateInvalid is TS's load failure text.
const errStateInvalid = "Cannot resume fx: the saved state is invalid or incompatible."

// IncompatibleStateError reports lifecycle or saved state this runtime cannot
// resume: state from the other fx runtime, or a state file that fails the
// schema (for example one written by a newer libfx).
type IncompatibleStateError struct {
	Message string
	// Reason details why, for logs; Message is the user-facing text.
	Reason string
}

func (e *IncompatibleStateError) Error() string { return e.Message }

const (
	errLegacyStateUnderLibfx = "this session was created by the sandbox-ACP fx adapter (go-ai <=0.5.x); resume it with fx.Settings{Runtime: fx.RuntimeSandboxACP} or start a new session"
	errLibfxStateUnderLegacy = "this session was created by the libfx fx runtime; resume it with fx.Settings{Runtime: fx.RuntimeLibfx} or start a new session"
)

// isLegacyACPData reports data shaped like the sandbox-ACP adapter's
// lifecycle data (it names an agent identity and no state file).
func isLegacyACPData(data json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	_, agent := probe["agentIdentity"]
	_, file := probe["stateFileName"]
	return agent && !file
}

// isLibfxData reports data shaped like the libfx runtime's lifecycle data.
func isLibfxData(data json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	_, agent := probe["agentIdentity"]
	_, file := probe["stateFileName"]
	return file && !agent
}

// parseLibfxResumeData validates lifecycle data like TS fxResumeStateSchema.
func parseLibfxResumeData(data json.RawMessage) (libfxResumeData, error) {
	if isLegacyACPData(data) {
		return libfxResumeData{}, &IncompatibleStateError{Message: errLegacyStateUnderLibfx, Reason: "sandbox-ACP lifecycle data"}
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &probe); err != nil || probe == nil {
		return libfxResumeData{}, fmt.Errorf("invalid fx lifecycle state data: must be an object")
	}
	var d libfxResumeData
	if err := json.Unmarshal(data, &d); err != nil {
		return libfxResumeData{}, fmt.Errorf("invalid fx lifecycle state data: %w", err)
	}
	if string(bytes.TrimSpace(probe["version"])) != "1" {
		return libfxResumeData{}, errors.New("invalid fx lifecycle state data: version must be 1")
	}
	if _, ok := probe["stateFileName"]; !ok || !stateFileNamePattern.MatchString(d.StateFileName) {
		return libfxResumeData{}, errors.New("invalid fx lifecycle state data: stateFileName is invalid")
	}
	return d, nil
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// validateStoredState mirrors TS storedStateSchema: required keys present,
// typed, and non-null where zod does not accept null.
func validateStoredState(text []byte) (*fxStoredState, error) {
	if err := securejson.Check(text); err != nil {
		return nil, err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(text, &probe); err != nil || probe == nil {
		return nil, errors.New("state must be an object")
	}
	var state fxStoredState
	if err := json.Unmarshal(text, &state); err != nil {
		return nil, err
	}
	if string(bytes.TrimSpace(probe["version"])) != "1" {
		return nil, errors.New("version must be 1")
	}
	if !containsString(supportedStateSDKVersions, state.SDKVersion) {
		return nil, fmt.Errorf("unsupported sdkVersion %q", state.SDKVersion)
	}
	for _, key := range []string{"provider", "checkpoint", "turn"} {
		if raw, ok := probe[key]; ok && isNull(raw) {
			return nil, fmt.Errorf("%s must not be null", key)
		}
	}
	if raw, ok := probe["provider"]; ok {
		var p string
		_ = json.Unmarshal(raw, &p)
		if p != "gateway" && p != "codex" && p != "grok" {
			return nil, errors.New("invalid provider")
		}
	}
	if raw, ok := probe["turn"]; ok {
		if err := validateTurnState(raw); err != nil {
			return nil, fmt.Errorf("turn: %w", err)
		}
	}
	return &state, nil
}

func validateTurnState(raw json.RawMessage) error {
	var turn map[string]json.RawMessage
	if err := json.Unmarshal(raw, &turn); err != nil || turn == nil {
		return errors.New("must be an object")
	}
	for _, key := range []string{"id", "prompt", "signature", "models", "tools", "emittedKeys", "steers"} {
		if v, ok := turn[key]; !ok || isNull(v) {
			return fmt.Errorf("%s is required", key)
		}
	}
	prompt := bytes.TrimSpace(turn["prompt"])
	if len(prompt) == 0 || (prompt[0] != '"' && prompt[0] != '[') {
		return errors.New("prompt must be a string or an array")
	}
	var models []map[string]json.RawMessage
	if err := json.Unmarshal(turn["models"], &models); err != nil {
		return err
	}
	for i, m := range models {
		for _, key := range []string{"fingerprint", "body", "status", "complete", "emittedParts", "attempt"} {
			if v, ok := m[key]; !ok || isNull(v) {
				return fmt.Errorf("models[%d].%s is required", i, key)
			}
		}
		for _, key := range []string{"emittedParts", "attempt"} {
			if !isNonNegativeInt(m[key]) {
				return fmt.Errorf("models[%d].%s must be a nonnegative integer", i, key)
			}
		}
		if out, ok := m["emittedOutput"]; ok {
			var o map[string]json.RawMessage
			if err := json.Unmarshal(out, &o); err != nil || o == nil || !isNonNegativeInt(o["text"]) || !isNonNegativeInt(o["reasoning"]) {
				return fmt.Errorf("models[%d].emittedOutput is invalid", i)
			}
			if f, ok := o["finished"]; ok && isNull(f) {
				return fmt.Errorf("models[%d].emittedOutput.finished must not be null", i)
			}
		}
	}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(turn["tools"], &tools); err != nil {
		return err
	}
	for i, tool := range tools {
		for _, key := range []string{"id", "name", "status"} {
			if v, ok := tool[key]; !ok || isNull(v) {
				return fmt.Errorf("tools[%d].%s is required", i, key)
			}
		}
		var status string
		_ = json.Unmarshal(tool["status"], &status)
		if status != toolWaiting && status != toolExecuting && status != toolCompleted {
			return fmt.Errorf("tools[%d].status is invalid", i)
		}
		for _, key := range []string{"approvalId", "approval", "result"} {
			if v, ok := tool[key]; ok && isNull(v) {
				return fmt.Errorf("tools[%d].%s must not be null", i, key)
			}
		}
		if v, ok := tool["result"]; ok {
			var r map[string]json.RawMessage
			if err := json.Unmarshal(v, &r); err != nil || r == nil || string(bytes.TrimSpace(r["type"])) != `"libfx.tool-result"` {
				return fmt.Errorf("tools[%d].result is invalid", i)
			}
			if t, ok := r["text"]; !ok || isNull(t) {
				return fmt.Errorf("tools[%d].result.text is required", i)
			}
			if im, ok := r["images"]; !ok || isNull(im) {
				return fmt.Errorf("tools[%d].result.images is required", i)
			}
		}
	}
	var steers []map[string]json.RawMessage
	if err := json.Unmarshal(turn["steers"], &steers); err != nil {
		return err
	}
	for i, s := range steers {
		if v, ok := s["text"]; !ok || isNull(v) || !isNonNegativeInt(s["beforeRequest"]) {
			return fmt.Errorf("steers[%d] is invalid", i)
		}
	}
	// The typed decode already checked the remaining field types.
	return nil
}

func isNonNegativeInt(raw json.RawMessage) bool {
	s := string(bytes.TrimSpace(raw))
	if s == "" || strings.ContainsAny(s, ".eE-") {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func containsString(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}

// stateStore is the sandbox store for fxStoredState (TS createFxStateStore).
type stateStore struct {
	sandbox  providerutils.SandboxSession
	fileName string
	filePath string

	mu  sync.Mutex
	err error // a failed write fails every later one, as TS's write chain does
}

func newStateStore(ctx context.Context, sandbox providerutils.SandboxSession, sessionID, workDir, stateFileName string) (*stateStore, error) {
	home, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: `printf '%s' "$HOME" | base64 -w0`})
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(home.Stdout)
	if home.ExitCode != 0 || trimmed == "" {
		return nil, errors.New("Unable to resolve sandbox HOME for fx state.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, errors.New("Unable to resolve sandbox HOME for fx state.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	homeDir := string(decoded)
	if !posixpath.IsAbs(homeDir) {
		return nil, errors.New("Sandbox HOME must be absolute.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	directory := posixpath.Join(harness.SessionDataDirectoryPath(harness.StateDirectoryPath(homeDir), sessionID), "fx")
	rel := posixRelative(workDir, directory)
	if rel == "" || (!strings.HasPrefix(rel, "../") && !posixpath.IsAbs(rel)) {
		return nil, errors.New("fx private state must be outside sessionWorkDir.")
	}
	fileName := stateFileName
	if fileName == "" {
		fileName = "state.json"
	}
	if !stateFileNamePattern.MatchString(fileName) {
		return nil, fmt.Errorf("invalid fx state file name %q", fileName)
	}
	prepared, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "mkdir -p -- " + harnessutil.ShellQuote(directory) + " && chmod 700 -- " + harnessutil.ShellQuote(directory),
	})
	if err != nil {
		return nil, err
	}
	if prepared.ExitCode != 0 {
		return nil, errors.New("Unable to create private fx state directory.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return &stateStore{sandbox: sandbox, fileName: fileName, filePath: posixpath.Join(directory, fileName)}, nil
}

func (s *stateStore) load(ctx context.Context) (*fxStoredState, error) {
	text, err := s.sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: s.filePath})
	if err != nil {
		return nil, err
	}
	if text == nil {
		return nil, errors.New("Cannot resume fx: the saved state file is missing.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	state, verr := validateStoredState([]byte(*text))
	if verr != nil {
		return nil, &IncompatibleStateError{Message: errStateInvalid, Reason: verr.Error()}
	}
	return state, nil
}

// save writes content atomically: a temp file, then mv.
func (s *stateStore) save(ctx context.Context, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	ctx = context.WithoutCancel(ctx)
	if err := s.sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: s.filePath + ".tmp", Content: string(content)}); err != nil {
		s.err = err
		return err
	}
	res, err := s.sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "mv -- " + harnessutil.ShellQuote(s.filePath+".tmp") + " " + harnessutil.ShellQuote(s.filePath),
	})
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("Unable to persist fx state: %s", res.Stderr) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if err != nil {
		s.err = err
	}
	return err
}

// posixRelative mirrors path.posix.relative for absolute paths.
func posixRelative(from, to string) string {
	f := strings.Split(strings.Trim(posixpath.Resolve("/", from), "/"), "/")
	t := strings.Split(strings.Trim(posixpath.Resolve("/", to), "/"), "/")
	if len(f) == 1 && f[0] == "" {
		f = nil
	}
	if len(t) == 1 && t[0] == "" {
		t = nil
	}
	i := 0
	for i < len(f) && i < len(t) && f[i] == t[i] {
		i++
	}
	parts := make([]string, 0, len(f)-i+len(t)-i)
	for range f[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, t[i:]...)
	return strings.Join(parts, "/")
}
