package libfx

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
)

// This file ports the part of libfx's createFxEngine (fx-sdk.js:2252-3494)
// that the TS harness uses: one session on one core, with host tools,
// instructions, a checkpoint to restore, semantic hooks, turns with
// steering and cancellation, and checkpoints. The journal, persistence,
// follow-ups, handoff and subscription authorizer are not ported.

// fxSdkAPIVersion is libfx's fxSdkApiVersion, recorded in checkpoint meta.
const fxSdkAPIVersion = 2

// Turn-event and tool limits (fx-sdk.js:15-63).
const (
	maxInstructionsBytes   = 64 << 10
	maxHostTools           = 64
	maxUnreadEvents        = 256
	maxUnreadEventBytes    = 1 << 20
	maxToolStartInputBytes = 64 << 10
	maxPromptImages        = 8
	maxPromptImageBytes    = (5 << 20) / 4 * 3
	maxPromptImagesBytes   = (8 << 20) / 4 * 3
	maxToolImageDataBytes  = 5 << 20
	maxToolImagesDataBytes = 8 << 20
)

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrNewerCheckpoint reports a checkpoint saved by a newer fx core
// (libfx FxJournalVersionError).
var ErrNewerCheckpoint = errors.New("fx checkpoint was created by a newer fx")

// NewerCheckpointError wraps ErrNewerCheckpoint with the core's message.
type NewerCheckpointError struct{ Message string }

func (e *NewerCheckpointError) Error() string { return e.Message }
func (e *NewerCheckpointError) Unwrap() error { return ErrNewerCheckpoint }

// Tool is one host tool the core may call (fx-sdk.js normalizeHostTools).
type Tool struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object.
	InputSchema any
	Idempotent  bool
	// ProviderExecuted declares a tool the provider runs itself; it has no
	// Execute.
	ProviderExecuted bool
	// Execute runs the tool. A *TypedToolResult (or TypedToolResult) is a
	// rich result; a string is sent as is; any other value is sent as JSON.
	// An error marks the call failed; a *ToolResultError carries a rich
	// result for the failure.
	Execute func(ctx context.Context, input json.RawMessage) (any, error)
}

