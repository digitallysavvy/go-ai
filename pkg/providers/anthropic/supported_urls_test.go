package anthropic

import (
	"regexp"
	"testing"
)

// Ports the "anthropic provider - supportedUrls" describe block of
// anthropic-provider.test.ts (ai@7.0.113): the direct Anthropic API accepts
// https image/* and application/pdf URLs directly instead of requiring the
// caller to download and base64-encode them first.
func TestLanguageModel_SupportedURLs(t *testing.T) {
	p := New(Config{APIKey: "test-api-key"})
	model := NewLanguageModel(p, "claude-3-haiku-20240307", nil)

	supported := model.SupportedURLs()

	imagePatterns, ok := supported["image/*"]
	if !ok || len(imagePatterns) == 0 {
		t.Fatalf("expected image/* patterns, got %v", supported)
	}
	if re := regexp.MustCompile(imagePatterns[0]); !re.MatchString("https://example.com/image.png") {
		t.Errorf("image/* pattern %q should match https URL", imagePatterns[0])
	}

	pdfPatterns, ok := supported["application/pdf"]
	if !ok || len(pdfPatterns) == 0 {
		t.Fatalf("expected application/pdf patterns, got %v", supported)
	}
	if re := regexp.MustCompile(pdfPatterns[0]); !re.MatchString("https://arxiv.org/pdf/2401.00001") {
		t.Errorf("application/pdf pattern %q should match https URL", pdfPatterns[0])
	}
}

// A Config.SupportedURLs override (used by Vertex-Anthropic and
// Bedrock-Anthropic to force base64 conversion) takes precedence over the
// package default.
func TestLanguageModel_SupportedURLs_ConfigOverride(t *testing.T) {
	p := New(Config{
		APIKey:        "test-api-key",
		SupportedURLs: func(string) map[string][]string { return map[string][]string{} },
	})
	model := NewLanguageModel(p, "claude-3-haiku-20240307", nil)

	if got := model.SupportedURLs(); len(got) != 0 {
		t.Errorf("expected empty supportedUrls override to be honored, got %v", got)
	}
}
