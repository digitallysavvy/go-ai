package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"weak"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DefaultOPAInput is the default input passed to OPA tool-call rules.
type DefaultOPAInput struct {
	Tool           map[string]string `json:"tool"`
	Args           any               `json:"args,omitempty"`
	Messages       []types.Message   `json:"messages"`
	RuntimeContext any               `json:"runtimeContext,omitempty"`
}

// OPAPolicyOptions configures OPAPolicy.
type OPAPolicyOptions struct {
	Path    string
	ToInput func(types.ToolApprovalOptions) any
}

// OptionalOPAPolicyOptions configures OptionalOPAPolicy.
type OptionalOPAPolicyOptions struct {
	OPAPolicyOptions
}

// OPAPolicy constructs a generic tool approval function backed by OPA.
//
// A model response can contain several tool calls. The SDK decides on all of
// them before it runs any of them and passes the same messages slice for each:
// the conversation before that response. A rule that counts earlier calls
// (a budget, "only one delete") would approve each call on its own. So every
// call after the first one of a response is evaluated twice: with the
// messages as passed, and with the earlier calls of the response appended as
// an assistant message (denied calls are followed by an execution-denied tool
// result). The more restrictive decision applies: denied, then
// user-approval, then not-applicable, then approved (TS 0b8cbfadb8).
//
// The response a call belongs to is identified by the messages slice (its
// first element's address and its length), as TS identifies it by the array.
// A caller that passes one slice for several responses makes the tool calls of
// the earlier ones count as earlier calls of the later ones, which can only
// make a decision more restrictive. An empty messages slice has no identity
// and is evaluated once.
func OPAPolicy(client PolicyClient, opts OPAPolicyOptions) types.GenericToolApprovalFunc {
	return NewOPAPolicy(client, opts).Func()
}

// OPAApproval is the OPA tool approval with its wrapMcpTools hook. TS hangs a
// symbol-keyed function on the approval function opaPolicy returns; a Go func
// value cannot carry one, so the hook is a method here. WrapMCPTools,
// NewShadow and OptionalOPAPolicy-style helpers recognize it through
// FallbackApproval. The plain function OPAPolicy returns takes the fallback
// from the options WrapMCPTools passes it, so it applies the fallback to every
// evaluation as well.
type OPAApproval struct {
	client   PolicyClient
	opts     OPAPolicyOptions
	fallback types.ToolApprovalStatus
	state    *opaState

	mu       sync.Mutex
	variants map[types.ToolApprovalStatus]*OPAApproval
}

// approvalFallbackKey is the ToolsContext key under which WrapMCPTools hands
// its fallback to a plain approval function. A Go func value carries no hidden
// property the way TS's symbol-keyed hook does, so the fallback travels in the
// options of the call instead: only the functions OPAPolicy and Shadow return
// read it, and they remove it before anything else (the policy input, a custom
// ToInput, the wrapped approval of a Shadow that is not OPA) sees the options.
const approvalFallbackKey = "\x00go-ai/policy.approvalFallback"

type approvalFallbackCarrier struct{ fallback types.ToolApprovalStatus }

// withApprovalFallback returns args carrying fallback; the caller's
// ToolsContext map is not modified.
func withApprovalFallback(args types.ToolApprovalOptions, fallback types.ToolApprovalStatus) types.ToolApprovalOptions {
	ctx := make(map[string]interface{}, len(args.ToolsContext)+1)
	for k, v := range args.ToolsContext {
		ctx[k] = v
	}
	ctx[approvalFallbackKey] = approvalFallbackCarrier{fallback: fallback}
	args.ToolsContext = ctx
	return args
}

// takeApprovalFallback removes the carrier from args.
func takeApprovalFallback(args types.ToolApprovalOptions) (types.ToolApprovalOptions, types.ToolApprovalStatus, bool) {
	c, ok := args.ToolsContext[approvalFallbackKey].(approvalFallbackCarrier)
	if !ok {
		return args, "", false
	}
	ctx := make(map[string]interface{}, len(args.ToolsContext))
	for k, v := range args.ToolsContext {
		if k != approvalFallbackKey {
			ctx[k] = v
		}
	}
	if len(ctx) == 0 {
		ctx = nil
	}
	args.ToolsContext = ctx
	return args, c.fallback, true
}

