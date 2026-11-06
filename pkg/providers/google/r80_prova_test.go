package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r80provaRunInteractions is TS runTransform (build-google-interactions-
// stream-transform.test.ts): the events go through the Interactions stream
// with generateId () => 'test-id', and the parts come back in the TS shape.
func r80provaRunInteractions(t *testing.T, events ...string) ([]map[string]interface{}, []*provider.StreamChunk) {
	t.Helper()
	var body strings.Builder
	for _, e := range events {
		body.WriteString("data: " + e + "\n\n")
	}
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body.String())), "", nil, nil, nil, 0)
	s.(*interactionsStream).generateID = func() string { return "test-id" }
	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}
	var parts []map[string]interface{}
	for _, p := range tsfixture.StreamToTS(chunks) {
		b, _ := json.Marshal(p)
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		parts = append(parts, m)
	}
	return parts, chunks
}

func r80provaJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func r80provaContains(parts []map[string]interface{}, want interface{}) bool {
	for _, p := range parts {
		if reflect.DeepEqual(interface{}(p), want) {
			return true
		}
	}
	return false
}

func r80provaFilter(parts []map[string]interface{}, prefix string) []interface{} {
	out := []interface{}{}
	for _, p := range parts {
		if strings.HasPrefix(p["type"].(string), prefix) {
			out = append(out, p)
		}
	}
	return out
}

// TS "emits processing steps as custom parts".
func TestR80provaInteractionsProcessingSteps(t *testing.T) {
	parts, _ := r80provaRunInteractions(t,
		`{"event_type":"interaction.created","interaction":{"id":"interaction-1","status":"in_progress"}}`,
		`{"event_type":"step.start","index":0,"step":{"type":"processing_call","id":"processing-1","signature":"call-signature"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"step.start","index":1,"step":{"type":"processing_result","call_id":"processing-1","signature":"result-signature"}}`,
		`{"event_type":"step.stop","index":1}`,
		`{"event_type":"interaction.completed","interaction":{"id":"interaction-1","status":"completed"}}`,
	)
	for _, want := range []string{
		`{"type":"custom","kind":"google.processing_call","providerMetadata":{"google":{"signature":"call-signature","interactionId":"interaction-1","processingId":"processing-1"}}}`,
		`{"type":"custom","kind":"google.processing_result","providerMetadata":{"google":{"signature":"result-signature","interactionId":"interaction-1","processingCallId":"processing-1"}}}`,
	} {
		if !r80provaContains(parts, r80provaJSON(t, want)) {
			t.Errorf("missing %s in %v", want, parts)
		}
	}
}

// TS "uses the generated block ID when a function call ID is empty".
func TestR80provaInteractionsEmptyFunctionCallID(t *testing.T) {
	parts, _ := r80provaRunInteractions(t,
		`{"event_type":"interaction.created","interaction":{"id":"interaction-1","status":"in_progress"}}`,
		`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"","name":"get_weather","arguments":{}}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","id":"","arguments":"{}"}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"interaction.completed","interaction":{"id":"interaction-1","status":"completed"}}`,
	)
	want := r80provaJSON(t, `[
		{"type":"tool-input-start","id":"interaction-1:0","toolName":"get_weather"},
		{"type":"tool-input-delta","id":"interaction-1:0","delta":"{}"},
		{"type":"tool-input-end","id":"interaction-1:0"},
		{"type":"tool-call","toolCallId":"interaction-1:0","toolName":"get_weather","input":"{}","providerMetadata":{"google":{"interactionId":"interaction-1"}}}]`)
	if got := r80provaFilter(parts, "tool-"); !reflect.DeepEqual(interface{}(got), want) {
		b, _ := json.Marshal(got)
		t.Fatalf("tool parts = %s", b)
	}
}