// ToolImage is one image of a typed tool result.
type ToolImage struct {
	Type      string `json:"type"`
	MimeType  string `json:"mimeType"`
	Data      string `json:"data,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
}

// TypedToolResult is libfx's `libfx.tool-result` (TS FxToolResult).
type TypedToolResult struct {
	Type    string      `json:"type"`
	Text    string      `json:"text"`
	Images  []ToolImage `json:"images"`
	IsError *bool       `json:"isError,omitempty"`
}

// ToolResultError is a tool failure carrying a typed result.
type ToolResultError struct{ Result TypedToolResult }

func (e *ToolResultError) Error() string { return e.Result.Text }

// HookEvent is one semantic hook event (libfx/hook params).
type HookEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	Raw  json.RawMessage `json:"-"`
}

// AgentOptions configures StartAgent.
type AgentOptions struct {
	Wasm             Source
	MemoryLimitPages uint32
	APIKey           string
	Provider         string
	Model            string
	Effort           string
	Fast             *bool
	Ultrafast        *bool
	Instructions     string
	Tools            []Tool
	// Checkpoint is restored into the new session.
	Checkpoint []byte
	// Fetch performs the core's requests. The agent retries a failed fetch
	// once, as libfx's transportFetch does.
	Fetch FetchFunc
	// OnHook receives semantic hook events for this session. Setting it
	// asks the core for hooks.
	OnHook func(HookEvent)
	// OnReceive sees every core notification except hooks, on the guest
	// goroutine and before the turn queues it (libfx's acp.receive event).
	OnReceive func(method string, params json.RawMessage)
	// Subscription supplies the credentials of a codex or grok agent
	// (libfx credentials, onCredentials, refreshLock). Required for those
	// providers and refused for any other.
	Subscription *SubscriptionAuth
	Stderr       io.Writer
}

// Agent is one fx session on one core.
type Agent struct {
	engine *Engine
	opts   AgentOptions
	tools  map[string]Tool
	meta   map[string]any
	auth   *authorizer

	mu               sync.Mutex
	sessionID        string
	active           *Turn
	closing          bool
	nextAttachmentID uint32
}

// StartAgent starts a core, initializes it with the host tools and
// instructions, opens a session, and restores Checkpoint when given.
func StartAgent(ctx context.Context, opts AgentOptions) (*Agent, error) {
	descriptors, tools, err := normalizeHostTools(opts.Tools)
	if err != nil {
		return nil, err
	}
	if len(opts.Instructions) > maxInstructionsBytes {
		return nil, fmt.Errorf("instructions exceed the %d byte libfx limit", maxInstructionsBytes)
	}
	if len(opts.Checkpoint) > MaxCheckpointBytes {
		return nil, errors.New("libfx checkpoint is too large")
	}
	a := &Agent{
		opts:             opts,
		tools:            tools,
		nextAttachmentID: 1,
		meta: map[string]any{
			"libfxVersion":   strconv.Itoa(fxSdkAPIVersion),
			"toolSchemaHash": toolSchemaHash(descriptors),
			"model":          opts.Model,
		},
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = AllowlistFetch(defaultFetchClient, DefaultAllowedOrigins)
	}
	switch opts.Provider {
	case "codex", "grok":
		if opts.Subscription == nil || opts.Subscription.Credentials == nil {
			return nil, fmt.Errorf("provider %q needs a credentials() function", opts.Provider)
		}
		auth, err := newAuthorizer(subscriptionSpecs(opts.Provider), *opts.Subscription, fetch)
		if err != nil {
			return nil, err
		}
		a.auth = auth
	case "", "gateway":
		if opts.Subscription != nil {
			return nil, errors.New(`credentials needs provider "codex", "grok" or a custom provider`)
		}
	default:
		return nil, errors.New(`provider must be "gateway", "codex", "grok" or a custom provider { name, baseUrl }`)
	}
	e, err := Start(ctx, Options{
		Wasm:                opts.Wasm,
		MemoryLimitPages:    opts.MemoryLimitPages,
		APIKey:              opts.APIKey,
		Provider:            opts.Provider,
		Model:               opts.Model,
		Effort:              opts.Effort,
		Fast:                opts.Fast,
		Ultrafast:           opts.Ultrafast,
		Fetch:               a.transportFetch(fetch),
		ExecuteTool:         a.executeHostTool,
		OnGuestNotification: a.onNotification,
		Stderr:              opts.Stderr,
	})
	if err != nil {
		return nil, err
	}
	a.engine = e
	if err := a.open(ctx, descriptors); err != nil {
		a.mu.Lock()
		a.closing = true
		a.mu.Unlock()
		e.AbortHostEffects()
		_ = e.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return a, nil
}

func (a *Agent) open(ctx context.Context, descriptors []map[string]any) error {
	caps := map[string]any{}
	if len(descriptors) > 0 || a.opts.Instructions != "" {
		caps["libfx"] = map[string]any{"tools": descriptors, "instructions": a.opts.Instructions}
	}
	if _, err := a.engine.Request(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": caps}); err != nil {
		return err
	}
	params := map[string]any{}
	if a.opts.OnHook != nil {
		params["hooks"] = map[string]any{"captureContent": false}
	}
	raw, err := a.engine.Request(ctx, "libfx/new", params)
	if err != nil {
		return err
	}
	var res struct {
		SessionID string `json:"sessionId"`
		Meta      struct {
			Fx struct {
				HooksVersion int `json:"hooksVersion"`
			} `json:"fx"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.SessionID == "" {
		return fmt.Errorf("fx libfx/new returned an invalid session: %s", raw)
	}
	a.mu.Lock()
	a.sessionID = res.SessionID
	a.mu.Unlock()
	if a.opts.OnHook != nil && res.Meta.Fx.HooksVersion != 1 {
		return errors.New("Semantic hooks require a matching libfx core with hooks version 1") //nolint:staticcheck // matches libfx's exact error text
	}
	if len(a.opts.Checkpoint) > 0 {
		ids, err := a.attach(a.opts.Checkpoint)
		if err != nil {
			return err
		}
		if _, err := a.engine.Request(ctx, "libfx/restore", map[string]any{"sessionId": res.SessionID, "checkpointAttachment": ids[0]}); err != nil {
			var rpcErr *RPCError
			if errors.As(err, &rpcErr) && strings.Contains(rpcErr.Message, "newer fx") {
				return &NewerCheckpointError{Message: rpcErr.Message}
			}
			return err
		}
	}
	return nil
}

