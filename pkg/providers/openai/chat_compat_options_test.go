package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS OpenAICompatibleChatLanguageModel reads <providerName> options (not
// "openai"), applies user/reasoningEffort/textVerbosity and passes unknown
// keys straight into the request body.
func TestChatCompatReadsProviderNamedOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "my-host", ChatProviderName: "my-host.chat"})
	m, _ := p.ChatModel("m")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: chatTestPrompt(),
		ProviderOptions: map[string]interface{}{"myHost": map[string]interface{}{
			"user": "u1", "reasoningEffort": "low", "textVerbosity": "high", "top_k": 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["user"] != "u1" || body["reasoning_effort"] != "low" || body["verbosity"] != "high" {
		t.Fatalf("named options not applied: %v", body)
	}
	if body["top_k"] != float64(5) {
		t.Fatalf("unknown option not passed through: %v", body)
	}
	if _, leaked := body["reasoningEffort"]; leaked {
		t.Fatalf("known option leaked as raw key: %v", body)
	}
}

// TS OpenAICompatibleChatLanguageModel passes unmodeled provider options to
// the wire verbatim (camelCase); it does not also derive the OpenAI-only
// snake_case fields, and has no reasoning-model/service-tier capability logic.
func TestChatCompatDoesNotConvertUnmodeledOptions(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "my-host", ChatProviderName: "my-host.chat"})
	m, _ := p.ChatModel("o3-mini")
	temp := 0.5
	maxTokens := 20
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:      chatTestPrompt(),
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		ProviderOptions: map[string]interface{}{"myHost": map[string]interface{}{
			"logprobs": true, "promptCacheKey": "k1", "serviceTier": "flex", "maxCompletionTokens": 5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"top_logprobs", "prompt_cache_key", "service_tier", "max_completion_tokens"} {
		if _, ok := body[k]; ok {
			t.Errorf("unexpected snake_case field %s in %v", k, body)
		}
	}
	if body["promptCacheKey"] != "k1" || body["serviceTier"] != "flex" || body["logprobs"] != true {
		t.Errorf("passthrough keys missing: %v", body)
	}
	if body["temperature"] != 0.5 || body["max_tokens"] != float64(20) {
		t.Errorf("reasoning-model gating applied to compat wrapper: %v", body)
	}
}

// TS compat ends an active reasoning block before the first tool-call delta.
func TestChatCompatStreamEndsReasoningBeforeToolCalls(t *testing.T) {
	sse := `data: {"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"think"}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"f","arguments":"{}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	s := newOpenAIStreamWithMetadata(io.NopCloser(strings.NewReader(sse)), false, "my.chat", nil)
	s.compat = true
	var seq []provider.ChunkType
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		seq = append(seq, c.Type)
	}
	endIdx, startIdx := -1, -1
	for i, ty := range seq {
		if ty == provider.ChunkTypeReasoningEnd && endIdx < 0 {
			endIdx = i
		}
		if ty == provider.ChunkTypeToolInputStart && startIdx < 0 {
			startIdx = i
		}
	}
	if endIdx < 0 || startIdx < 0 || endIdx > startIdx {
		t.Fatalf("reasoning-end must precede tool-input-start: %v", seq)
	}
}

// TS compat flush(): no finish_reason at all -> error part + error finish.
func TestChatCompatStreamWithoutFinishReasonIsError(t *testing.T) {
	sse := `data: {"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}

data: [DONE]

`
	s := newOpenAIStreamWithMetadata(io.NopCloser(strings.NewReader(sse)), false, "my.chat", nil)
	s.compat = true
	var sawErr bool
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError && c.Text == "Response stream ended without a finish reason." {
			sawErr = true
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if !sawErr || finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("sawErr=%v finish=%+v", sawErr, finish)
	}
}

// TS: a response without `usage` yields the null usage (unknown counts), and
// the compat accounting ignores cache_write_tokens (noCache = prompt - cached).
func TestChatUsageNullAndCompatAccounting(t *testing.T) {
	serve := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
	}
	noUsage := serve(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`)
	defer noUsage.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: noUsage.URL}).ChatModel("gpt-4o")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v, want null usage", res.Usage)
	}

	withUsage := serve(`{"choices":[{"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":10,"cache_write_tokens":20},"extra":1}}`)
	defer withUsage.Close()
	for _, compat := range []bool{false, true} {
		cfg := Config{APIKey: "k", BaseURL: withUsage.URL}
		if compat {
			cfg.OpenAICompatible, cfg.Name, cfg.ChatProviderName = true, "my-host", "my-host.chat"
		}
		m, _ := New(cfg).ChatModel("gpt-4o")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
		if err != nil {
			t.Fatal(err)
		}
		d := res.Usage.InputDetails
		wantNoCache := int64(70)
		if compat {
			wantNoCache = 90
		}
		if d == nil || *d.NoCacheTokens != wantNoCache || (compat && d.CacheWriteTokens != nil) || (!compat && d.CacheWriteTokens == nil) {
			t.Fatalf("compat=%v input details = %+v", compat, d)
		}
		if _, hasExtra := res.Usage.Raw["extra"]; hasExtra != compat {
			t.Fatalf("compat=%v raw=%v", compat, res.Usage.Raw)
		}
	}
}
