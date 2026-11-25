package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Twins of workflow-agent-tool-approval-config.test.ts and
// workflow-agent-tool-metadata-approval.test.ts: the TS tests run the same
// scripted conversation through ToolLoopAgent (core) and WorkflowAgent in
// generate and stream mode and compare. Go compares agent.ToolLoopAgent with
// WorkflowAgent the same way.

type port2carryResponse struct {
	text  string
	calls []types.ToolCall
}

// port2carryModel answers its scripted responses in order, for generate and
// stream; a call past the script fails the run.
type port2carryModel struct {
	mu        sync.Mutex
	responses []port2carryResponse
	reqs      []*provider.GenerateOptions
}

func (m *port2carryModel) SpecificationVersion() string   { return "v3" }
func (m *port2carryModel) Provider() string               { return "mock" }
func (m *port2carryModel) ModelID() string                { return "mock-model" }
func (m *port2carryModel) SupportsTools() bool            { return true }
func (m *port2carryModel) SupportsStructuredOutput() bool { return true }
func (m *port2carryModel) SupportsImageInput() bool       { return false }

func (m *port2carryModel) next(o *provider.GenerateOptions) (port2carryResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, o)
	if len(m.reqs) > len(m.responses) {
		return port2carryResponse{}, errors.New("unexpected model call")
	}
	return m.responses[len(m.reqs)-1], nil
}

func (m *port2carryModel) DoGenerate(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	r, err := m.next(o)
	if err != nil {
		return nil, err
	}
	fr := types.FinishReasonStop
	if len(r.calls) > 0 {
		fr = types.FinishReasonToolCalls
	}
	return &types.GenerateResult{Text: r.text, ToolCalls: r.calls, FinishReason: fr}, nil
}

func (m *port2carryModel) DoStream(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	r, err := m.next(o)
	if err != nil {
		return nil, err
	}
	var chunks []provider.StreamChunk
	if r.text != "" {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, Text: r.text})
	}
	for i := range r.calls {
		c := r.calls[i]
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &c})
	}
	fr := types.FinishReasonStop
	if len(r.calls) > 0 {
		fr = types.FinishReasonToolCalls
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: fr})
	return testutil.NewMockTextStream(chunks), nil
}

func port2carryActionCall() types.ToolCall {
	return types.ToolCall{ID: "call-1", ToolName: "action", Arguments: map[string]interface{}{"value": "requested"}}
}

func port2carryActionResponse() port2carryResponse {
	return port2carryResponse{text: "Checking the action.", calls: []types.ToolCall{port2carryActionCall()}}
}

func port2carryDone() port2carryResponse { return port2carryResponse{text: "Done."} }

var port2carryValueSchema = map[string]interface{}{
	"type": "object", "required": []interface{}{"value"},
	"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
}

// port2carryCounter counts executions of tools.
type port2carryCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *port2carryCounter) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[name]
}

func (c *port2carryCounter) reset() {
	c.mu.Lock()
	c.n = map[string]int{}
	c.mu.Unlock()
}

func (c *port2carryCounter) tool(name string, needsApproval interface{}) types.Tool {
	return types.Tool{
		Name: name, Description: name, Parameters: port2carryValueSchema, ToolApproval: needsApproval,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			c.mu.Lock()
			if c.n == nil {
				c.n = map[string]int{}
			}
			c.n[name]++
			c.mu.Unlock()
			return "executed", nil
		},
	}
}

func port2carryPrompt() []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Perform the action."}}}}
}

func port2carryApprovalResponse(approvalID string, approved bool) types.Message {
	return types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: approvalID, Approved: approved}}}
}

// port2carryAgentSpec is what both agents are built from.
type port2carryAgentSpec struct {
	tools          []types.Tool
	toolApproval   types.ToolApprovalConfig
	runtimeContext interface{}
	// prepareRole, when set, makes prepareStep replace the runtime context
	// {"role": prepareRole} and record the contexts it saw.
	prepareRole  string
	seenContexts *[]interface{}
	secret       []byte
	instructions string
	prepareMsgs  func(step int) []types.Message
}

type port2carryRun struct {
	steps    []types.StepResult
	messages []types.Message
	model    *port2carryModel
}

func (r port2carryRun) approvalRequests() []types.ToolApprovalRequestContent {
	var out []types.ToolApprovalRequestContent
	for _, s := range r.steps {
		for _, p := range s.Content {
			switch v := p.(type) {
			case types.ToolApprovalRequestContent:
				out = append(out, v)
			case *types.ToolApprovalRequestContent:
				out = append(out, *v)
			}
		}
	}
	return out
}

func (r port2carryRun) contentPartTypes(sorted bool) [][]string {
	var out [][]string
	for _, s := range r.steps {
		var names []string
		for _, p := range s.Content {
			names = append(names, port2carryPartType(p))
		}
		if sorted {
			sort.Strings(names)
		}
		out = append(out, names)
	}
	return out
}

