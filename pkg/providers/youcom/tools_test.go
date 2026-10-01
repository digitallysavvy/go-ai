package youcom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestYouSearchFactory(t *testing.T) {
	tool := YouSearch()

	if tool.Name != ToolIDSearch {
		t.Fatalf("tool name = %q, want %q", tool.Name, ToolIDSearch)
	}
	if tool.ProviderExecuted {
		t.Fatal("ProviderExecuted = true, want false for TS-parity local tool")
	}
	if tool.ProviderName != ProviderName {
		t.Fatalf("ProviderName = %q, want %q", tool.ProviderName, ProviderName)
	}
	if tool.Execute == nil {
		t.Fatal("Execute is nil")
	}
	if tool.ProviderMetadata[ProviderKey] == nil {
		t.Fatalf("ProviderMetadata missing %q key: %#v", ProviderKey, tool.ProviderMetadata)
	}
}

func TestYouSearchExecuteMissingAPIKey(t *testing.T) {
	t.Setenv("YDC_API_KEY", "")
	tool := YouSearch(YouToolsConfig{})
	_, err := tool.Execute(t.Context(), map[string]interface{}{"query": "test"}, types.ToolExecutionOptions{})
	if err == nil || err.Error() != "YDC_API_KEY is required. Set it in environment variables or pass it in config." {
		t.Fatalf("error = %v, want TS missing API key error", err)
	}
}

