package providerutils

import "testing"

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
