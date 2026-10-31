package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22338: Azure embeddings reuse the OpenAI embedding model, so the
// top-level dimensions maps to `dimensions`, providerOptions.openai wins, and
// providerOptions.openai.user is forwarded.
func TestAzureEmbeddingDimensionsAndUser(t *testing.T) {
	ten := 10
	cases := []struct {
		name     string
		opts     *provider.EmbedModelOptions
		wantDims interface{}
		wantUser interface{}
	}{
		{"unset", &provider.EmbedModelOptions{}, nil, nil},
		{"top-level", &provider.EmbedModelOptions{Dimensions: &ten}, float64(10), nil},
		{"provider option wins", &provider.EmbedModelOptions{
			Dimensions:      &ten,
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"dimensions": float64(5), "user": "u1"}},
		}, float64(5), "u1"},
		{"nil options", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]interface{}
			p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				return azureJSONResponse(200, `{"object":"list","data":[{"index":0,"embedding":[1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`, nil), nil
			})
			m := NewEmbeddingModel(p, "dep")
			if _, err := m.DoEmbed(context.Background(), "x", tc.opts); err != nil {
				t.Fatal(err)
			}
			if body["dimensions"] != tc.wantDims || body["user"] != tc.wantUser {
				t.Fatalf("body = %#v, want dimensions=%v user=%v", body, tc.wantDims, tc.wantUser)
			}
		})
	}
}

// TS #22209: Azure chat completions use OpenAI's chat converter, so failed tool
// executions are sent as {"error": value}.
func TestAzureChatWrapsToolErrorOutputs(t *testing.T) {
	var body map[string]interface{}
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
	})
	out := types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "boom"}
	_, err := NewLanguageModel(p, "dep").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &out}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := body["messages"].([]interface{})
	last, _ := msgs[len(msgs)-1].(map[string]interface{})
	if last["content"] != `{"error":"boom"}` {
		t.Fatalf("tool message = %#v", last)
	}
}
