package anthropic

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createJsonResponseHandler(anthropicResponseSchema): a non-conforming 200
// body is an APICallError "Invalid JSON response" caused by a
// TypeValidationError; a conforming one decodes as before.
func TestDoGenerate_ResponseSchemaMismatchIsInvalidJSONResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		wantErr bool
	}{
		"missing usage":         {`{"type":"message","content":[{"type":"text","text":"hi"}]}`, true},
		"text block no text":    {`{"type":"message","content":[{"type":"text"}],"usage":{"input_tokens":1,"output_tokens":1}}`, true},
		"unknown block type":    {`{"type":"message","content":[{"type":"bogus"}],"usage":{"input_tokens":1,"output_tokens":1}}`, true},
		"wrong type literal":    {`{"type":"error","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`, true},
		"minimal valid":         {`{"type":"message","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1}}`, false},
		"container_upload ok":   {`{"type":"message","content":[{"type":"container_upload","file_id":"f"}],"usage":{"input_tokens":1,"output_tokens":1}}`, false},
		"tool_use input object": {`{"type":"message","content":[{"type":"tool_use","id":"t","name":"n","input":{"a":1}}],"usage":{"input_tokens":1,"output_tokens":1}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			prov := New(Config{APIKey: "k", BaseURL: server.URL})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
			_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			var perr *providererrors.ProviderError
			isInvalid := errors.As(err, &perr) && perr.Message == "Invalid JSON response"
			if tc.wantErr {
				if !isInvalid || perr.StatusCode != 200 || !providererrors.IsValidationError(perr.Cause) {
					t.Fatalf("err = %#v, want Invalid JSON response caused by a validation error", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
