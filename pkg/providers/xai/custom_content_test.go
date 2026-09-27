package xai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestXAIConvertResponseTextString verifies that a plain string content field
// is correctly mapped to GenerateResult.Text.
func TestXAIConvertResponseTextString(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role:    "assistant",
					Content: xaiMessageContent{Text: "Hello, world!"},
				},
			},
		},
	}

	result := m.convertResponse(resp, "")
	if result.Text != "Hello, world!" {
		t.Errorf("Text = %q, want %q", result.Text, "Hello, world!")
	}
	if len(result.Content) != 0 {
		t.Errorf("Content length = %d, want 0", len(result.Content))
	}
}

// TestXAIConvertResponseArrayKnownTypes verifies that array-form content with
// known type "text" is merged into GenerateResult.Text.
func TestXAIConvertResponseArrayKnownTypes(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}
	rawPart := json.RawMessage(`{"type":"text","text":"Part one. "}`)
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role: "assistant",
					Content: xaiMessageContent{
						Parts: []xaiContentPart{
							{Type: "text", Text: "Part one. ", Raw: rawPart},
						},
					},
				},
			},
		},
	}

	result := m.convertResponse(resp, "")
	if result.Text != "Part one. " {
		t.Errorf("Text = %q, want %q", result.Text, "Part one. ")
	}
	if len(result.Content) != 0 {
		t.Errorf("Content should be empty for known types, got %d parts", len(result.Content))
	}
}

// TestXAIConvertResponseUnknownContentEmitsCustomContent verifies that an
// unknown content block type in the XAI response is emitted as CustomContent
// with kind "xai.{type}" rather than being silently dropped.
func TestXAIConvertResponseUnknownContentEmitsCustomContent(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}
	rawCitation := json.RawMessage(`{"type":"citation","url":"https://x.ai","title":"xAI"}`)
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role: "assistant",
					Content: xaiMessageContent{
						Parts: []xaiContentPart{
							{Type: "text", Text: "See reference.", Raw: json.RawMessage(`{"type":"text","text":"See reference."}`)},
							{Type: "citation", Raw: rawCitation},
						},
					},
				},
			},
		},
	}

	result := m.convertResponse(resp, "")

	// Text from the "text" part should be in result.Text
	if result.Text != "See reference." {
		t.Errorf("Text = %q, want %q", result.Text, "See reference.")
	}

	// Unknown "citation" part should emit as CustomContent
	if len(result.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(result.Content))
	}
	cc, ok := result.Content[0].(types.CustomContent)
	if !ok {
		t.Fatalf("Content[0] type = %T, want types.CustomContent", result.Content[0])
	}
	if cc.Kind != "xai.citation" {
		t.Errorf("Kind = %q, want \"xai.citation\"", cc.Kind)
	}
	// ProviderMetadata should be the raw JSON of the unknown part
	if cc.ProviderMetadata == nil {
		t.Error("ProviderMetadata should not be nil")
	}
	if string(cc.ProviderMetadata) != string(rawCitation) {
		t.Errorf("ProviderMetadata = %s, want %s", cc.ProviderMetadata, rawCitation)
	}
}

// TestXAIMessageContentUnmarshalString verifies JSON string → xaiMessageContent.Text.
func TestXAIMessageContentUnmarshalString(t *testing.T) {
	var c xaiMessageContent
	if err := json.Unmarshal([]byte(`"hello"`), &c); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if c.Text != "hello" {
		t.Errorf("Text = %q, want \"hello\"", c.Text)
	}
	if len(c.Parts) != 0 {
		t.Errorf("Parts should be empty, got %d", len(c.Parts))
	}
}

// TestXAIConvertResponseCitationsEmitSourceContent verifies that response
// citations are mapped to SourceContent parts (type:"source", sourceType:"url").
func TestXAIConvertResponseCitationsEmitSourceContent(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role:    "assistant",
					Content: xaiMessageContent{Text: "See citations."},
				},
			},
		},
		Citations: []string{"https://x.ai/news", "https://docs.x.ai/api"},
	}

	result := m.convertResponse(resp, "")

	if result.Text != "See citations." {
		t.Errorf("Text = %q, want %q", result.Text, "See citations.")
	}
	if len(result.Content) != 2 {
		t.Fatalf("Content length = %d, want 2", len(result.Content))
	}

	for i, wantURL := range []string{"https://x.ai/news", "https://docs.x.ai/api"} {
		src, ok := result.Content[i].(types.SourceContent)
		if !ok {
			t.Fatalf("Content[%d] type = %T, want types.SourceContent", i, result.Content[i])
		}
		if src.SourceType != "url" {
			t.Errorf("Content[%d].SourceType = %q, want \"url\"", i, src.SourceType)
		}
		if src.URL != wantURL {
			t.Errorf("Content[%d].URL = %q, want %q", i, src.URL, wantURL)
		}
		if src.ID == "" {
			t.Errorf("Content[%d].ID should be non-empty", i)
		}
	}
}

// TestXAIConvertResponseCitationsAndUnknownParts verifies that when a response
// has both citations and unknown array content parts, both are correctly mapped:
// citations → SourceContent, unknowns → CustomContent.
func TestXAIConvertResponseCitationsAndUnknownParts(t *testing.T) {
	m := &LanguageModel{modelID: "grok-3"}
	rawCitation := json.RawMessage(`{"type":"citation","url":"https://x.ai"}`)
	resp := xaiResponse{
		Choices: []xaiChoice{
			{
				FinishReason: "stop",
				Message: xaiMessage{
					Role: "assistant",
					Content: xaiMessageContent{
						Parts: []xaiContentPart{
							{Type: "text", Text: "Answer.", Raw: json.RawMessage(`{"type":"text","text":"Answer."}`)},
							{Type: "citation", Raw: rawCitation},
						},
					},
				},
			},
		},
		Citations: []string{"https://x.ai/news"},
	}

	result := m.convertResponse(resp, "")

	if result.Text != "Answer." {
		t.Errorf("Text = %q, want \"Answer.\"", result.Text)
	}
	// Expect: 1 CustomContent (unknown array part) + 1 SourceContent (citations field)
	if len(result.Content) != 2 {
		t.Fatalf("Content length = %d, want 2", len(result.Content))
	}

	if _, ok := result.Content[0].(types.CustomContent); !ok {
		t.Errorf("Content[0] type = %T, want types.CustomContent", result.Content[0])
	}
	src, ok := result.Content[1].(types.SourceContent)
	if !ok {
		t.Fatalf("Content[1] type = %T, want types.SourceContent", result.Content[1])
	}
	if src.URL != "https://x.ai/news" {
		t.Errorf("SourceContent.URL = %q, want \"https://x.ai/news\"", src.URL)
	}
}

// TestXAIMessageContentUnmarshalArray verifies JSON array → xaiMessageContent.Parts.
func TestXAIMessageContentUnmarshalArray(t *testing.T) {
	raw := `[{"type":"text","text":"hi"},{"type":"citation","url":"https://x.ai"}]`
	var c xaiMessageContent
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(c.Parts) != 2 {
		t.Fatalf("Parts length = %d, want 2", len(c.Parts))
	}
	if c.Parts[0].Type != "text" {
		t.Errorf("Parts[0].Type = %q, want \"text\"", c.Parts[0].Type)
	}
	if c.Parts[0].Text != "hi" {
		t.Errorf("Parts[0].Text = %q, want \"hi\"", c.Parts[0].Text)
	}
	if c.Parts[1].Type != "citation" {
		t.Errorf("Parts[1].Type = %q, want \"citation\"", c.Parts[1].Type)
	}
}
