package providerutils

import (
	"net/http"
	"testing"
)

// cr-r88-provb-to-provg 1 (TS 6b97088662): Responses message content is a
// discriminated union of output_text and refusal parts.
func TestR88integResponsesRefusalContentMember(t *testing.T) {
	validate := ResponsesResponseValidator("openai")
	ok := `{"output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"refusal","refusal":"No."},{"type":"output_text","text":"x","annotations":[]}]}]}`
	if err := validate(http.StatusOK, nil, []byte(ok)); err != nil {
		t.Fatalf("refusal part rejected: %v", err)
	}
	for _, bad := range []string{
		`{"output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"refusal"}]}]}`,
		`{"output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"refusal","refusal":5}]}]}`,
		`{"output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"other","text":"x"}]}]}`,
	} {
		if err := validate(http.StatusOK, nil, []byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// cr-r88-provb-to-provg 2 and cr-r88-provb-to-integrator 2 (TS f6dbf04b5d,
// 646a7e3c2e): native chat message / delta `refusal` is a nullish string and
// logprobs.refusal has the content entry shape; openai-compatible stays
// lenient (TS types refusal as unknown there).
func TestR88integChatRefusalSchemas(t *testing.T) {
	resp := func(msg, logprobs string) []byte {
		return []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":null` + msg + `}` + logprobs + `,"finish_reason":"stop"}]}`)
	}
	for _, tc := range []struct {
		body []byte
		ok   bool
	}{
		{resp(`,"refusal":null`, ""), true},
		{resp(`,"refusal":"No."`, `,"logprobs":{"refusal":[{"token":"No","logprob":-1,"top_logprobs":[]}]}`), true},
		{resp(`,"refusal":5`, ""), false},
		{resp("", `,"logprobs":{"refusal":[{"token":"a"}]}`), false},
	} {
		err := ValidateChatResponse(tc.body, ResponseKindOpenAI)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.body, err, tc.ok)
		}
	}
	if err := ValidateChatResponse(resp(`,"refusal":false`, ""), ResponseKindOpenAICompatible); err != nil {
		t.Errorf("compat rejected a non-string refusal: %v", err)
	}

	chunk := func(delta, logprobs string) string {
		return `{"choices":[{"index":0,"delta":{"role":"assistant"` + delta + `}` + logprobs + `,"finish_reason":null}]}`
	}
	native := ChatChunkShape{Kind: ChunkKindOpenAI}
	for _, tc := range []struct {
		data string
		ok   bool
	}{
		{chunk(`,"refusal":"No"`, `,"logprobs":{"refusal":[{"token":"No","logprob":-1,"top_logprobs":[]}]}`), true},
		{chunk(`,"refusal":null`, ""), true},
		{chunk(`,"refusal":5`, ""), false},
		{chunk("", `,"logprobs":{"refusal":[{"token":"a"}]}`), false},
	} {
		msg, err := CheckChatChunk(tc.data, native)
		if (msg == "" && err == nil) != tc.ok {
			t.Errorf("%s: msg %q err %v, want ok=%v", tc.data, msg, err, tc.ok)
		}
	}
	if msg, err := CheckChatChunk(chunk(`,"refusal":5`, ""), ChatChunkShape{Kind: ChunkKindOpenAICompatible}); msg != "" || err != nil {
		t.Errorf("compat chunk rejected a non-string refusal: %q %v", msg, err)
	}
}

// cr-r88-provb-to-integrator 1 (TS 6b97088662): response.refusal.delta and
// .done are modeled Responses chunk types.
func TestR88integResponsesRefusalChunks(t *testing.T) {
	for _, tc := range []struct {
		data string
		ok   bool
	}{
		{`{"type":"response.refusal.delta","item_id":"m","output_index":0,"content_index":0,"delta":"No"}`, true},
		{`{"type":"response.refusal.done","item_id":"m","refusal":"No."}`, true},
		{`{"type":"response.refusal.delta","item_id":"m","delta":5}`, false},
		{`{"type":"response.refusal.done","item_id":"m"}`, false},
		{`{"type":"response.refusal.delta","delta":"x"}`, false},
	} {
		msg, err := CheckResponsesChunk(tc.data)
		if (msg == "" && err == nil) != tc.ok {
			t.Errorf("%s: msg %q err %v, want ok=%v", tc.data, msg, err, tc.ok)
		}
	}
}

// prova r88 open item, class sweep (TS safeParseJSON): the shared nested
// error schema does not read a body with a __proto__ key.
func TestR88integNestedErrorMessageSecureParse(t *testing.T) {
	if msg, code := NestedErrorMessage(400, []byte(`{"error":{"message":"m","code":7}}`)); msg != "m" || code != "7" {
		t.Fatalf("clean body: %q %q", msg, code)
	}
	if msg, code := NestedErrorMessage(400, []byte(`{"error":{"message":"m","code":7},"__proto__":{}}`)); msg != "Bad Request" || code != "" {
		t.Fatalf("__proto__ body: %q %q", msg, code)
	}
	if msg, _ := NestedErrorMessage(400, []byte(`{"error":{"message":"m","code":7,"constructor":{"prototype":{}}}}`)); msg != "Bad Request" {
		t.Fatalf("constructor.prototype body: %q", msg)
	}
}
