package fx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Real-core session tests: ports of TS fx-session-native.test.ts (Gateway
// cases) run on fx-core.wasm. They skip when the pinned module is not
// cached; GOAI_FX_WASM_TEST=1 downloads it.

func bashEffects(sb *fakeSandbox, effects *[]string, mu *sync.Mutex, match func(string) bool, stdout string) {
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "realpath") {
			return providerutils.SandboxRunResult{Stdout: "L3dvcmtzcGFjZQA="}, nil // "/workspace\x00"
		}
		if match(opts.Command) {
			mu.Lock()
			*effects = append(*effects, opts.Command)
			mu.Unlock()
			return providerutils.SandboxRunResult{Stdout: stdout}, nil
		}
		return providerutils.SandboxRunResult{}, nil
	}
}

// TestLibfxRealCoreReplaysCompletedEffectWithoutRerunning mirrors TS
// "replays a completed sandbox effect through a new native runtime without
// executing it twice".
func TestLibfxRealCoreReplaysCompletedEffectWithoutRerunning(t *testing.T) {
	gw := &gatewayServer{t: t, fixtures: []string{"gateway-0.sse.txt"}}
	h := newRealHarness(t, gw, Settings{})
	sb := newFakeSandbox()
	var mu sync.Mutex
	var effects []string
	bashEffects(sb, &effects, &mu, func(c string) bool { return c == "printf LIBFX_FIXTURE" }, "LIBFX_FIXTURE")
	ctx := context.Background()
	start := startOpts(sb, "native-replay")
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	hasResult := newSignal()
	var suspended chan *harness.ContinueTurnState
	suspended = make(chan *harness.ContinueTurnState, 1)
	before := &partLog{}
	before.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeToolResult {
			hasResult.fire()
			go func() {
				st, err := session.DoSuspendTurn(ctx)
				if err != nil {
					t.Errorf("suspend: %v", err)
				}
				suspended <- st
			}()
		}
	}
	opts := harness.TurnSettings{Model: "anthropic/claude-haiku-4-5"}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Use bash to run exactly printf LIBFX_FIXTURE. Then reply with READY."), Emit: before.emit})
	if err != nil {
		t.Fatal(err)
	}
	hasResult.wait(t)
	continueFrom := <-suspended
	_ = waitDone(t, control)
	mu.Lock()
	if len(effects) != 1 {
		t.Fatalf("effects = %v", effects)
	}
	mu.Unlock()
	if n := gw.postCount(); n != 1 {
		t.Fatalf("posts = %d", n)
	}
	calls := before.ofType(harness.PartTypeToolCall)
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d", len(calls))
	}
	want := harness.ToolCallPart{ToolCallID: "toolu_01KswTwrpijqi88ffraLQVsY", ToolName: "bash", Input: `{"command":"printf LIBFX_FIXTURE"}`, ProviderExecuted: true, NativeName: "bash"}
	if got := *calls[0].(*harness.ToolCallPart); !reflect.DeepEqual(got, want) {
		t.Errorf("tool-call = %+v", got)
	}
	results := before.ofType(harness.PartTypeToolResult)
	if r := results[0].(*harness.ToolResultPart); r.IsError || !strings.Contains(mustJSON(r.Result), `"stdout":"LIBFX_FIXTURE"`) {
		t.Errorf("tool-result = %+v", r)
	}

	gw.setFixtures("gateway-1.sse.txt")
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	after := &partLog{}
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued turn: %v", err)
	}
	mu.Lock()
	if len(effects) != 1 {
		t.Errorf("effect ran again: %v", effects)
	}
	mu.Unlock()
	if n := gw.postCount(); n != 2 {
		t.Errorf("posts = %d, want 2", n)
	}
	if n := len(after.ofType(harness.PartTypeToolCall)) + len(after.ofType(harness.PartTypeToolResult)); n != 0 {
		t.Errorf("continuation re-emitted %d tool parts", n)
	}
	if got := after.text(); got != "READY" {
		t.Errorf("continued text = %q", got)
	}
	if len(after.ofType(harness.PartTypeFinish)) != 1 {
		t.Errorf("no finish part: %+v", after.snapshot())
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestLibfxRealCoreSteeringPersistsAcrossColdContinuation mirrors TS
// "persists native steering and reapplies it before the next request on
// cold continuation".
func TestLibfxRealCoreSteeringPersistsAcrossColdContinuation(t *testing.T) {
	gw := &gatewayServer{t: t, fixtures: []string{"gateway-0.sse.txt"}}
	h := newRealHarness(t, gw, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	start := startOpts(sb, "steered-replay")
	opts := harness.TurnSettings{Model: "anthropic/claude-haiku-4-5", Tools: []harness.ToolSpec{{Name: "bash"}}}
	session, err := h.DoStart(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	waiting := newSignal()
	log := &partLog{on: func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeFinishStep {
			waiting.fire()
		}
	}}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Use bash to run exactly printf LIBFX_FIXTURE. Then reply with READY."), Emit: log.emit})
	if err != nil {
		t.Fatal(err)
	}
	waiting.wait(t)
	if err := control.(harness.UserMessageSubmitter).SubmitUserMessage(ctx, "After the tool, reply STEERED."); err != nil {
		t.Fatal(err)
	}
	continueFrom, err := session.DoSuspendTurn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitDone(t, control)
	if _, stored := sb.stateFile(t); !strings.Contains(stored, "After the tool, reply STEERED.") {
		t.Fatalf("state does not hold the steer: %s", stored)
	}
	gw.setFixtures("gateway-1.sse.txt")
	resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.DoDestroy(ctx) //nolint:errcheck
	continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	if err := continued.SubmitToolResult(ctx, harness.ToolResultSubmission{ToolCallID: "toolu_01KswTwrpijqi88ffraLQVsY", Output: "LIBFX_FIXTURE"}); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, continued); err != nil {
		t.Fatalf("continued: %v", err)
	}
	if !strings.Contains(gw.body(1), "After the tool, reply STEERED.") {
		t.Errorf("continued request lacks the steer: %.400s", gw.body(1))
	}
	if n := gw.postCount(); n != 2 {
		t.Errorf("posts = %d", n)
	}
}

// TestLibfxRealCoreNewPromptAfterAbort mirrors TS "accepts a new prompt on
// the same session after a caller abort".
func TestLibfxRealCoreNewPromptAfterAbort(t *testing.T) {
	gw := &gatewayServer{t: t}
	gw.respond = func(req *http.Request, _ string, _ int) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(readFixture(t, "gateway-1.sse.txt"))), Request: req}, nil
	}
	h := newRealHarness(t, gw, Settings{})
	sb := newFakeSandbox()
	ctx := context.Background()
	session, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: "abort", SessionWorkDir: "/workspace", BuiltinToolFiltering: &harness.BuiltinToolFiltering{Mode: harness.BuiltinToolFilteringAllow}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	reason := &abortReason{}
	turnCtx, cancel := context.WithCancelCause(ctx)
	first := &partLog{}
	first.on = func(p harness.StreamPart) {
		if p.PartType() == harness.PartTypeTextDelta {
			cancel(reason)
		}
	}
	opts := harness.TurnSettings{Model: "anthropic/claude-haiku-4-5"}
	control, err := session.DoPromptTurn(turnCtx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Reply READY."), Emit: first.emit})
	if err != nil {
		t.Fatal(err)
	}
	err = waitDone(t, control)
	if err != reason {
		t.Fatalf("first turn err = %v, want the abort reason", err)
	}
	if errs := first.ofType(harness.PartTypeError); len(errs) != 1 || errs[0].(*harness.ErrorPart).Error != reason {
		t.Errorf("error parts = %+v", errs)
	}
	if len(first.ofType(harness.PartTypeFinish)) != 0 {
		t.Error("aborted turn emitted finish")
	}
	second := &partLog{}
	control2, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Reply READY again."), Emit: second.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, control2); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if got := second.text(); got != "READY" {
		t.Errorf("second text = %q", got)
	}
	if len(second.ofType(harness.PartTypeError)) != 0 {
		t.Errorf("second turn errored: %+v", second.snapshot())
	}
	if n := gw.postCount(); n != 2 {
		t.Errorf("posts = %d", n)
	}
}

