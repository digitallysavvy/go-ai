package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r90carryMeta struct {
	Body interface{} `json:"body,omitempty"`
}

type r90carryEvent struct {
	Tools     map[string]types.Tool `json:"tools"`
	StepTools []types.Tool          `json:"stepTools"`
	Request   r90carryMeta          `json:"request"`
	Response  []r90carryMeta        `json:"response"`
	Steps     []types.StepResult    `json:"steps"`
	FinalStep types.StepResult      `json:"finalStep"`
	Prompt    string                `json:"prompt"`
}

func r90carryJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func r90carryOpenAIBody(secret string) map[string]interface{} {
	return map[string]interface{}{
		"model": "gpt-4o",
		"tools": []interface{}{
			map[string]interface{}{"type": "code_interpreter", "container": map[string]interface{}{"type": "auto",
				"network_policy": map[string]interface{}{"type": "allowlist", "domain_secrets": []interface{}{
					map[string]interface{}{"domain": "example.com", "name": "KEY", "value": secret}}}}},
			map[string]interface{}{"type": "shell", "environment": map[string]interface{}{"type": "container_auto",
				"network_policy": map[string]interface{}{"type": "allowlist", "domain_secrets": []interface{}{
					map[string]interface{}{"domain": "example.com", "name": "KEY", "value": secret}}}}},
			map[string]interface{}{"type": "mcp", "server_label": "docs", "authorization": secret,
				"headers": map[string]interface{}{"X-Key": secret}},
			map[string]interface{}{"type": "function", "name": "f", "parameters": map[string]interface{}{"secret": "keep"}},
		},
	}
}

// TS create-telemetry-dispatcher.test.ts "applies provider-owned views
// throughout SDK metadata without mutating originals" (404ce3b14d /
// 9f37b45262), with the OpenAI rules: every SDK metadata container is
// redacted, the prompt is not, and the original is unchanged.
func TestR90carryRedactCredentialsThroughoutMetadata(t *testing.T) {
	const secret = "test-secret"
	mcp := types.Tool{Name: "docs", Type: types.ToolTypeProviderDefined, ProviderID: "openai.mcp",
		ProviderArgs: map[string]interface{}{"serverLabel": "docs", "authorization": secret, "headers": map[string]interface{}{"X-Key": secret}}}
	gatewayBody := map[string]interface{}{"tools": []map[string]interface{}{{
		"type": "provider", "id": "openai.code_interpreter", "name": "python",
		"args": map[string]interface{}{"container": map[string]interface{}{"networkPolicy": map[string]interface{}{
			"domainSecrets": []interface{}{map[string]interface{}{"name": "KEY", "value": secret}}}}},
	}}}
	step := types.StepResult{
		Request:  types.StepRequest{Body: r90carryOpenAIBody(secret)},
		Response: types.StepResponse{Body: r90carryOpenAIBody(secret)},
	}
	original := r90carryEvent{
		Tools:     map[string]types.Tool{"docs": mcp},
		StepTools: []types.Tool{mcp},
		Request:   r90carryMeta{Body: r90carryOpenAIBody(secret)},
		Response:  []r90carryMeta{{Body: gatewayBody}, {Body: "plain"}},
		Steps:     []types.StepResult{step},
		FinalStep: step,
		Prompt:    "my " + secret,
	}
	before := r90carryJSON(t, original)
	got := r90carryJSON(t, FilterEvent(original, nil))
	want := strings.ReplaceAll(before, secret, "[redacted]")
	want = strings.Replace(want, `"prompt":"my [redacted]"`, `"prompt":"my `+secret+`"`, 1)
	if got != want {
		t.Errorf("redacted =\n%s\nwant\n%s", got, want)
	}
	if after := r90carryJSON(t, original); after != before {
		t.Errorf("original mutated:\n%s", after)
	}
}

