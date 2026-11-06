package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86provbShellEnvironmentGoTypedArgs: TS-shaped openai.shell args built
// with Go-typed values ([]map[string]interface{} skills and domain secrets,
// a types.ProviderReference) map like their JSON form; the skills and
// secrets were dropped and the reference failed to resolve.
func TestR86provbShellEnvironmentGoTypedArgs(t *testing.T) {
	typed := map[string]interface{}{"environment": map[string]interface{}{
		"type": "containerAuto",
		"networkPolicy": map[string]interface{}{
			"type":           "allowlist",
			"allowedDomains": []string{"example.com"},
			"domainSecrets":  []map[string]interface{}{{"domain": "example.com", "name": "TOKEN", "value": "v"}},
		},
		"skills": []map[string]interface{}{
			{"type": "skillReference", "providerReference": types.ProviderReference{"openai": "skill_1"}},
			{"type": "inline", "name": "n", "description": "d", "source": map[string]interface{}{"type": "base64", "mediaType": "application/zip", "data": "AA=="}},
		},
	}}
	var jsonForm map[string]interface{}
	b, _ := json.Marshal(typed)
	if err := json.Unmarshal(b, &jsonForm); err != nil {
		t.Fatal(err)
	}
	encode := func(args map[string]interface{}) string {
		env, err := shellEnvironmentFromArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(env)
		return string(out)
	}
	got, want := encode(typed), encode(jsonForm)
	if got != want {
		t.Fatalf("typed = %s\njson  = %s", got, want)
	}
	const wire = `{"type":"container_auto","network_policy":{"type":"allowlist","allowed_domains":["example.com"],"domain_secrets":[{"domain":"example.com","name":"TOKEN","value":"v"}]},"skills":[{"type":"skill_reference","skill_id":"skill_1"},{"type":"inline","name":"n","description":"d","source":{"type":"base64","media_type":"application/zip","data":"AA=="}}]}`
	if got != wire {
		t.Fatalf("env = %s\nwant  %s", got, wire)
	}
}

// TestR86provbComputerCallGoTypedActions: a replayed computer call whose
// Arguments hold Go-typed lists ([]map[string]interface{}) sends the same
// computer_call as its JSON form; the actions and safety checks were dropped.
func TestR86provbComputerCallGoTypedActions(t *testing.T) {
	typed := map[string]interface{}{
		"status":              "completed",
		"pendingSafetyChecks": []map[string]interface{}{{"id": "s1", "code": "c", "message": "m"}},
		"actions":             []map[string]interface{}{{"type": "click", "button": "left", "x": 1, "y": 2}},
	}
	var jsonForm map[string]interface{}
	b, _ := json.Marshal(typed)
	if err := json.Unmarshal(b, &jsonForm); err != nil {
		t.Fatal(err)
	}
	encode := func(args map[string]interface{}) string {
		input, err := convertCall(t, "openai.computer", args)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(input)
		return string(out)
	}
	got, want := encode(typed), encode(jsonForm)
	if got != want {
		t.Fatalf("typed = %s\njson  = %s", got, want)
	}
	if !strings.Contains(got, `"button":"left"`) || !strings.Contains(got, `"id":"s1"`) {
		t.Fatalf("computer_call = %s", got)
	}
}

// r86provbArgCases: raw Go tool input text -> TS 30548ee7f9 arguments
// computed under Node (see prompt.r86provbToolInputCases): serialize is
// convert-to-openai-responses-input.ts serializeToolCallArguments, stringify
// is convert-to-xai-responses-input.ts JSON.stringify(part.input).
var r86provbArgCases = []struct{ raw, serialize, stringify string }{
	{`{"a":1}`, `{"a":1}`, `{"a":1}`},
	{`[1,2]`, `[1,2]`, `[1,2]`},
	{`"{\"a\":1}"`, `{"a":1}`, `"{\"a\":1}"`},
	{`"[1]"`, `[1]`, `"[1]"`},
	{`"abc"`, `"abc"`, `"abc"`},
	{`abc`, `"abc"`, `"abc"`},
	{`{"a":`, `"{\"a\":"`, `"{\"a\":"`},
	{`123`, `123`, `123`},
	{`"\"x\""`, `"x"`, `"\"x\""`},
}

func r86provbItemArguments(t *testing.T, items []interface{}) string {
	t.Helper()
	b, _ := json.Marshal(items)
	var decoded []map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, item := range decoded {
		if args, ok := item["arguments"].(string); ok {
			return args
		}
	}
	t.Fatalf("no function_call in %s", b)
	return ""
}

func TestR86provbReplayedToolCallArgumentsMatchNode(t *testing.T) {
	for _, tc := range r86provbArgCases {
		legacy := types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "f", RawArguments: tc.raw}}}}}
		parts := types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "f", Input: tc.raw}}}}}
		for name, p := range map[string]types.Prompt{"ToolCalls": legacy, "content": parts} {
			items, _, err := ConvertPromptToInputWithOptions(p, "system", ConvertOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := r86provbItemArguments(t, items); got != tc.serialize {
				t.Errorf("openai %s %s: arguments = %s, want %s", name, tc.raw, got, tc.serialize)
			}
			items, _, err = ConvertPromptToXAIInput(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := r86provbItemArguments(t, items); got != tc.stringify {
				t.Errorf("xai %s %s: arguments = %s, want %s", name, tc.raw, got, tc.stringify)
			}
		}
	}
}
