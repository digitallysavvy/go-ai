package openai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestChatFileDataOnlyParts(t *testing.T) {
	m := &LanguageModel{provider: New(Config{APIKey: "k"}), modelID: "gpt-4o"}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x.test/a.png", MediaType: "image/png"}, MediaType: "image/png"},
		types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2}, MediaType: "image/png"}, MediaType: "image/png"},
	}}}}}
	body, _, err := m.buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body["messages"])
	want := `[{"content":[{"image_url":{"url":"https://x.test/a.png"},"type":"image_url"},{"image_url":{"url":"data:image/png;base64,AQI="},"type":"image_url"}],"role":"user"}]`
	if string(b) != want {
		t.Fatalf("messages = %s\nwant %s", b, want)
	}
}