// attach places payloads beside the next frame, dropping ones an earlier
// frame left (fx-sdk.js attachBytes).
func (a *Agent) attach(payloads ...[]byte) ([]uint32, error) {
	a.engine.DiscardAttachments()
	ids := make([]uint32, 0, len(payloads))
	for _, p := range payloads {
		a.mu.Lock()
		id := a.nextAttachmentID
		if id == 0x7fffffff {
			a.nextAttachmentID = 1
		} else {
			a.nextAttachmentID = id + 1
		}
		a.mu.Unlock()
		if err := a.engine.WriteAttachment(id, p); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// SessionID is the core's session id.
func (a *Agent) SessionID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID
}

func (a *Agent) currentTurn(t *Turn) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return t != nil && a.active == t && !a.closing && !t.isCancelled()
}

// onNotification runs on the guest goroutine (fx-sdk.js setLineHandler).
func (a *Agent) onNotification(ctx context.Context, method string, params json.RawMessage, size int) {
	sessionID := a.SessionID()
	if method == "libfx/hook" {
		var h struct {
			RootSessionID string          `json:"rootSessionId"`
			Version       int             `json:"version"`
			Type          string          `json:"type"`
			Data          json.RawMessage `json:"data"`
		}
		if a.opts.OnHook != nil && json.Unmarshal(params, &h) == nil && h.RootSessionID == sessionID && h.Version == 1 {
			a.opts.OnHook(HookEvent{Type: h.Type, Data: h.Data, Raw: params})
		}
		return
	}
	if a.opts.OnReceive != nil {
		a.opts.OnReceive(method, params)
	}
	if method != "session/update" {
		return
	}
	var p struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil || p.SessionID != sessionID {
		return
	}
	a.mu.Lock()
	t := a.active
	a.mu.Unlock()
	if t != nil {
		t.push(ctx, p.Update, size)
	}
}

// transportFetch wraps the host fetch: a failed attempt is retried once
// unless the request was aborted (fx-sdk.js transportFetch).
func (a *Agent) transportFetch(fetch FetchFunc) FetchFunc {
	return func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			var err error
			body, err = io.ReadAll(req.Body)
			_ = req.Body.Close()
			if err != nil {
				return nil, err
			}
		}
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			a.mu.Lock()
			t := a.active
			a.mu.Unlock()
			if t != nil && t.isCancelled() {
				a.engine.AbortHostEffects()
				return nil, context.Canceled
			}
			clone := req.Clone(req.Context())
			if body != nil {
				clone.Body = io.NopCloser(strings.NewReader(string(body)))
				clone.ContentLength = int64(len(body))
				clone.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(string(body))), nil }
			}
			var resp *http.Response
			var err error
			if spec := a.auth.specFor(endpointOf(req.URL)); spec != nil {
				resp, err = a.auth.do(spec, endpointOf(req.URL), fetch, clone, body)
			} else {
				resp, err = fetch(clone)
			}
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
		}
		return nil, lastErr
	}
}

// executeHostTool runs a host tool for the active turn (fx-sdk.js
// executeHostTool). It never returns an error: a failure is an error result.
func (a *Agent) executeHostTool(ctx context.Context, name string, input json.RawMessage, _ string) (ToolResult, error) {
	a.mu.Lock()
	t := a.active
	a.mu.Unlock()
	if !a.currentTurn(t) {
		return ToolResult{IsError: true, Cancelled: true}, nil
	}
	tool, ok := a.tools[name]
	tctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := t.addToolController(cancel)
	defer stop()
	var res ToolResult
	if !ok || tool.Execute == nil {
		res = ToolResult{Content: "unknown host tool: " + name, IsError: true}
	} else {
		value, err := tool.Execute(tctx, input)
		switch {
		case tctx.Err() != nil:
		case err != nil:
			res.IsError = true
			var typed *ToolResultError
			if errors.As(err, &typed) {
				content, rich, cerr := hostToolContent(typed.Result)
				if cerr != nil {
					res.Content = err.Error()
				} else {
					res.Content, res.Rich = content, rich
				}
			} else {
				res.Content = err.Error()
			}
		default:
			content, rich, cerr := hostToolContent(value)
			if cerr != nil {
				res = ToolResult{Content: cerr.Error(), IsError: true}
			} else {
				res.Content, res.Rich = content, rich
				if typed, ok := asTyped(value); ok && typed.IsError != nil && *typed.IsError {
					res.IsError = true
				}
			}
		}
	}
	if res.IsError && res.Content == "" {
		res.Content = fmt.Sprintf("Tool %s failed without a message", name)
	}
	res.Cancelled = tctx.Err() != nil || !a.currentTurn(t)
	return res, nil
}

