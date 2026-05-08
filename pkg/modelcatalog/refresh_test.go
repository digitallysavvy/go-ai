package modelcatalog

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestRenderModelIDsGoProducesParsableFile(t *testing.T) {
	src, err := RenderModelIDsGo("openai", "OpenAI model catalog.", []ModelID{
		{ID: "gpt-5.5"},
		{ID: "gpt-5.5-chat-latest"},
		{ID: "gpt-5.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "model_ids.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated file did not parse: %v\n%s", err, src)
	}
	if got := strings.Count(string(src), "gpt-5.5"); got != 2 {
		t.Fatalf("duplicate filtering failed, gpt-5.5 occurrences = %d\n%s", got, src)
	}
}

func TestConstNameFromModelID(t *testing.T) {
	tests := map[string]string{
		"gpt-5.5":             "ModelGpt55",
		"mistral-medium-3.5":  "ModelMistralMedium35",
		"openai/gpt-5.5-chat": "ModelOpenaiGpt55Chat",
		"claude_4_5-20250929": "ModelClaude4520250929",
		"!!!":                 "ModelID",
	}
	for input, want := range tests {
		if got := ConstNameFromModelID(input); got != want {
			t.Fatalf("ConstNameFromModelID(%q) = %q, want %q", input, got, want)
		}
	}
}