// TS "preserves generated block IDs when built-in tool deltas contain empty IDs".
func TestR80provaInteractionsBuiltinToolEmptyIDs(t *testing.T) {
	parts, _ := r80provaRunInteractions(t,
		`{"event_type":"interaction.created","interaction":{"id":"interaction-1","status":"in_progress"}}`,
		`{"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"","arguments":{}}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","id":"","arguments":{"queries":["weather"]}}}`,
		`{"event_type":"step.stop","index":0}`,
		`{"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"","result":null}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","call_id":"","result":[]}}`,
		`{"event_type":"step.stop","index":1}`,
		`{"event_type":"interaction.completed","interaction":{"id":"interaction-1","status":"completed"}}`,
	)
	want := r80provaJSON(t, `[
		{"type":"tool-call","toolCallId":"interaction-1:0","toolName":"google_search","input":"{\"queries\":[\"weather\"]}","providerExecuted":true},
		{"type":"tool-result","toolCallId":"interaction-1:1","toolName":"google_search","result":[]}]`)
	if got := r80provaFilter(parts, "tool-"); !reflect.DeepEqual(interface{}(got), want) {
		b, _ := json.Marshal(got)
		t.Fatalf("tool parts = %s", b)
	}
}

// TS "preserves the event type, code, and raw error event".
func TestR80provaInteractionsErrorEvent(t *testing.T) {
	ev := `{"event_type":"error","event_id":"event-error","error":{"code":"429","message":"Rate limit reached"}}`
	parts, chunks := r80provaRunInteractions(t, ev)
	var errChunk *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			errChunk = c
		}
	}
	if errChunk == nil {
		t.Fatalf("no error part: %v", parts)
	}
	var se *providererrors.StreamProviderError
	if !errors.As(errChunk.Err, &se) {
		t.Fatalf("error = %#v, want a StreamProviderError", errChunk.Err)
	}
	if se.Message != "Rate limit reached" || se.Type != "error" || se.Code != "429" || !reflect.DeepEqual(se.Data, r80provaJSON(t, ev)) {
		t.Fatalf("error = %#v", se)
	}
	last := parts[len(parts)-1]
	if last["type"] != "finish" || last["finishReason"].(map[string]interface{})["unified"] != "error" {
		t.Fatalf("last part = %v", last)
	}
}

// TS "handles the last status %s as %s" and "preserves an explicit
// completion event without a status".
func TestR80provaInteractionsLastStatus(t *testing.T) {
	for _, tc := range []struct{ status, unified string }{
		{"", "interrupted"},
		{"in_progress", "interrupted"},
		{"requires_action", "tool-calls"},
		{"completed", "stop"},
		{"cancelled", "other"},
		{"unknown_status", "other"},
	} {
		events := []string{
			`{"event_type":"interaction.created","interaction":{"id":"i1","status":"in_progress"}}`,
			`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
			`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"Partial answer"}}`,
		}
		if tc.status != "" {
			events = append(events, `{"event_type":"interaction.status_update","status":"`+tc.status+`"}`)
		}
		parts, _ := r80provaRunInteractions(t, events...)
		var finishes []map[string]interface{}
		text := false
		for _, p := range parts {
			switch p["type"] {
			case "error":
				t.Errorf("%q: error part %v", tc.status, p)
			case "finish":
				finishes = append(finishes, p)
			case "text-delta":
				text = text || p["delta"] == "Partial answer"
			}
		}
		wantRaw := interface{}(tc.status)
		if tc.unified == "interrupted" {
			wantRaw = nil
		}
		if !text || len(finishes) != 1 || !reflect.DeepEqual(finishes[0]["finishReason"], map[string]interface{}{"unified": tc.unified, "raw": wantRaw}) {
			t.Errorf("%q: text=%v finishes=%v", tc.status, text, finishes)
		}
	}
	parts, _ := r80provaRunInteractions(t, `{"event_type":"interaction.completed","interaction":{"id":"i1"}}`)
	last := parts[len(parts)-1]
	if last["type"] != "finish" || !reflect.DeepEqual(last["finishReason"], map[string]interface{}{"unified": "other", "raw": nil}) {
		t.Fatalf("completion without status: %v", parts)
	}
}
