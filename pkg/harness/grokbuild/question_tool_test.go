package grokbuild

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var nativeRequestJSON = []byte(`{
	"sessionId": "session-1",
	"toolCallId": "call-1",
	"mode": "default",
	"questions": [
		{
			"question": "Which framework?",
			"options": [
				{"label": "React", "description": "React framework"},
				{"label": "Vue", "description": "Vue framework"}
			],
			"multi_select": false
		}
	]
}`)

func toAny(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Mirrors "translates the native request".
func TestFromNativeRequest(t *testing.T) {
	part := AskUserQuestions.FromNativeRequest(toAny(t, nativeRequestJSON), nil)
	if part == nil {
		t.Fatal("expected a tool-call part")
	}
	if part.ToolCallID != "call-1" || part.ToolName != "askUserQuestions" || part.NativeName != "ask_user_question" || part.ProviderExecuted {
		t.Errorf("part = %+v", part)
	}
	wantInput := `{"allowPartialAnswers":true,"questions":[{"id":"question-1","question":"Which framework?","options":[{"id":"option-1","label":"React","description":"React framework"},{"id":"option-2","label":"Vue","description":"Vue framework"}],"allowMultiple":false,"allowFreeForm":true}]}`
	if part.Input != wantInput {
		t.Errorf("input = %s\nwant   = %s", part.Input, wantInput)
	}
}

// Mirrors "translates selected and freeform answers".
func TestToNativeResponse(t *testing.T) {
	outputValue := map[string]any{
		"action": "answered",
		"answers": map[string]any{
			"question-1": map[string]any{
				"optionIds": []any{"option-1"},
				"freeform":  "Svelte",
			},
		},
	}
	toolResult := types.ToolResultContent{
		ToolCallID: "call-1",
		ToolName:   "askUserQuestions",
		Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: outputValue},
	}

	got := AskUserQuestions.ToNativeResponse(toAny(t, nativeRequestJSON), toolResult)
	want := map[string]any{
		"outcome": "accepted",
		"answers": map[string][]string{"Which framework?": {"React", "Other"}},
		"annotations": map[string]map[string]string{
			"Which framework?": {"notes": "Svelte"},
		},
	}
	gotMap, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map[string]any", got)
	}
	if gotMap["outcome"] != want["outcome"] {
		t.Errorf("outcome = %v", gotMap["outcome"])
	}
	if !reflect.DeepEqual(gotMap["answers"], want["answers"]) {
		t.Errorf("answers = %v, want %v", gotMap["answers"], want["answers"])
	}
	if !reflect.DeepEqual(gotMap["annotations"], want["annotations"]) {
		t.Errorf("annotations = %v, want %v", gotMap["annotations"], want["annotations"])
	}
}

func TestToNativeResponse_Cancelled(t *testing.T) {
	toolResult := types.ToolResultContent{
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]any{"action": "cancelled"}},
	}
	got := AskUserQuestions.ToNativeResponse(toAny(t, nativeRequestJSON), toolResult)
	if !reflect.DeepEqual(got, map[string]any{"outcome": "cancelled"}) {
		t.Errorf("got %v", got)
	}
}

func TestMatchesNativeRequest(t *testing.T) {
	other := []byte(`{"sessionId":"session-2","toolCallId":"call-2","mode":"default","questions":[{"question":"Which framework?","options":[{"label":"React","description":"React framework"},{"label":"Vue","description":"Vue framework"}],"multi_select":false}]}`)
	if !AskUserQuestions.MatchesNativeRequest(toAny(t, nativeRequestJSON), toAny(t, other)) {
		t.Error("expected requests with identical questions/mode to match")
	}
	different := []byte(`{"sessionId":"session-3","toolCallId":"call-3","mode":"plan","questions":[]}`)
	if AskUserQuestions.MatchesNativeRequest(toAny(t, nativeRequestJSON), toAny(t, different)) {
		t.Error("expected requests with a different mode not to match")
	}
}

func TestFromNativeRequest_InvalidRequest(t *testing.T) {
	part := AskUserQuestions.FromNativeRequest(toAny(t, []byte(`{"not":"valid"}`)), nil)
	if part != nil {
		t.Errorf("expected nil for an invalid native request, got %+v", part)
	}
}

// zod requires every field; json.Unmarshal alone would accept these.
func TestFromNativeRequest_MissingRequiredFields(t *testing.T) {
	for name, raw := range map[string]string{
		"no toolCallId": `{"sessionId":"s","mode":"default","questions":[]}`,
		"no questions":  `{"sessionId":"s","toolCallId":"c","mode":"default"}`,
		"no options":    `{"sessionId":"s","toolCallId":"c","mode":"default","questions":[{"question":"q"}]}`,
		"no label":      `{"sessionId":"s","toolCallId":"c","mode":"plan","questions":[{"question":"q","options":[{"description":"d"}]}]}`,
		"bad multi":     `{"sessionId":"s","toolCallId":"c","mode":"plan","questions":[{"question":"q","options":[],"multi_select":"yes"}]}`,
	} {
		if part := AskUserQuestions.FromNativeRequest(toAny(t, []byte(raw)), nil); part != nil {
			t.Errorf("%s: expected nil, got %+v", name, part)
		}
	}
	ok := `{"sessionId":"s","toolCallId":"c","mode":"plan","questions":[{"question":"q","options":[{"label":"l","description":"d","preview":null}],"multi_select":null}]}`
	if AskUserQuestions.FromNativeRequest(toAny(t, []byte(ok)), nil) == nil {
		t.Error("nullable preview/multi_select must be accepted")
	}
}

// TS flatMap yields [] (not null) for an answer with no selected options.
func TestToNativeResponse_EmptyAnswerIsEmptyArray(t *testing.T) {
	toolResult := types.ToolResultContent{
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]any{
			"action":  "answered",
			"answers": map[string]any{"question-1": map[string]any{"optionIds": []any{}}},
		}},
	}
	got := AskUserQuestions.ToNativeResponse(toAny(t, nativeRequestJSON), toolResult)
	data, _ := json.Marshal(got)
	if want := `{"answers":{"Which framework?":[]},"outcome":"accepted"}`; string(data) != want {
		t.Errorf("got %s want %s", data, want)
	}
}

// TS positionalIdIndex uses JS Number(), so "option-2.0", "option- 2" and
// "option-0x2" all resolve to the second option.
func TestPositionalIDIndex_JSNumberSemantics(t *testing.T) {
	for id, want := range map[string]int{"option-1": 0, "option-2.0": 1, "option- 2": 1, "option-0x2": 1, "option-1e0": 0, "option-3": 2} {
		got, ok := positionalIDIndex(id, "option-")
		if !ok || got != want {
			t.Errorf("%q = %d,%v want %d", id, got, ok, want)
		}
	}
	for _, id := range []string{"option-", "option-0", "option--1", "option-1.5", "option-abc", "option-1_0", "option-Infinity", "x-1"} {
		if got, ok := positionalIDIndex(id, "option-"); ok {
			t.Errorf("%q accepted as %d", id, got)
		}
	}
}