func asTyped(v any) (TypedToolResult, bool) {
	switch r := v.(type) {
	case TypedToolResult:
		return r, r.Type == "libfx.tool-result"
	case *TypedToolResult:
		if r != nil {
			return *r, r.Type == "libfx.tool-result"
		}
	}
	return TypedToolResult{}, false
}

// hostToolContent encodes a tool's value for the core (fx-sdk.js
// hostToolContent).
func hostToolContent(v any) (string, bool, error) {
	if typed, ok := asTyped(v); ok {
		if len(typed.Images) > 8 {
			return "", false, errors.New("invalid typed tool result")
		}
		images := make([]ToolImage, 0, len(typed.Images))
		for _, img := range typed.Images {
			if img.Type != "image" || img.MimeType == "" || len(img.MimeType) > 128 || (img.Data == "" && img.SourceRef == "") || (len(img.Data) > maxToolImageDataBytes && img.SourceRef == "") {
				return "", false, errors.New("invalid tool image")
			}
			if len(img.Data) > maxToolImageDataBytes {
				img.Data = ""
			}
			images = append(images, img)
		}
		// libfx's order: drop the data of referenced images when the images
		// or the encoded result are over the limit, and only then fail.
		if imageDataBytes(images) > maxToolImagesDataBytes {
			images = omitReferencedImageData(images)
		}
		if imageDataBytes(images) > maxToolImagesDataBytes {
			return "", false, errors.New("tool images exceed the result limit")
		}
		encode := func() []byte {
			content, _ := jsstr.Marshal(struct {
				Text   string      `json:"text"`
				Images []ToolImage `json:"images"`
			}{typed.Text, images})
			return content
		}
		content := encode()
		if len(content) > maxToolImagesDataBytes {
			images = omitReferencedImageData(images)
			content = encode()
		}
		if len(content) > maxToolImagesDataBytes {
			return "", false, errors.New("typed tool result exceeds the result limit")
		}
		return string(content), true, nil
	}
	switch s := v.(type) {
	case string:
		return s, false, nil
	case nil:
		return "null", false, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "null", false, nil
	}
	return string(data), false, nil
}

// imageDataBytes is libfx promptImageDataBytes for base64 image data.
func imageDataBytes(images []ToolImage) int {
	total := 0
	for _, img := range images {
		total += len(img.Data)
	}
	return total
}

func omitReferencedImageData(images []ToolImage) []ToolImage {
	out := make([]ToolImage, len(images))
	for i, img := range images {
		if img.SourceRef != "" {
			img.Data = ""
		}
		out[i] = img
	}
	return out
}

// normalizeHostTools validates tools and builds the initialize descriptors
// (fx-sdk.js normalizeHostTools).
func normalizeHostTools(tools []Tool) ([]map[string]any, map[string]Tool, error) {
	if len(tools) > maxHostTools {
		return nil, nil, fmt.Errorf("tools cannot contain more than %d entries", maxHostTools)
	}
	descriptors := make([]map[string]any, 0, len(tools))
	byName := map[string]Tool{}
	for i, t := range tools {
		if !toolNamePattern.MatchString(t.Name) {
			return nil, nil, fmt.Errorf("tool %d has an invalid name", i)
		}
		if _, dup := byName[t.Name]; dup {
			return nil, nil, fmt.Errorf("duplicate tool name: %s", t.Name)
		}
		byName[t.Name] = t
		if t.ProviderExecuted {
			if t.Execute != nil {
				return nil, nil, fmt.Errorf("provider-executed tool %s must not define execute()", t.Name)
			}
			descriptors = append(descriptors, map[string]any{"name": t.Name, "providerExecuted": true})
			continue
		}
		if t.Execute == nil {
			return nil, nil, fmt.Errorf("tool %s requires execute()", t.Name)
		}
		schema, err := json.Marshal(t.InputSchema)
		if err != nil || len(schema) == 0 || schema[0] != '{' {
			return nil, nil, fmt.Errorf("tool %s requires an object inputSchema", t.Name)
		}
		d := map[string]any{"name": t.Name, "description": t.Description, "inputSchema": json.RawMessage(schema)}
		if t.Idempotent {
			d["idempotent"] = true
		}
		descriptors = append(descriptors, d)
	}
	return descriptors, byName, nil
}