func port2carryPartType(p types.ContentPart) string {
	t := reflect.TypeOf(p)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

// toolMessagesOfSecondCall are the tool-role messages the model got on its
// second call.
func (r port2carryRun) toolMessagesOfSecondCall() string {
	if len(r.model.reqs) < 2 {
		return "<no second call>"
	}
	var tools []types.Message
	for _, m := range r.model.reqs[1].Prompt.Messages {
		if m.Role == types.RoleTool {
			tools = append(tools, m)
		}
	}
	b, _ := json.Marshal(tools)
	return string(b)
}

// comparable is the generated messages (the last limit) as JSON with approval
// ids normalized.
func (r port2carryRun) comparable(limit int) string {
	msgs := r.messages
	if limit >= 0 && len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	b, _ := json.Marshal(msgs)
	text := string(b)
	for i, req := range r.approvalRequests() {
		text = strings.ReplaceAll(text, req.ApprovalID, "approval-"+string(rune('0'+i)))
	}
	return text
}

func port2carryCore(t *testing.T, mode string, spec port2carryAgentSpec, responses []port2carryResponse, messages []types.Message, callApproval types.ToolApprovalConfig) port2carryRun {
	t.Helper()
	model := &port2carryModel{responses: responses}
	cfg := agent.AgentConfig{Model: model, Tools: spec.tools, ToolApproval: spec.toolApproval, RuntimeContext: spec.runtimeContext, System: spec.instructions}
	if spec.prepareRole != "" {
		cfg.PrepareStep = func(_ context.Context, o ai.PrepareStepOptions) ai.PrepareStepOptions {
			*spec.seenContexts = append(*spec.seenContexts, o.RuntimeContext)
			o.RuntimeContext = map[string]interface{}{"role": spec.prepareRole}
			return o
		}
	}
	if spec.prepareMsgs != nil {
		cfg.PrepareStep = func(_ context.Context, o ai.PrepareStepOptions) ai.PrepareStepOptions {
			if m := spec.prepareMsgs(o.StepNumber); m != nil {
				o.Messages = m
			}
			return o
		}
	}
	a := agent.NewToolLoopAgent(cfg)
	call := agent.AgentGenerateOptions{Messages: messages, ToolApproval: callApproval}
	if mode == "generate" {
		res, err := a.Generate(context.Background(), call)
		if err != nil {
			t.Fatalf("core generate: %v", err)
		}
		return port2carryRun{steps: res.Steps, messages: res.ResponseMessages, model: model}
	}
	res, err := a.Stream(context.Background(), agent.AgentStreamOptions{AgentGenerateOptions: call})
	if err != nil {
		t.Fatalf("core stream: %v", err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatalf("core stream: %v", err)
	}
	return port2carryRun{steps: res.Steps(), messages: res.ResponseMessages(), model: model}
}

func (spec port2carryAgentSpec) workflow(model provider.LanguageModel) WorkflowAgent {
	w := WorkflowAgent{Model: model, Tools: spec.tools, ToolApproval: spec.toolApproval, RuntimeContext: spec.runtimeContext, ExperimentalToolApprovalSecret: spec.secret}
	if spec.instructions != "" {
		w.Instructions = spec.instructions
	}
	if spec.prepareRole != "" {
		w.PrepareStep = func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
			*spec.seenContexts = append(*spec.seenContexts, o.RuntimeContext)
			o.RuntimeContext = map[string]interface{}{"role": spec.prepareRole}
			return o, nil
		}
	}
	if spec.prepareMsgs != nil {
		w.PrepareStep = func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
			if m := spec.prepareMsgs(o.StepNumber); m != nil {
				// TS prepareStep may return system messages in messages; no
				// AllowSystemInMessages opt-in is needed (core accepts them
				// in PrepareStep output, as TS convertToLanguageModelPrompt).
				o.Messages = m
			}
			return o, nil
		}
	}
	return w
}

func port2carryWorkflowRun(t *testing.T, mode string, spec port2carryAgentSpec, responses []port2carryResponse, messages []types.Message, callApproval types.ToolApprovalConfig, onChunk func(provider.StreamChunk)) port2carryRun {
	t.Helper()
	model := &port2carryModel{responses: responses}
	cfg := spec.workflow(model)
	a, err := NewWorkflowAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "generate" {
		res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: messages, ToolApproval: callApproval})
		if err != nil {
			t.Fatalf("workflow generate: %v", err)
		}
		return port2carryRun{steps: res.Steps, messages: res.ResponseMessages, model: model}
	}
	res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: messages, ToolApproval: callApproval, OnChunk: onChunk})
	if err != nil {
		t.Fatalf("workflow stream: %v", err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatalf("workflow stream: %v", err)
	}
	return port2carryRun{steps: res.Steps(), messages: res.ResponseMessages(), model: model}
}

func port2carryModes(t *testing.T, f func(t *testing.T, mode string)) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) { f(t, mode) })
	}
}

func port2carryExpectSameGenerated(t *testing.T, core, workflow port2carryRun) {
	t.Helper()
	if w, c := workflow.comparable(len(core.messages)), core.comparable(-1); w != c {
		t.Fatalf("generated messages differ\nworkflow: %s\ncore:     %s", w, c)
	}
}

func port2carryExpectRequest(t *testing.T, run port2carryRun) types.ToolApprovalRequestContent {
	t.Helper()
	reqs := run.approvalRequests()
	if len(reqs) != 1 || reqs[0].ToolCall.ID != "call-1" || reqs[0].ApprovalID == "" {
		t.Fatalf("approval requests = %+v, want one for call-1", reqs)
	}
	return reqs[0]
}

func port2carryPerToolFunc(status types.ToolApprovalStatus) types.SingleToolApprovalFunc {
	return func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
		return types.ToolApprovalResult{Status: status}
	}
}

// TS 'requests approval for a tool without needsApproval using %s'.
func TestPort2carryApprovalConfigRequestsApproval(t *testing.T) {
	configs := map[string]types.ToolApprovalConfig{
		"a per-tool status":   map[string]interface{}{"action": types.ToolApprovalStatusUserApproval},
		"a per-tool function": map[string]interface{}{"action": port2carryPerToolFunc(types.ToolApprovalStatusUserApproval)},
		"a generic function": types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		}),
	}
	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			port2carryModes(t, func(t *testing.T, mode string) {
				ctr := &port2carryCounter{}
				spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: cfg}
				core := port2carryCore(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil)
				ctr.reset()
				wf := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil, nil)
				if ctr.count("action") != 0 {
					t.Fatal("the tool ran before approval")
				}
				for _, run := range []port2carryRun{core, wf} {
					if len(run.model.reqs) != 1 || len(run.steps) != 1 {
						t.Fatalf("model calls = %d, steps = %d, want 1 and 1", len(run.model.reqs), len(run.steps))
					}
					port2carryExpectRequest(t, run)
				}
				port2carryExpectSameGenerated(t, core, wf)
			})
		})
	}
}

// issueAndResume runs the first turn on both agents, then resumes each with
// its own approval response for decision.
func port2carryIssueAndResume(t *testing.T, mode string, spec port2carryAgentSpec, ctr *port2carryCounter, approved bool) (core, wf port2carryRun, coreExec, wfExec int) {
	t.Helper()
	issuedCore := port2carryCore(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil)
	ctr.reset()
	issuedWF := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil, nil)
	port2carryExpectSameGenerated(t, issuedCore, issuedWF)
	if ctr.count("action") != 0 {
		t.Fatal("the tool ran before approval")
	}
	resume := func(issued port2carryRun) []types.Message {
		msgs := append(port2carryPrompt(), issued.messages...)
		return append(msgs, port2carryApprovalResponse(port2carryExpectRequest(t, issued).ApprovalID, approved))
	}
	ctr.reset()
	core = port2carryCore(t, mode, spec, []port2carryResponse{port2carryDone()}, resume(issuedCore), nil)
	coreExec = ctr.count("action")
	ctr.reset()
	wf = port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryDone()}, resume(issuedWF), nil, nil)
	wfExec = ctr.count("action")
	return core, wf, coreExec, wfExec
}