type abortReason struct{}

func (*abortReason) Error() string { return "Stop the first turn" }

// TestLibfxRealCoreCredentialBrokering checks that the Gateway credential
// never reaches fx-core: the core gets and sends a placeholder, the network
// gets the real key on Gateway requests only, and a response that echoes
// the key is redacted before the core or the state file sees it.
func TestLibfxRealCoreCredentialBrokering(t *testing.T) {
	const secret = "real-gateway-secret-0123456789"
	echo := strings.Replace(readFixture(t, "gateway-1.sse.txt"), `"delta":"READY"`, `"delta":"READY `+secret+`"`, 1)
	if !strings.Contains(echo, secret) {
		t.Fatal("fixture edit failed")
	}
	gw := &gatewayServer{t: t}
	gw.respond = func(req *http.Request, _ string, index int) (*http.Response, error) {
		body := readFixture(t, "gateway-0.sse.txt")
		if index == 1 {
			body = echo
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	h := newRealHarness(t, gw, Settings{Auth: harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": secret})})
	var mu sync.Mutex
	var guestKeys []string
	var coreHeaders []http.Header
	var coreBodies []string
	inner := h.newEngine
	h.newEngine = func(ctx context.Context, o runtimeOptions) (fxEngine, error) {
		mu.Lock()
		guestKeys = append(guestKeys, o.APIKey)
		mu.Unlock()
		fetch := o.Fetch
		o.Fetch = func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			coreHeaders = append(coreHeaders, req.Header.Clone())
			mu.Unlock()
			resp, err := fetch(req)
			if err != nil || resp.Body == nil {
				return resp, err
			}
			data, rerr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			mu.Lock()
			coreBodies = append(coreBodies, string(data))
			mu.Unlock()
			resp.Body = io.NopCloser(strings.NewReader(string(data)))
			return resp, rerr
		}
		return inner(ctx, o)
	}
	sb := newFakeSandbox()
	bashEffects(sb, new([]string), &sync.Mutex{}, func(string) bool { return false }, "")
	ctx := context.Background()
	session, err := h.DoStart(ctx, startOpts(sb, "brokering"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.DoDestroy(ctx) //nolint:errcheck
	log := &partLog{}
	control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Model: "anthropic/claude-haiku-4-5"}, Prompt: harness.TextPrompt("go"), Emit: log.emit})
	if err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, control); err != nil {
		t.Fatalf("turn: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(guestKeys) != 1 || guestKeys[0] == "" || strings.Contains(guestKeys[0], secret) {
		t.Fatalf("the core was given %q", guestKeys)
	}
	placeholder := guestKeys[0]
	sawPlaceholder := false
	for _, hdr := range coreHeaders {
		for name, values := range hdr {
			for _, v := range values {
				if strings.Contains(v, secret) {
					t.Errorf("core request header %s carries the real key", name)
				}
				if strings.Contains(v, placeholder) {
					sawPlaceholder = true
				}
			}
		}
	}
	if !sawPlaceholder {
		t.Errorf("the core never sent the placeholder credential: %v", coreHeaders)
	}
	for _, b := range coreBodies {
		if strings.Contains(b, secret) {
			t.Error("a response body reached the core with the real key")
		}
	}
	gw.mu.Lock()
	for _, req := range gw.posts {
		if got := req.Header.Get("Authorization"); got != "Bearer "+secret {
			t.Errorf("network Authorization = %q", got)
		}
		if strings.Contains(strings.Join(req.Header.Values("Authorization"), ""), placeholder) {
			t.Error("the placeholder reached the network")
		}
		if req.Header.Get("x-client-app") != clientApp || req.Header.Get("user-agent") != clientApp {
			t.Errorf("client app headers = %q / %q", req.Header.Get("x-client-app"), req.Header.Get("user-agent"))
		}
	}
	gw.mu.Unlock()
	if strings.Contains(sb.allFiles(), secret) {
		t.Error("the sandbox state holds the real key")
	}
	if strings.Contains(log.text(), secret) {
		t.Error("the stream carried the real key")
	}
}
