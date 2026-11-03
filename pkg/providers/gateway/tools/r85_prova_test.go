package tools

import (
	"reflect"
	"testing"
)

// deepseek-search.test.ts / deepseek-fetch.test.ts (TS 2099d455e4): the
// config is the tool's args as given, and the shared gateway tool error keeps
// passthrough fields and accepts gateway_rate_limit / unknown.
func TestR85provaDeepSeekToolsArgsAndErrors(t *testing.T) {
	count, need, q := 5, true, "orig"
	s := NewDeepSeekSearch(DeepSeekSearchConfig{Query: "q", Count: &count, NeedContent: &need, OriQuestion: &q, Localization: "1.2.3.4"}).ToTool()
	want := map[string]interface{}{"query": "q", "count": 5, "needContent": true, "oriQuestion": "orig", "localization": "1.2.3.4"}
	if !reflect.DeepEqual(s.ProviderArgs, want) || s.ProviderID != "gateway.deepseek_search" || !s.ProviderExecuted {
		t.Fatalf("search tool = %#v", s)
	}
	f := NewDeepSeekFetch(DeepSeekFetchConfig{URL: "https://example.com"}).ToTool()
	if f.ProviderArgs["url"] != "https://example.com" || f.ProviderID != "gateway.deepseek_fetch" {
		t.Fatalf("fetch tool = %#v", f)
	}
	for name, tool := range map[string]map[string]interface{}{
		"deepseek_search": s.OutputSchema.(map[string]interface{}),
		"deepseek_fetch":  f.OutputSchema.(map[string]interface{}),
		"tako":            NewTakoSearch(TakoSearchConfig{}).ToTool().OutputSchema.(map[string]interface{}),
		"parallel":        NewParallelSearch(ParallelSearchConfig{}).ToTool().OutputSchema.(map[string]interface{}),
	} {
		for _, ok := range []string{
			`{"error":"gateway_rate_limit","message":"slow down","retryAfter":"5","statusText":"Too Many Requests","statusCode":429,"extra":1}`,
			`{"error":"unknown","message":"m"}`,
		} {
			if err := validateParallel(t, tool, ok); err != nil {
				t.Errorf("%s: %s rejected: %v", name, ok, err)
			}
		}
	}
	if err := validateParallel(t, s.OutputSchema.(map[string]interface{}), `{"results":[{"title":null,"url":"https://x","extra":true}]}`); err != nil {
		t.Errorf("search result rejected: %v", err)
	}
	if err := validateParallel(t, f.OutputSchema.(map[string]interface{}), `{"title":null}`); err == nil {
		t.Error("fetch output without content accepted")
	}
}