// TS 'runs an approved call and continues like ToolLoopAgent'.
func TestPort2carryApprovalConfigApprovedCallContinues(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusUserApproval}}
		core, wf, coreExec, wfExec := port2carryIssueAndResume(t, mode, spec, ctr, true)
		if coreExec != 1 || wfExec != 1 {
			t.Fatalf("executions core=%d workflow=%d, want 1 each", coreExec, wfExec)
		}
		port2carryExpectSameGenerated(t, core, wf)
		if !strings.Contains(wf.comparable(-1), `"executed"`) {
			t.Fatalf("workflow messages lack the tool output: %s", wf.comparable(-1))
		}
	})
}

// TS 'does not run a denied call'.
func TestPort2carryApprovalConfigDeniedCallNotRun(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusUserApproval}}
		core, wf, _, wfExec := port2carryIssueAndResume(t, mode, spec, ctr, false)
		if wfExec != 0 {
			t.Fatalf("a denied call ran %d times", wfExec)
		}
		port2carryExpectSameGenerated(t, core, wf)
		if strings.Contains(wf.comparable(-1), `"executed"`) {
			t.Fatalf("workflow messages carry a tool output: %s", wf.comparable(-1))
		}
	})
}

// TS 'applies the configuration over needsApproval: true'.
func TestPort2carryApprovalConfigOverridesNeedsApprovalTrue(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", true)}, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusNotApplicable}}
		responses := []port2carryResponse{port2carryActionResponse(), port2carryDone()}
		core := port2carryCore(t, mode, spec, responses, port2carryPrompt(), nil)
		if ctr.count("action") != 1 {
			t.Fatalf("core executions = %d", ctr.count("action"))
		}
		ctr.reset()
		wf := port2carryWorkflowRun(t, mode, spec, responses, port2carryPrompt(), nil, nil)
		if ctr.count("action") != 1 {
			t.Fatalf("workflow executions = %d", ctr.count("action"))
		}
		for _, run := range []port2carryRun{core, wf} {
			if len(run.model.reqs) != 2 || len(run.approvalRequests()) != 0 {
				t.Fatalf("model calls = %d, approval requests = %d, want 2 and 0", len(run.model.reqs), len(run.approvalRequests()))
			}
		}
		port2carryExpectSameGenerated(t, core, wf)
	})
}

// TS 'keeps needsApproval for tools without a configuration entry'.
func TestPort2carryApprovalConfigKeepsNeedsApprovalWithoutEntry(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		other := types.Tool{Name: "other", Parameters: port2carryValueSchema}
		spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", true), other}, toolApproval: map[string]interface{}{"other": types.ToolApprovalStatusNotApplicable}}
		core := port2carryCore(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil)
		if ctr.count("action") != 0 {
			t.Fatal("core ran the tool")
		}
		wf := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil, nil)
		if ctr.count("action") != 0 {
			t.Fatal("workflow ran the tool")
		}
		for _, run := range []port2carryRun{core, wf} {
			if len(run.model.reqs) != 1 {
				t.Fatalf("model calls = %d, want 1", len(run.model.reqs))
			}
			port2carryExpectRequest(t, run)
		}
		port2carryExpectSameGenerated(t, core, wf)
	})
}

// TS 'lets a per-call configuration override the constructor value'.
func TestPort2carryApprovalConfigPerCallOverridesConstructor(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		tools := []types.Tool{ctr.tool("action", true)}
		asks := port2carryWorkflowRun(t, mode,
			port2carryAgentSpec{tools: tools, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusNotApplicable}},
			[]port2carryResponse{port2carryActionResponse()}, port2carryPrompt(),
			map[string]interface{}{"action": types.ToolApprovalStatusUserApproval}, nil)
		if ctr.count("action") != 0 || len(asks.model.reqs) != 1 {
			t.Fatalf("executions = %d, model calls = %d, want 0 and 1", ctr.count("action"), len(asks.model.reqs))
		}
		port2carryExpectRequest(t, asks)

		runs := port2carryWorkflowRun(t, mode,
			port2carryAgentSpec{tools: tools, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusUserApproval}},
			[]port2carryResponse{port2carryActionResponse(), port2carryDone()}, port2carryPrompt(),
			map[string]interface{}{"action": types.ToolApprovalStatusNotApplicable}, nil)
		if ctr.count("action") != 1 || len(runs.model.reqs) != 2 || len(runs.approvalRequests()) != 0 {
			t.Fatalf("executions = %d, model calls = %d, requests = %d, want 1, 2, 0", ctr.count("action"), len(runs.model.reqs), len(runs.approvalRequests()))
		}
	})
}

// TS 'signs configuration approval requests and rejects tampered responses'.
func TestPort2carryApprovalConfigSignsRequestsAndRejectsTampered(t *testing.T) {
	secret := []byte("tool-approval-config-secret")
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{
			tools: []types.Tool{ctr.tool("action", nil)}, secret: secret,
			toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusUserApproval},
		}
		issued := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryActionResponse()}, port2carryPrompt(), nil, nil)
		req := port2carryExpectRequest(t, issued)
		if req.ApprovalID != "approval-call-1" || req.Signature == "" {
			t.Fatalf("request = %+v, want approval-call-1 with a signature", req)
		}
		if ctr.count("action") != 0 {
			t.Fatal("the tool ran before approval")
		}
		ok, err := ai.VerifyToolApprovalSignature(secret, req.Signature, req.ApprovalID, "call-1", "action", map[string]interface{}{"value": "requested"})
		if err != nil || !ok {
			t.Fatalf("signature verify = %v, %v", ok, err)
		}

		tampered := append([]types.Message(nil), issued.messages...)
		for i, m := range tampered {
			if m.Role != types.RoleAssistant {
				continue
			}
			parts := make([]types.ContentPart, len(m.Content))
			for j, p := range m.Content {
				if tc, ok := p.(types.ToolCallContent); ok {
					tc.Arguments = map[string]interface{}{"value": "tampered"}
					p = tc
				}
				parts[j] = p
			}
			tampered[i].Content = parts
		}
		msgs := append(append(port2carryPrompt(), tampered...), port2carryApprovalResponse("approval-call-1", true))
		// The tampered resume must not run the tool; whether the run itself
		// reports the failure or answers is not asserted (TS ignores it).
		model := &port2carryModel{responses: []port2carryResponse{port2carryDone()}}
		a, err := NewWorkflowAgent(spec.workflow(model))
		if err != nil {
			t.Fatal(err)
		}
		if mode == "generate" {
			_, _ = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: msgs})
		} else if res, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs}); err == nil {
			_ = res.ConsumeStream()
		}
		if ctr.count("action") != 0 {
			t.Fatal("a tampered approval response ran the tool")
		}
	})
}

