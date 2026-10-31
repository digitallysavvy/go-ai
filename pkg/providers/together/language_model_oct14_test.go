package together

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22466: provider-reference files resolve through the provider name
// ("togetherai") to a file_id wire part; an unresolvable reference fails the
// call before any request is sent.
func TestLanguageModelResolvesFileReferences(t *testing.T) {
	var seen map[string]interface{}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "meta-llama/test")

	prompt := func(ref types.ProviderReference) types.Prompt {
		return types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{types.FileContent{
				FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: ref},
				MediaType: "application/pdf",
			}},
		}}}
	}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: prompt(types.ProviderReference{"togetherai": "file-pdf-123", "together": "file-wrong"}),
	}); err != nil {
		t.Fatal(err)
	}
	messages := seen["messages"].([]interface{})
	part := messages[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
	if part["type"] != "file" || part["file"].(map[string]interface{})["file_id"] != "file-pdf-123" {
		t.Errorf("part = %#v", part)
	}

	requests = 0
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt(types.ProviderReference{"openai": "file-other"})})
	var noSuch *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &noSuch) || requests != 0 {
		t.Errorf("err = %v requests = %d", err, requests)
	}
	if _, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: prompt(types.ProviderReference{"openai": "file-other"})}); !errors.As(err, &noSuch) {
		t.Errorf("stream err = %v", err)
	}
}
