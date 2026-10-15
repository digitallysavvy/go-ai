package deepseek

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS sends response_format only for responseFormat.type === 'json'; a text
// response format (Output.text) adds nothing to the request.
func TestTextResponseFormatSendsNoResponseFormat(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat")
	body, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "text"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("response_format = %#v, want none", body["response_format"])
	}
	body, _, _ = m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}, false)
	if rf, _ := body["response_format"].(map[string]interface{}); rf["type"] != "json_object" {
		t.Fatalf("response_format = %#v", body["response_format"])
	}
}

// Message and file-part provider options are validated like TS
// parseProviderOptions (deepseekAssistantMessageProviderOptions /
// deepseekFilePartProviderOptions): a wrong type is an InvalidArgumentError.
func TestMessageAndFilePartProviderOptionsValidated(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat")
	user := func(messageOpts, fileOpts map[string]interface{}) provider.GenerateOptions {
		return provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
			Role:            types.RoleUser,
			ProviderOptions: map[string]interface{}{"deepseek": messageOpts},
			Content: []types.ContentPart{
				types.TextContent{Text: "hi"},
				types.ImageContent{Image: []byte{1}, MimeType: "image/png", ProviderOptions: map[string]interface{}{"deepseek": fileOpts}},
			},
		}}}}
	}
	for name, opts := range map[string]provider.GenerateOptions{
		"name type":    user(map[string]interface{}{"name": 5}, nil),
		"prefix false": user(map[string]interface{}{"prefix": false}, nil),
		"detail enum":  user(nil, map[string]interface{}{"imageDetail": "huge"}),
		"fileData":     user(nil, map[string]interface{}{"fileData": false}),
	} {
		opts := opts
		_, _, err := m.buildRequestBodyWithWarnings(&opts, false)
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) || invalid.Message != "invalid deepseek provider options" {
			t.Fatalf("%s: error = %v, want InvalidArgumentError", name, err)
		}
	}
	// An empty name is still sent (TS checks != null).
	ok := user(map[string]interface{}{"name": ""}, nil)
	body, _, err := m.buildRequestBodyWithWarnings(&ok, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := body["messages"].([]map[string]interface{})
	if len(msgs) != 1 || msgs[0]["name"] != "" {
		t.Fatalf("messages = %#v", body["messages"])
	}
}