// TS 'continues after a call denied by %s like ToolLoopAgent'.
func TestPort2carryApprovalAutomaticDenialContinues(t *testing.T) {
	reason := "policy"
	configs := map[string]types.ToolApprovalConfig{
		"the denied status": map[string]interface{}{"action": types.ToolApprovalStatusDenied},
		"the denied status with a reason": map[string]interface{}{"action": types.SingleToolApprovalFunc(func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
		})},
	}
	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			port2carryModes(t, func(t *testing.T, mode string) {
				ctr := &port2carryCounter{}
				spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: cfg}
				responses := []port2carryResponse{port2carryActionResponse(), port2carryDone()}
				core := port2carryCore(t, mode, spec, responses, port2carryPrompt(), nil)
				if ctr.count("action") != 0 || len(core.model.reqs) != 2 {
					t.Fatalf("core: executions = %d, model calls = %d", ctr.count("action"), len(core.model.reqs))
				}
				wf := port2carryWorkflowRun(t, mode, spec, responses, port2carryPrompt(), nil, nil)
				if ctr.count("action") != 0 || len(wf.model.reqs) != 2 {
					t.Fatalf("workflow: executions = %d, model calls = %d", ctr.count("action"), len(wf.model.reqs))
				}
				if !reflect.DeepEqual(wf.contentPartTypes(false), core.contentPartTypes(false)) {
					t.Fatalf("content part types differ: %v vs %v", wf.contentPartTypes(false), core.contentPartTypes(false))
				}
				if w, c := wf.toolMessagesOfSecondCall(), core.toolMessagesOfSecondCall(); w != c {
					t.Fatalf("tool messages of the second call differ\nworkflow: %s\ncore:     %s", w, c)
				}
			})
		})
	}
}

// TS 'runs a call approved by the configuration without pausing'.
func TestPort2carryApprovalConfigApprovedRunsWithoutPausing(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusApproved}}
		responses := []port2carryResponse{port2carryActionResponse(), port2carryDone()}
		core := port2carryCore(t, mode, spec, responses, port2carryPrompt(), nil)
		if ctr.count("action") != 1 || len(core.model.reqs) != 2 {
			t.Fatalf("core: executions = %d, model calls = %d", ctr.count("action"), len(core.model.reqs))
		}
		ctr.reset()
		wf := port2carryWorkflowRun(t, mode, spec, responses, port2carryPrompt(), nil, nil)
		if ctr.count("action") != 1 || len(wf.model.reqs) != 2 {
			t.Fatalf("workflow: executions = %d, model calls = %d", ctr.count("action"), len(wf.model.reqs))
		}
		if !reflect.DeepEqual(wf.contentPartTypes(false), core.contentPartTypes(false)) {
			t.Fatalf("content part types differ: %v vs %v", wf.contentPartTypes(false), core.contentPartTypes(false))
		}
		if w, c := wf.toolMessagesOfSecondCall(), core.toolMessagesOfSecondCall(); w != c {
			t.Fatalf("tool messages of the second call differ\nworkflow: %s\ncore:     %s", w, c)
		}
	})
}

// TS 'denies, asks, and runs calls in one step like ToolLoopAgent'.
func TestPort2carryApprovalDeniesAsksAndRunsInOneStep(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		spec := port2carryAgentSpec{
			tools: []types.Tool{ctr.tool("deniedAction", nil), ctr.tool("askedAction", nil), ctr.tool("plainAction", nil)},
			toolApproval: map[string]interface{}{
				"deniedAction": types.ToolApprovalStatusDenied, "askedAction": types.ToolApprovalStatusUserApproval,
			},
		}
		call := func(id, name, value string) types.ToolCall {
			return types.ToolCall{ID: id, ToolName: name, Arguments: map[string]interface{}{"value": value}}
		}
		responses := []port2carryResponse{{text: "Checking the actions.", calls: []types.ToolCall{
			call("call-denied", "deniedAction", "denied"), call("call-asked", "askedAction", "asked"), call("call-plain", "plainAction", "plain"),
		}}}
		core := port2carryCore(t, mode, spec, responses, port2carryPrompt(), nil)
		coreExec := [3]int{ctr.count("deniedAction"), ctr.count("askedAction"), ctr.count("plainAction")}
		ctr.reset()
		wf := port2carryWorkflowRun(t, mode, spec, responses, port2carryPrompt(), nil, nil)
		wfExec := [3]int{ctr.count("deniedAction"), ctr.count("askedAction"), ctr.count("plainAction")}
		if coreExec != [3]int{0, 0, 1} || wfExec != coreExec {
			t.Fatalf("executions core=%v workflow=%v, want [0 0 1] for both", coreExec, wfExec)
		}
		if !reflect.DeepEqual(wf.contentPartTypes(true), core.contentPartTypes(true)) {
			t.Fatalf("sorted content part types differ: %v vs %v", wf.contentPartTypes(true), core.contentPartTypes(true))
		}
		for name, run := range map[string]port2carryRun{"core": core, "workflow": wf} {
			if len(run.model.reqs) != 1 {
				t.Fatalf("%s model calls = %d, want 1", name, len(run.model.reqs))
			}
			type req struct {
				id        string
				automatic bool
			}
			var got []req
			for _, r := range run.approvalRequests() {
				got = append(got, req{r.ToolCall.ID, r.IsAutomatic})
			}
			if want := []req{{"call-denied", true}, {"call-asked", false}}; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s approval requests = %v, want %v", name, got, want)
			}
		}
	})
}

// port2carryRevoking is the spec of the prepareStep revocation cases: an
// admin may run the action; prepareStep downgrades the context before the
// step that requests the call.
func port2carryRevoking(ctr *port2carryCounter, revoked types.ToolApprovalStatus) (port2carryAgentSpec, *[]interface{}) {
	seen := &[]interface{}{}
	return port2carryAgentSpec{
		tools:          []types.Tool{ctr.tool("action", nil)},
		runtimeContext: map[string]interface{}{"role": "admin"},
		toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			if role, _ := o.RuntimeContext.(map[string]interface{})["role"].(string); role == "admin" {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
			}
			return types.ToolApprovalResult{Status: revoked}
		}),
		prepareRole:  "guest",
		seenContexts: seen,
	}, seen
}

