package azure

import (
	"testing"

	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

func TestAzureHostedToolWrappers(t *testing.T) {
	tools := []struct {
		name string
		got  string
	}{
		{"code interpreter", CodeInterpreter(openaitool.CodeInterpreterConfig{}).Name},
		{"file search", FileSearch(openaitool.FileSearchConfig{VectorStoreIDs: []string{"vs_1"}}).Name},
		{"image generation", ImageGeneration(openaitool.ImageGenerationConfig{}).Name},
		{"web search", WebSearch(openaitool.WebSearchConfig{}).Name},
		{"web search preview", WebSearchPreview(openaitool.WebSearchPreviewConfig{}).Name},
	}
	want := []string{
		"openai.code_interpreter",
		"openai.file_search",
		"openai.image_generation",
		"openai.web_search",
		"openai.web_search_preview",
	}
	for i, tt := range tools {
		if tt.got != want[i] {
			t.Fatalf("%s wrapper name = %q, want %q", tt.name, tt.got, want[i])
		}
	}
}
