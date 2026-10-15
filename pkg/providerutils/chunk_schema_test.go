package providerutils

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestValidateChatChunkSchemas(t *testing.T) {
	compat := ChatChunkShape{}
	openai := ChatChunkShape{Kind: ChunkKindOpenAI}
	completion := ChatChunkShape{Kind: ChunkKindOpenAICompletion}
	groq := ChatChunkShape{Kind: ChunkKindGroq}
	deepseek := ChatChunkShape{Kind: ChunkKindDeepSeek}
	moon := ChatChunkShape{Kind: ChunkKindMoonshot}
	mistral := ChatChunkShape{Kind: ChunkKindMistral}
	alibaba := ChatChunkShape{Kind: ChunkKindAlibaba}
	type tc struct {
		name  string
		in    string
		shape ChatChunkShape
		bad   bool
	}
	for _, c := range []tc{
		{"compat empty choices", `{"choices":[]}`, compat, false},
		{"compat no index ok", `{"choices":[{"delta":{"content":"a"}}]}`, compat, false},
		{"compat content array", `{"choices":[{"delta":{"content":[{"type":"text","text":"a"}]}}]}`, compat, false},
		{"compat content number", `{"choices":[{"delta":{"content":5}}]}`, compat, true},
		{"compat role empty", `{"choices":[{"delta":{"role":""}}]}`, compat, false},
		{"compat role tool", `{"choices":[{"delta":{"role":"tool"}}]}`, compat, true},
		{"compat created float", `{"created":1.5,"choices":[]}`, compat, false},
		{"compat created string", `{"created":"1","choices":[]}`, compat, true},
		{"compat usage tokens string", `{"choices":[],"usage":{"prompt_tokens":"1"}}`, compat, true},
		{"compat tool call no function", `{"choices":[{"delta":{"tool_calls":[{"index":0}]}}]}`, compat, true},
		{"compat default error", `{"error":{"message":"x","code":429}}`, compat, false},
		{"compat string error", `{"error":"boom"}`, compat, true},
		{"fireworks string error", `{"error":"boom"}`, ChatChunkShape{Error: ChunkErrorStringOrObject}, false},
		{"cerebras flat error", `{"message":"a","type":"b","param":"c","code":"d"}`, ChatChunkShape{Error: ChunkErrorCerebras}, false},
		{"no choices", `{}`, compat, true},
		{"openai needs index", `{"choices":[{"delta":{}}]}`, openai, true},
		{"openai index ok", `{"choices":[{"index":0,"delta":null}]}`, openai, false},
		{"openai role", `{"choices":[{"index":0,"delta":{"role":"user"}}]}`, openai, true},
		{"openai content array", `{"choices":[{"index":0,"delta":{"content":[]}}]}`, openai, true},
		{"openai tool index", `{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{}}]}}]}`, openai, true},
		{"openai annotation", `{"choices":[{"index":0,"delta":{"annotations":[{"type":"url_citation","url_citation":{"start_index":0,"end_index":1,"url":"u"}}]}}]}`, openai, true},
		{"openai usage detail string", `{"choices":[],"usage":{"completion_tokens_details":{"reasoning_tokens":"2"}}}`, openai, true},
		{"openai error", `{"error":{"message":"x"}}`, openai, false},
		{"completion text", `{"choices":[{"index":0,"text":"a"}]}`, completion, false},
		{"completion usage partial", `{"choices":[],"usage":{"prompt_tokens":1}}`, completion, true},
		{"completion text number", `{"choices":[{"index":0,"text":1}]}`, completion, true},
		{"groq ok", `{"choices":[{"index":0,"delta":{"content":"a"}}],"x_groq":{"usage":{"prompt_tokens":1}}}`, groq, false},
		{"groq x_groq string", `{"choices":[],"x_groq":"u"}`, groq, true},
		{"groq tool type null", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":null,"function":{}}]}}]}`, groq, true},
		{"groq error needs type", `{"error":{"message":"x"}}`, groq, true},
		{"deepseek no index ok", `{"choices":[{"delta":{"reasoning_content":"r"}}]}`, deepseek, false},
		{"deepseek logprob bytes missing", `{"choices":[{"logprobs":{"content":[{"token":"a","logprob":1,"top_logprobs":[]}]}}]}`, deepseek, true},
		{"deepseek object literal", `{"object":"x","choices":[]}`, deepseek, true},
		{"moon choice usage", `{"choices":[{"usage":{"cached_tokens":"1"}}]}`, moon, true},
		{"moon error code number", `{"error":{"message":"x","code":5}}`, moon, true},
		{"mistral ok", `{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"a"}]}}]}`, mistral, false},
		{"mistral unknown part", `{"choices":[{"index":0,"delta":{"content":[{"type":"audio"}]}}]}`, mistral, true},
		{"mistral usage partial", `{"choices":[],"usage":{"prompt_tokens":1}}`, mistral, true},
		{"mistral error frame", `{"error":{"message":"x"}}`, mistral, true},
		{"mistral role", `{"choices":[{"index":0,"delta":{"role":null}}]}`, mistral, true},
		{"alibaba ok", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0}]}}]}`, alibaba, false},
		{"alibaba delta required", `{"choices":[{"index":0}]}`, alibaba, true},
		{"alibaba usage", `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, alibaba, true},
	} {
		if err := ValidateChatChunk([]byte(c.in), c.shape); (err != nil) != c.bad {
			t.Errorf("%s: %s: err = %v, want error %v", c.name, c.in, err, c.bad)
		}
	}
}

func TestChunkParseFailureTypes(t *testing.T) {
	msg, err := CheckChatChunk(`{"choices":[{"delta":{"role":"x"}}]}`, ChatChunkShape{Kind: ChunkKindOpenAI})
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) || msg != err.Error() || !strings.Contains(msg, "Type validation failed") {
		t.Fatalf("schema failure: msg=%q err=%T", msg, err)
	}
	msg, err = CheckChatChunk(`{"choices":`, ChatChunkShape{})
	var je *providererrors.JSONParseError
	if !errors.As(err, &je) || msg != err.Error() || !strings.Contains(msg, "\nError message: ") {
		t.Fatalf("json failure: msg=%q err=%T", msg, err)
	}
	if msg, err = CheckChatChunk(`{"choices":[]}`, ChatChunkShape{}); err != nil || msg != "" {
		t.Fatalf("valid frame: %q %v", msg, err)
	}
}
