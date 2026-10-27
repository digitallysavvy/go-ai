package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from openai-responses-language-model.test.ts (TS #23635).

func promptCachePrompt() types.Prompt {
	return types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}}
}

func TestResponsesPromptCacheOptions12hAndPrewarm(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-6.1-sol")
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: promptCachePrompt(),
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"promptCacheOptions": map[string]interface{}{"mode": "implicit", "ttl": "12h", "prewarm": true},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body["prompt_cache_options"])
	if string(raw) != `{"mode":"implicit","prewarm":true,"ttl":"12h"}` {
		t.Fatalf("prompt_cache_options = %s", raw)
	}
}

func TestResponsesPromptCacheOptionsRejectsInvalid(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-6.1-sol")
	for name, opt := range map[string]map[string]interface{}{
		"ttl 24h":        {"ttl": "24h"},
		"mode manual":    {"mode": "manual"},
		"prewarm string": {"prewarm": "yes"},
	} {
		_, _, err := model.buildRequestBody(&provider.GenerateOptions{
			Prompt:          promptCachePrompt(),
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"promptCacheOptions": opt}},
		}, false)
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
	}
}

func TestResponsesPromptCacheBreakpoint12hTTL(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-6.1-sol")
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.TextContent{Text: "Stable prefix", ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"promptCacheBreakpoint": map[string]interface{}{"mode": "explicit", "ttl": "12h"}},
		}}},
	}}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body["input"])
	if !strings.Contains(string(raw), `"prompt_cache_breakpoint":{"mode":"explicit","ttl":"12h"}`) {
		t.Fatalf("input = %s", raw)
	}
}

const prewarmUsageJSON = `{"input_tokens":2048,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":2048,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":2048}},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2048}`

func checkPrewarmUsage(t *testing.T, u types.Usage) {
	t.Helper()
	d := u.InputDetails
	if u.InputTokens == nil || *u.InputTokens != 2048 || d == nil || *d.NoCacheTokens != 0 || *d.CacheReadTokens != 0 ||
		d.CacheWriteTokens == nil || *d.CacheWriteTokens != 2048 || u.OutputTokens == nil || *u.OutputTokens != 0 {
		t.Fatalf("usage = %+v / %+v", u, d)
	}
	// The TTL breakdown reaches usage.raw.
	details, _ := u.Raw["input_tokens_details"].(map[string]interface{})["cache_write_tokens_details"].(map[string]interface{})
	if details["ttl_12h_tokens"] != float64(2048) || details["ttl_30m_tokens"] != float64(0) {
		t.Fatalf("usage.raw = %#v", u.Raw)
	}
}

func TestResponsesPrewarmGenerateReturnsEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"resp_prewarm","object":"response","created_at":1741257730,"status":"completed","error":null,"incomplete_details":null,"model":"gpt-6.1-sol","output":[],"usage":%s}`, prewarmUsageJSON)
	}))
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-6.1-sol")
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: promptCachePrompt(),
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"promptCacheOptions": map[string]interface{}{"ttl": "12h", "prewarm": true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "" || len(res.Content) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("text=%q content=%#v warnings=%#v", res.Text, res.Content, res.Warnings)
	}
	checkPrewarmUsage(t, res.Usage)
}

func TestResponsesPrewarmStreamFinishesWithNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"type":"response.created","response":{"id":"resp_prewarm","created_at":1,"model":"gpt-6.1-sol"}}`+"\n\n")
		_, _ = fmt.Fprintf(w, `data: {"type":"response.completed","response":{"incomplete_details":null,"output":[],"usage":%s}}`+"\n\n", prewarmUsageJSON)
	}))
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-6.1-sol")
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: promptCachePrompt()})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeText, provider.ChunkTypeTextStart, provider.ChunkTypeReasoning:
			t.Fatalf("unexpected content chunk %v", c.Type)
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if finish == nil || finish.Usage == nil {
		t.Fatalf("finish = %#v", finish)
	}
	checkPrewarmUsage(t, *finish.Usage)
}

// zod's object schema strips unknown keys, so TS never sends them.
func TestResponsesPromptCacheOptionsDropsUnknownKeys(t *testing.T) {
	model := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-6.1-sol")
	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: promptCachePrompt(),
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"promptCacheOptions": map[string]interface{}{"ttl": "30m", "extra": 1},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body["prompt_cache_options"])
	if string(raw) != `{"ttl":"30m"}` {
		t.Fatalf("prompt_cache_options = %s", raw)
	}
}
