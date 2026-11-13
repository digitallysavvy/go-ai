package openai

import (
	"strings"
	"testing"
)

// r96provbHTML is a provider string with the characters encoding/json
// escapes as <, > and & and JSON.stringify writes as is.
const r96provbHTML = "<a&b>"

// TestR96provbMetadataJSONStringify: provider metadata the SDK serializes
// from provider values is written like JSON.stringify (jsonstr), so <, >
// and & stay literal (main ruling 4, write side: the same serializer maps a
// non-finite number to null).
func TestR96provbMetadataJSONStringify(t *testing.T) {
	for name, raw := range map[string][]byte{
		"toRawMetadata":     toRawMetadata(map[string]interface{}{"openai": map[string]interface{}{"itemId": r96provbHTML}}),
		"reasoningMetadata": openAIResponsesReasoningMetadataWithEncrypted("openai", r96provbHTML, nil),
		"marshalOrNil":      marshalOrNil(map[string]interface{}{"openai": map[string]interface{}{"itemId": r96provbHTML}}),
		"completion":        mustMarshalCompletionProviderMetadata(map[string]interface{}{"openai": map[string]interface{}{"id": r96provbHTML}}),
		"logprobs":          normalizeResponsesLogprobs([]byte(`[{"token":"<a&b>","logprob":-1,"top_logprobs":[]}]`)),
	} {
		if !strings.Contains(string(raw), r96provbHTML) {
			t.Errorf("%s: %s, want %q written literally", name, raw, r96provbHTML)
		}
	}
}
