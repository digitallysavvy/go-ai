package providerutils

import (
	"net/http"
	"strings"
	"testing"
)

// cr-r95-carry-to-integrator CR 1: TS JSON.parse reads a number beyond the
// double range as Infinity, and zod's object schemas strip it under an
// undeclared key, so the body or frame passes. encoding/json into
// interface{} failed the whole value as a JSON parse error.
func TestR95integValidatorsReadOutOfRangeNumbers(t *testing.T) {
	withExtra := func(body string) string {
		i := strings.LastIndex(body, "}")
		return body[:i] + `,"x":1e400,"y":-1e400,"z":1e-400}` + body[i+1:]
	}
	httpOK := func(v func(int, http.Header, []byte) error) func(string) error {
		return func(body string) error { return v(http.StatusOK, nil, []byte(body)) }
	}
	chatResp := `{"choices":[{"index":0,"message":{"role":"assistant","content":"a"},"finish_reason":"stop"}]}`
	for _, c := range []struct {
		name  string
		check func(string) error
		body  string
	}{
		{"NodeResponseValidator / MediaResponseValidator", httpOK(MediaResponseValidator("openai", SchemaOpenAIEmbedding)), `{"data":[{"embedding":[1]}]}`},
		{"MediaSchemaParser", httpOK(MediaSchemaParser(SchemaGatewayVideoEvent)), `{"type":"result","videos":[]}`},
		{"CheckPerplexityAgentChunk", func(b string) error {
			if msg, err := CheckPerplexityAgentChunk(b); msg != "" {
				return err
			}
			return nil
		}, `{"type":"response.output_text.delta","delta":"a","output_index":0}`},
		{"ValidateResponsesChunk", func(b string) error { return ValidateResponsesChunk([]byte(b)) }, `{"type":"response.output_text.delta","item_id":"m","delta":"a"}`},
		{"ValidateXAIResponsesChunk", func(b string) error { return ValidateXAIResponsesChunk([]byte(b)) }, `{"type":"response.output_text.delta","item_id":"i","output_index":0,"content_index":0,"delta":"a"}`},
		{"XAIResponsesResponseValidator", httpOK(XAIResponsesResponseValidator("xai")), `{"object":"response","output":[],"status":"completed"}`},
		{"ValidateChatResponse", func(b string) error { return ValidateChatResponse([]byte(b), ResponseKindOpenAI) }, chatResp},
		{"ResponsesResponseValidator", httpOK(ResponsesResponseValidator("openai")), `{"output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"output_text","text":"x","annotations":[]}]}]}`},
		{"HuggingFaceResponsesResponseValidator", httpOK(HuggingFaceResponsesResponseValidator("huggingface")), `{"id":"r","model":"m","object":"response","created_at":1,"status":"completed","error":null,"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto","tools":[],"temperature":1,"top_p":1,"output":[]}`},
		{"MistralConversationResponseValidator", httpOK(MistralConversationResponseValidator("mistral")), `{"conversation_id":"c","outputs":[]}`},
		{"MistralConversationStreamContentError", func(b string) error { return MistralConversationStreamContentError([]byte(b)) }, `{"type":"text","text":"a"}`},
		// MistralConversationUsageError is not here: mistralUsageSchema is
		// .catchall(z.json()), so zod rejects an undeclared key holding
		// Infinity (TestR96integMistralJSONValueSites).
	} {
		if err := c.check(c.body); err != nil {
			t.Errorf("%s: base body rejected: %v", c.name, err)
			continue
		}
		if err := c.check(withExtra(c.body)); err != nil {
			t.Errorf("%s: out-of-range numbers under undeclared keys rejected: %v", c.name, err)
		}
	}
}
