package tools

import (
	"testing"
)

func TestAnthropicWebSearch20260209Serialization(t *testing.T) {
	maxUses := 5
	tool := WebSearch20260209(WebSearch20260209Config{
		MaxUses:        &maxUses,
		AllowedDomains: []string{"wikipedia.org", "docs.anthropic.com"},
		BlockedDomains: []string{"spam.com"},
	})

	if tool.Name != "anthropic.web_search_20260209" {
		t.Errorf("tool.Name = %q, want %q", tool.Name, "anthropic.web_search_20260209")
	}
	if !tool.ProviderExecuted {
		t.Error("tool.ProviderExecuted should be true")
	}

	opts, ok := tool.ProviderOptions.(*webSearch20260209Opts)
	if !ok {
		t.Fatalf("tool.ProviderOptions is not *webSearch20260209Opts")
	}

	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_search_20260209" {
		t.Errorf("apiMap[type] = %v, want web_search_20260209", apiMap["type"])
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
	if _, hasUserLoc := apiMap["user_location"]; hasUserLoc {
		t.Error("apiMap should not have user_location when UserLocation is nil")
	}
}

func TestAnthropicWebSearch20260209SerializationEmpty(t *testing.T) {
	// Empty config: only type field should be present
	tool := WebSearch20260209(WebSearch20260209Config{})

	opts := tool.ProviderOptions.(*webSearch20260209Opts)
	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_search_20260209" {
		t.Errorf("apiMap[type] = %v, want web_search_20260209", apiMap["type"])
	}
	if _, hasMaxUses := apiMap["max_uses"]; hasMaxUses {
		t.Error("apiMap should not have max_uses when MaxUses is nil")
	}
	if _, hasAllowed := apiMap["allowed_domains"]; hasAllowed {
		t.Error("apiMap should not have allowed_domains when AllowedDomains is empty")
	}
	if _, hasBlocked := apiMap["blocked_domains"]; hasBlocked {
		t.Error("apiMap should not have blocked_domains when BlockedDomains is empty")
	}
}

func TestAnthropicWebSearch20260209WithUserLocation(t *testing.T) {
	tool := WebSearch20260209(WebSearch20260209Config{
		UserLocation: &WebSearchUserLocation{
			Type:     "approximate",
			City:     "San Francisco",
			Country:  "US",
			Timezone: "America/Los_Angeles",
		},
	})

	opts := tool.ProviderOptions.(*webSearch20260209Opts)
	apiMap := opts.ToAnthropicAPIMap()

	userLoc, ok := apiMap["user_location"].(map[string]interface{})
	if !ok {
		t.Fatalf("apiMap[user_location] is not a map")
	}
	if userLoc["type"] != "approximate" {
		t.Errorf("user_location.type = %v, want approximate", userLoc["type"])
	}
	if userLoc["city"] != "San Francisco" {
		t.Errorf("user_location.city = %v, want San Francisco", userLoc["city"])
	}
	if userLoc["country"] != "US" {
		t.Errorf("user_location.country = %v, want US", userLoc["country"])
	}
	if userLoc["timezone"] != "America/Los_Angeles" {
		t.Errorf("user_location.timezone = %v, want America/Los_Angeles", userLoc["timezone"])
	}
	// Region not set — must not appear in map
	if _, hasRegion := userLoc["region"]; hasRegion {
		t.Error("user_location should not have region when Region is empty")
	}
}

func TestAnthropicWebSearch20260209ResultParsing(t *testing.T) {
	title := "Example Domain"
	pageAge := "2024-01-15"
	_ = title
	_ = pageAge

	input := `[
		{
			"type": "web_search_result",
			"url": "https://example.com",
			"title": "Example Domain",
			"pageAge": "2024-01-15",
			"encryptedContent": "enc_abc123xyz"
		},
		{
			"type": "web_search_result",
			"url": "https://other.com",
			"title": null,
			"pageAge": null,
			"encryptedContent": "enc_def456"
		}
	]`

	results, err := ParseWebSearchResults([]byte(input))
	if err != nil {
		t.Fatalf("ParseWebSearchResults returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result
	r0 := results[0]
	if r0.Type != "web_search_result" {
		t.Errorf("results[0].Type = %q, want web_search_result", r0.Type)
	}
	if r0.URL != "https://example.com" {
		t.Errorf("results[0].URL = %q, want https://example.com", r0.URL)
	}
	if r0.Title == nil || *r0.Title != "Example Domain" {
		t.Errorf("results[0].Title = %v, want &'Example Domain'", r0.Title)
	}
	if r0.PageAge == nil || *r0.PageAge != "2024-01-15" {
		t.Errorf("results[0].PageAge = %v, want &'2024-01-15'", r0.PageAge)
	}
	if r0.EncryptedContent != "enc_abc123xyz" {
		t.Errorf("results[0].EncryptedContent = %q, want enc_abc123xyz", r0.EncryptedContent)
	}

	// Second result — nullable fields are nil
	r1 := results[1]
	if r1.Title != nil {
		t.Errorf("results[1].Title should be nil, got %v", r1.Title)
	}
	if r1.PageAge != nil {
		t.Errorf("results[1].PageAge should be nil, got %v", r1.PageAge)
	}
	if r1.EncryptedContent != "enc_def456" {
		t.Errorf("results[1].EncryptedContent = %q, want enc_def456", r1.EncryptedContent)
	}
}

func TestAnthropicWebSearch20260209ResultParsingError(t *testing.T) {
	_, err := ParseWebSearchResults([]byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
