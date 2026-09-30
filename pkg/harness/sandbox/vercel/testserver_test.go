package vercel

// A minimal in-memory Vercel Sandbox API server used by every httptest-based
// test in this package. It implements just enough of the wire protocol
// (see client.go / types.go, ported from
// node_modules/@vercel/sandbox/dist/api-client/*.js) to exercise APIClient,
// *Sandbox, *Session, *NetworkSession, and the provider constructors
// end-to-end.

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

type fakeCommand struct {
	id       string
	stdout   string
	stderr   string
	exitCode int
	// waitCh is closed once the command is considered finished, so a
	// wait=true GET can block until then. For every command created by this
	// fake server, it is closed immediately (no real async execution).
}

type fakeSandboxState struct {
	name              string
	persistent        bool
	currentSnapshotID string
	sessionID         string
	cwd               string
	routes            []sandboxRouteWire
	networkPolicy     *NetworkPolicy
	files             map[string][]byte // tar entry name -> content
	stopped           bool
}

// CommandScript answers one RunCommand/RunCommandDetached call.
type CommandScript struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type fakeServer struct {
	mu           sync.Mutex
	srv          *httptest.Server
	byName       map[string]*fakeSandboxState
	bySession    map[string]*fakeSandboxState
	commands     map[string]*fakeCommand
	nextID       int
	nextSnapshot int

	// CommandFn answers every command run against any session; nil defaults
	// to `{exitCode:0}` with no output. Tests set this to script specific
	// commands.
	CommandFn func(sessionID, command string, args []string, cwd string, env map[string]string) CommandScript

	// snapshotOnStop, when true, makes StopSession return a fresh snapshot id
	// (simulating a sandbox that snapshots on stop).
	snapshotOnStop bool

	// forceCreateError, when set, makes every create request fail with this
	// error instead of creating a sandbox.
	forceCreateError *APIError

	// lastRequests records decoded bodies per path, for assertions.
	createRequests []createSandboxRequest
	updateRequests []struct {
		Name string
		Req  updateSandboxRequest
	}
	getRequests []struct {
		Name   string
		Resume string
	}
}

func newFakeServer() *fakeServer {
	f := &fakeServer{
		byName:    map[string]*fakeSandboxState{},
		bySession: map[string]*fakeSandboxState{},
		commands:  map[string]*fakeCommand{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/sandboxes", f.handleCreateOrList)
	mux.HandleFunc("/v3/sandboxes", f.handleCreateOrList)
	mux.HandleFunc("/v2/sandboxes/", f.handleSandboxPath)
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeServer) Close()      { f.srv.Close() }
func (f *fakeServer) URL() string { return f.srv.URL }

func (f *fakeServer) id() string {
	f.nextID++
	return fmt.Sprintf("id_%d", f.nextID)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponseBody{Error: struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}{Message: message, Code: code}})
}

func (f *fakeServer) snapshotToWire(s *fakeSandboxState) sandboxWire {
	var np *NetworkPolicy
	if s.networkPolicy != nil {
		np = s.networkPolicy
	}
	return sandboxWire{
		Name: s.name, Persistent: s.persistent, CurrentSnapshotID: s.currentSnapshotID,
		NetworkPolicy: np, CurrentSessionID: s.sessionID,
	}
}

func (f *fakeServer) sessionToWire(s *fakeSandboxState) sessionWire {
	return sessionWire{ID: s.sessionID, CWD: s.cwd, NetworkPolicy: s.networkPolicy, Status: "running"}
}

func (f *fakeServer) handleCreateOrList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	var req createSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	f.mu.Lock()
	f.createRequests = append(f.createRequests, req)
	if f.forceCreateError != nil {
		status, code, message := f.forceCreateError.StatusCode, f.forceCreateError.Code, f.forceCreateError.Message
		f.mu.Unlock()
		writeAPIError(w, status, code, message)
		return
	}
	name := req.Name
	if name == "" {
		name = "sbx_" + f.id()
	}
	if existing, ok := f.byName[name]; ok && !existing.stopped {
		f.mu.Unlock()
		writeAPIError(w, 409, "already_exists", "sandbox name already exists")
		return
	}
	sessionID := "sess_" + f.id()
	state := &fakeSandboxState{
		name: name, persistent: req.Persistent != nil && *req.Persistent,
		sessionID: sessionID, cwd: "/vercel/sandbox",
		routes: []sandboxRouteWire{{Port: 3000, Subdomain: name + "-3000", URL: "https://" + name + "-3000.vercel.run"}},
		files:  map[string][]byte{},
	}
	if req.Source != nil && req.Source.Type == "snapshot" {
		state.currentSnapshotID = req.Source.SnapshotID
	}
	if req.NetworkPolicy != nil {
		state.networkPolicy = req.NetworkPolicy
	}
	f.byName[name] = state
	f.bySession[sessionID] = state
	f.mu.Unlock()

	writeJSON(w, 200, sandboxAndSessionResponse{
		Sandbox: f.snapshotToWire(state), Session: f.sessionToWire(state), Routes: state.routes,
	})
}

