package ai

import "testing"

// TS processUIMessageStream passes no title for tool-input-error: a part it
// creates has none, and an existing part keeps its own title.
func TestR66ToolInputErrorIgnoresChunkTitle(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "tool-input-error", "toolCallId": "c1", "toolName": "w", "input": "x", "errorText": "bad", "title": "Weather"}, nil)
	s.apply(UIMessageChunk{"type": "tool-input-start", "toolCallId": "c2", "toolName": "w", "title": "Own"}, nil)
	s.apply(UIMessageChunk{"type": "tool-input-error", "toolCallId": "c2", "toolName": "w", "input": "x", "errorText": "bad", "title": "Other"}, nil)
	parts := uiParts(s.message)
	if len(parts) != 2 {
		t.Fatalf("parts = %v", parts)
	}
	if title, ok := asUIPartChunk(parts[0])["title"]; ok {
		t.Errorf("new part title = %v, want none", title)
	}
	if title := asUIPartChunk(parts[1])["title"]; title != "Own" {
		t.Errorf("existing part title = %v, want Own", title)
	}
}

// TS updates a data part whose id matches, an empty id included
// (`dataChunk.id != null`).
func TestR66DataPartEmptyIDUpdatesInPlace(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "data-x", "id": "", "data": 1}, nil)
	s.apply(UIMessageChunk{"type": "data-x", "id": "", "data": 2}, nil)
	parts := uiParts(s.message)
	if len(parts) != 1 || asUIPartChunk(parts[0])["data"] != 2 {
		t.Fatalf("parts = %v, want one data part with data 2", parts)
	}
}

// TS applies a start chunk's messageId when it is not null, an empty id
// included.
func TestR66StartChunkEmptyMessageIDApplied(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "start", "messageId": ""}, nil)
	if id, ok := s.message["id"]; !ok || id != "" {
		t.Fatalf("message id = %v, want empty", id)
	}
}