func TestR90carryRedactCredentialsNoChangeKeepsValues(t *testing.T) {
	tools := []types.Tool{{Name: "f", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "search", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search", ProviderArgs: map[string]interface{}{"k": "v"}}}
	e := TelemetryStepStartEvent{StepTools: tools}
	out := FilterEvent(e, nil)
	if &out.StepTools[0] != &tools[0] {
		t.Error("unchanged tool list was copied")
	}
	// An xAI-style tool keyed by its own id is left alone.
	xai := types.Tool{Name: "m", ProviderID: "xai.mcp", ProviderArgs: map[string]interface{}{"authorization": "keep"}}
	if got := FilterEvent(TelemetryStepStartEvent{StepTools: []types.Tool{xai}}, nil); got.StepTools[0].ProviderArgs["authorization"] != "keep" {
		t.Error("xai.mcp args redacted")
	}
}

type r90carryShellNetworkPolicy struct {
	Type          string              `json:"type"`
	DomainSecrets []map[string]string `json:"domain_secrets,omitempty"`
}

type r90carryShellEnv struct {
	Type          string                      `json:"type"`
	NetworkPolicy *r90carryShellNetworkPolicy `json:"network_policy,omitempty"`
}

type r90carryMCPConfig struct {
	ServerLabel   string            `json:"serverLabel"`
	Authorization string            `json:"authorization,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// Go's OpenAI tool constructors keep the typed config in ProviderOptions;
// its credentials are redacted in the telemetry copy as well.
func TestR90carryRedactsTypedProviderOptions(t *testing.T) {
	const secret = "typed-secret"
	tools := []types.Tool{
		{Name: "openai.mcp", ProviderID: "openai.mcp", ProviderOptions: r90carryMCPConfig{ServerLabel: "s", Authorization: secret, Headers: map[string]string{"X": secret}}},
		{Name: "shell", ProviderID: "openai.shell", ProviderOptions: &r90carryShellEnv{Type: "container_auto",
			NetworkPolicy: &r90carryShellNetworkPolicy{Type: "allowlist", DomainSecrets: []map[string]string{{"name": "K", "value": secret}}}}},
		{Name: "python", ProviderID: "openai.code_interpreter", ProviderOptions: map[string]interface{}{"container": map[string]interface{}{
			"networkPolicy": map[string]interface{}{"domainSecrets": []interface{}{map[string]interface{}{"value": secret}}}}}},
	}
	before := r90carryJSON(t, []interface{}{tools[0].ProviderOptions, tools[1].ProviderOptions, tools[2].ProviderOptions})
	out := FilterEvent(LanguageModelCallStartEvent{Tools: tools}, nil).Tools.([]types.Tool)
	opts := make([]interface{}, len(out))
	for i, tool := range out {
		opts[i] = tool.ProviderOptions
	}
	got := r90carryJSON(t, opts)
	if strings.Contains(got, secret) || strings.Count(got, "[redacted]") != 4 {
		t.Errorf("typed options not redacted: %s", got)
	}
	if r90carryJSON(t, []interface{}{tools[0].ProviderOptions, tools[1].ProviderOptions, tools[2].ProviderOptions}) != before {
		t.Error("caller tools mutated")
	}
}

// A JSON-text body keeps its Go type; a body without known tools is
// returned as is.
func TestR90carryRedactsJSONTextBody(t *testing.T) {
	const secret = "text-secret"
	raw, _ := json.Marshal(r90carryOpenAIBody(secret))
	for _, body := range []interface{}{string(raw), json.RawMessage(raw), raw} {
		out, ok := redactBody(body)
		if !ok {
			t.Fatalf("%T not redacted", body)
		}
		var text string
		switch v := out.(type) {
		case string:
			text = v
		case json.RawMessage:
			text = string(v)
		case []byte:
			text = string(v)
		default:
			t.Fatalf("%T became %T", body, out)
		}
		if strings.Contains(text, secret) || !strings.Contains(text, "[redacted]") {
			t.Errorf("%T body: %s", body, text)
		}
	}
	if _, ok := redactBody(`{"tools":[{"type":"function","name":"f"}]}`); ok {
		t.Error("function-only body changed")
	}
}

// The diagnostic channel receives the redacted copy (TS tracing-channel
// publisher redacts the event, 404ce3b14d).
func TestR90carryDiagnosticEventRedacted(t *testing.T) {
	const secret = "diag-secret"
	var got string
	unsub := SubscribeDiagnosticTyped(DiagnosticEventOnAbort, func(_ context.Context, e TelemetryAbortEvent) error {
		got = r90carryJSON(t, e.Steps)
		return nil
	})
	defer unsub()
	steps := []types.StepResult{{Request: types.StepRequest{Body: r90carryOpenAIBody(secret)}}}
	FireOnAbort(context.Background(), TelemetryAbortEvent{Steps: steps})
	if got == "" || strings.Contains(got, secret) || !strings.Contains(got, "[redacted]") {
		t.Errorf("abort steps on the channel: %s", got)
	}
	if !strings.Contains(r90carryJSON(t, steps), secret) {
		t.Error("caller steps mutated")
	}
}