// toolSchemaHash is libfx's FNV-1a hash of the sorted descriptors. Go
// serializes schema keys sorted, so it can differ from a JS host's hash; it
// only feeds the core's informational checkpoint-mismatch report.
func toolSchemaHash(descriptors []map[string]any) string {
	sorted := append([]map[string]any(nil), descriptors...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i]["name"].(string) < sorted[j]["name"].(string) })
	shape, _ := json.Marshal(sorted)
	hash := uint32(0x811c9dc5)
	for _, b := range shape {
		hash = (hash ^ uint32(b)) * 0x01000193
	}
	return fmt.Sprintf("%08x", hash)
}

// Checkpoint returns the session state, when no turn runs.
func (a *Agent) Checkpoint(ctx context.Context) ([]byte, error) {
	a.mu.Lock()
	switch {
	case a.closing:
		a.mu.Unlock()
		return nil, errors.New("fx agent is closed")
	case a.active != nil:
		a.mu.Unlock()
		return nil, errors.New("cannot checkpoint while a prompt is active")
	}
	sessionID := a.sessionID
	a.mu.Unlock()
	raw, err := a.engine.Request(ctx, "libfx/checkpoint", map[string]any{"sessionId": sessionID, "checkpointMeta": a.meta})
	if err != nil {
		return nil, err
	}
	var res struct {
		CheckpointAttachment int32 `json:"checkpointAttachment"`
	}
	_ = json.Unmarshal(raw, &res)
	var data []byte
	if res.CheckpointAttachment > 0 {
		data = a.engine.TakeAttachment(res.CheckpointAttachment)
	}
	if len(data) == 0 {
		return nil, errors.New("fx returned an invalid checkpoint")
	}
	return data, nil
}

// Close cancels the active turn, waits for it, and stops the core.
func (a *Agent) Close(ctx context.Context) error {
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		<-a.engine.Done()
		return nil
	}
	t := a.active
	a.mu.Unlock()
	if t != nil {
		t.Cancel()
		// A core that does not answer the cancelled prompt is stopped by
		// Engine.Close, which also fails the prompt request.
		wctx, cancel := context.WithTimeout(ctx, closeGrace)
		_, _ = t.Wait(wctx)
		cancel()
	}
	a.mu.Lock()
	a.closing = true
	a.mu.Unlock()
	return a.engine.Close(ctx)
}

// PromptBlock is one prompt block: text, or an image with base64 data.
type PromptBlock struct {
	Type     string
	Text     string
	Data     string
	MimeType string
}

// Event is one normalized turn event (fx-sdk.js normalizeTurn).
type Event struct {
	// Type is text_delta, reasoning_delta, user_message, tool_start or
	// tool_end.
	Type           string
	Delta          string
	Text           string
	ID             string
	Name           string
	Input          json.RawMessage
	InputTruncated bool
	Content        *string
	IsError        bool
}

// TurnResult is a finished turn's stop reason and usage.
type TurnResult struct {
	StopReason string
	Usage      map[string]int64
}

type queuedUpdate struct {
	update json.RawMessage
	size   int
}

// Turn is one prompt turn. Next drains its events; Wait returns its result.
type Turn struct {
	agent *Agent
	id    string

	mu          sync.Mutex
	queue       []queuedUpdate
	queuedBytes int
	changed     chan struct{} // closed and replaced when the queue changes
	cancelled   bool
	finished    bool
	terminal    error
	controllers map[int]context.CancelFunc
	nextCtrl    int

	done   chan struct{}
	result TurnResult
	err    error

	// Normalization state, touched only by the Next caller.
	toolNames map[string]string
	started   map[string]bool
}