// TS 'denies a call when prepareStep revokes the permission'.
func TestPort2carryApprovalPrepareStepRevokesDenied(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		responses := []port2carryResponse{port2carryActionResponse(), port2carryDone()}
		coreSpec, coreSeen := port2carryRevoking(ctr, types.ToolApprovalStatusDenied)
		core := port2carryCore(t, mode, coreSpec, responses, port2carryPrompt(), nil)
		if ctr.count("action") != 0 || len(core.model.reqs) != 2 {
			t.Fatalf("core: executions = %d, model calls = %d", ctr.count("action"), len(core.model.reqs))
		}
		ctr.reset()
		wfSpec, wfSeen := port2carryRevoking(ctr, types.ToolApprovalStatusDenied)
		wf := port2carryWorkflowRun(t, mode, wfSpec, responses, port2carryPrompt(), nil, nil)
		if ctr.count("action") != 0 || len(wf.model.reqs) != 2 {
			t.Fatalf("workflow: executions = %d, model calls = %d", ctr.count("action"), len(wf.model.reqs))
		}
		// the revoked context carries into the step after the denial
		want := []interface{}{map[string]interface{}{"role": "admin"}, map[string]interface{}{"role": "guest"}}
		if !reflect.DeepEqual(*coreSeen, want) || !reflect.DeepEqual(*wfSeen, *coreSeen) {
			t.Fatalf("prepareStep saw core=%v workflow=%v, want %v", *coreSeen, *wfSeen, want)
		}
		if !reflect.DeepEqual(wf.contentPartTypes(false), core.contentPartTypes(false)) {
			t.Fatalf("content part types differ: %v vs %v", wf.contentPartTypes(false), core.contentPartTypes(false))
		}
		if w, c := wf.toolMessagesOfSecondCall(), core.toolMessagesOfSecondCall(); w != c {
			t.Fatalf("tool messages of the second call differ\nworkflow: %s\ncore:     %s", w, c)
		}
		if strings.Contains(wf.comparable(-1), `"executed"`) {
			t.Fatal("workflow messages carry a tool output")
		}
	})
}

// TS 'requires approval when prepareStep revokes the permission'.
func TestPort2carryApprovalPrepareStepRevokesUserApproval(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		responses := []port2carryResponse{port2carryActionResponse()}
		coreSpec, coreSeen := port2carryRevoking(ctr, types.ToolApprovalStatusUserApproval)
		core := port2carryCore(t, mode, coreSpec, responses, port2carryPrompt(), nil)
		ctr.reset()
		wfSpec, wfSeen := port2carryRevoking(ctr, types.ToolApprovalStatusUserApproval)
		wf := port2carryWorkflowRun(t, mode, wfSpec, responses, port2carryPrompt(), nil, nil)
		if ctr.count("action") != 0 {
			t.Fatal("the tool ran")
		}
		for name, run := range map[string]port2carryRun{"core": core, "workflow": wf} {
			if len(run.model.reqs) != 1 {
				t.Fatalf("%s model calls = %d, want 1", name, len(run.model.reqs))
			}
			port2carryExpectRequest(t, run)
			for _, r := range run.approvalRequests() {
				if r.IsAutomatic {
					t.Fatalf("%s request is automatic", name)
				}
			}
		}
		if !reflect.DeepEqual(*wfSeen, *coreSeen) {
			t.Fatalf("prepareStep saw workflow=%v core=%v", *wfSeen, *coreSeen)
		}
		port2carryExpectSameGenerated(t, core, wf)
	})
}

// port2carryToolCallNames are the tool names of the assistant tool calls.
func port2carryToolCallNames(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, p := range m.Content {
			if tc, ok := p.(types.ToolCallContent); ok {
				out = append(out, tc.ToolName)
			}
		}
	}
	return out
}

// TS 'does not authorize action from a %s sibling'.
func TestPort2carryApprovalHistoryDoesNotAuthorizeFromSibling(t *testing.T) {
	for _, sibling := range []string{"alone", "denied", "pending", "invalid", "unknown", "reversed"} {
		t.Run(sibling, func(t *testing.T) {
			port2carryModes(t, func(t *testing.T, mode string) {
				ctr := &port2carryCounter{}
				var mu sync.Mutex
				var approvalMessages [][]types.Message
				action := port2carryActionCall()
				attestArgs := map[string]interface{}{"value": "attestation"}
				if sibling == "invalid" {
					attestArgs = map[string]interface{}{}
				}
				attest := types.ToolCall{ID: "attest-call", ToolName: "attest", Arguments: attestArgs}
				calls := []types.ToolCall{action, attest}
				switch sibling {
				case "alone":
					calls = []types.ToolCall{action}
				case "reversed":
					calls = []types.ToolCall{attest, action}
				}
				tools := []types.Tool{ctr.tool("action", nil)}
				if sibling != "unknown" {
					tools = append(tools, ctr.tool("attest", nil))
				}
				spec := port2carryAgentSpec{
					tools: tools,
					toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
						mu.Lock()
						approvalMessages = append(approvalMessages, o.Messages)
						mu.Unlock()
						if o.ToolCall.ToolName == "attest" {
							if sibling == "pending" {
								return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
							}
							return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
						}
						if names := port2carryToolCallNames(o.Messages); len(names) > 0 && names[len(names)-1] == "attest" {
							return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
						}
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
					}),
				}
				run := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{{text: "Checking the action.", calls: calls}, port2carryDone()}, port2carryPrompt(), nil, nil)
				if ctr.count("action") != 0 || ctr.count("attest") != 0 {
					t.Fatalf("executions action=%d attest=%d, want 0", ctr.count("action"), ctr.count("attest"))
				}
				if len(approvalMessages) == 0 {
					t.Fatal("the policy was not evaluated")
				}
				for _, msgs := range approvalMessages {
					if !reflect.DeepEqual(msgs, run.model.reqs[0].Prompt.Messages) {
						t.Fatalf("approval messages differ from the model prompt: %+v vs %+v", msgs, run.model.reqs[0].Prompt.Messages)
					}
				}
				found := false
				for _, r := range run.approvalRequests() {
					if r.ToolCall.ToolName == "action" {
						found = true
					}
				}
				if !found {
					t.Fatalf("no approval request for action: %+v", run.approvalRequests())
				}
				for _, s := range run.steps {
					if len(s.ToolResults) != 0 {
						t.Fatalf("step %d has tool results: %+v", s.StepNumber, s.ToolResults)
					}
				}
			})
		})
	}
}

