package tools

import (
	"testing"
)

func TestAnthropicWebFetch20260318Serialization(t *testing.T) {
	maxUses := 3
	maxTokens := 8192
	useCache := false
	tool := WebFetch20260318(WebFetch20260318Config{
		MaxUses:           &maxUses,
		AllowedDomains:    []string{"docs.anthropic.com"},
		BlockedDomains:    []string{"ads.example.com"},
		Citations:         &WebFetchCitations{Enabled: true},
		MaxContentTokens:  &maxTokens,
		UseCache:          &useCache,
		ResponseInclusion: "excluded",
	})

	if tool.Name != "anthropic.web_fetch_20260318" {
		t.Errorf("tool.Name = %q, want %q", tool.Name, "anthropic.web_fetch_20260318")
	}
	if !tool.ProviderExecuted {
		t.Error("tool.ProviderExecuted should be true")
	}

	opts, ok := tool.ProviderOptions.(*webFetch20260318Opts)
	if !ok {
		t.Fatalf("tool.ProviderOptions is not *webFetch20260318Opts")
	}

	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_fetch_20260318" {
		t.Errorf("apiMap[type] = %v, want web_fetch_20260318", apiMap["type"])
	}
	if apiMap["name"] != "web_fetch" {
		t.Errorf("apiMap[name] = %v, want web_fetch", apiMap["name"])
	}
	if apiMap["max_uses"] != 3 {
		t.Errorf("apiMap[max_uses] = %v, want 3", apiMap["max_uses"])
	}
	citations, ok := apiMap["citations"].(map[string]interface{})
	if !ok || citations["enabled"] != true {
		t.Errorf("apiMap[citations] = %v, want {enabled:true}", apiMap["citations"])
	}
	if apiMap["max_content_tokens"] != 8192 {
		t.Errorf("apiMap[max_content_tokens] = %v, want 8192", apiMap["max_content_tokens"])
	}
	if apiMap["use_cache"] != false {
		t.Errorf("apiMap[use_cache] = %v, want false", apiMap["use_cache"])
	}
	if apiMap["response_inclusion"] != "excluded" {
		t.Errorf("apiMap[response_inclusion] = %v, want excluded", apiMap["response_inclusion"])
	}
}

func TestAnthropicWebFetch20260318SerializationEmpty(t *testing.T) {
	tool := WebFetch20260318(WebFetch20260318Config{})
	opts := tool.ProviderOptions.(*webFetch20260318Opts)
	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_fetch_20260318" {
		t.Errorf("apiMap[type] = %v, want web_fetch_20260318", apiMap["type"])
	}
	for _, key := range []string{"max_uses", "citations", "max_content_tokens", "use_cache", "response_inclusion", "allowed_domains", "blocked_domains"} {
		if _, has := apiMap[key]; has {
			t.Errorf("apiMap should not have %q when unset", key)
		}
	}
}

func TestAnthropicWebFetch20260318ResultParsing(t *testing.T) {
	input := `{
		"type": "web_fetch_result",
		"url": "https://example.com/page",
		"content": {
			"type": "document",
			"title": "Example",
			"source": {
				"type": "text",
				"mediaType": "text/plain",
				"data": "hello"
			}
		},
		"retrievedAt": "2026-03-18T00:00:00Z"
	}`

	result, err := ParseWebFetch20260318Result([]byte(input))
	if err != nil {
		t.Fatalf("ParseWebFetch20260318Result returned error: %v", err)
	}
	if result.URL != "https://example.com/page" {
		t.Errorf("result.URL = %q, want https://example.com/page", result.URL)
	}
	if !result.Content.Source.IsPlainText() {
		t.Error("result.Content.Source.IsPlainText() should be true")
	}
}

func TestAnthropicWebFetch20260318ResultParsingError(t *testing.T) {
	_, err := ParseWebFetch20260318Result([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