func (f *fakeServer) handleSandboxPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v2/sandboxes/")
	if rest == "sessions" || strings.HasPrefix(rest, "sessions/") {
		f.handleSessionPath(w, r, strings.TrimPrefix(rest, "sessions/"))
		return
	}
	name := rest
	switch r.Method {
	case http.MethodGet:
		f.handleGetSandbox(w, r, name)
	case http.MethodPatch:
		f.handleUpdateSandbox(w, r, name)
	case http.MethodDelete:
		f.handleDeleteSandbox(w, name)
	default:
		writeAPIError(w, 405, "method_not_allowed", "method not allowed")
	}
}

func (f *fakeServer) handleGetSandbox(w http.ResponseWriter, r *http.Request, name string) {
	resume := r.URL.Query().Get("resume")
	f.mu.Lock()
	f.getRequests = append(f.getRequests, struct {
		Name   string
		Resume string
	}{name, resume})
	state, ok := f.byName[name]
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, 404, "not_found", "sandbox not found")
		return
	}
	if resume == "true" && state.stopped {
		state.stopped = false
	}
	writeJSON(w, 200, sandboxAndSessionResponse{
		Sandbox: f.snapshotToWire(state), Session: f.sessionToWire(state), Routes: state.routes,
	})
}

func (f *fakeServer) handleUpdateSandbox(w http.ResponseWriter, r *http.Request, name string) {
	var req updateSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	f.mu.Lock()
	f.updateRequests = append(f.updateRequests, struct {
		Name string
		Req  updateSandboxRequest
	}{name, req})
	state, ok := f.byName[name]
	if !ok {
		f.mu.Unlock()
		writeAPIError(w, 404, "not_found", "sandbox not found")
		return
	}
	if req.Persistent != nil {
		state.persistent = *req.Persistent
	}
	var routes []sandboxRouteWire
	if req.Ports != nil {
		routes = make([]sandboxRouteWire, len(req.Ports))
		for i, p := range req.Ports {
			routes[i] = sandboxRouteWire{Port: p, Subdomain: fmt.Sprintf("%s-%d", state.name, p), URL: fmt.Sprintf("https://%s-%d.vercel.run", state.name, p)}
		}
		state.routes = routes
	}
	if req.NetworkPolicy != nil {
		state.networkPolicy = req.NetworkPolicy
	}
	resp := updateSandboxResponse{Sandbox: f.snapshotToWire(state), Routes: routes}
	f.mu.Unlock()
	writeJSON(w, 200, resp)
}

func (f *fakeServer) handleDeleteSandbox(w http.ResponseWriter, name string) {
	f.mu.Lock()
	state, ok := f.byName[name]
	if ok {
		delete(f.byName, name)
		delete(f.bySession, state.sessionID)
	}
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, 404, "not_found", "sandbox not found")
		return
	}
	writeJSON(w, 200, updateSandboxResponse{Sandbox: f.snapshotToWire(state)})
}

func (f *fakeServer) handleSessionPath(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.SplitN(rest, "/", 2)
	sessionID := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	f.mu.Lock()
	state, ok := f.bySession[sessionID]
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, 404, "not_found", "session not found")
		return
	}
	switch {
	case sub == "stop" && r.Method == http.MethodPost:
		f.handleStop(w, state)
	case sub == "network-policy" && r.Method == http.MethodPost:
		f.handleSessionNetworkPolicy(w, r, state)
	case sub == "cmd" && r.Method == http.MethodPost:
		f.handleRunCommand(w, r, state)
	case sub == "fs/mkdir" && r.Method == http.MethodPost:
		f.handleMkdir(w, r, state)
	case sub == "fs/write" && r.Method == http.MethodPost:
		f.handleWriteFiles(w, r, state)
	case sub == "fs/read" && r.Method == http.MethodPost:
		f.handleReadFile(w, r, state)
	case strings.HasPrefix(sub, "cmd/"):
		f.handleCommandSubpath(w, r, state, strings.TrimPrefix(sub, "cmd/"))
	default:
		writeAPIError(w, 404, "not_found", "unknown session endpoint "+sub)
	}
}

