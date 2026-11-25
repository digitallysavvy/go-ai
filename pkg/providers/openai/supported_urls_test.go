package openai

import "testing"

// TS openaiChat supportedUrls forwards http(s) image URLs; the compatible
// wrappers default to {} (so URLs are downloaded); Responses also forwards PDFs.
func TestSupportedURLs(t *testing.T) {
	chat := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	if got := chat.SupportedURLs(); len(got["image/*"]) != 1 {
		t.Fatalf("chat = %#v", got)
	}
	compat := NewLanguageModel(New(Config{APIKey: "k", OpenAICompatible: true}), "m")
	if got := compat.SupportedURLs(); len(got) != 0 {
		t.Fatalf("compat = %#v", got)
	}
	resp := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	if got := resp.SupportedURLs(); len(got["image/*"]) != 1 || len(got["application/pdf"]) != 1 {
		t.Fatalf("responses = %#v", got)
	}
}
