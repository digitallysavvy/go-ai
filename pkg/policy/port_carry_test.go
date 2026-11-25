package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// funcClient adapts a function to PolicyClient.
type portcarryClient struct {
	mu    sync.Mutex
	fn    func(path string, input any) (any, error)
	calls []any
}

func (c *portcarryClient) Evaluate(_ context.Context, path string, input any) (any, error) {
	c.mu.Lock()
	c.calls = append(c.calls, input)
	c.mu.Unlock()
	return c.fn(path, input)
}

func (c *portcarryClient) messages(i int) []types.Message {
	return c.calls[i].(DefaultOPAInput).Messages
}

func portcarryMessages() []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "perform the requested actions"}}}}
}

func portcarryCall(id, name string, args map[string]interface{}) types.ToolCall {
	return types.ToolCall{ID: id, ToolName: name, Arguments: args}
}

func portcarryCharge(id string, amount float64) types.ToolCall {
	return portcarryCall(id, "charge", map[string]interface{}{"amount": amount})
}

func portcarryApprove(fn types.GenericToolApprovalFunc, call types.ToolCall, messages []types.Message) PolicyDecision {
	return decisionFromApproval(fn(types.ToolApprovalOptions{ToolCall: call, Messages: messages}))
}

// chargedAmount stands in for a Rego rule over input.messages: the sum of all
// charge tool calls in the conversation, optionally without denied ones.
func portcarryCharged(in DefaultOPAInput, skipDenied bool) float64 {
	denied := map[string]bool{}
	for _, m := range in.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if r, ok := p.(types.ToolResultContent); ok && r.Output != nil && r.Output.Type == types.ToolResultOutputExecutionDenied {
				denied[r.ToolCallID] = true
			}
		}
	}
	total := 0.0
	for _, m := range in.Messages {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, p := range m.Content {
			if c, ok := p.(types.ToolCallContent); ok && c.ToolName == "charge" && (!skipDenied || !denied[c.ToolCallID]) {
				total += c.Arguments["amount"].(float64)
			}
		}
	}
	return total
}

func portcarryBudget(ceiling float64, skipDenied bool) *portcarryClient {
	return &portcarryClient{fn: func(_ string, input any) (any, error) {
		in := input.(DefaultOPAInput)
		amount := in.Args.(map[string]interface{})["amount"].(float64)
		if portcarryCharged(in, skipDenied)+amount <= ceiling {
			return map[string]any{"decision": "allow"}, nil
		}
		return map[string]any{"decision": "deny", "reason": "conversation budget exceeded"}, nil
	}}
}

func portcarryStatus(d PolicyDecision) string { return string(d.Type) + "/" + d.Reason }

func TestPortcarryOPAEvaluatesLaterCallWithEarlierCallsAppended(t *testing.T) {
	c := &portcarryClient{fn: func(string, any) (any, error) { return map[string]any{"decision": "allow"}, nil }}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	if len(c.calls) != 3 {
		t.Fatalf("evaluations = %d", len(c.calls))
	}
	if len(c.messages(0)) != 1 || len(c.messages(1)) != 1 {
		t.Fatal("first evaluations must see the messages as passed")
	}
	got := c.messages(2)
	if len(got) != 2 || got[1].Role != types.RoleAssistant {
		t.Fatalf("second evaluation messages = %+v", got)
	}
	tc := got[1].Content[0].(types.ToolCallContent)
	if tc.ToolCallID != "call-1" || tc.ToolName != "charge" || tc.Arguments["amount"] != 60.0 {
		t.Fatalf("appended call = %+v", tc)
	}
}

