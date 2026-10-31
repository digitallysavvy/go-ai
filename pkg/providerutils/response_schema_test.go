package providerutils

import "testing"

func TestValidateChatResponse(t *testing.T) {
	cases := []struct {
		name string
		kind ChatResponseKind
		body string
		ok   bool
	}{
		{"openai minimal", ResponseKindOpenAI, `{"choices":[{"index":0,"message":{"content":"x"}}]}`, true},
		{"openai null usage and extras", ResponseKindOpenAI, `{"id":null,"choices":[{"index":0,"message":{"role":"assistant","content":null,"refusal":null},"finish_reason":null}],"usage":null,"system_fingerprint":"f"}`, true},
		{"openai choice without index", ResponseKindOpenAI, `{"choices":[{"message":{"content":"x"}}]}`, false},
		{"openai tool call without type", ResponseKindOpenAI, `{"choices":[{"index":0,"message":{"tool_calls":[{"id":"1","function":{"name":"n","arguments":"{}"}}]}}]}`, false},
		{"compat minimal", ResponseKindOpenAICompatible, `{"choices":[{"message":{"content":"x"}}]}`, true},
		{"compat tool call without type", ResponseKindOpenAICompatible, `{"choices":[{"message":{"tool_calls":[{"id":"1","function":{"name":"n","arguments":"{}"}}]}}]}`, true},
		{"compat array content", ResponseKindOpenAICompatible, `{"choices":[{"message":{"content":[{"type":"text","text":"x"}]}}]}`, true},
		{"compat choices missing", ResponseKindOpenAICompatible, `{"id":"x"}`, false},
		{"completion", ResponseKindOpenAICompletion, `{"choices":[{"text":"x","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, true},
		{"completion null finish reason", ResponseKindOpenAICompletion, `{"choices":[{"text":"x","finish_reason":null}]}`, false},
		{"compat completion usage needs total", ResponseKindCompatCompletion, `{"choices":[{"text":"x","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2}}`, false},
		{"groq", ResponseKindGroq, `{"choices":[{"index":0,"message":{"content":"x"},"finish_reason":"stop"}]}`, true},
		{"groq without index", ResponseKindGroq, `{"choices":[{"message":{"content":"x"}}]}`, false},
		{"deepseek", ResponseKindDeepSeek, `{"choices":[{"message":{"content":"x"}}]}`, true},
		{"deepseek wrong object", ResponseKindDeepSeek, `{"object":"other","choices":[]}`, false},
		{"moonshot", ResponseKindMoonshot, `{"choices":[{"message":{"content":"x"}}]}`, true},
		{"mistral needs object and usage", ResponseKindMistral, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"x"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, true},
		{"mistral without usage", ResponseKindMistral, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"x"}}]}`, false},
		{"alibaba", ResponseKindAlibaba, `{"choices":[{"index":0,"message":{"content":"x"}}]}`, true},
		{"alibaba usage needs totals", ResponseKindAlibaba, `{"choices":[],"usage":{"prompt_tokens":1}}`, false},
		{"cohere", ResponseKindCohere, `{"message":{"role":"assistant","content":[{"type":"text","text":"x"}]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":1,"output_tokens":1}}}`, true},
		{"cohere without finish reason", ResponseKindCohere, `{"message":{"role":"assistant"},"usage":{"tokens":{"input_tokens":1,"output_tokens":1}}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateChatResponse([]byte(c.body), c.kind)
			if (err == nil) != c.ok {
				t.Fatalf("ValidateChatResponse err = %v, want ok=%v", err, c.ok)
			}
		})
	}
}
