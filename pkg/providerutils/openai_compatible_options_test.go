package providerutils

import "testing"

func TestResolveOpenAICompatibleProviderOptionsMergeOrder(t *testing.T) {
	opts, warnings := ResolveOpenAICompatibleProviderOptions("test-provider", map[string]interface{}{
		"openai-compatible": map[string]interface{}{"reasoningEffort": "low", "user": "deprecated"},
		"openaiCompatible":  map[string]interface{}{"reasoningEffort": "medium"},
		"test-provider":     map[string]interface{}{"reasoningEffort": "high"},
		"testProvider":      map[string]interface{}{"user": "camel"},
	})

	if got := opts["reasoningEffort"]; got != "high" {
		t.Fatalf("reasoningEffort = %v, want high", got)
	}
	if got := opts["user"]; got != "camel" {
		t.Fatalf("user = %v, want camel", got)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings len = %d, want 2", len(warnings))
	}
	if warnings[0].Type != "deprecated" || warnings[0].Feature != "providerOptions key 'openai-compatible'" {
		t.Fatalf("unexpected first warning: %#v", warnings[0])
	}
	if warnings[1].Type != "deprecated" || warnings[1].Feature != "providerOptions key 'test-provider'" {
		t.Fatalf("unexpected second warning: %#v", warnings[1])
	}
}

func TestToOpenAICompatibleCamelCase(t *testing.T) {
	tests := map[string]string{
		"openai-compatible": "openaiCompatible",
		"reasoning_effort":  "reasoningEffort",
		"alreadyCamel":      "alreadyCamel",
	}
	for input, want := range tests {
		if got := ToOpenAICompatibleCamelCase(input); got != want {
			t.Fatalf("ToOpenAICompatibleCamelCase(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOpenAICompatibleCommonOptionWarnings(t *testing.T) {
	warnings := OpenAICompatibleCommonOptionWarnings(map[string]interface{}{
		"reasoning-effort": "high",
		"textVerbosity":    "low",
	})

	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1", len(warnings))
	}
	if warnings[0].Type != "deprecated" || warnings[0].Feature != "provider option key 'reasoning-effort'" {
		t.Fatalf("unexpected warning: %#v", warnings[0])
	}
}