func TestPortcarryOPADeniesLimitExceededWithEarlierCalls(t *testing.T) {
	approval := OPAPolicy(portcarryBudget(100, false), OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	a := portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	b := portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	if portcarryStatus(a) != "approved/" || portcarryStatus(b) != "denied/conversation budget exceeded" {
		t.Fatalf("decisions %v %v", a, b)
	}
}

func TestPortcarryOPAAppendsExecutionDeniedForDeniedEarlierCalls(t *testing.T) {
	c := portcarryBudget(100, true)
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	d1 := portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	d2 := portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	d3 := portcarryApprove(approval, portcarryCharge("call-3", 30), msgs)
	if d1.Type != "approved" || d2.Type != "denied" || d3.Type != "approved" {
		t.Fatalf("decisions %v %v %v", d1, d2, d3)
	}
	last := c.messages(len(c.calls) - 1)
	if len(last) != 3 || last[2].Role != types.RoleTool {
		t.Fatalf("last evaluation messages = %+v", last)
	}
	res := last[2].Content[0].(types.ToolResultContent)
	if res.ToolCallID != "call-2" || res.Output.Type != types.ToolResultOutputExecutionDenied || res.Output.Reason != "conversation budget exceeded" {
		t.Fatalf("denied result = %+v", res)
	}
}

func TestPortcarryOPAEarlierCallThatHasNotRunDoesNotSatisfyRule(t *testing.T) {
	c := &portcarryClient{fn: func(_ string, input any) (any, error) {
		in := input.(DefaultOPAInput)
		attested := false
		for _, m := range in.Messages {
			for _, p := range m.Content {
				if tc, ok := p.(types.ToolCallContent); ok && m.Role == types.RoleAssistant && tc.ToolName == "attest" {
					attested = true
				}
			}
		}
		if in.Tool["name"] == "attest" || attested {
			return map[string]any{"decision": "allow"}, nil
		}
		return map[string]any{"decision": "deny", "reason": "attest first"}, nil
	}}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	a := portcarryApprove(approval, portcarryCall("call-1", "attest", map[string]interface{}{}), msgs)
	b := portcarryApprove(approval, portcarryCall("call-2", "protectedAction", map[string]interface{}{}), msgs)
	if portcarryStatus(a) != "approved/" || portcarryStatus(b) != "denied/attest first" {
		t.Fatalf("decisions %v %v", a, b)
	}
	if len(c.calls) != 2 {
		t.Fatalf("evaluations = %d, a standing denial is not evaluated again", len(c.calls))
	}
}

func TestPortcarryOPAUserApprovalWhenOnlySecondEvaluationAsks(t *testing.T) {
	c := &portcarryClient{fn: func(_ string, input any) (any, error) {
		if portcarryCharged(input.(DefaultOPAInput), false) == 0 {
			return map[string]any{"decision": "allow"}, nil
		}
		return map[string]any{"decision": "requires-approval", "reason": "second charge"}, nil
	}}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	a := portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	b := portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	if portcarryStatus(a) != "approved/" || portcarryStatus(b) != "user-approval/second charge" {
		t.Fatalf("decisions %v %v", a, b)
	}
}

func TestPortcarryOPADeniesWhenSecondEvaluationFails(t *testing.T) {
	n := 0
	c := &portcarryClient{fn: func(string, any) (any, error) {
		n++
		if n == 3 {
			return nil, errors.New("OPA unreachable")
		}
		return map[string]any{"decision": "allow"}, nil
	}}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	b := portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	if portcarryStatus(b) != "denied/policy evaluation failed: OPA unreachable" {
		t.Fatalf("decision %v", b)
	}
}

func TestPortcarryOPADifferentResponsesAreIndependent(t *testing.T) {
	approval := OPAPolicy(portcarryBudget(100, false), OPAPolicyOptions{Path: "p"})
	a := portcarryApprove(approval, portcarryCharge("call-1", 60), portcarryMessages())
	b := portcarryApprove(approval, portcarryCharge("call-2", 60), portcarryMessages())
	if a.Type != "approved" || b.Type != "approved" {
		t.Fatalf("decisions %v %v", a, b)
	}
}

// A new response whose tool call reuses the ID of a completed call is a new
// call, not the re-evaluation of the one in messages (its input map differs).
func TestPortcarryOPACountsCallWhoseIDWasUsedByCompletedCall(t *testing.T) {
	approval := OPAPolicy(portcarryBudget(100, false), OPAPolicyOptions{Path: "p"})
	msgs := append(portcarryMessages(),
		types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call-0", ToolName: "charge", Arguments: map[string]interface{}{"amount": 0.0}}}})
	a := portcarryApprove(approval, portcarryCharge("call-0", 60), msgs)
	b := portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	if a.Type != "approved" || b.Type != "denied" {
		t.Fatalf("decisions %v %v", a, b)
	}
}

func TestPortcarryOPAEvaluatesOnceForToolCallInMessages(t *testing.T) {
	c := &portcarryClient{fn: func(string, any) (any, error) { return map[string]any{"decision": "allow"}, nil }}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	first, second := portcarryCharge("call-1", 60), portcarryCharge("call-2", 60)
	conversation := append(portcarryMessages(), types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{ToolCallID: "call-1", ToolName: "charge", Arguments: first.Arguments},
		types.ToolCallContent{ToolCallID: "call-2", ToolName: "charge", Arguments: second.Arguments},
	}})
	portcarryApprove(approval, first, conversation)
	portcarryApprove(approval, second, conversation)
	if len(c.calls) != 2 {
		t.Fatalf("evaluations = %d, want one per call", len(c.calls))
	}
	// the top-level ToolCalls field counts as well
	conv2 := append(portcarryMessages(), types.Message{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{first}})
	before := len(c.calls)
	portcarryApprove(approval, first, conv2)
	if len(c.calls)-before != 1 {
		t.Fatalf("Message.ToolCalls re-evaluation ran %d times", len(c.calls)-before)
	}
}

