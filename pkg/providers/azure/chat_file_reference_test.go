package azure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS Azure chat wraps OpenAIChatLanguageModel, which resolves provider file
// references with the fixed "openai" key into file.file_id (and errors when
// the reference has no "openai" entry).
func TestAzureChatFileReference(t *testing.T) {
	var body map[string]interface{}
	p := newAzureProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		return azureJSONResponse(200, `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, nil), nil
	})
	mk := func(ref types.ProviderReference) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				MediaType: "application/pdf",
				FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: ref},
			}},
		}}}}
	}
	m := NewLanguageModel(p, "dep")
	if _, err := m.DoGenerate(context.Background(), mk(types.ProviderReference{"openai": "file-1"})); err != nil {
		t.Fatal(err)
	}
	msgs, _ := body["messages"].([]interface{})
	content, _ := msgs[0].(map[string]interface{})["content"].([]interface{})
	part, _ := content[0].(map[string]interface{})
	file, _ := part["file"].(map[string]interface{})
	if part["type"] != "file" || file["file_id"] != "file-1" {
		t.Fatalf("part = %#v", part)
	}

	var noSuch *providererrors.NoSuchProviderReferenceError
	if _, err := m.DoGenerate(context.Background(), mk(types.ProviderReference{"other": "x"})); !errors.As(err, &noSuch) {
		t.Fatalf("DoGenerate err = %v, want NoSuchProviderReferenceError", err)
	}
	if _, err := m.DoStream(context.Background(), mk(types.ProviderReference{"other": "x"})); !errors.As(err, &noSuch) {
		t.Fatalf("DoStream err = %v, want NoSuchProviderReferenceError", err)
	}
}
