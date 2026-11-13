package gateway

import (
	"encoding/json"
	"testing"
)

// TS passes the Gateway usage through untouched: a fractional count is a
// number like any other, so the typed counts read it (truncated) instead of
// losing the whole inputTokens object.
func TestR97provgV4UsageFractionalCounts(t *testing.T) {
	var u v4Usage
	if err := json.Unmarshal([]byte(`{"inputTokens":{"total":10.5,"noCache":4.5},"outputTokens":{"total":2,"text":2},"raw":{"prompt_tokens":10.5}}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.usage.InputTokens == nil || *u.usage.InputTokens != 10 {
		t.Fatalf("input tokens = %v, want 10", u.usage.InputTokens)
	}
	if u.usage.InputDetails == nil || u.usage.InputDetails.NoCacheTokens == nil || *u.usage.InputDetails.NoCacheTokens != 4 {
		t.Fatalf("input details = %+v", u.usage.InputDetails)
	}
	if u.usage.Raw["prompt_tokens"] != 10.5 {
		t.Fatalf("raw = %#v", u.usage.Raw)
	}
	var flat v4Usage
	if err := json.Unmarshal([]byte(`{"promptTokens":3.5,"completionTokens":1}`), &flat); err != nil {
		t.Fatal(err)
	}
	if flat.usage.InputTokens == nil || *flat.usage.InputTokens != 3 || flat.usage.TotalTokens == nil || *flat.usage.TotalTokens != 4 {
		t.Fatalf("flat usage = %+v", flat.usage)
	}
}

// TS parseServerEvent returns a JSON null message as the event and the
// session's handleServerEvent throws reading event.type (cr-r97-provb-to-provg 3).
func TestR97provgRealtimeParseNullEvent(t *testing.T) {
	_, err := (&RealtimeModel{}).ParseServerEvent(json.RawMessage(` null `))
	if err == nil || err.Error() != "Cannot read properties of null (reading 'type')" {
		t.Fatalf("err = %v, want the TypeError", err)
	}
	if events, err := (&RealtimeModel{}).ParseServerEvent(json.RawMessage(`{"type":"session-created"}`)); err != nil || len(events) != 1 {
		t.Fatalf("events = %v, %v", events, err)
	}
}