func TestPortcarryOPACustomToInput(t *testing.T) {
	var inputs []any
	c := &portcarryClient{fn: func(_ string, in any) (any, error) {
		inputs = append(inputs, in)
		return map[string]any{"decision": "allow"}, nil
	}}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p", ToInput: func(o types.ToolApprovalOptions) any {
		return map[string]any{"action": o.ToolCall.ToolName, "messageCount": len(o.Messages)}
	}})
	msgs := portcarryMessages()
	portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	want := []int{1, 1, 2}
	if len(inputs) != 3 {
		t.Fatalf("evaluations = %d", len(inputs))
	}
	for i, in := range inputs {
		if in.(map[string]any)["messageCount"] != want[i] {
			t.Fatalf("input %d = %v", i, in)
		}
	}

	// A ToInput that ignores the messages is evaluated once per call.
	c2 := &portcarryClient{fn: func(string, any) (any, error) { return map[string]any{"decision": "allow"}, nil }}
	approval2 := OPAPolicy(c2, OPAPolicyOptions{Path: "p", ToInput: func(o types.ToolApprovalOptions) any {
		return map[string]any{"action": o.ToolCall.ToolName}
	}})
	msgs2 := portcarryMessages()
	portcarryApprove(approval2, portcarryCharge("call-1", 60), msgs2)
	portcarryApprove(approval2, portcarryCharge("call-2", 60), msgs2)
	if len(c2.calls) != 2 {
		t.Fatalf("message-independent ToInput evaluated %d times", len(c2.calls))
	}
}

func TestPortcarryOPAEvaluatesInCallOrderWhenRequestedTogether(t *testing.T) {
	budget := portcarryBudget(100, true)
	c := &portcarryClient{fn: func(path string, input any) (any, error) {
		// Earlier tool calls answer slower than later ones.
		amount := input.(DefaultOPAInput).Args.(map[string]interface{})["amount"].(float64)
		time.Sleep(time.Duration(amount) * time.Millisecond)
		return budget.fn(path, input)
	}}
	approval := OPAPolicy(c, OPAPolicyOptions{Path: "p"})
	msgs := portcarryMessages()
	calls := []types.ToolCall{portcarryCharge("call-1", 60), portcarryCharge("call-2", 60), portcarryCharge("call-3", 30)}
	got := make([]PolicyDecision, 3)
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		// Requested in order; each goroutine registers before the next starts.
		go func() { defer wg.Done(); got[i] = portcarryApprove(approval, call, msgs) }()
		time.Sleep(10 * time.Millisecond)
	}
	wg.Wait()
	if got[0].Type != "approved" || got[1].Type != "denied" || got[2].Type != "approved" {
		t.Fatalf("decisions %v", got)
	}
}

func TestPortcarryOPAKeepsEvaluatingAfterPanickingToInput(t *testing.T) {
	approval := OPAPolicy(portcarryBudget(100, false), OPAPolicyOptions{Path: "p", ToInput: func(o types.ToolApprovalOptions) any {
		if o.ToolCall.ID == "call-1" {
			panic("toInput failed")
		}
		return nil
	}})
	msgs := portcarryMessages()
	func() {
		defer func() { _ = recover() }()
		portcarryApprove(approval, portcarryCharge("call-1", 60), msgs)
	}()
	// call-1 has no decision, so it still counts.
	b := portcarryApprove(approval, portcarryCharge("call-2", 60), msgs)
	if portcarryStatus(b) != "denied/conversation budget exceeded" {
		t.Fatalf("decision %v", b)
	}
}

