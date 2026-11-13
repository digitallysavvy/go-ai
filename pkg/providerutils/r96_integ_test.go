package providerutils

import (
	"net/http"
	"testing"
)

func r96integHTTPOK(v func(int, http.Header, []byte) error) func(string) error {
	return func(body string) error { return v(http.StatusOK, nil, []byte(body)) }
}

// carry / provg CR (round 96): mistralUsageSchema is .catchall(z.json()) and
// the conversation info / arguments records hold z.json(): a non-finite
// number there (JSON.parse of 1e400) fails like zod, a finite one passes.
func TestR96integMistralJSONValueSites(t *testing.T) {
	resp := func(usageExtra string) string {
		return `{"id":"i","created":1,"model":"m","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"a"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2` + usageExtra + `}}`
	}
	conv := func(info, args string) string {
		return `{"conversation_id":"c","outputs":[{"type":"tool.execution","id":"t","name":"web_search","arguments":"{}","info":` + info + `},` +
			`{"type":"function.call","tool_call_id":"f","name":"n","arguments":` + args + `}]}`
	}
	for _, c := range []struct {
		name    string
		check   func(string) error
		ok, bad string
	}{
		{"chat usage extra", func(b string) error { return ValidateChatResponse([]byte(b), ResponseKindMistral) }, resp(`,"x":[1]`), resp(`,"x":[1e400]`)},
		{"chat completion details extra", func(b string) error { return ValidateChatResponse([]byte(b), ResponseKindMistral) },
			resp(`,"completion_tokens_details":{"y":2}`), resp(`,"completion_tokens_details":{"y":-1e400}`)},
		{"conversation info", r96integHTTPOK(MistralConversationResponseValidator("mistral")), conv(`{"a":1}`, `"{}"`), conv(`{"a":1e400}`, `"{}"`)},
		{"conversation arguments", r96integHTTPOK(MistralConversationResponseValidator("mistral")), conv(`null`, `{"a":{"b":1}}`), conv(`null`, `{"a":{"b":1e400}}`)},
		{"conversation usage", func(b string) error { return MistralConversationUsageError([]byte(b)) }, `{"z":"s"}`, `{"z":1e400}`},
	} {
		if err := c.check(c.ok); err != nil {
			t.Errorf("%s: finite value: err = %v", c.name, err)
		}
		if err := c.check(c.bad); err == nil {
			t.Errorf("%s: 1e400: err = nil, want the z.json() failure", c.name)
		}
	}
}

// provg CR §2: the gateway speech / transcription usage is
// z.optional(z.record(z.string(), z.json())): 1e400 there fails.
func TestR96integGatewayMediaUsageJSONValue(t *testing.T) {
	for _, c := range []struct {
		schema  MediaResponseSchema
		ok, bad string
	}{
		{SchemaGatewaySpeech, `{"audio":"a","usage":{"k":[1]}}`, `{"audio":"a","usage":{"k":[1e400]}}`},
		{SchemaGatewayTranscription, `{"text":"t","usage":{"seconds":2}}`, `{"text":"t","usage":{"seconds":1e400}}`},
	} {
		v := r96integHTTPOK(MediaResponseValidator("gateway", c.schema))
		if err := v(c.ok); err != nil {
			t.Errorf("%s: finite: err = %v", c.schema, err)
		}
		if err := v(c.bad); err == nil {
			t.Errorf("%s: 1e400: err = nil, want Invalid JSON response", c.schema)
		}
	}
}
