package providerutils

import (
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// A raw chunk value beyond the double range reads as +Inf (JSON.parse), a
// fractional integer in the same frame still truncates, and the value
// re-serializes as null (JSON.stringify).
func TestR96carryUnmarshalChunkOutOfRangeRawValue(t *testing.T) {
	var chunk struct {
		Index int                    `json:"index"`
		Raw   map[string]interface{} `json:"raw"`
	}
	if err := UnmarshalChunk([]byte(`{"index":2.0,"raw":{"x":1e400,"y":[-1e400]}}`), &chunk); err != nil {
		t.Fatalf("UnmarshalChunk: %v", err)
	}
	if chunk.Index != 2 {
		t.Errorf("index = %d", chunk.Index)
	}
	if f, _ := chunk.Raw["x"].(float64); !math.IsInf(f, 1) {
		t.Errorf("raw.x = %#v", chunk.Raw["x"])
	}
	out, err := jsonstr.Marshal(chunk.Raw)
	if err != nil || string(out) != `{"x":null,"y":[null]}` {
		t.Errorf("re-serialized = %s, %v", out, err)
	}
}

// TS usage schemas hold their values to JSON values (OpenAI Responses
// intersects jsonObjectSchema, xAI and Mistral .catchall(z.json())), so a
// number JSON.parse read as Infinity under an undeclared usage key fails
// the frame; finite extra keys pass.
func TestR96carryUsageSchemasRejectInfinity(t *testing.T) {
	respUsage := func(extra string) string {
		return `{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2` + extra + `}}}`
	}
	xaiUsage := func(extra string) string {
		return `{"type":"response.completed","response":{"object":"response","output":[],"status":"completed","usage":{"input_tokens":1,"output_tokens":2` + extra + `}}}`
	}
	mistral := func(extra string) string {
		return `{"choices":[{"delta":{},"index":0}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3` + extra + `}}`
	}
	cases := []struct {
		name  string
		check func(string) error
		data  func(string) string
	}{
		{"openai responses", r96carryValidateResponses, respUsage},
		{"xai responses", func(s string) error { return ValidateXAIResponsesChunk([]byte(s)) }, xaiUsage},
		{"mistral", func(s string) error { return ValidateChatChunk([]byte(s), ChatChunkShape{Kind: ChunkKindMistral}) }, mistral},
	}
	for _, tc := range cases {
		if err := tc.check(tc.data(`,"extra":{"n":[1]}`)); err != nil {
			t.Errorf("%s: finite extra key failed: %v", tc.name, err)
		}
		if err := tc.check(tc.data(`,"extra":{"n":[1e400]}`)); err == nil {
			t.Errorf("%s: an infinite extra usage value passed", tc.name)
		}
	}
	if err := ValidateResponsesChunk([]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"tool_search_output","id":"t","execution":"server","call_id":null,"status":"completed","tools":[{"x":1e400}]}}`)); err == nil {
		t.Error("tool_search_output tools took an infinite value")
	}
}

func r96carryValidateResponses(s string) error { return ValidateResponsesChunk([]byte(s)) }

// xaiResponsesResponseSchema declares incomplete_details: {reason} nullish.
func TestR96carryXAIResponseIncompleteDetails(t *testing.T) {
	base := `{"type":"response.completed","response":{"object":"response","output":[],"status":"incomplete","incomplete_details":%s}}`
	for _, ok := range []string{`null`, `{"reason":"max_output_tokens"}`} {
		if err := ValidateXAIResponsesChunk([]byte(strings.Replace(base, "%s", ok, 1))); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	if err := ValidateXAIResponsesChunk([]byte(strings.Replace(base, "%s", `"x"`, 1))); err == nil {
		t.Error("a string incomplete_details passed")
	}
}

// TS parseJsonEventStream (safeParseJSON) turns a frame with a forbidden
// prototype key into a JSONParseError; a frame without one decodes.
func TestR96carryChunkForbiddenPrototype(t *testing.T) {
	frame := `{"choices":[],"__proto__":{"polluted":true}}`
	want := "JSON parsing failed: Text: " + frame + ".\nError message: Object contains forbidden prototype property"
	var v struct {
		Choices []interface{} `json:"choices"`
	}
	if err := UnmarshalChunk([]byte(frame), &v); err == nil || err.Error() != want {
		t.Errorf("UnmarshalChunk = %v, want %q", err, want)
	}
	if msg, err := CheckChatChunk(frame, ChatChunkShape{}); msg != want || !providererrors.IsJSONParseError(err) {
		t.Errorf("CheckChatChunk = %q, %T", msg, err)
	}
	if err := UnmarshalChunk([]byte(`{"choices":[],"constructor":"x"}`), &v); err != nil {
		t.Errorf("accepted frame failed: %v", err)
	}
}

// cr from provb: a cause of securejson.ErrForbiddenPrototype (or a
// JSONParseError from the caller's decode) is TS's JSONParseError.
func TestR96carryChunkParseFailureForbiddenCause(t *testing.T) {
	frame := `{"type":"x"}`
	want := "JSON parsing failed: Text: " + frame + ".\nError message: Object contains forbidden prototype property"
	for _, cause := range []error{securejson.ErrForbiddenPrototype, providererrors.NewJSONParseError("inner", securejson.ErrForbiddenPrototype)} {
		msg, err := ChunkParseFailure(frame, cause)
		if msg != want || !providererrors.IsJSONParseError(err) || err.Error() != want {
			t.Errorf("ChunkParseFailure(%v) = %q, %T", cause, msg, err)
		}
	}
}