func TestYouSearchExecuteReturnsRawAPIResponse(t *testing.T) {
	var gotKey string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"web":[{"title":"A","url":"https://a.test","description":"desc"}]}}`))
	}))
	defer server.Close()

	tool := YouSearch(YouToolsConfig{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	result, err := tool.Execute(t.Context(), map[string]interface{}{"query": "golang", "count": 3}, types.ToolExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "test-key" {
		t.Fatalf("X-API-Key = %q, want test-key", gotKey)
	}
	if gotPath != "/search" {
		t.Fatalf("path = %q, want /search", gotPath)
	}
	raw, ok := result.(map[string]interface{})
	if !ok || raw["results"] == nil {
		t.Fatalf("result = %#v, want raw API response map", result)
	}
}

func TestYouSearchRejectsIncludeAndExcludeDomains(t *testing.T) {
	tool := YouSearch(YouToolsConfig{APIKey: "test-key"})
	_, err := tool.Execute(t.Context(), map[string]interface{}{
		"query":           "golang",
		"include_domains": []interface{}{"go.dev"},
		"exclude_domains": []interface{}{"example.com"},
	}, types.ToolExecutionOptions{})
	if err == nil || err.Error() != "Cannot combine include_domains and exclude_domains" {
		t.Fatalf("error = %v, want include/exclude validation error", err)
	}
}

func TestYouSearchValidatesTSInputSchema(t *testing.T) {
	tool := YouSearch(YouToolsConfig{APIKey: "test-key"})

	cases := []map[string]interface{}{
		{"count": 1},
		{"query": "golang", "count": 101},
		{"query": "golang", "offset": 10},
		{"query": "golang", "safesearch": "safe"},
		{"query": "golang", "livecrawl_formats": []interface{}{"pdf"}},
		{"query": "golang", "count": 1.5},
	}
	for _, input := range cases {
		if _, err := tool.Execute(t.Context(), input, types.ToolExecutionOptions{}); err == nil {
			t.Fatalf("Execute(%#v) error = nil, want schema validation error", input)
		}
	}
}

func TestYouResearchValidatesTSInputSchema(t *testing.T) {
	tool := YouResearch(YouToolsConfig{APIKey: "test-key"})

	cases := []map[string]interface{}{
		{},
		{"input": ""},
		{"input": "go", "research_effort": "quick"},
	}
	for _, input := range cases {
		if _, err := tool.Execute(t.Context(), input, types.ToolExecutionOptions{}); err == nil {
			t.Fatalf("Execute(%#v) error = nil, want schema validation error", input)
		}
	}
}

func TestYouContentsValidatesTSInputSchema(t *testing.T) {
	tool := YouContents(YouToolsConfig{APIKey: "test-key"})

	cases := []map[string]interface{}{
		{},
		{"urls": []interface{}{}},
		{"urls": []interface{}{"not a url"}},
		{"urls": []interface{}{"https://example.com"}, "formats": []interface{}{"text"}},
		{"urls": []interface{}{"https://example.com"}, "format": "metadata"},
		{"urls": []interface{}{"https://example.com"}, "crawl_timeout": 0},
	}
	for _, input := range cases {
		if _, err := tool.Execute(t.Context(), input, types.ToolExecutionOptions{}); err == nil {
			t.Fatalf("Execute(%#v) error = nil, want schema validation error", input)
		}
	}
}

func TestYouContentsNormalizesFormatToFormats(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"url":"https://example.com","markdown":"ok"}]`))
	}))
	defer server.Close()

	tool := YouContents(YouToolsConfig{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	_, err := tool.Execute(t.Context(), map[string]interface{}{
		"urls":   []interface{}{"https://example.com"},
		"format": "html",
	}, types.ToolExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["format"]; ok {
		t.Fatalf("format should not be sent in request body: %#v", gotBody)
	}
	formats, ok := gotBody["formats"].([]interface{})
	if !ok || len(formats) != 1 || formats[0] != "html" {
		t.Fatalf("formats = %#v, want [html]", gotBody["formats"])
	}
}

func TestYouContentsDefaultsFormatsToMarkdown(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"url":"https://example.com","markdown":"ok"}]`))
	}))
	defer server.Close()

	tool := YouContents(YouToolsConfig{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	_, err := tool.Execute(t.Context(), map[string]interface{}{
		"urls": []interface{}{"https://example.com"},
	}, types.ToolExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	formats, ok := gotBody["formats"].([]interface{})
	if !ok || len(formats) != 1 || formats[0] != "markdown" {
		t.Fatalf("formats = %#v, want [markdown]", gotBody["formats"])
	}
}

func TestYouAPIErrorField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"limit reached"}`))
	}))
	defer server.Close()

	tool := YouSearch(YouToolsConfig{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	_, err := tool.Execute(t.Context(), map[string]interface{}{"query": "golang"}, types.ToolExecutionOptions{})
	if err == nil || err.Error() != "You.com API Error: limit reached" {
		t.Fatalf("error = %v, want API error field", err)
	}
}

func TestYouResearchAndContentsFactories(t *testing.T) {
	research := YouResearch()
	if research.ProviderExecuted || research.Name != ToolIDResearch {
		t.Fatalf("bad TS-parity research tool: %#v", research)
	}
	contents := YouContents()
	if contents.ProviderExecuted || contents.Name != ToolIDContents {
		t.Fatalf("bad TS-parity contents tool: %#v", contents)
	}
}

func TestSchemasMatchYouComAPIFields(t *testing.T) {
	search := YouSearch().Parameters.(map[string]interface{})
	searchProps := search["properties"].(map[string]interface{})
	for _, field := range []string{"query", "count", "freshness", "offset", "country", "safesearch", "livecrawl", "livecrawl_formats", "language", "include_domains", "exclude_domains", "crawl_timeout"} {
		if _, ok := searchProps[field]; !ok {
			t.Fatalf("search schema missing %q", field)
		}
	}
	if _, ok := searchProps["safe_search"]; ok {
		t.Fatal("search schema should use safesearch, not safe_search")
	}

	research := YouResearch().Parameters.(map[string]interface{})
	researchProps := research["properties"].(map[string]interface{})
	if _, ok := researchProps["input"]; !ok {
		t.Fatal("research schema missing input")
	}
	if _, ok := researchProps["research_effort"]; !ok {
		t.Fatal("research schema missing research_effort")
	}
	if _, ok := researchProps["query"]; ok {
		t.Fatal("research schema should use input, not query")
	}

	contents := YouContents().Parameters.(map[string]interface{})
	contentsProps := contents["properties"].(map[string]interface{})
	for _, field := range []string{"urls", "formats", "format", "crawl_timeout"} {
		if _, ok := contentsProps[field]; !ok {
			t.Fatalf("contents schema missing %q", field)
		}
	}
}