// TS 'uses prepared pre-response history for %s, preserving execution context'.
func TestPort2carryApprovalUsesPreparedPreResponseHistory(t *testing.T) {
	history := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Perform the action."}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "prior", ToolName: "attest", Arguments: map[string]interface{}{"value": "attestation"}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "prior", ToolName: "attest", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "attested"}}}},
	}
	for _, configuration := range []string{"generic", "per-tool", "needsApproval"} {
		t.Run(configuration, func(t *testing.T) {
			port2carryModes(t, func(t *testing.T, mode string) {
				var mu sync.Mutex
				var approvalMessages [][]types.Message
				var executionToolNames [][]string
				hasAttestation := func(msgs []types.Message) bool {
					mu.Lock()
					approvalMessages = append(approvalMessages, msgs)
					mu.Unlock()
					names := port2carryToolCallNames(msgs)
					return len(names) > 0 && names[len(names)-1] == "attest"
				}
				action := types.Tool{
					Name: "action", Description: "action", Parameters: port2carryValueSchema,
					Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						executionToolNames = append(executionToolNames, port2carryToolCallNames(o.Messages))
						mu.Unlock()
						return "protected output", nil
					},
				}
				var approval types.ToolApprovalConfig
				switch configuration {
				case "generic":
					approval = types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
						if hasAttestation(o.Messages) {
							return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
						}
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
					})
				case "per-tool":
					approval = map[string]interface{}{"action": types.SingleToolApprovalFunc(func(_ map[string]interface{}, o types.SingleToolApprovalOptions) types.ToolApprovalResult {
						if hasAttestation(o.Messages) {
							return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
						}
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
					})}
				default:
					action.ToolApproval = types.ToolNeedsApprovalFunc(func(_ context.Context, _ map[string]interface{}, o types.ToolNeedsApprovalOptions) bool {
						return !hasAttestation(o.Messages)
					})
				}
				spec := port2carryAgentSpec{
					tools: []types.Tool{action}, toolApproval: approval, instructions: "Do not include instructions in approval messages.",
					prepareMsgs: func(step int) []types.Message {
						if step != 0 {
							return nil
						}
						return append([]types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Prepared instructions."}}}}, history...)
					},
				}
				port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryActionResponse(), port2carryDone()}, port2carryPrompt(), nil, nil)
				mu.Lock()
				defer mu.Unlock()
				if len(executionToolNames) != 1 || !reflect.DeepEqual(executionToolNames[0], []string{"attest"}) {
					t.Fatalf("tool execution saw tool calls %v, want [[attest]]", executionToolNames)
				}
				if len(approvalMessages) != 1 || !reflect.DeepEqual(approvalMessages[0], history) {
					t.Fatalf("approval messages = %+v, want the prepared history once", approvalMessages)
				}
			})
		})
	}
}

// TS 'offers a call and result that completed in an earlier step'.
func TestPort2carryApprovalHistoryAcrossSteps(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		var mu sync.Mutex
		var approvalMessages [][]types.Message
		var executedForToolNames []string
		actionExecutions := 0
		action := types.Tool{
			Name: "action", Description: "action", Parameters: port2carryValueSchema,
			Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
				mu.Lock()
				defer mu.Unlock()
				executedForToolNames = port2carryToolCallNames(o.Messages)
				actionExecutions++
				return "protected output", nil
			},
		}
		hasAttestResult := func(msgs []types.Message) bool {
			for _, m := range msgs {
				for _, p := range m.Content {
					if tr, ok := p.(types.ToolResultContent); ok && m.Role == types.RoleTool && tr.ToolName == "attest" {
						return true
					}
				}
			}
			return false
		}
		spec := port2carryAgentSpec{
			tools: []types.Tool{ctr.tool("attest", nil), action}, instructions: "Instructions are never approval messages.",
			toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				if o.ToolCall.ToolName != "action" {
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
				}
				mu.Lock()
				approvalMessages = append(approvalMessages, o.Messages)
				mu.Unlock()
				names := port2carryToolCallNames(o.Messages)
				if len(names) > 0 && names[len(names)-1] == "attest" && hasAttestResult(o.Messages) {
					return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
				}
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}),
		}
		responses := []port2carryResponse{
			{calls: []types.ToolCall{{ID: "attest-call", ToolName: "attest", Arguments: map[string]interface{}{"value": "attestation"}}}},
			port2carryActionResponse(),
			port2carryDone(),
		}
		run := port2carryWorkflowRun(t, mode, spec, responses, port2carryPrompt(), nil, nil)
		mu.Lock()
		defer mu.Unlock()
		if ctr.count("attest") != 1 || actionExecutions != 1 || !reflect.DeepEqual(executedForToolNames, []string{"attest"}) {
			t.Fatalf("attest executions = %d, action executions = %d, tool names at execution = %v", ctr.count("attest"), actionExecutions, executedForToolNames)
		}
		if len(approvalMessages) != 1 {
			t.Fatalf("policy evaluations for action = %d, want 1", len(approvalMessages))
		}
		msgs := approvalMessages[0]
		if names := port2carryToolCallNames(msgs); !reflect.DeepEqual(names, []string{"attest"}) {
			t.Fatalf("approval messages tool calls = %v, want [attest]", names)
		}
		var results []types.ToolResultContent
		for _, m := range msgs {
			for _, p := range m.Content {
				if tr, ok := p.(types.ToolResultContent); ok {
					results = append(results, tr)
				}
			}
		}
		if len(results) != 1 || results[0].ToolName != "attest" || results[0].Output == nil || results[0].Output.Value != "attested" && results[0].Output.Value != "executed" {
			t.Fatalf("approval messages tool results = %+v, want the attest result", results)
		}
		second := run.model.reqs[1].Prompt
		hasSystem := second.System != ""
		for _, m := range second.Messages {
			if m.Role == types.RoleSystem {
				hasSystem = true
			}
		}
		if !hasSystem {
			t.Fatal("the model did not receive the instructions")
		}
		for _, m := range msgs {
			if m.Role == types.RoleSystem {
				t.Fatal("the policy saw a system message")
			}
		}
		var nonSystem []types.Message
		for _, m := range second.Messages {
			if m.Role != types.RoleSystem {
				nonSystem = append(nonSystem, m)
			}
		}
		if !reflect.DeepEqual(msgs, nonSystem) {
			t.Fatalf("policy messages differ from the model input without system messages\npolicy: %+v\nmodel:  %+v", msgs, nonSystem)
		}
	})
}

