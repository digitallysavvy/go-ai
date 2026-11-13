package moonshot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from moonshotai-chat-language-model.test.ts "prompt cache writes"
// and convert-moonshotai-chat-usage.test.ts (TS #23887).

const cacheWriteUsageJSON = `{"prompt_tokens":22459,"completion_tokens":44,"total_tokens":22503,"prompt_tokens_details":{"cached_tokens":1024,"cache_write_tokens":21248}}`

func TestConvertMoonshotUsage_CacheWriteTokens(t *testing.T) {
	usage, err := ConvertMoonshotUsageRaw(json.RawMessage(`{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	d := usage.InputDetails
	if *usage.InputTokens != 100 || *d.NoCacheTokens != 20 || *d.CacheReadTokens != 20 || d.CacheWriteTokens == nil || *d.CacheWriteTokens != 60 {
		t.Fatalf("input details = %+v", d)
	}
}

func TestPromptCacheOptions(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3.1-pro")
	for name, tc := range map[string]struct {
		opts map[string]interface{}
		want interface{}
	}{
		"ttl":         {map[string]interface{}{"promptCacheOptions": map[string]interface{}{"ttl": "1h"}}, map[string]interface{}{"mode": "implicit", "ttl": "1h"}},
		"default":     {map[string]interface{}{"promptCacheOptions": map[string]interface{}{}}, map[string]interface{}{"mode": "implicit"}},
		"unset":       {nil, nil},
		"explicit 5m": {map[string]interface{}{"promptCacheOptions": map[string]interface{}{"mode": "implicit", "ttl": "5m"}}, map[string]interface{}{"mode": "implicit", "ttl": "5m"}},
	} {
		po := map[string]interface{}(nil)
		if tc.opts != nil {
			po = map[string]interface{}{"moonshotai": tc.opts}
		}
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"), ProviderOptions: po}, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, has := body["prompt_cache_options"]
		if tc.want == nil {
			if has {
				t.Errorf("%s: unexpected prompt_cache_options %v", name, got)
			}
			continue
		}
		b1, _ := json.Marshal(got)
		b2, _ := json.Marshal(tc.want)
		if string(b1) != string(b2) {
			t.Errorf("%s: prompt_cache_options = %s, want %s", name, b1, b2)
		}
	}
	for _, bad := range []map[string]interface{}{{"ttl": "2h"}, {"mode": "explicit"}} {
		_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{"moonshotai": map[string]interface{}{"promptCacheOptions": bad}}}, false)
		if err == nil {
			t.Errorf("%v: expected validation error", bad)
		}
	}
}

func TestCacheWriteTokensMetadata(t *testing.T) {
	var mu sync.Mutex
	withHeaders := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		h := withHeaders
		mu.Unlock()
		if h {
			w.Header().Set("msh-usage-cache-write-tokens-5m", "0")
			w.Header().Set("msh-usage-cache-write-tokens-1h", "21248")
		}
		if req["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"c","created":1,"model":"kimi-k3.1-pro","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":"stop"}],"usage":` + cacheWriteUsageJSON + "}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","created":1,"model":"kimi-k3.1-pro","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":` + cacheWriteUsageJSON + "}"))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "kimi-k3.1-pro")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}

	checkUsage := func(t *testing.T, u *types.Usage) {
		t.Helper()
		d := u.InputDetails
		if *u.InputTokens != 22459 || *d.NoCacheTokens != 187 || *d.CacheReadTokens != 1024 || d.CacheWriteTokens == nil || *d.CacheWriteTokens != 21248 {
			t.Fatalf("usage = %+v / %+v", u, d)
		}
	}
	wantMeta := func(t *testing.T, meta map[string]json.RawMessage, present bool) {
		t.Helper()
		raw, has := meta["cacheWriteTokens"]
		if has != present {
			t.Fatalf("cacheWriteTokens present = %v, want %v (%v)", has, present, meta)
		}
		if present && string(raw) != `{"ttl1h":21248,"ttl5m":0}` {
			t.Fatalf("cacheWriteTokens = %s", raw)
		}
	}

	t.Run("generate", func(t *testing.T) {
		res, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		checkUsage(t, &res.Usage)
		wantMeta(t, res.ProviderMetadata["moonshotai"].(map[string]json.RawMessage), true)
	})

	t.Run("stream", func(t *testing.T) {
		stream, err := model.DoStream(context.Background(), opts)
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
			if c.Type == provider.ChunkTypeFinish {
				finish = c
			}
		}
		if finish == nil {
			t.Fatal("no finish chunk")
		}
		checkUsage(t, finish.Usage)
		var meta map[string]map[string]json.RawMessage
		if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
			t.Fatal(err)
		}
		wantMeta(t, meta["moonshotai"], true)
	})

	t.Run("omitted without headers", func(t *testing.T) {
		mu.Lock()
		withHeaders = false
		mu.Unlock()
		res, err := model.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		wantMeta(t, res.ProviderMetadata["moonshotai"].(map[string]json.RawMessage), false)
	})
}