func (t *Turn) signalLocked() {
	close(t.changed)
	t.changed = make(chan struct{})
}

func (t *Turn) isCancelled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cancelled
}

func (t *Turn) addToolController(cancel context.CancelFunc) func() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelled {
		cancel()
		return func() {}
	}
	id := t.nextCtrl
	t.nextCtrl++
	t.controllers[id] = cancel
	return func() {
		t.mu.Lock()
		delete(t.controllers, id)
		t.mu.Unlock()
	}
}

// push queues a raw update, blocking (the guest) while the unread-event
// limits are reached, until the consumer reads, the turn ends or ctx ends.
func (t *Turn) push(ctx context.Context, update json.RawMessage, size int) {
	update = append(json.RawMessage(nil), update...)
	for {
		t.mu.Lock()
		if t.cancelled || t.finished {
			t.mu.Unlock()
			return
		}
		if len(t.queue) == 0 || (len(t.queue) < maxUnreadEvents && size <= maxUnreadEventBytes-t.queuedBytes) {
			t.queue = append(t.queue, queuedUpdate{update: update, size: size})
			t.queuedBytes += size
			t.signalLocked()
			t.mu.Unlock()
			return
		}
		wait := t.changed
		t.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return
		}
	}
}

// Next returns the next event, io.EOF when the turn has ended, or the
// turn's error. It blocks until an event arrives or ctx ends.
func (t *Turn) Next(ctx context.Context) (Event, error) {
	for {
		t.mu.Lock()
		if len(t.queue) > 0 {
			q := t.queue[0]
			t.queue = t.queue[1:]
			t.queuedBytes -= q.size
			t.signalLocked()
			t.mu.Unlock()
			if ev, ok := t.eventFor(q.update); ok {
				return ev, nil
			}
			continue
		}
		if t.terminal != nil {
			err := t.terminal
			t.mu.Unlock()
			return Event{}, err
		}
		if t.finished {
			t.mu.Unlock()
			return Event{}, io.EOF
		}
		wait := t.changed
		t.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		}
	}
}