// TS 'stream workflow agent automatic approval decisions > writes a denied
// tool output and no approval request for a denied call' (the writable is
// OnChunk here).
func TestPort2carryStreamDeniedCallWritesDeniedOutputNoRequest(t *testing.T) {
	ctr := &port2carryCounter{}
	var mu sync.Mutex
	var chunks []provider.StreamChunk
	spec := port2carryAgentSpec{tools: []types.Tool{ctr.tool("action", nil)}, toolApproval: map[string]interface{}{"action": types.ToolApprovalStatusDenied}}
	run := port2carryWorkflowRun(t, "stream", spec, []port2carryResponse{port2carryActionResponse(), port2carryDone()}, port2carryPrompt(), nil,
		func(c provider.StreamChunk) { mu.Lock(); chunks = append(chunks, c); mu.Unlock() })
	if len(run.steps) != 2 || ctr.count("action") != 0 {
		t.Fatalf("steps = %d, executions = %d, want 2 and 0", len(run.steps), ctr.count("action"))
	}
	mu.Lock()
	defer mu.Unlock()
	denied, requests := 0, 0
	for _, c := range chunks {
		switch {
		case c.Type == provider.ChunkTypeToolOutputDenied && c.ToolResult != nil && c.ToolResult.ToolCallID == "call-1":
			denied++
		case c.Type == provider.ChunkTypeToolApprovalRequest && c.ToolApprovalRequest != nil && c.ToolApprovalRequest.ToolCallID == "call-1":
			// Automatic requests are bookkeeping, not an approval UI
			// (TS writes only requests that need a user's answer).
			if !c.ToolApprovalRequest.IsAutomatic {
				requests++
			}
		}
	}
	if denied != 1 || requests != 0 {
		t.Fatalf("tool-output-denied chunks = %d, approval-request chunks = %d, want 1 and 0", denied, requests)
	}
}

// Twins of workflow-agent-tool-metadata-approval.test.ts.

var port2carryRecordsSchema = map[string]interface{}{
	"type": "object", "properties": map[string]interface{}{"table": map[string]interface{}{"type": "string"}},
}

type port2carryObserved struct {
	Metadata map[string]interface{}
	Dynamic  bool
	Title    string
	Input    map[string]interface{}
}

func port2carryObserve(c types.ToolCall) port2carryObserved {
	return port2carryObserved{Metadata: c.ToolMetadata, Dynamic: c.Dynamic, Title: c.Title, Input: c.Arguments}
}

func port2carryDeleteTool(ctr *port2carryCounter, dynamic bool, metadata map[string]interface{}, inputs *[]map[string]interface{}) types.Tool {
	t := types.Tool{
		Name: "deleteRecords", Description: "Delete records. Admin only.", Title: "Delete records", Metadata: metadata,
		Parameters: port2carryRecordsSchema,
		Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			ctr.mu.Lock()
			if ctr.n == nil {
				ctr.n = map[string]int{}
			}
			ctr.n["deleteRecords"]++
			ctr.mu.Unlock()
			if inputs != nil {
				*inputs = append(*inputs, in)
			}
			return "deleted", nil
		},
	}
	if dynamic {
		t.Type = types.ToolTypeDynamic
	}
	return t
}

func port2carryDeleteCall() port2carryResponse {
	return port2carryResponse{calls: []types.ToolCall{{ID: "call-1", ToolName: "deleteRecords", Arguments: map[string]interface{}{"table": "users"}}}}
}

func port2carryDeleteMessages() []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Delete the records."}}}}
}

var port2carryServerFields = port2carryObserved{
	Metadata: map[string]interface{}{"requiresAdmin": true}, Dynamic: true, Title: "Delete records", Input: map[string]interface{}{"table": "users"},
}

// TS 'does not run a protected dynamic tool that the policy marks as %s'.
func TestPort2carryMetadataPolicyBlocksProtectedDynamicTool(t *testing.T) {
	for _, status := range []types.ToolApprovalStatus{types.ToolApprovalStatusDenied, types.ToolApprovalStatusUserApproval} {
		t.Run(string(status), func(t *testing.T) {
			port2carryModes(t, func(t *testing.T, mode string) {
				ctr := &port2carryCounter{}
				var mu sync.Mutex
				var seen []port2carryObserved
				reason := "admin-only tool"
				spec := port2carryAgentSpec{
					tools: []types.Tool{port2carryDeleteTool(ctr, true, map[string]interface{}{"requiresAdmin": true}, nil)},
					toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
						mu.Lock()
						seen = append(seen, port2carryObserve(o.ToolCall))
						mu.Unlock()
						if o.ToolCall.ToolMetadata["requiresAdmin"] == true {
							return types.ToolApprovalResult{Status: status, Reason: &reason}
						}
						return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
					}),
				}
				run := port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryDeleteCall(), port2carryDone()}, port2carryDeleteMessages(), nil, nil)
				if ctr.count("deleteRecords") != 0 {
					t.Fatal("the protected tool ran")
				}
				if !reflect.DeepEqual(seen, []port2carryObserved{port2carryServerFields}) {
					t.Fatalf("policy saw %+v, want the server-defined fields once", seen)
				}
				reqs := run.approvalRequests()
				if len(reqs) != 1 || reqs[0].ToolCall.ID != "call-1" {
					t.Fatalf("approval requests = %+v, want one for call-1", reqs)
				}
			})
		})
	}
}

