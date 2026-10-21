package mistral

import (
	"context"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS forwards promptCacheKey whenever it is not undefined, including "".
func TestMistralForwardsEmptyPromptCacheKey(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-large-latest")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"promptCacheKey": ""}},
	}, false)
	if v, ok := body["prompt_cache_key"]; !ok || v != "" {
		t.Fatalf("prompt_cache_key = %#v (present %v), want empty string", v, ok)
	}
}

// TS createJsonResponseHandler(mistralConversationResponseSchema) rejects a
// body that does not match, and an absent usage is the null usage.
func TestConversationResponseSchemaAndNullUsage(t *testing.T) {
	var captured map[string]interface{}
	bad := conversationServer(t, "application/json", `{"conversation_id":"c","outputs":[{"type":"mystery.output"}]}`, &captured)
	_, err := bad.ConversationModel("mistral-medium-latest").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("hi")})
	if err == nil || !providerutils.IsInvalidJSONResponse(err) {
		t.Fatalf("err = %v, want invalid JSON response", err)
	}

	ok := conversationServer(t, "application/json", `{"conversation_id":"c","outputs":[{"type":"message.output","content":"Hi"}]}`, &captured)
	res, err := ok.ConversationModel("mistral-medium-latest").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens != nil || res.Usage.OutputTokens != nil {
		t.Errorf("usage = %+v, want the null usage", res.Usage)
	}
}
