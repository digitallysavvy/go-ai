package openai

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func chatFileReferenceOptions(ref types.ProviderReference) *provider.GenerateOptions {
	return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{types.FileContent{
			MediaType: "application/pdf",
			FileData:  types.FileData{Type: types.FileDataTypeReference, Reference: ref},
		}},
	}}}}
}

// TS convert-to-openai-chat-messages resolves provider file references with
// the fixed "openai" key; the openai-compatible chat models (baseten,
// cerebras, deepinfra) resolve their own provider name (TS #22466). A
// reference with no ID for the provider is an error, never an empty file part.
func TestChatFileReferenceResolvesProviderKey(t *testing.T) {
	cases := []struct {
		name         string
		cfg          Config
		wantProvider string
	}{
		{"openai", Config{APIKey: "k"}, "openai"},
		{"azure", Config{APIKey: "k", Name: "azure"}, "openai"},
		{"baseten", Config{APIKey: "k", ChatProviderName: "baseten.chat", OpenAICompatible: true}, "baseten"},
		// TS bedrock-mantle wraps OpenAIChatLanguageModel: fixed "openai" key.
		{"bedrock-mantle", Config{APIKey: "k", Name: "bedrock-mantle", ChatProviderName: "bedrock-mantle.chat"}, "openai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewLanguageModel(New(tc.cfg), "gpt-4o")
			body, _, err := m.buildRequestBodyWithWarnings(chatFileReferenceOptions(types.ProviderReference{tc.wantProvider: "file-123", "other": "file-other"}), false)
			if err != nil {
				t.Fatal(err)
			}
			want := []map[string]interface{}{{
				"role": "user",
				"content": []map[string]interface{}{{
					"type": "file", "file": map[string]interface{}{"file_id": "file-123"},
				}},
			}}
			if got := body["messages"]; !reflect.DeepEqual(got, want) {
				t.Errorf("messages = %#v\nwant %#v", got, want)
			}

			_, _, err = m.buildRequestBodyWithWarnings(chatFileReferenceOptions(types.ProviderReference{"other": "file-other"}), false)
			var noSuch *providererrors.NoSuchProviderReferenceError
			if !errors.As(err, &noSuch) || noSuch.Provider != tc.wantProvider {
				t.Errorf("err = %v, want NoSuchProviderReferenceError for %q", err, tc.wantProvider)
			}
		})
	}
}
