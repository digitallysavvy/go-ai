package providerutils

import (
	"math"
	"strings"
	"testing"
)

// TS openaiResponsesChunkSchema validates response.completed / incomplete /
// failed `response.prompt_cache_diagnostics` (5a645e92d2). Node, OpenAI
// Responses doStream over these frames: {type:"miss"} and null finish with
// stop; {type:5}, {reason:"x"} and a string cache_missed_tokens give a
// TypeValidationError error part and finish with error.
func TestR97carryResponsesChunkPromptCacheDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		diag string
		ok   bool
	}{
		{`{"type":"miss"}`, true},
		{`null`, true},
		{`{"type":"miss","reason":null,"cache_missed_tokens":3,"comparison_reusable_tokens":null}`, true},
		{`{"type":5}`, false},
		{`{"reason":"x"}`, false},
		{`{"type":"miss","cache_missed_tokens":"1"}`, false},
	} {
		for _, frame := range []string{
			`{"type":"response.completed","response":{"prompt_cache_diagnostics":` + tc.diag + `,"usage":{"input_tokens":1,"output_tokens":1}}}`,
			`{"type":"response.incomplete","response":{"prompt_cache_diagnostics":` + tc.diag + `}}`,
			`{"type":"response.failed","sequence_number":1,"response":{"prompt_cache_diagnostics":` + tc.diag + `}}`,
		} {
			err := ValidateResponsesChunk([]byte(frame))
			if (err == nil) != tc.ok {
				t.Errorf("ValidateResponsesChunk(%s) = %v, want ok=%v", frame, err, tc.ok)
			}
		}
	}
}

// UnmarshalChunkPlain is UnmarshalChunk for a TS `response.json()` body: a
// __proto__ key is an ordinary property, and fractional counts still decode
// into integer fields.
func TestR97carryUnmarshalChunkPlain(t *testing.T) {
	body := []byte(`{"__proto__":{"x":1},"count":2.0,"value":"v"}`)
	var out struct {
		Count int    `json:"count"`
		Value string `json:"value"`
	}
	if err := UnmarshalChunkPlain(body, &out); err != nil || out.Count != 2 || out.Value != "v" {
		t.Fatalf("UnmarshalChunkPlain = %+v, %v", out, err)
	}
	if err := UnmarshalChunk(body, &out); err == nil {
		t.Fatal("UnmarshalChunk accepted a __proto__ key")
	}
}

// z.number() accepts a count past the int64 range (1e20); the integer field
// takes the nearest int64 instead of failing the frame. A number beyond the
// double range (Infinity) still fails, as zod rejects it.
func TestR97carryUnmarshalChunkClampsPastInt64(t *testing.T) {
	var out struct {
		A int64 `json:"a"`
		B int64 `json:"b"`
	}
	if err := UnmarshalChunk([]byte(`{"a":1e20,"b":-1e20}`), &out); err != nil || out.A != math.MaxInt64 || out.B != math.MinInt64 {
		t.Fatalf("UnmarshalChunk = %+v, %v", out, err)
	}
	if err := UnmarshalChunk([]byte(`{"a":1e400}`), &out); err == nil {
		t.Fatal("UnmarshalChunk accepted 1e400 in an integer field")
	}
}

// zod reports catchall issues in the object's key order, and the chunk
// error names the first one: for two out-of-range usage extras that is the
// first on the wire (z_x), not the first sorted (b_y).
func TestR97carryChunkIssueInJSKeyOrder(t *testing.T) {
	frame := `{"choices":[{"delta":{"content":"x"},"index":0}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"z_x":1e400,"b_y":1e400}}`
	msg, err := CheckChatChunk(frame, ChatChunkShape{Kind: ChunkKindMistral})
	if err == nil || !strings.Contains(msg, `"path":["usage","z_x"]`) {
		t.Fatalf("CheckChatChunk = %q, want the issue for usage.z_x", msg)
	}
}