// forFallback is the approval that applies fallback to every evaluation. Each
// fallback has its own evaluation history (TS createApproval(fallback)).
func (a *OPAApproval) forFallback(fallback types.ToolApprovalStatus) *OPAApproval {
	if fallback == a.fallback {
		return a
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v, ok := a.variants[fallback]; ok {
		return v
	}
	if a.variants == nil {
		a.variants = map[types.ToolApprovalStatus]*OPAApproval{}
	}
	v := &OPAApproval{client: a.client, opts: a.opts, fallback: fallback, state: newOPAState()}
	a.variants[fallback] = v
	return v
}

// FallbackApproval is an approval that can apply a WrapMCPTools fallback to
// each of its internal evaluations (TS applyApprovalFallback hook).
type FallbackApproval interface {
	// ApplyApprovalFallback returns an approval function that applies
	// fallback to every evaluation before the decisions are compared.
	ApplyApprovalFallback(fallback types.ToolApprovalStatus) types.GenericToolApprovalFunc
}

// NewOPAPolicy is OPAPolicy returning the approval object, so WrapMCPTools can
// apply its fallback to every evaluation.
func NewOPAPolicy(client PolicyClient, opts OPAPolicyOptions) *OPAApproval {
	return &OPAApproval{client: client, opts: opts, state: newOPAState()}
}

// Func returns the approval as a GenericToolApprovalFunc.
func (a *OPAApproval) Func() types.GenericToolApprovalFunc {
	return func(approval types.ToolApprovalOptions) types.ToolApprovalResult {
		if stripped, fallback, ok := takeApprovalFallback(approval); ok {
			return a.forFallback(fallback).approve(stripped)
		}
		return a.approve(approval)
	}
}

// ApplyApprovalFallback implements FallbackApproval. The returned function
// shares no evaluation history with a (TS createApproval(fallback)).
func (a *OPAApproval) ApplyApprovalFallback(fallback types.ToolApprovalStatus) types.GenericToolApprovalFunc {
	b := &OPAApproval{client: a.client, opts: a.opts, fallback: fallback, state: newOPAState()}
	return b.Func()
}

// decide evaluates the policy once. Fail closed: a backend error is a denial
// the model can see, not an error out of the approval.
func (a *OPAApproval) decide(input any) PolicyDecision {
	if a.client == nil {
		return approvalDecision(denied(fmt.Errorf("policy client is nil")))
	}
	result, err := a.client.Evaluate(context.Background(), a.opts.Path, input)
	if err != nil {
		return approvalDecision(denied(err))
	}
	return NormalizeOPADecision(result)
}

func approvalDecision(r types.ToolApprovalResult) PolicyDecision {
	d := PolicyDecision{Type: r.Status}
	if r.Reason != nil {
		d.Reason = *r.Reason
	}
	return d
}

// effectiveType counts a decision as its own type, or as the fallback when it
// is not-applicable. Only used for comparing; the engine decision is returned.
func (a *OPAApproval) effectiveType(d PolicyDecision) types.ToolApprovalStatus {
	if a.fallback != "" && a.fallback != types.ToolApprovalStatusNotApplicable && d.Type == types.ToolApprovalStatusNotApplicable {
		return a.fallback
	}
	return d.Type
}

func (a *OPAApproval) effectiveDecision(d PolicyDecision) PolicyDecision {
	if t := a.effectiveType(d); t != d.Type {
		return PolicyDecision{Type: t}
	}
	return d
}

func (a *OPAApproval) approve(approval types.ToolApprovalOptions) types.ToolApprovalResult {
	toOpaInput := func(messages []types.Message) any {
		o := approval
		o.Messages = messages
		return opaInput(o, a.opts.ToInput)
	}
	messages := approval.Messages

	// The SDK checks a tool call that a user approved once more before it
	// runs it. messages then contain the response with that tool call, so
	// there is nothing to append and a single evaluation.
	if isToolCallFromMessages(messages, approval.ToolCall) {
		return approvalFromDecision(a.decide(toOpaInput(messages)))
	}

	resp, prev, mine := a.state.enter(messages, approval.ToolCall)
	var earlier []*opaEvaluated
	var current *opaEvaluated
	if resp != nil {
		earlier, current = resp.earlier, resp.current
	}
	if prev != nil {
		<-prev
	}
	if mine != nil {
		defer close(mine)
	}

	opaIn := toOpaInput(messages)
	decision := a.decide(opaIn)
	// A denial cannot become more restrictive.
	if a.effectiveType(decision) != types.ToolApprovalStatusDenied {
		withEarlier := appendEarlierToolCalls(messages, earlier)
		if len(withEarlier) != len(messages) {
			second := toOpaInput(withEarlier)
			// A custom ToInput that ignores messages yields the same input
			// twice, and evaluating it again cannot change the decision.
			if a.opts.ToInput == nil || !isSameOPAInput(opaIn, second) {
				decision = a.moreRestrictive(decision, a.decide(second))
			}
		}
	}
	if current != nil {
		eff := a.effectiveDecision(decision)
		current.decision = &eff
	}
	return approvalFromDecision(decision)
}

var opaRestrictiveness = map[types.ToolApprovalStatus]int{
	types.ToolApprovalStatusApproved:      0,
	types.ToolApprovalStatusNotApplicable: 1,
	types.ToolApprovalStatusUserApproval:  2,
	types.ToolApprovalStatusDenied:        3,
}

// moreRestrictive returns other when it restricts the call more than decision
// does. Decisions tying after the fallback compare again by their own type.
func (a *OPAApproval) moreRestrictive(decision, other PolicyDecision) PolicyDecision {
	o, c := opaRestrictiveness[a.effectiveType(other)], opaRestrictiveness[a.effectiveType(decision)]
	if o > c {
		return other
	}
	if o < c {
		return decision
	}
	if opaRestrictiveness[other.Type] > opaRestrictiveness[decision.Type] {
		return other
	}
	return decision
}

func isSameOPAInput(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// opaEvaluated is a tool call this policy has evaluated for one response.
type opaEvaluated struct {
	id       string
	name     string
	input    map[string]interface{}
	decision *PolicyDecision // nil until its evaluation has finished
}

type opaResponse struct {
	toolCalls []*opaEvaluated
	last      chan struct{}
}

type opaResponseKey struct {
	first weak.Pointer[types.Message]
	n     int
}

// opaState is TS's WeakMap<messages, ResponseEvaluations>: weak keys, so a
// response's entry goes away with its messages.
type opaState struct {
	mu        sync.Mutex
	responses map[opaResponseKey]*opaResponse
}

func newOPAState() *opaState { return &opaState{responses: map[opaResponseKey]*opaResponse{}} }

type opaEntry struct {
	earlier []*opaEvaluated
	current *opaEvaluated
}

// enter registers the call for its response and returns the calls evaluated
// before it, the channel to wait on (the previous evaluation of the response)
// and the channel to close when this evaluation is done. An empty messages
// slice has no identity: nothing is remembered.
func (s *opaState) enter(messages []types.Message, call types.ToolCall) (*opaEntry, <-chan struct{}, chan struct{}) {
	if len(messages) == 0 {
		return nil, nil, nil
	}
	key := opaResponseKey{first: weak.Make(&messages[0]), n: len(messages)}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.responses {
		if k.first.Value() == nil {
			delete(s.responses, k)
		}
	}
	resp := s.responses[key]
	if resp == nil {
		done := make(chan struct{})
		close(done)
		resp = &opaResponse{last: done}
		s.responses[key] = resp
	}
	entry := &opaEntry{earlier: append([]*opaEvaluated(nil), resp.toolCalls...)}
	entry.current = &opaEvaluated{id: call.ID, name: call.ToolName, input: call.Arguments}
	resp.toolCalls = append(resp.toolCalls, entry.current)
	prev := resp.last
	mine := make(chan struct{})
	resp.last = mine
	return entry, prev, mine
}

// appendEarlierToolCalls returns messages followed by the earlier tool calls
// of the same response in the shape they have once in the conversation: an
// assistant message with the calls and, for denied calls, a tool message with
// their execution-denied results. It returns messages itself when there is
// nothing to append.
func appendEarlierToolCalls(messages []types.Message, earlier []*opaEvaluated) []types.Message {
	if len(earlier) == 0 {
		return messages
	}
	calls := make([]types.ContentPart, 0, len(earlier))
	var results []types.ContentPart
	for _, e := range earlier {
		calls = append(calls, types.ToolCallContent{ToolCallID: e.id, ToolName: e.name, Arguments: e.input})
		if e.decision != nil && e.decision.Type == types.ToolApprovalStatusDenied {
			out := &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}
			if e.decision.Reason != "" {
				out.Reason = e.decision.Reason
			}
			results = append(results, types.ToolResultContent{ToolCallID: e.id, ToolName: e.name, Output: out})
		}
	}
	out := make([]types.Message, 0, len(messages)+2)
	out = append(out, messages...)
	out = append(out, types.Message{Role: types.RoleAssistant, Content: calls})
	if len(results) > 0 {
		out = append(out, types.Message{Role: types.RoleTool, Content: results})
	}
	return out
}

// isToolCallFromMessages reports whether the call is one of the tool calls in
// messages. When the SDK checks a tool call a user approved, it passes the
// tool call of the conversation with the same input map; a tool call of a new
// response is parsed from that response, so its map is a new one, also when it
// carries the ID of a tool call in messages (some providers number the calls
// of every response from zero). A nil input cannot be told apart this way and
// is treated as new, which can only make the decision more restrictive.
func isToolCallFromMessages(messages []types.Message, call types.ToolCall) bool {
	if call.Arguments == nil {
		return false
	}
	want := reflect.ValueOf(call.Arguments).Pointer()
	for _, m := range messages {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == call.ID && tc.Arguments != nil && reflect.ValueOf(tc.Arguments).Pointer() == want {
				return true
			}
		}
		for _, part := range m.Content {
			var id string
			var args map[string]interface{}
			switch p := part.(type) {
			case types.ToolCallContent:
				id, args = p.ToolCallID, p.Arguments
			case *types.ToolCallContent:
				if p == nil {
					continue
				}
				id, args = p.ToolCallID, p.Arguments
			default:
				continue
			}
			if id == call.ID && args != nil && reflect.ValueOf(args).Pointer() == want {
				return true
			}
		}
	}
	return false
}