// fac1d6a588: a fallback of denied applies to every evaluation, so a sibling
// call's user-approval cannot lift an unmatched call.
func portcarryHardAllowlistClient() *portcarryClient {
	return &portcarryClient{fn: func(_ string, input any) (any, error) {
		in := input.(DefaultOPAInput)
		switch in.Tool["name"] {
		case "read_config":
			return map[string]any{"decision": "allow"}, nil
		case "delete_backup":
			for _, m := range in.Messages {
				for _, p := range m.Content {
					if tc, ok := p.(types.ToolCallContent); ok && tc.ToolName == "read_config" {
						return map[string]any{"decision": "requires-approval", "reason": "after read"}, nil
					}
				}
			}
		}
		return nil, nil
	}}
}

func TestPortcarryWrapMCPToolsAppliesFallbackToEveryOPAEvaluation(t *testing.T) {
	tools := map[string]types.Tool{"read_config": {Name: "read_config"}, "delete_backup": {Name: "delete_backup"}}
	run := func(approval types.ToolApprovalConfig, fallback types.ToolApprovalStatus) []PolicyDecision {
		wrapped := WrapMCPTools(tools, approval, fallback).ToolApproval.(types.GenericToolApprovalFunc)
		msgs := portcarryMessages()
		return []PolicyDecision{
			portcarryApprove(wrapped, portcarryCall("c1", "read_config", map[string]interface{}{}), msgs),
			portcarryApprove(wrapped, portcarryCall("c2", "delete_backup", map[string]interface{}{}), msgs),
		}
	}

	// Hard allowlist: the unmatched call is denied although the evaluation
	// with the sibling asks for approval.
	got := run(NewOPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), types.ToolApprovalStatusDenied)
	if got[0].Type != "approved" || got[1].Type != "denied" {
		t.Fatalf("hard allowlist: %v", got)
	}
	// Documented gap: a plain function carries no hook, so the fallback only
	// reaches the combined decision.
	got = run(OPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), types.ToolApprovalStatusDenied)
	if got[1].Type != "user-approval" {
		t.Fatalf("plain func: %v", got)
	}
	// user-approval (default) keeps the engine's reason.
	got = run(NewOPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), types.ToolApprovalStatusUserApproval)
	if portcarryStatus(got[1]) != "user-approval/after read" {
		t.Fatalf("user-approval fallback: %v", got)
	}
	// approved fallback: the unmatched first evaluation counts as approved,
	// the sibling evaluation's user-approval wins, with its reason.
	got = run(NewOPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), types.ToolApprovalStatusApproved)
	if portcarryStatus(got[1]) != "user-approval/after read" {
		t.Fatalf("approved fallback: %v", got)
	}
	// A call without sibling calls: the fallback applies to the single evaluation.
	wrapped := WrapMCPTools(tools, NewOPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), types.ToolApprovalStatusDenied).ToolApproval.(types.GenericToolApprovalFunc)
	if d := portcarryApprove(wrapped, portcarryCall("c2", "delete_backup", map[string]interface{}{}), portcarryMessages()); d.Type != "denied" {
		t.Fatalf("no siblings: %v", d)
	}
}

func TestPortcarryWrapMCPToolsAppliesFallbackThroughShadow(t *testing.T) {
	tools := map[string]types.Tool{"read_config": {Name: "read_config"}, "delete_backup": {Name: "delete_backup"}}
	var events []PolicyDecisionEvent
	var mu sync.Mutex
	shadow := NewShadow(NewOPAPolicy(portcarryHardAllowlistClient(), OPAPolicyOptions{Path: "p"}), ShadowOptions{Enforce: true, OnDecision: func(e PolicyDecisionEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}})
	wrapped := WrapMCPTools(tools, shadow, types.ToolApprovalStatusDenied).ToolApproval.(types.GenericToolApprovalFunc)
	msgs := portcarryMessages()
	portcarryApprove(wrapped, portcarryCall("c1", "read_config", map[string]interface{}{}), msgs)
	d := portcarryApprove(wrapped, portcarryCall("c2", "delete_backup", map[string]interface{}{}), msgs)
	if d.Type != "denied" {
		t.Fatalf("shadow enforced + denied fallback: %v", d)
	}
}