func (f *fakeServer) handleStop(w http.ResponseWriter, state *fakeSandboxState) {
	state.stopped = true
	resp := stopSessionResponse{Session: f.sessionToWire(state)}
	if f.snapshotOnStop {
		f.nextSnapshot++
		id := fmt.Sprintf("snap_%d", f.nextSnapshot)
		state.currentSnapshotID = id
		resp.Snapshot = &struct {
			ID string `json:"id"`
		}{ID: id}
		sandboxCopy := f.snapshotToWire(state)
		resp.Sandbox = &sandboxCopy
	}
	writeJSON(w, 200, resp)
}

func (f *fakeServer) handleSessionNetworkPolicy(w http.ResponseWriter, r *http.Request, state *fakeSandboxState) {
	var policy NetworkPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	state.networkPolicy = &policy
	writeJSON(w, 200, sessionResponse{Session: f.sessionToWire(state)})
}

func (f *fakeServer) handleRunCommand(w http.ResponseWriter, r *http.Request, state *fakeSandboxState) {
	var req runCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	script := CommandScript{}
	if f.CommandFn != nil {
		script = f.CommandFn(state.sessionID, req.Command, req.Args, req.CWD, req.Env)
	}
	f.mu.Lock()
	cmdID := "cmd_" + f.id()
	f.commands[cmdID] = &fakeCommand{id: cmdID, stdout: script.Stdout, stderr: script.Stderr, exitCode: script.ExitCode}
	f.mu.Unlock()

	exitCode := script.ExitCode
	initial := commandWire{ID: cmdID, Name: req.Command, Args: req.Args, CWD: req.CWD, SessionID: state.sessionID}
	final := initial
	final.ExitCode = &exitCode

	if !req.Wait {
		writeJSON(w, 200, commandResponse{Command: initial})
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)
	writeNDJSON(w, commandResponse{Command: initial})
	if flusher != nil {
		flusher.Flush()
	}
	if script.Stdout != "" {
		writeNDJSON(w, ndjsonLine{Stream: "stdout", Data: script.Stdout})
	}
	if script.Stderr != "" {
		writeNDJSON(w, ndjsonLine{Stream: "stderr", Data: script.Stderr})
	}
	writeNDJSON(w, commandResponse{Command: final})
	if flusher != nil {
		flusher.Flush()
	}
}

func (f *fakeServer) handleCommandSubpath(w http.ResponseWriter, r *http.Request, state *fakeSandboxState, rest string) {
	parts := strings.SplitN(rest, "/", 2)
	cmdID := parts[0]
	f.mu.Lock()
	cmd, ok := f.commands[cmdID]
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, 404, "not_found", "command not found")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		exitCode := cmd.exitCode
		writeJSON(w, 200, commandResponse{Command: commandWire{ID: cmd.id, SessionID: state.sessionID, ExitCode: &exitCode}})
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		if cmd.stdout != "" {
			writeNDJSON(w, ndjsonLine{Stream: "stdout", Data: cmd.stdout})
		}
		if cmd.stderr != "" {
			writeNDJSON(w, ndjsonLine{Stream: "stderr", Data: cmd.stderr})
		}
		if flusher != nil {
			flusher.Flush()
		}
	case len(parts) == 2 && parts[1] == "kill" && r.Method == http.MethodPost:
		writeJSON(w, 200, commandResponse{Command: commandWire{ID: cmd.id, SessionID: state.sessionID}})
	default:
		writeAPIError(w, 404, "not_found", "unknown command endpoint")
	}
}

func writeNDJSON(w io.Writer, v interface{}) {
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n"))
}

func (f *fakeServer) handleMkdir(w http.ResponseWriter, r *http.Request, state *fakeSandboxState) {
	var req mkdirRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	writeJSON(w, 200, map[string]interface{}{})
}

func (f *fakeServer) handleWriteFiles(w http.ResponseWriter, r *http.Request, state *fakeSandboxState) {
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeAPIError(w, 400, "bad_request", err.Error())
			return
		}
		content, _ := io.ReadAll(tr)
		f.mu.Lock()
		state.files[hdr.Name] = content
		f.mu.Unlock()
	}
	writeJSON(w, 200, map[string]interface{}{})
}

func (f *fakeServer) handleReadFile(w http.ResponseWriter, r *http.Request, state *fakeSandboxState) {
	var req readFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	name, err := normalizePath(req.Path, orDefault(req.CWD, state.cwd), "/")
	if err != nil {
		writeAPIError(w, 400, "bad_request", err.Error())
		return
	}
	f.mu.Lock()
	content, ok := state.files[name]
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, 404, "not_found", "file not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(200)
	_, _ = w.Write(content)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
