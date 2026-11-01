package ai

import (
	"encoding/json"
	"testing"
)

// Coordinator ruling (r75, stream open item 1): TS processUIMessageStream
// merges message metadata with mergeObjects(existing, incoming). An object
// existing value with a number or boolean incoming value keeps the object
// ({...base} with no own keys to copy). A string or array incoming value
// replaces it in Go (known difference "Message metadata merge with
// non-object values"), as does any value over a non-object existing one.
func TestR75Integ_MessageMetadataMergeNonObject(t *testing.T) {
	object := map[string]interface{}{"a": 1.0, "nested": map[string]interface{}{"b": true}}
	for _, c := range []struct {
		name     string
		existing interface{}
		incoming interface{}
		want     interface{}
	}{
		{"object then number", object, 42.0, object},
		{"object then int", object, 7, object},
		{"object then json.Number", object, json.Number("3"), object},
		{"object then boolean", object, false, object},
		{"object then string", object, "s", "s"},
		{"object then array", object, []interface{}{1.0}, []interface{}{1.0}},
		{"number then object", 1.0, map[string]interface{}{"x": 1.0}, map[string]interface{}{"x": 1.0}},
		{"number then number", 1.0, 2.0, 2.0},
		{"object then object", object, map[string]interface{}{"nested": map[string]interface{}{"c": 1.0}},
			map[string]interface{}{"a": 1.0, "nested": map[string]interface{}{"b": true, "c": 1.0}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newUIMessageCallbackState(nil, "m")
			s.mergeMetadata(c.existing)
			s.mergeMetadata(c.incoming)
			if got, want := r75integJSON(t, s.message["metadata"]), r75integJSON(t, c.want); got != want {
				t.Fatalf("metadata = %s, want %s", got, want)
			}
		})
	}

	// Through the chunk path: a message-metadata chunk with a number after a
	// start chunk with object metadata keeps the object.
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "start", "messageMetadata": map[string]interface{}{"k": "v"}}, nil)
	s.apply(UIMessageChunk{"type": "message-metadata", "messageMetadata": 5.0}, nil)
	if got := r75integJSON(t, s.message["metadata"]); got != `{"k":"v"}` {
		t.Fatalf("chunk path metadata = %s, want the start object kept", got)
	}
}

func r75integJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
