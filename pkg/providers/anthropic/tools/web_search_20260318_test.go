package tools

import (
	"testing"
)

// Ports the shape of anthropic-prepare-tools.test.ts "anthropic.web_search_20260318"
// serialization case: type/name, all optional fields, and responseInclusion.
func TestAnthropicWebSearch20260318Serialization(t *testing.T) {
	maxUses := 5
	tool := WebSearch20260318(WebSearch20260318Config{
		MaxUses:           &maxUses,
		AllowedDomains:    []string{"wikipedia.org", "docs.anthropic.com"},
		BlockedDomains:    []string{"spam.com"},
		ResponseInclusion: "excluded",
	})

	if tool.Name != "anthropic.web_search_20260318" {
		t.Errorf("tool.Name = %q, want %q", tool.Name, "anthropic.web_search_20260318")
	}
	if !tool.ProviderExecuted {
		t.Error("tool.ProviderExecuted should be true")
	}

	opts, ok := tool.ProviderOptions.(*webSearch20260318Opts)
	if !ok {
		t.Fatalf("tool.ProviderOptions is not *webSearch20260318Opts")
	}

	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_search_20260318" {
		t.Errorf("apiMap[type] = %v, want web_search_20260318", apiMap["type"])
	}
	if apiMap["name"] != "web_search" {
		t.Errorf("apiMap[name] = %v, want web_search", apiMap["name"])
	}
	if apiMap["max_uses"] != 5 {
		t.Errorf("apiMap[max_uses] = %v, want 5", apiMap["max_uses"])
	}
	allowedDomains, ok := apiMap["allowed_domains"].([]string)
	if !ok || len(allowedDomains) != 2 || allowedDomains[0] != "wikipedia.org" {
		t.Errorf("apiMap[allowed_domains] = %v, want [wikipedia.org docs.anthropic.com]", apiMap["allowed_domains"])
	}
	blockedDomains, ok := apiMap["blocked_domains"].([]string)
	if !ok || len(blockedDomains) != 1 || blockedDomains[0] != "spam.com" {
		t.Errorf("apiMap[blocked_domains] = %v, want [spam.com]", apiMap["blocked_domains"])
	}
	if apiMap["response_inclusion"] != "excluded" {
		t.Errorf("apiMap[response_inclusion] = %v, want excluded", apiMap["response_inclusion"])
	}
}

func TestAnthropicWebSearch20260318SerializationEmpty(t *testing.T) {
	tool := WebSearch20260318(WebSearch20260318Config{})
	opts := tool.ProviderOptions.(*webSearch20260318Opts)
	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_search_20260318" {
		t.Errorf("apiMap[type] = %v, want web_search_20260318", apiMap["type"])
	}
	if _, has := apiMap["max_uses"]; has {
		t.Error("apiMap should not have max_uses when MaxUses is nil")
	}
	if _, has := apiMap["response_inclusion"]; has {
		t.Error("apiMap should not have response_inclusion when unset")
	}
	if _, has := apiMap["user_location"]; has {
		t.Error("apiMap should not have user_location when nil")
	}
}

func TestAnthropicWebSearch20260318WithUserLocation(t *testing.T) {
	tool := WebSearch20260318(WebSearch20260318Config{
		UserLocation: &WebSearchUserLocation{
			Type:    "approximate",
			Region:  "California",
			Country: "US",
		},
	})

	opts := tool.ProviderOptions.(*webSearch20260318Opts)
	apiMap := opts.ToAnthropicAPIMap()

	userLoc, ok := apiMap["user_location"].(map[string]interface{})
	if !ok {
		t.Fatalf("apiMap[user_location] is not a map")
	}
	if userLoc["type"] != "approximate" {
		t.Errorf("user_location.type = %v, want approximate", userLoc["type"])
	}
	if userLoc["region"] != "California" {
		t.Errorf("user_location.region = %v, want California", userLoc["region"])
	}
	if userLoc["country"] != "US" {
		t.Errorf("user_location.country = %v, want US", userLoc["country"])
	}
}

func TestAnthropicWebSearch20260318ResultParsing(t *testing.T) {
	input := `[
		{
			"type": "web_search_result",
			"url": "https://example.com",
			"title": "Example Domain",
			"pageAge": "2026-03-18",
			"encryptedContent": "enc_abc123xyz"
		}
	]`

	results, err := ParseWebSearch20260318Results([]byte(input))
	if err != nil {
		t.Fatalf("ParseWebSearch20260318Results returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].EncryptedContent != "enc_abc123xyz" {
		t.Errorf("results[0].EncryptedContent = %q, want enc_abc123xyz", results[0].EncryptedContent)
	}
}

func TestAnthropicWebSearch20260318ResultParsingError(t *testing.T) {
	_, err := ParseWebSearch20260318Results([]byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
