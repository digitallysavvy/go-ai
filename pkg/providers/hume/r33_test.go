package hume

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// zod keeps empty strings, strips the losing union member's keys, and
// `'generationId' in context` is true for "".
func TestR33_ContextFollowsZodParse(t *testing.T) {
	m := &SpeechModel{provider: New(Config{APIKey: "k"})}
	body, _ := m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "x", ProviderOptions: map[string]interface{}{"hume": map[string]interface{}{
		"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{
			"text": "t", "description": "", "voice": map[string]interface{}{"id": "", "name": "n", "provider": "HUME_AI"},
		}}},
	}}})
	got := providerutils.StringifyOrdered(body, humeSpeechBodyOrder)
	if !strings.Contains(got, `"context":{"utterances":[{"text":"t","description":"","voice":{"id":"","provider":"HUME_AI"}}]}`) {
		t.Errorf("body = %s", got)
	}
	body, _ = m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "x", ProviderOptions: map[string]interface{}{"hume": map[string]interface{}{
		"context": map[string]interface{}{"generationId": ""},
	}}})
	if ctx, _ := body["context"].(map[string]interface{}); ctx["generation_id"] != "" {
		t.Errorf("context = %v", body["context"])
	}
}