// TS 'passes the server-defined fields and the refined input to the policy'.
func TestPort2carryMetadataPolicySeesRefinedInput(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		var seen []port2carryObserved
		var executed []map[string]interface{}
		spec := port2carryAgentSpec{
			tools: []types.Tool{port2carryDeleteTool(ctr, true, map[string]interface{}{"requiresAdmin": true}, &executed)},
			toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				seen = append(seen, port2carryObserve(o.ToolCall))
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
			}),
		}
		model := &port2carryModel{responses: []port2carryResponse{port2carryDeleteCall(), port2carryDone()}}
		cfg := spec.workflow(model)
		cfg.ExperimentalRefineToolInput = map[string]ai.ToolInputRefiner{
			"deleteRecords": func(_ context.Context, o ai.ToolInputRefinementOptions) (map[string]interface{}, error) {
				out := map[string]interface{}{"dryRun": true}
				for k, v := range o.ToolCall.Arguments {
					out[k] = v
				}
				return out, nil
			},
		}
		a, err := NewWorkflowAgent(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "generate" {
			_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: port2carryDeleteMessages()})
		} else {
			var res *WorkflowStreamResult
			if res, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: port2carryDeleteMessages()}); err == nil {
				err = res.ConsumeStream()
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		want := port2carryServerFields
		want.Input = map[string]interface{}{"table": "users", "dryRun": true}
		if !reflect.DeepEqual(seen, []port2carryObserved{want}) {
			t.Fatalf("policy saw %+v, want %+v", seen, want)
		}
		if ctr.count("deleteRecords") != 1 || !reflect.DeepEqual(executed, []map[string]interface{}{want.Input}) {
			t.Fatalf("executions = %d with inputs %v, want one with the refined input", ctr.count("deleteRecords"), executed)
		}
	})
}

// TS 'passes the server-defined fields for a static tool without a runtime validator'.
func TestPort2carryMetadataPolicySeesStaticToolFields(t *testing.T) {
	port2carryModes(t, func(t *testing.T, mode string) {
		ctr := &port2carryCounter{}
		var seen []port2carryObserved
		spec := port2carryAgentSpec{
			tools: []types.Tool{port2carryDeleteTool(ctr, false, map[string]interface{}{"requiresAdmin": true}, nil)},
			toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
				seen = append(seen, port2carryObserve(o.ToolCall))
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
			}),
		}
		port2carryWorkflowRun(t, mode, spec, []port2carryResponse{port2carryDeleteCall(), port2carryDone()}, port2carryDeleteMessages(), nil, nil)
		want := port2carryServerFields
		want.Dynamic = false
		if !reflect.DeepEqual(seen, []port2carryObserved{want}) || ctr.count("deleteRecords") != 1 {
			t.Fatalf("policy saw %+v, executions = %d, want %+v and 1", seen, ctr.count("deleteRecords"), want)
		}
	})
}

// TS 'resumed approvals receive the server-defined tool fields > runs an
// approved dynamic call that the policy still requires approval for'.
func TestPort2carryMetadataResumedApprovalRunsDynamicCall(t *testing.T) {
	ctr := &port2carryCounter{}
	tools := []types.Tool{port2carryDeleteTool(ctr, true, map[string]interface{}{"requiresAdmin": true}, nil)}
	policy := func(seen *[]port2carryObserved) types.GenericToolApprovalFunc {
		return func(o types.ToolApprovalOptions) types.ToolApprovalResult {
			if seen != nil {
				*seen = append(*seen, port2carryObserve(o.ToolCall))
			}
			if o.ToolCall.Dynamic {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		}
	}
	issued := port2carryWorkflowRun(t, "generate", port2carryAgentSpec{tools: tools, toolApproval: policy(nil)},
		[]port2carryResponse{port2carryDeleteCall()}, port2carryDeleteMessages(), nil, nil)
	if ctr.count("deleteRecords") != 0 {
		t.Fatal("the tool ran before approval")
	}
	reqs := issued.approvalRequests()
	if len(reqs) == 0 {
		t.Fatal("missing tool approval request")
	}
	var seen []port2carryObserved
	msgs := append(append(port2carryDeleteMessages(), issued.messages...), port2carryApprovalResponse(reqs[0].ApprovalID, true))
	resumed := port2carryWorkflowRun(t, "generate", port2carryAgentSpec{tools: tools, toolApproval: policy(&seen)},
		[]port2carryResponse{port2carryDone()}, msgs, nil, nil)
	if len(seen) == 0 {
		t.Fatal("the policy was not evaluated on resume")
	}
	for _, o := range seen {
		if !reflect.DeepEqual(o, port2carryServerFields) {
			t.Fatalf("policy saw %+v on resume, want %+v", o, port2carryServerFields)
		}
	}
	if ctr.count("deleteRecords") != 1 || !strings.Contains(resumed.comparable(-1), `"deleted"`) {
		t.Fatalf("executions = %d, messages = %s", ctr.count("deleteRecords"), resumed.comparable(-1))
	}
}

// TS '... > does not run an approved call that the policy reports as not needing approval'.
func TestPort2carryMetadataResumedApprovalNotNeededIsRefused(t *testing.T) {
	ctr := &port2carryCounter{}
	tools := []types.Tool{port2carryDeleteTool(ctr, false, map[string]interface{}{"readOnlyHint": true}, nil)}
	issued := port2carryWorkflowRun(t, "generate", port2carryAgentSpec{tools: tools, toolApproval: types.GenericToolApprovalFunc(func(types.ToolApprovalOptions) types.ToolApprovalResult {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
	})}, []port2carryResponse{port2carryDeleteCall()}, port2carryDeleteMessages(), nil, nil)
	if ctr.count("deleteRecords") != 0 {
		t.Fatal("the tool ran before approval")
	}
	reqs := issued.approvalRequests()
	if len(reqs) == 0 {
		t.Fatal("missing tool approval request")
	}
	msgs := append(append(port2carryDeleteMessages(), issued.messages...), port2carryApprovalResponse(reqs[0].ApprovalID, true))
	resumed := port2carryWorkflowRun(t, "generate", port2carryAgentSpec{tools: tools, toolApproval: types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
		if o.ToolCall.ToolMetadata["readOnlyHint"] == true {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
	})}, []port2carryResponse{port2carryDone()}, msgs, nil, nil)
	if ctr.count("deleteRecords") != 0 {
		t.Fatal("an approval the policy no longer asks for ran the tool")
	}
	var results []types.ToolResultContent
	for _, m := range resumed.messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if tr, ok := p.(types.ToolResultContent); ok {
				results = append(results, tr)
			}
		}
	}
	if len(results) != 1 || results[0].ToolCallID != "call-1" || results[0].Output == nil || results[0].Output.Type != types.ToolResultOutputErrorText ||
		!strings.Contains(textOf(results[0].Output.Value), "does not require approval") {
		t.Fatalf("tool results = %+v, want one error-text containing 'does not require approval'", results)
	}
}
