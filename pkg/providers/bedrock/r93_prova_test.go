package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createRerankingModel passes `region: loadSetting(...)` when the model is
// created, so the model ARN keeps that region when AWS_REGION changes later.
func TestR93provaRerankRegionReadAtCreation(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	m, err := New(Config{APIKey: "k"}).RerankingModel("cohere.rerank-v3-5:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_REGION", "eu-west-1")
	body, err := m.(*RerankingModel).buildRequestBody(&provider.RerankOptions{Query: "q", Documents: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := body["rerankingConfiguration"].(map[string]interface{})["bedrockRerankingConfiguration"].(map[string]interface{})
	arn := cfg["modelConfiguration"].(map[string]interface{})["modelArn"]
	if arn != "arn:aws:bedrock:us-west-2::foundation-model/cohere.rerank-v3-5:0" {
		t.Errorf("modelArn = %v, want the creation-time region us-west-2", arn)
	}
}

// TS amazon-bedrock-image-model.ts: `size.split('x').map(Number)`, each
// dimension sent when truthy, so a fractional or negative dimension is sent
// as the number and a zero or non-numeric one is left out.
func TestR93provaImageSizeIsJSNumber(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k", Region: "us-east-1"}), "amazon.nova-canvas-v1:0")
	for size, want := range map[string]string{
		"1024.5x0":  `{"width":1024.5}`,
		"-5x10":     `{"height":10,"width":-5}`,
		"abcx768":   `{"height":768}`,
		"1024x1024": `{"height":1024,"width":1024}`,
	} {
		body, _, err := model.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "p", Size: size})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(body["imageGenerationConfig"])
		if string(got) != want {
			t.Errorf("size %q: imageGenerationConfig = %s, want %s", size, got, want)
		}
	}
}

// TS convertToAmazonBedrockChatMessages pushes the cache point of every
// assistant part, custom and reasoning-file parts included.
func TestR93provaCachePointOnCustomAndReasoningFileParts(t *testing.T) {
	cache := map[string]interface{}{"amazonBedrock": map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default"}}}
	_, messages, err := convertToBedrockChatMessages([]types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "a"},
			types.CustomContent{Kind: "x.y", ProviderOptions: cache},
			types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1}, ProviderOptions: cache},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(messages[1]["content"])
	if strings.Count(string(got), `"cachePoint"`) != 2 {
		t.Errorf("assistant content = %s, want a cache point for each part", got)
	}
}
