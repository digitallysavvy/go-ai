package ai

import (
	"encoding/json"
	"sync"
	"testing"
)

// r76streamMeta is a typed message metadata value, as a Go caller writes it
// (TS: a plain object).
type r76streamMeta struct {
	CreatedAt string `json:"createdAt,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
}

// r76streamScore is a named number type: JSON number, not an object.
type r76streamScore int

// TS processUIMessageStream merges message metadata with
// mergeObjects(existing, incoming), which treats every object alike. A Go
// value that encodes as a JSON object (a struct, a typed map,
// json.RawMessage) is merged like a map[string]interface{}, also nested.
func TestR76StreamMetadataMergeTypedObjects(t *testing.T) {
	for _, c := range []struct {
		name     string
		existing interface{}
		incoming interface{}
		want     string
	}{
		{"struct then map", r76streamMeta{CreatedAt: "t0"}, map[string]interface{}{"model": "m"}, `{"createdAt":"t0","model":"m"}`},
		{"map then struct", map[string]interface{}{"model": "m"}, r76streamMeta{Tokens: 5}, `{"model":"m","tokens":5}`},
		{"struct then struct", r76streamMeta{CreatedAt: "t0"}, &r76streamMeta{Tokens: 5}, `{"createdAt":"t0","tokens":5}`},
		{"typed map then map", map[string]string{"a": "1"}, map[string]interface{}{"b": "2"}, `{"a":"1","b":"2"}`},
		{"raw then map", json.RawMessage(`{"a":{"x":1}}`), map[string]interface{}{"a": map[string]interface{}{"y": 2.0}}, `{"a":{"x":1,"y":2}}`},
		{"nested struct", map[string]interface{}{"usage": r76streamMeta{CreatedAt: "t0"}}, map[string]interface{}{"usage": map[string]interface{}{"tokens": 3.0}}, `{"usage":{"createdAt":"t0","tokens":3}}`},
		{"struct then named number", r76streamMeta{CreatedAt: "t0"}, r76streamScore(4), `{"createdAt":"t0"}`},
		{"struct then string", r76streamMeta{CreatedAt: "t0"}, "s", `"s"`},
		{"nil pointer then map", (*r76streamMeta)(nil), map[string]interface{}{"a": 1.0}, `{"a":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newUIMessageCallbackState(nil, "m")
			s.message["metadata"] = c.existing
			s.mergeMetadata(c.incoming)
			r75streamAssertJSON(t, "metadata", s.message["metadata"], c.want)
		})
	}

	// Through CreateUIMessageStreamWithOptions: the continued assistant
	// message has typed metadata, the start chunk adds a key, the finish
	// chunk another; onEnd's response message carries all three.
	var mu sync.Mutex
	var endMessage interface{}
	_, errs := r75streamRunUIChunks(t, []UIMessageChunk{
		{"type": "start", "messageMetadata": map[string]interface{}{"model": "m"}},
		{"type": "finish", "messageMetadata": r76streamMeta{Tokens: 7}},
	}, UIMessageStreamOptions{
		OriginalMessages: []UIMessageChunk{
			{"id": "u1", "role": "user", "parts": []interface{}{}},
			{"id": "a1", "role": "assistant", "parts": []interface{}{}, "metadata": r76streamMeta{CreatedAt: "t0"}},
		},
		OnEnd: func(event map[string]interface{}) {
			mu.Lock()
			defer mu.Unlock()
			endMessage = event["responseMessage"]
		},
	})
	if len(errs) != 0 {
		t.Fatalf("stream errors: %v", errs)
	}
	mu.Lock()
	defer mu.Unlock()
	msg, ok := endMessage.(UIMessageChunk)
	if !ok {
		t.Fatalf("response message is %T", endMessage)
	}
	r75streamAssertJSON(t, "onEnd metadata", msg["metadata"], `{"createdAt":"t0","model":"m","tokens":7}`)
}

// cr-r76-carry-to-stream.md: the WorkflowAgent option exists (honored by gen's
// tool executor and the legacy agent loop; behavior tests are gen's/carry's).
func TestR76StreamInternalClientToolResultsWithoutCallMetadataField(t *testing.T) {
	opts := InternalOptions{ClientToolResultsWithoutCallMetadata: true}
	if !opts.ClientToolResultsWithoutCallMetadata {
		t.Fatal("field not set")
	}
}
