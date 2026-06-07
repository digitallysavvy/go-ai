package policy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/middleware"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type stubClient struct {
	result any
	err    error
	calls  []stubCall
}

type stubCall struct {
	path  string
	input any
}

func (s *stubClient) Evaluate(ctx context.Context, path string, input any) (any, error) {
	s.calls = append(s.calls, stubCall{path: path, input: input})
	return s.result, s.err
}

func approvalArgs(toolName string, input map[string]interface{}) types.ToolApprovalOptions {
	return types.ToolApprovalOptions{
		ToolCall:       types.ToolCall{ID: "call-" + toolName, ToolName: toolName, Arguments: input},
		Messages:       []types.Message{{Role: types.RoleUser}},
		RuntimeContext: map[string]interface{}{"role": "reviewer"},
	}
}

func TestOPAPolicyNormalizesDecisionsAndDefaultInput(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: map[string]any{"decision": "deny", "reason": "no pushes"}}
	approval := OPAPolicy(client, OPAPolicyOptions{Path: "agent/call/decision"})

	got := approval(approvalArgs("git", map[string]interface{}{"args": []interface{}{"push"}}))
	if got.Status != types.ToolApprovalStatusDenied || got.Reason == nil || *got.Reason != "no pushes" {
		t.Fatalf("approval = %+v, want denied reason", got)
	}
	if len(client.calls) != 1 || client.calls[0].path != "agent/call/decision" {
		t.Fatalf("calls = %+v", client.calls)
	}
	input, ok := client.calls[0].input.(DefaultOPAInput)
	if !ok {
		t.Fatalf("input type = %T, want DefaultOPAInput", client.calls[0].input)
	}
	if input.Tool["name"] != "git" || input.RuntimeContext == nil || len(input.Messages) != 1 {
		t.Fatalf("unexpected default input: %+v", input)
	}
}

func TestOPAPolicyCustomInputAndFailClosed(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: map[string]any{"decision": "allow"}}
	approval := OPAPolicy(client, OPAPolicyOptions{
		Path: "p",
		ToInput: func(opts types.ToolApprovalOptions) any {
			return map[string]any{"action": opts.ToolCall.ToolName}
		},
	})
	got := approval(approvalArgs("search", nil))
	if got.Status != types.ToolApprovalStatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if !reflect.DeepEqual(client.calls[0].input, map[string]any{"action": "search"}) {
		t.Fatalf("custom input = %#v", client.calls[0].input)
	}

	failing := OPAPolicy(&stubClient{err: errors.New("OPA unreachable")}, OPAPolicyOptions{Path: "p"})
	denied := failing(approvalArgs("git", nil))
	if denied.Status != types.ToolApprovalStatusDenied || denied.Reason == nil || *denied.Reason != "policy evaluation failed: OPA unreachable" {
		t.Fatalf("failing approval = %+v", denied)
	}
}

func TestOPAPolicyNilCustomInputFallsBackToDefault(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: map[string]any{"decision": "allow"}}
	approval := OPAPolicy(client, OPAPolicyOptions{
		Path:    "p",
		ToInput: func(types.ToolApprovalOptions) any { return nil },
	})
	if got := approval(approvalArgs("git", nil)); got.Status != types.ToolApprovalStatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	input, ok := client.calls[0].input.(DefaultOPAInput)
	if !ok {
		t.Fatalf("input type = %T, want DefaultOPAInput", client.calls[0].input)
	}
	if input.Tool["name"] != "git" {
		t.Fatalf("input = %+v, want default input", input)
	}
}

