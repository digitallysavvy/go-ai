package perplexity

import "testing"

// TS createJsonResponseHandler(perplexityAgentResponseSchema) rejects a
// usage object without the required token counts, a message content part
// without a type and an error object without a message.
func TestR46_AgentResponseSchemaRejectsMalformedMembers(t *testing.T) {
	cases := map[string]string{
		"usage without counts":      `{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[],"usage":{}}`,
		"content part without type": `{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[{"type":"message","content":[{"text":"x"}]}]}`,
		"error without message":     `{"id":"r","created_at":1,"model":"m","object":"response","status":"failed","output":[],"error":{"code":"c"}}`,
	}
	for name, body := range cases {
		if err := validatePerplexityAgentResponse([]byte(body)); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
	ok := `{"id":"r","created_at":1,"model":"m","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"x"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`
	if err := validatePerplexityAgentResponse([]byte(ok)); err != nil {
		t.Errorf("valid body rejected: %v", err)
	}
}