// OptionalOPAPolicy returns nil when no client is supplied, matching the TS
// helper's default allow-all behavior when policy is not configured.
func OptionalOPAPolicy(client PolicyClient, opts OptionalOPAPolicyOptions) types.ToolApprovalConfig {
	if client == nil {
		return nil
	}
	return OPAPolicy(client, opts.OPAPolicyOptions)
}

func opaInput(approval types.ToolApprovalOptions, mapper func(types.ToolApprovalOptions) any) any {
	if mapper != nil {
		if input := mapper(approval); input != nil {
			return input
		}
	}
	messages := approval.Messages
	if messages == nil {
		messages = []types.Message{}
	}
	// A nil map would encode as JSON null; TS passes the parsed input, which is
	// {} for a tool called without arguments.
	args := approval.ToolCall.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	return DefaultOPAInput{
		Tool:           map[string]string{"name": approval.ToolCall.ToolName},
		Args:           args,
		Messages:       messages,
		RuntimeContext: approval.RuntimeContext,
	}
}

func denied(err error) types.ToolApprovalResult {
	reason := "policy evaluation failed: " + errorMessage(err)
	return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
}

func errorMessage(cause any) string {
	switch v := cause.(type) {
	case nil:
		return "<nil>"
	case error:
		return v.Error()
	case string:
		return v
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}