func TestDefaultOPAInputJSONIncludesMessages(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(DefaultOPAInput{
		Tool:     map[string]string{"name": "git"},
		Messages: []types.Message{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["messages"]; !ok {
		t.Fatalf("json = %s, want messages property", data)
	}
}

func TestOPAPolicyDefaultInputUsesEmptyMessagesArray(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: map[string]any{"decision": "allow"}}
	approval := OPAPolicy(client, OPAPolicyOptions{Path: "p"})
	args := approvalArgs("git", nil)
	args.Messages = nil
	if got := approval(args); got.Status != types.ToolApprovalStatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	input := client.calls[0].input.(DefaultOPAInput)
	if input.Messages == nil || len(input.Messages) != 0 {
		t.Fatalf("messages = %#v, want empty non-nil slice", input.Messages)
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	messages, ok := encoded["messages"].([]any)
	if !ok || len(messages) != 0 {
		t.Fatalf("json = %s, want empty messages array", data)
	}
}

func TestOptionalOPAPolicy(t *testing.T) {
	t.Parallel()
	if got := OptionalOPAPolicy(nil, OptionalOPAPolicyOptions{}); got != nil {
		t.Fatalf("OptionalOPAPolicy(nil) = %#v, want nil", got)
	}
	client := &stubClient{result: map[string]any{"decision": "allow"}}
	cfg := OptionalOPAPolicy(client, OptionalOPAPolicyOptions{OPAPolicyOptions: OPAPolicyOptions{Path: "p"}})
	fn, ok := cfg.(types.GenericToolApprovalFunc)
	if !ok {
		t.Fatalf("optional config = %T, want GenericToolApprovalFunc", cfg)
	}
	if got := fn(approvalArgs("git", nil)); got.Status != types.ToolApprovalStatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
}

func TestNormalizeOPADecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  any
		want PolicyDecision
	}{
		{name: "explicit allow", raw: map[string]any{"decision": "allow", "reason": "role"}, want: PolicyDecision{Type: types.ToolApprovalStatusApproved, Reason: "role"}},
		{name: "explicit deny", raw: map[string]any{"decision": "deny", "reason": "no"}, want: PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "no"}},
		{name: "requires approval", raw: map[string]any{"decision": "requires-approval"}, want: PolicyDecision{Type: types.ToolApprovalStatusUserApproval}},
		{name: "not applicable", raw: map[string]any{"decision": "not-applicable"}, want: PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}},
		{name: "legacy bool allow", raw: map[string]any{"allow": true}, want: PolicyDecision{Type: types.ToolApprovalStatusApproved}},
		{name: "legacy bool deny", raw: map[string]any{"allow": false, "reason": "no rule"}, want: PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "no rule"}},
		{name: "native string map", raw: map[string]string{"decision": "deny", "reason": "no"}, want: PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "no"}},
		{name: "native bool map", raw: map[string]bool{"allow": true}, want: PolicyDecision{Type: types.ToolApprovalStatusApproved}},
		{name: "wrapped result", raw: map[string]any{"result": map[string]any{"allow": true}}, want: PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}},
		{name: "batch expression", raw: []any{map[string]any{"expressions": []any{map[string]any{"value": map[string]any{"allow": false}}}}}, want: PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}},
		{name: "primitive", raw: "yes", want: PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}},
		{name: "json bytes", raw: []byte(`{"decision":"allow"}`), want: PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeOPADecision(tc.raw); got != tc.want {
				t.Fatalf("NormalizeOPADecision() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHTTPPolicyClient(t *testing.T) {
	t.Parallel()
	var requestPath string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("missing auth header")
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"result":{"decision":"deny","reason":"blocked"}}`))
	}))
	defer server.Close()

	client := HTTPPolicyClient(server.URL, HTTPClientOptions{Headers: map[string]string{"Authorization": "Bearer test"}})
	raw, err := client.Evaluate(context.Background(), "agent/call/decision", map[string]any{"tool": "git"})
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if requestPath != "/v1/data/agent/call/decision" {
		t.Fatalf("request path = %q", requestPath)
	}
	if _, ok := requestBody["input"].(map[string]any); !ok {
		t.Fatalf("request body = %#v", requestBody)
	}
	if got := NormalizeOPADecision(raw); got != (PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "blocked"}) {
		t.Fatalf("decision = %+v", got)
	}
}

func TestHTTPPolicyClientNon2xxRejects(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusForbidden)
	}))
	defer server.Close()
	_, err := HTTPPolicyClient(server.URL).Evaluate(context.Background(), "p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeWASMPolicy struct {
	results []WASMPolicyResult
}

func (f fakeWASMPolicy) Evaluate(input any) ([]WASMPolicyResult, error) {
	return f.results, nil
}

func TestWASMPolicyClientEvaluate(t *testing.T) {
	t.Parallel()
	client, err := WASMPolicyClient([]byte{0}, WASMClientOptions{
		LoadPolicy: func(wasm []byte, data any) (LoadedWASMPolicy, error) {
			return fakeWASMPolicy{results: []WASMPolicyResult{{Result: false}, {Result: true}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("WASMPolicyClient error = %v", err)
	}
	got, err := client.Evaluate(context.Background(), "ignored", nil)
	if err != nil {
		t.Fatalf("Evaluate error = %v", err)
	}
	if got != false {
		t.Fatalf("result = %#v, want false", got)
	}
}

func TestWASMPolicyClientNoRuntimeAndNoResult(t *testing.T) {
	t.Parallel()
	if _, err := WASMPolicyClient([]byte{0}); err == nil || !strings.Contains(err.Error(), `Cannot import "@open-policy-agent/opa-wasm"`) {
		t.Fatalf("missing runtime error = %v, want TS-style optional peer error", err)
	}
	if _, err := WASMPolicyClient([]byte{0}, WASMClientOptions{
		LoadPolicy: func(wasm []byte, data any) (LoadedWASMPolicy, error) {
			return nil, nil
		},
	}); err == nil {
		t.Fatal("expected nil policy loader error")
	}
	client, err := WASMPolicyClient([]byte{0}, WASMClientOptions{
		LoadPolicy: func(wasm []byte, data any) (LoadedWASMPolicy, error) {
			return fakeWASMPolicy{}, nil
		},
	})
	if err != nil {
		t.Fatalf("WASMPolicyClient error = %v", err)
	}
	if _, err := client.Evaluate(context.Background(), "p", nil); err == nil {
		t.Fatal("expected no result error")
	}
}

func TestShadowReportsDecisionAndApprovesByDefault(t *testing.T) {
	t.Parallel()
	events := make(chan PolicyDecisionEvent, 1)
	wrapped := Shadow(types.GenericToolApprovalFunc(func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
		reason := "pushes"
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
	}), ShadowOptions{OnDecision: func(event PolicyDecisionEvent) { events <- event }})

	got := wrapped(approvalArgs("git", map[string]interface{}{"args": []interface{}{"push"}}))
	if got.Status != types.ToolApprovalStatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	select {
	case event := <-events:
		if event.Decision.Type != types.ToolApprovalStatusDenied || event.Effective.Type != types.ToolApprovalStatusApproved || event.Enforced {
			t.Fatalf("event = %+v", event)
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", event.Timestamp); err != nil {
			t.Fatalf("timestamp = %q, want JavaScript ISO milliseconds: %v", event.Timestamp, err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestShadowEnforceAndSwallowTelemetryPanic(t *testing.T) {
	t.Parallel()
	wrapped := Shadow(types.GenericToolApprovalFunc(func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
	}), ShadowOptions{Enforce: true, OnDecision: func(event PolicyDecisionEvent) { panic("logger down") }})
	if got := wrapped(approvalArgs("git", nil)); got.Status != types.ToolApprovalStatusDenied {
		t.Fatalf("status = %q, want denied", got.Status)
	}
}

func TestShadowMapApprovalObjectDecision(t *testing.T) {
	t.Parallel()
	events := make(chan PolicyDecisionEvent, 1)
	wrapped := Shadow(map[string]interface{}{
		"git": map[string]interface{}{"type": "denied", "reason": "no git in shadow"},
	}, ShadowOptions{Enforce: true, OnDecision: func(event PolicyDecisionEvent) { events <- event }})
	got := wrapped(approvalArgs("git", nil))
	if got.Status != types.ToolApprovalStatusDenied || got.Reason == nil || *got.Reason != "no git in shadow" {
		t.Fatalf("status = %+v, want denied reason", got)
	}
	select {
	case event := <-events:
		if event.Decision != (PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "no git in shadow"}) {
			t.Fatalf("event decision = %+v", event.Decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestShadowNativeMapApprovalObjectDecision(t *testing.T) {
	t.Parallel()
	wrapped := Shadow(map[string]interface{}{
		"git": map[string]string{"type": "denied", "reason": "no git in shadow"},
	}, ShadowOptions{Enforce: true})
	got := wrapped(approvalArgs("git", nil))
	if got.Status != types.ToolApprovalStatusDenied || got.Reason == nil || *got.Reason != "no git in shadow" {
		t.Fatalf("status = %+v, want denied reason", got)
	}
}

func TestWrapMCPTools(t *testing.T) {
	t.Parallel()
	tools := map[string]types.Tool{
		"search":      {Name: "search"},
		"createIssue": {Name: "createIssue"},
		"deleteRepo":  {Name: "deleteRepo"},
	}
	wrapped := WrapMCPTools(tools, map[string]types.ToolApprovalValue{
		"search": types.ToolApprovalStatusApproved,
	}, types.ToolApprovalStatusDenied)
	approval := wrapped.ToolApproval.(map[string]types.ToolApprovalValue)
	if approval["search"] != types.ToolApprovalStatusApproved || approval["deleteRepo"] != types.ToolApprovalStatusDenied {
		t.Fatalf("approval map = %#v", approval)
	}
	wrapped.Tools["extra"] = types.Tool{Name: "extra"}
	if _, ok := tools["extra"]; !ok {
		t.Fatalf("tools map reference changed")
	}

	fnWrapped := WrapMCPTools(tools, types.GenericToolApprovalFunc(func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	}))
	fn := fnWrapped.ToolApproval.(types.GenericToolApprovalFunc)
	if got := fn(approvalArgs("search", nil)); got.Status != types.ToolApprovalStatusUserApproval {
		t.Fatalf("fallback status = %q, want user-approval", got.Status)
	}

	nilWrapped := WrapMCPTools(tools, map[string]types.ToolApprovalValue{
		"search": nil,
	}, types.ToolApprovalStatusDenied)
	nilApproval := nilWrapped.ToolApproval.(map[string]types.ToolApprovalValue)
	if nilApproval["search"] != types.ToolApprovalStatusDenied {
		t.Fatalf("nil approval entry = %#v, want fallback", nilApproval["search"])
	}
}

func TestOPACapabilityMiddleware(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: []any{"search", "openai.web_search", "google.google_search"}}
	model := &testutil.MockLanguageModel{ModelName: "mock"}
	mw := OPACapabilityMiddleware(client, CapabilityMiddlewareOptions{Path: "agent/tools/allowed"})
	if mw.SpecificationVersion != "v4" {
		t.Fatalf("specification version = %q, want v4", mw.SpecificationVersion)
	}
	wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{mw}, nil, nil)
	opts := &provider.GenerateOptions{Tools: []types.Tool{
		{Name: "search"},
		{Name: "deleteRepo"},
		{Name: "web_search", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search"},
		{Name: "google_search", Type: types.ToolTypeProviderDefined, ProviderName: "google", ProviderID: "google_search"},
		{Name: "functionWithProviderID", ProviderID: "google.google_search"},
	}}
	if _, err := wrapped.DoGenerate(context.Background(), opts); err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if len(model.GenerateCalls) != 1 {
		t.Fatal("expected generate call")
	}
	gotTools := model.GenerateCalls[0].Tools
	if len(gotTools) != 2 || gotTools[0].Name != "search" || gotTools[1].Name != "web_search" {
		t.Fatalf("filtered tools = %+v", gotTools)
	}
	if len(client.calls) != 1 || client.calls[0].path != "agent/tools/allowed" {
		t.Fatalf("client calls = %+v", client.calls)
	}
}

func TestOPACapabilityMiddlewareProviderToolMatchesOnlyIDOrName(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: []any{"google.google_search"}}
	model := &testutil.MockLanguageModel{ModelName: "mock"}
	mw := OPACapabilityMiddleware(client)
	params := &provider.GenerateOptions{Tools: []types.Tool{
		{Name: "google_search", Type: types.ToolTypeProviderDefined, ProviderName: "google", ProviderID: "google_search"},
	}}
	out, err := mw.TransformParams(context.Background(), "generate", params, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tools) != 0 {
		t.Fatalf("filtered tools = %+v, want empty slice because TS matches only provider id or name", out.Tools)
	}
}

func TestOPACapabilityMiddlewareAcceptsNativeGoAllowlists(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{ModelName: "mock"}
	for _, tc := range []struct {
		name   string
		result any
	}{
		{name: "slice", result: []string{"search"}},
		{name: "object", result: map[string][]string{"tools": []string{"search"}}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mw := OPACapabilityMiddleware(&stubClient{result: tc.result})
			params := &provider.GenerateOptions{Tools: []types.Tool{{Name: "search"}, {Name: "writeFile"}}}
			out, err := mw.TransformParams(context.Background(), "generate", params, model)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Tools) != 1 || out.Tools[0].Name != "search" {
				t.Fatalf("filtered tools = %+v, want search only", out.Tools)
			}
		})
	}
}

func TestOPACapabilityMiddlewareFailClosedAndIdentity(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{ModelName: "mock"}
	params := &provider.GenerateOptions{Tools: []types.Tool{{Name: "search"}}}
	identity := OPACapabilityMiddleware(&stubClient{result: []any{"search"}})
	out, err := identity.TransformParams(context.Background(), "generate", params, model)
	if err != nil {
		t.Fatal(err)
	}
	if out != params {
		t.Fatal("expected params identity when no tools removed")
	}

	failClosed := OPACapabilityMiddleware(&stubClient{err: errors.New("OPA unreachable")})
	out, err = failClosed.TransformParams(context.Background(), "generate", params, model)
	if err != nil {
		t.Fatal(err)
	}
	if out == params || out.Tools != nil {
		t.Fatalf("fail-closed tools = %#v", out.Tools)
	}

	malformed := OPACapabilityMiddleware(&stubClient{result: []byte(`["search"]`)})
	out, err = malformed.TransformParams(context.Background(), "generate", params, model)
	if err != nil {
		t.Fatal(err)
	}
	if out == params || out.Tools != nil {
		t.Fatalf("raw JSON allowlist tools = %#v, want fail-closed nil", out.Tools)
	}
}

func TestOPACapabilityMiddlewareNilCustomInputFallsBackToDefault(t *testing.T) {
	t.Parallel()
	client := &stubClient{result: []any{"search"}}
	mw := OPACapabilityMiddleware(client, CapabilityMiddlewareOptions{
		Path:    "p",
		ToInput: func(*provider.GenerateOptions) any { return nil },
	})
	params := &provider.GenerateOptions{Tools: []types.Tool{{Name: "search"}}}
	out, err := mw.TransformParams(context.Background(), "generate", params, &testutil.MockLanguageModel{ModelName: "mock"})
	if err != nil {
		t.Fatal(err)
	}
	if out != params {
		t.Fatal("expected identity when no tools removed")
	}
	input, ok := client.calls[0].input.(DefaultOPACapabilityInput)
	if !ok {
		t.Fatalf("input type = %T, want DefaultOPACapabilityInput", client.calls[0].input)
	}
	if !reflect.DeepEqual(input.Messages, params.Prompt) {
		t.Fatalf("messages = %#v, want default prompt %#v", input.Messages, params.Prompt)
	}
}

func TestDefaultOPACapabilityInputJSONIncludesEmptyProviderOptions(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(DefaultOPACapabilityInput{
		Messages:        []any{},
		ProviderOptions: map[string]interface{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	providerOptions, ok := got["providerOptions"].(map[string]any)
	if !ok || len(providerOptions) != 0 {
		t.Fatalf("json = %s, want empty providerOptions object", data)
	}
}
