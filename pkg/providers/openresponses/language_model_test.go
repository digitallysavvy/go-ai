package openresponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BUG-T12: reasoning output items that have encrypted_content but no ID must still
// be included in the response text (#12869).
func TestConvertResponse_ReasoningWithEncryptedContentNoID(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				// No ID — the item arrives without one, but has encrypted_content.
				Type:             "reasoning",
				EncryptedContent: "enc-abc123",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "thinking step"},
				},
			},
			{
				Type: "message",
				Content: []ContentPart{
					{Type: "output_text", Text: "final answer"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	// The reasoning text should appear in the combined output.
	if !strings.Contains(result.Text, "thinking step") {
		t.Errorf("expected reasoning summary in text, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "final answer") {
		t.Errorf("expected message text in output, got %q", result.Text)
	}
}

// TestConvertResponse_ReasoningWithIDNoEncryptedContent verifies that a reasoning item
// with an ID (but no encrypted_content) is still included.
func TestConvertResponse_ReasoningWithIDNoEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				ID:   "item_001",
				Type: "reasoning",
				Summary: []ContentPart{
					{Type: "text", Text: "reasoning text"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	if !strings.Contains(result.Text, "reasoning text") {
		t.Errorf("expected reasoning text in output, got %q", result.Text)
	}
}

// TestConvertResponse_ReasoningWithNoIDNoEncryptedContent verifies that a reasoning
// item with neither ID nor encrypted_content is skipped.
func TestConvertResponse_ReasoningWithNoIDNoEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				// Neither ID nor EncryptedContent — should be ignored.
				Type: "reasoning",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "should be ignored"},
				},
			},
			{
				Type: "message",
				Content: []ContentPart{
					{Type: "output_text", Text: "only this"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	if strings.Contains(result.Text, "should be ignored") {
		t.Errorf("reasoning item without ID or encrypted_content should be skipped, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "only this") {
		t.Errorf("expected message text in output, got %q", result.Text)
	}
}

func TestConvertResponse_FunctionCallPreservesProviderMetadata(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				ID:        "fc_item_1",
				Type:      "function_call",
				CallID:    "call_1",
				Name:      "weather",
				Arguments: `{"city":"nyc"}`,
				Namespace: "weather",
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v, want one", result.ToolCalls)
	}
	metadata, ok := result.ToolCalls[0].ProviderMetadata["open-responses"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata = %+v, want open-responses payload", result.ToolCalls[0].ProviderMetadata)
	}
	if metadata["itemId"] != "fc_item_1" || metadata["namespace"] != "weather" {
		t.Fatalf("provider metadata payload = %+v", metadata)
	}
}

// TestConvertResponse_MessageTextCarriesItemIDAndAnnotations verifies that
// convertResponse emits a types.TextContent per output_text content part
// (not just the plain-string result.Text), carrying {itemId, annotations?}
// provider metadata, mirroring TS's `content.push({type: 'text', text:
// contentPart.text, providerMetadata: {[providerOptionsName]: {itemId:
// part.id, ...(annotations.length > 0 && {annotations})}}})`.
func TestConvertResponse_MessageTextCarriesItemIDAndAnnotations(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				ID:   "msg_1",
				Type: "message",
				Content: []ContentPart{
					{
						Type: "output_text",
						Text: "hello world",
						Annotations: []Annotation{
							{Type: "url_citation", URL: "https://example.com", Title: "Example", StartIndex: 0, EndIndex: 5},
						},
					},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}
	if result.Text != "hello world" {
		t.Fatalf("result.Text = %q, want %q", result.Text, "hello world")
	}
	if len(result.Content) != 1 {
		t.Fatalf("result.Content = %+v, want 1 TextContent part", result.Content)
	}
	text, ok := result.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("result.Content[0] = %T, want types.TextContent", result.Content[0])
	}
	if text.Text != "hello world" {
		t.Fatalf("text.Text = %q, want %q", text.Text, "hello world")
	}
	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(text.ProviderMetadata, &payload); err != nil {
		t.Fatalf("ProviderMetadata unmarshal failed: %v", err)
	}
	meta, ok := payload["open-responses"]
	if !ok {
		t.Fatalf("ProviderMetadata = %s, want open-responses key", text.ProviderMetadata)
	}
	if meta["itemId"] != "msg_1" {
		t.Fatalf("itemId = %v, want msg_1", meta["itemId"])
	}
	annotations, ok := meta["annotations"].([]interface{})
	if !ok || len(annotations) != 1 {
		t.Fatalf("annotations = %+v, want 1 entry", meta["annotations"])
	}
	annotation := annotations[0].(map[string]interface{})
	if annotation["type"] != "url_citation" || annotation["url"] != "https://example.com" {
		t.Fatalf("annotation = %+v", annotation)
	}
}

// TestConvertResponse_MessageTextWithoutAnnotationsOmitsAnnotationsKey
// verifies that the annotations key is omitted entirely (not an empty
// array) when the content part carries none, matching TS's
// `...(annotations.length > 0 && {annotations})` spread.
func TestConvertResponse_MessageTextWithoutAnnotationsOmitsAnnotationsKey(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{ID: "msg_2", Type: "message", Content: []ContentPart{{Type: "output_text", Text: "plain"}}},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}
	text := result.Content[0].(types.TextContent)
	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(text.ProviderMetadata, &payload); err != nil {
		t.Fatalf("ProviderMetadata unmarshal failed: %v", err)
	}
	if _, ok := payload["open-responses"]["annotations"]; ok {
		t.Fatalf("payload = %+v, want no annotations key", payload["open-responses"])
	}
}

// TestConvertResponse_ReasoningPopulatesContentWithEncryptedContent verifies
// that convertResponse stores the reasoning block in result.Content as a
// ReasoningContent with EncryptedContent set, enabling the input-side
// round-trip on subsequent turns (#12869).
func TestConvertResponse_ReasoningPopulatesContentWithEncryptedContent(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{
				Type:             "reasoning",
				EncryptedContent: "enc-round-trip",
				Summary: []ContentPart{
					{Type: "summary_text", Text: "my reasoning"},
				},
			},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	// result.Content must contain exactly one ReasoningContent part.
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result.Content))
	}
	rc, ok := result.Content[0].(types.ReasoningContent)
	if !ok {
		t.Fatalf("expected ReasoningContent, got %T", result.Content[0])
	}
	if rc.EncryptedContent != "enc-round-trip" {
		t.Errorf("EncryptedContent = %q, want %q", rc.EncryptedContent, "enc-round-trip")
	}
	if rc.Text != "my reasoning" {
		t.Errorf("Text = %q, want %q", rc.Text, "my reasoning")
	}
}

