package claudecode

import (
	"context"
	"testing"
)

// TS `pickAnthropic` uses `??`: a present-but-empty ANTHROPIC_API_KEY wins
// over the apiKeyHelper value (and yields no credential), while an absent one
// falls back to the helper.
func TestPickAnthropicNullishCoalescing(t *testing.T) {
	helper := func() string { return "helper-key" }

	got, err := pickAnthropic(context.Background(), map[string]string{"ANTHROPIC_API_KEY": ""}, helper, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ANTHROPIC_API_KEY"]; ok {
		t.Errorf("empty ANTHROPIC_API_KEY must not fall back to the helper: %v", got)
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "helper-key" {
		t.Errorf("absent ANTHROPIC_AUTH_TOKEN must use the helper: %v", got)
	}

	got, err = pickAnthropic(context.Background(), map[string]string{}, helper, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got["ANTHROPIC_API_KEY"] != "helper-key" || got["ANTHROPIC_AUTH_TOKEN"] != "helper-key" {
		t.Errorf("absent keys must use the helper: %v", got)
	}
}

// The LSP/SendMessage/ReportFindings/... schemas keep the zod constraints.
func TestBuiltinToolSchemaConstraints(t *testing.T) {
	tools := builtinTools()

	lsp := tools["LSP"].Parameters.(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"line", "character"} {
		f := lsp[field].(map[string]any)
		if f["type"] != "integer" || f["exclusiveMinimum"] != 0 {
			t.Errorf("LSP.%s = %v, want positive integer", field, f)
		}
	}

	send := tools["SendMessage"].Parameters.(map[string]any)["properties"].(map[string]any)
	if send["summary"].(map[string]any)["maxLength"] != 200 {
		t.Errorf("SendMessage.summary = %v", send["summary"])
	}
	msg := send["message"].(map[string]any)["anyOf"].([]any)
	if len(msg) != 2 {
		t.Fatalf("SendMessage.message anyOf = %v", msg)
	}
	if variants := msg[1].(map[string]any)["oneOf"].([]any); len(variants) != 3 {
		t.Errorf("structured team message variants = %d, want 3", len(variants))
	}

	findings := tools["ReportFindings"].Parameters.(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)
	if findings["maxItems"] != 32 {
		t.Errorf("ReportFindings.findings = %v", findings)
	}

	files := tools["SendUserFile"].Parameters.(map[string]any)["properties"].(map[string]any)["files"].(map[string]any)["anyOf"].([]any)
	if files[1].(map[string]any)["minItems"] != 1 {
		t.Errorf("SendUserFile.files array = %v", files[1])
	}

	guide := tools["ShareOnboardingGuide"].Parameters.(map[string]any)["properties"].(map[string]any)["short_code"].(map[string]any)
	if guide["pattern"] != "^[A-Za-z0-9_-]{1,64}$" {
		t.Errorf("ShareOnboardingGuide.short_code = %v", guide)
	}

	design := tools["DesignSync"].Parameters.(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"writes", "deletes", "files", "paths", "assets"} {
		if design[field].(map[string]any)["maxItems"] != 256 {
			t.Errorf("DesignSync.%s = %v", field, design[field])
		}
	}
}