func (t *Turn) eventFor(raw json.RawMessage) (Event, bool) {
	var u struct {
		SessionUpdate string          `json:"sessionUpdate"`
		ToolCallID    string          `json:"toolCallId"`
		Name          string          `json:"name"`
		ToolName      string          `json:"toolName"`
		Title         string          `json:"title"`
		Status        string          `json:"status"`
		RawInput      json.RawMessage `json:"rawInput"`
		Content       json.RawMessage `json:"content"`
		Meta          struct {
			Fx struct {
				Steering struct {
					InputID string `json:"inputId"`
				} `json:"steering"`
			} `json:"fx"`
		} `json:"_meta"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return Event{}, false
	}
	var block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	switch u.SessionUpdate {
	case "agent_message_chunk":
		_ = json.Unmarshal(u.Content, &block)
		if block.Text == "" || strings.HasPrefix(block.Text, "[context]") {
			return Event{}, false
		}
		return Event{Type: "text_delta", Delta: block.Text}, true
	case "agent_thought_chunk":
		_ = json.Unmarshal(u.Content, &block)
		if block.Text == "" {
			return Event{}, false
		}
		return Event{Type: "reasoning_delta", Delta: block.Text}, true
	case "user_message_chunk":
		_ = json.Unmarshal(u.Content, &block)
		if block.Type != "text" {
			return Event{}, false
		}
		return Event{Type: "user_message", Text: block.Text, ID: u.Meta.Fx.Steering.InputID}, true
	case "tool_call":
		name := firstNonEmpty(u.Name, u.ToolName, u.Title, "tool")
		t.toolNames[u.ToolCallID] = name
		if t.started[u.ToolCallID] {
			return Event{}, false
		}
		t.started[u.ToolCallID] = true
		ev := Event{Type: "tool_start", ID: u.ToolCallID, Name: name}
		if len(u.RawInput) > 0 && string(u.RawInput) != "null" {
			if len(u.RawInput) <= maxToolStartInputBytes {
				ev.Input = u.RawInput
			} else {
				ev.InputTruncated = true
			}
		}
		return ev, true
	case "tool_call_update":
		if u.Status != "completed" && u.Status != "failed" {
			return Event{}, false
		}
		ev := Event{Type: "tool_end", ID: u.ToolCallID, Name: firstNonEmpty(t.toolNames[u.ToolCallID], "tool"), IsError: u.Status == "failed"}
		var entries []struct {
			Content struct {
				Type string  `json:"type"`
				Text *string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(u.Content, &entries) == nil {
			for _, e := range entries {
				if e.Content.Type == "text" {
					ev.Content = e.Content.Text
					break
				}
			}
		}
		return ev, true
	}
	return Event{}, false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Prompt starts a turn. ctx cancellation cancels the turn.
func (a *Agent) Prompt(ctx context.Context, blocks []PromptBlock) (*Turn, error) {
	a.mu.Lock()
	switch {
	case a.closing:
		a.mu.Unlock()
		return nil, errors.New("fx agent is closed")
	case a.active != nil:
		a.mu.Unlock()
		return nil, errors.New("a prompt is already in progress for this session")
	}
	a.mu.Unlock()
	prompt, images, err := preparePrompt(blocks)
	if err != nil {
		return nil, err
	}
	t := &Turn{
		agent:       a,
		id:          "turn_" + randomID(),
		changed:     make(chan struct{}),
		controllers: map[int]context.CancelFunc{},
		done:        make(chan struct{}),
		toolNames:   map[string]string{},
		started:     map[string]bool{},
	}
	if ctx.Err() != nil {
		t.finished = true
		t.result = TurnResult{StopReason: "cancelled"}
		close(t.done)
		return t, nil
	}
	a.mu.Lock()
	if a.active != nil {
		a.mu.Unlock()
		return nil, errors.New("a prompt is already in progress for this session")
	}
	a.active = t
	sessionID := a.sessionID
	a.mu.Unlock()
	a.engine.OpenSteering()
	stop := context.AfterFunc(ctx, t.Cancel)
	go func() {
		defer stop()
		res, err := a.sendPrompt(sessionID, t.id, prompt, images)
		t.finish(res, err)
	}()
	return t, nil
}

func (a *Agent) sendPrompt(sessionID, turnID string, prompt []map[string]any, images [][]byte) (TurnResult, error) {
	if len(images) > 0 {
		ids, err := a.attach(images...)
		if err != nil {
			return TurnResult{}, err
		}
		next := 0
		for _, block := range prompt {
			if block["type"] == "image" {
				block["_meta"] = map[string]any{"fx": map[string]any{"attachment": ids[next]}}
				next++
			}
		}
	}
	params := map[string]any{"sessionId": sessionID, "prompt": prompt}
	if a.opts.OnHook != nil {
		params["_meta"] = map[string]any{"fx": map[string]any{"turnId": turnID}}
	}
	raw, err := a.engine.Request(context.Background(), "session/prompt", params)
	if err != nil {
		return TurnResult{}, err
	}
	var res struct {
		StopReason string                     `json:"stopReason"`
		Usage      map[string]json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return TurnResult{}, fmt.Errorf("fx session/prompt returned an invalid result: %w", err)
	}
	usage := map[string]int64{}
	for _, key := range []string{"inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens", "reasoningTokens"} {
		if v, ok := res.Usage[key]; ok {
			if n, err := strconv.ParseInt(string(v), 10, 64); err == nil && n >= -(1<<53-1) && n <= 1<<53-1 {
				usage[key] = n
			}
		}
	}
	return TurnResult{StopReason: res.StopReason, Usage: usage}, nil
}

// preparePrompt validates blocks and splits image bytes out as attachments
// (fx-sdk.js normalizePromptInput and preparePromptImages).
func preparePrompt(blocks []PromptBlock) ([]map[string]any, [][]byte, error) {
	prompt := make([]map[string]any, 0, len(blocks))
	var images [][]byte
	total := 0
	for i, b := range blocks {
		switch b.Type {
		case "text":
			prompt = append(prompt, map[string]any{"type": "text", "text": b.Text})
		case "image":
			if len(images) >= maxPromptImages {
				return nil, nil, fmt.Errorf("prompt cannot contain more than %d images", maxPromptImages)
			}
			if b.MimeType == "" || len(b.MimeType) > 128 {
				return nil, nil, fmt.Errorf("image prompt block %d requires a mimeType", i)
			}
			data, err := base64.StdEncoding.DecodeString(b.Data)
			if err != nil || len(data) == 0 {
				return nil, nil, fmt.Errorf("image prompt block %d requires base64 data", i)
			}
			if len(data) > maxPromptImageBytes {
				return nil, nil, fmt.Errorf("image prompt block %d exceeds the %d byte libfx limit", i, maxPromptImageBytes)
			}
			total += len(data)
			if total > maxPromptImagesBytes {
				return nil, nil, fmt.Errorf("prompt images exceed the %d byte libfx limit", maxPromptImagesBytes)
			}
			images = append(images, data)
			prompt = append(prompt, map[string]any{"type": "image", "mimeType": b.MimeType})
		default:
			return nil, nil, fmt.Errorf("unsupported prompt block type: %s", b.Type)
		}
	}
	frame, _ := json.Marshal(map[string]any{"sessionId": "", "prompt": prompt})
	if len(frame)+total/3*4+256 > MaxPromptFrameBytes {
		return nil, nil, fmt.Errorf("prompt exceeds the %d byte libfx frame limit", MaxPromptFrameBytes)
	}
	return prompt, images, nil
}

func (t *Turn) finish(res TurnResult, err error) {
	a := t.agent
	t.mu.Lock()
	cancelled := t.cancelled
	t.mu.Unlock()
	if err != nil && cancelled {
		var rpcErr *RPCError
		if errors.As(err, &rpcErr) && rpcErr.Message == "Cancelled" {
			res, err = TurnResult{StopReason: "cancelled"}, nil
		}
	}
	if err == nil && cancelled {
		res.StopReason = "cancelled"
	}
	a.mu.Lock()
	if a.active == t {
		a.active = nil
	}
	a.mu.Unlock()
	a.engine.CloseSteering()
	t.mu.Lock()
	t.finished = true
	t.terminal = err
	t.result, t.err = res, err
	t.controllers = map[int]context.CancelFunc{}
	t.signalLocked()
	t.mu.Unlock()
	close(t.done)
}

// Done is closed when the turn's result is known.
func (t *Turn) Done() <-chan struct{} { return t.done }

// Wait returns the turn's result once it is known.
func (t *Turn) Wait(ctx context.Context) (TurnResult, error) {
	select {
	case <-t.done:
		return t.result, t.err
	case <-ctx.Done():
		return TurnResult{}, ctx.Err()
	}
}

// Steer queues text for the running turn; the core takes it at its next
// boundary (fx-sdk.js turn.steer with the web core's steering queue).
func (t *Turn) Steer(text string) (string, error) {
	if text == "" {
		return "", errors.New("steering text cannot be empty")
	}
	if !utf8.ValidString(text) {
		return "", errors.New("steering text must be valid UTF-8")
	}
	if len(text) > maxSteeringMessageBytes {
		return "", fmt.Errorf("steering text exceeds the %d byte libfx limit", maxSteeringMessageBytes)
	}
	a := t.agent
	a.mu.Lock()
	closing := a.closing
	a.mu.Unlock()
	t.mu.Lock()
	running := !t.finished && !t.cancelled
	t.mu.Unlock()
	if !running || !a.currentTurn(t) {
		return "", errors.New("no prompt is running")
	}
	if closing {
		return "", errors.New("fx agent is closing")
	}
	id := "in_" + randomID()
	if err := a.engine.Steer(id, text); err != nil {
		return "", err
	}
	return id, nil
}

// Cancel cancels the turn: steering closes, the core is told, and every
// in-flight host effect is aborted (fx-sdk.js turn.cancel).
func (t *Turn) Cancel() {
	t.mu.Lock()
	if t.finished || t.cancelled {
		t.mu.Unlock()
		return
	}
	t.cancelled = true
	controllers := t.controllers
	t.controllers = map[int]context.CancelFunc{}
	t.signalLocked()
	t.mu.Unlock()
	a := t.agent
	a.engine.CloseSteering()
	a.mu.Lock()
	closing := a.closing
	sessionID := a.sessionID
	a.mu.Unlock()
	if !closing {
		_ = a.engine.Notify("session/cancel", map[string]any{"sessionId": sessionID})
	}
	for _, c := range controllers {
		c()
	}
	a.engine.AbortHostEffects()
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