// TestConvertResponse_ReasoningNoContentWhenNeitherIDNorEncrypted verifies
// that result.Content is empty when the reasoning item has neither ID nor
// EncryptedContent (the item is skipped entirely).
func TestConvertResponse_ReasoningNoContentWhenNeitherIDNorEncrypted(t *testing.T) {
	response := OpenResponsesResponse{
		Output: []OutputItem{
			{Type: "reasoning", Summary: []ContentPart{{Type: "summary_text", Text: "ignore me"}}},
		},
	}

	lm := &LanguageModel{}
	result, convertErr := lm.convertResponse(response)
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	if len(result.Content) != 0 {
		t.Errorf("expected no content parts, got %d", len(result.Content))
	}
}

func TestBuildRequestBody_ReasoningSummaryOption(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	m := NewLanguageModel(p, "gpt-5")
	reasoning := types.ReasoningMedium

	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"reasoningSummary": "detailed",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	reasoningBody, ok := body["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning body missing: %#v", body)
	}
	if reasoningBody["effort"] != "medium" {
		t.Fatalf("effort = %v, want medium", reasoningBody["effort"])
	}
	if reasoningBody["summary"] != "detailed" {
		t.Fatalf("summary = %v, want detailed", reasoningBody["summary"])
	}
}

func TestBuildRequestBody_ReasoningSummaryNullSuppressesDefaultLikeTypeScript(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	m := NewLanguageModel(p, "gpt-5")
	reasoning := types.ReasoningMedium

	body, warnings, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"reasoningSummary": nil,
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}
	for _, warning := range warnings {
		if warning.Feature == "reasoningSummary" {
			t.Fatalf("unexpected reasoningSummary warning: %#v", warning)
		}
	}

	reasoningBody, ok := body["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning body missing: %#v", body)
	}
	if reasoningBody["effort"] != "medium" {
		t.Fatalf("effort = %v, want medium", reasoningBody["effort"])
	}
	if _, ok := reasoningBody["summary"]; ok {
		t.Fatalf("summary = %v, want omitted for explicit null", reasoningBody["summary"])
	}
}

func TestBuildRequestBody_TopLevelReasoningDoesNotDefaultSummaryLikeOpenResponsesTypeScript(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	m := NewLanguageModel(p, "gpt-5")
	reasoning := types.ReasoningHigh

	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &reasoning,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody() error = %v", err)
	}

	reasoningBody, ok := body["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoning body missing: %#v", body)
	}
	if reasoningBody["effort"] != "high" {
		t.Fatalf("effort = %v, want high", reasoningBody["effort"])
	}
	if _, ok := reasoningBody["summary"]; ok {
		t.Fatalf("summary = %v, want omitted unless providerOptions reasoningSummary is set", reasoningBody["summary"])
	}
}

func TestBuildRequestBody_ReasoningHighAndXHighMapping(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	m := NewLanguageModel(p, "gpt-5")

	minimal := types.ReasoningMinimal
	bodyMinimal, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &minimal,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(minimal) error = %v", err)
	}
	// Row 3b9f025: Open Responses has no "minimal" effort value, so TS's
	// effortMap maps minimal -> low (with a compatibility warning).
	reasoningMinimal := bodyMinimal["reasoning"].(map[string]interface{})
	if reasoningMinimal["effort"] != "low" {
		t.Fatalf("minimal effort = %v, want low (mapped)", reasoningMinimal["effort"])
	}

	high := types.ReasoningHigh
	bodyHigh, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &high,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(high) error = %v", err)
	}
	reasoningHigh := bodyHigh["reasoning"].(map[string]interface{})
	if reasoningHigh["effort"] != "high" {
		t.Fatalf("high effort = %v, want high", reasoningHigh["effort"])
	}

	xhigh := types.ReasoningXHigh
	bodyXHigh, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &xhigh,
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody(xhigh) error = %v", err)
	}
	reasoningXHigh := bodyXHigh["reasoning"].(map[string]interface{})
	if reasoningXHigh["effort"] != "xhigh" {
		t.Fatalf("xhigh effort = %v, want xhigh", reasoningXHigh["effort"])
	}
}
