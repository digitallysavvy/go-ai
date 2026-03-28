package tools

import (
	"testing"
)

func TestAnthropicWebFetch20260209Serialization(t *testing.T) {
	maxUses := 3
	maxTokens := 8192
	tool := WebFetch20260209(WebFetch20260209Config{
		MaxUses:          &maxUses,
		AllowedDomains:   []string{"docs.anthropic.com"},
		BlockedDomains:   []string{"ads.example.com"},
		Citations:        &WebFetchCitations{Enabled: true},
		MaxContentTokens: &maxTokens,
	})

	if tool.Name != "anthropic.web_fetch_20260209" {
		t.Errorf("tool.Name = %q, want %q", tool.Name, "anthropic.web_fetch_20260209")
	}
	if !tool.ProviderExecuted {
		t.Error("tool.ProviderExecuted should be true")
	}

	opts, ok := tool.ProviderOptions.(*webFetch20260209Opts)
	if !ok {
		t.Fatalf("tool.ProviderOptions is not *webFetch20260209Opts")
	}

	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_fetch_20260209" {
		t.Errorf("apiMap[type] = %v, want web_fetch_20260209", apiMap["type"])
	}
	if apiMap["max_uses"] != 3 {
		t.Errorf("apiMap[max_uses] = %v, want 3", apiMap["max_uses"])
	}
	allowedDomains, ok := apiMap["allowed_domains"].([]string)
	if !ok || len(allowedDomains) != 1 || allowedDomains[0] != "docs.anthropic.com" {
		t.Errorf("apiMap[allowed_domains] = %v, want [docs.anthropic.com]", apiMap["allowed_domains"])
	}
	blockedDomains, ok := apiMap["blocked_domains"].([]string)
	if !ok || len(blockedDomains) != 1 || blockedDomains[0] != "ads.example.com" {
		t.Errorf("apiMap[blocked_domains] = %v, want [ads.example.com]", apiMap["blocked_domains"])
	}
	citations, ok := apiMap["citations"].(map[string]interface{})
	if !ok {
		t.Fatalf("apiMap[citations] is not a map")
	}
	if citations["enabled"] != true {
		t.Errorf("citations.enabled = %v, want true", citations["enabled"])
	}
	if apiMap["max_content_tokens"] != 8192 {
		t.Errorf("apiMap[max_content_tokens] = %v, want 8192", apiMap["max_content_tokens"])
	}
}

func TestAnthropicWebFetch20260209SerializationEmpty(t *testing.T) {
	tool := WebFetch20260209(WebFetch20260209Config{})
	opts := tool.ProviderOptions.(*webFetch20260209Opts)
	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "web_fetch_20260209" {
		t.Errorf("apiMap[type] = %v, want web_fetch_20260209", apiMap["type"])
	}
	if _, has := apiMap["max_uses"]; has {
		t.Error("apiMap should not have max_uses when MaxUses is nil")
	}
	if _, has := apiMap["citations"]; has {
		t.Error("apiMap should not have citations when Citations is nil")
	}
	if _, has := apiMap["max_content_tokens"]; has {
		t.Error("apiMap should not have max_content_tokens when MaxContentTokens is nil")
	}
}

func TestAnthropicWebFetch20260209PDFContent(t *testing.T) {
	input := `{
		"type": "web_fetch_result",
		"url": "https://example.com/document.pdf",
		"content": {
			"type": "document",
			"title": "Annual Report 2025",
			"source": {
				"type": "base64",
				"mediaType": "application/pdf",
				"data": "JVBERi0xLjQK..."
			}
		},
		"retrievedAt": "2026-02-09T12:00:00Z"
	}`

	result, err := ParseWebFetchResult([]byte(input))
	if err != nil {
		t.Fatalf("ParseWebFetchResult returned error: %v", err)
	}

	if result.Type != "web_fetch_result" {
		t.Errorf("result.Type = %q, want web_fetch_result", result.Type)
	}
	if result.URL != "https://example.com/document.pdf" {
		t.Errorf("result.URL = %q, want https://example.com/document.pdf", result.URL)
	}
	if result.RetrievedAt == nil || *result.RetrievedAt != "2026-02-09T12:00:00Z" {
		t.Errorf("result.RetrievedAt = %v, want &'2026-02-09T12:00:00Z'", result.RetrievedAt)
	}

	doc := result.Content
	if doc.Type != "document" {
		t.Errorf("content.Type = %q, want document", doc.Type)
	}
	if doc.Title == nil || *doc.Title != "Annual Report 2025" {
		t.Errorf("content.Title = %v, want &'Annual Report 2025'", doc.Title)
	}

	src := doc.Source
	if !src.IsPDF() {
		t.Error("source.IsPDF() should be true for type=base64")
	}
	if src.IsPlainText() {
		t.Error("source.IsPlainText() should be false for type=base64")
	}
	if src.Type != "base64" {
		t.Errorf("source.Type = %q, want base64", src.Type)
	}
	if src.MediaType != "application/pdf" {
		t.Errorf("source.MediaType = %q, want application/pdf", src.MediaType)
	}
	if src.Data != "JVBERi0xLjQK..." {
		t.Errorf("source.Data = %q, want JVBERi0xLjQK...", src.Data)
	}
}

func TestAnthropicWebFetch20260209TextContent(t *testing.T) {
	input := `{
		"type": "web_fetch_result",
		"url": "https://example.com/page",
		"content": {
			"type": "document",
			"title": null,
			"citations": {"enabled": true},
			"source": {
				"type": "text",
				"mediaType": "text/plain",
				"data": "Hello, world! This is the page content."
			}
		},
		"retrievedAt": null
	}`

	result, err := ParseWebFetchResult([]byte(input))
	if err != nil {
		t.Fatalf("ParseWebFetchResult returned error: %v", err)
	}

	if result.RetrievedAt != nil {
		t.Errorf("result.RetrievedAt should be nil, got %v", result.RetrievedAt)
	}

	doc := result.Content
	if doc.Title != nil {
		t.Errorf("content.Title should be nil, got %v", doc.Title)
	}
	if doc.Citations == nil || !doc.Citations.Enabled {
		t.Error("content.Citations.Enabled should be true")
	}

	src := doc.Source
	if !src.IsPlainText() {
		t.Error("source.IsPlainText() should be true for type=text")
	}
	if src.IsPDF() {
		t.Error("source.IsPDF() should be false for type=text")
	}
	if src.Type != "text" {
		t.Errorf("source.Type = %q, want text", src.Type)
	}
	if src.MediaType != "text/plain" {
		t.Errorf("source.MediaType = %q, want text/plain", src.MediaType)
	}
	if src.Data != "Hello, world! This is the page content." {
		t.Errorf("source.Data = %q, want full text", src.Data)
	}
}

func TestAnthropicWebFetch20260209ResultParsedError(t *testing.T) {
	_, err := ParseWebFetchResult([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestWebFetchSourceHelpers(t *testing.T) {
	pdf := WebFetchSource{Type: "base64", MediaType: "application/pdf", Data: "abc"}
	if !pdf.IsPDF() {
		t.Error("IsPDF() should be true for type=base64")
	}
	if pdf.IsPlainText() {
		t.Error("IsPlainText() should be false for type=base64")
	}

	text := WebFetchSource{Type: "text", MediaType: "text/plain", Data: "hello"}
	if text.IsPDF() {
		t.Error("IsPDF() should be false for type=text")
	}
	if !text.IsPlainText() {
		t.Error("IsPlainText() should be true for type=text")
	}
}
