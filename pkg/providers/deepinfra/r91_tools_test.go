package deepinfra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// tools-08 (TS deepinfra-image-model.ts:233, createJsonErrorResponseHandler):
// the error data is the error schema's zod output, with undeclared keys
// stripped; a body that does not match gives the status text and no data.
func TestR91toolsDeepInfraImageErrorData(t *testing.T) {
	cases := []struct {
		name, body string
		edit       bool
		message    string
		data       interface{}
	}{
		{"generate", `{"detail":{"error":"bad","loc":[1]},"request_id":"r"}`, false, "bad", map[string]interface{}{"detail": map[string]interface{}{"error": "bad"}}},
		{"generate mismatch", `{"detail":"bad"}`, false, "Bad Request", nil},
		{"edit", `{"error":{"message":"m","x":1},"request_id":"r"}`, true, "m", map[string]interface{}{"error": map[string]interface{}{"message": "m"}}},
		{"edit without error", `{"request_id":"r"}`, true, "Unknown error", map[string]interface{}{}},
		{"edit null error", `{"error":null}`, true, "Bad Request", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			model, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
			opts := &provider.ImageGenerateOptions{Prompt: "a cat"}
			if tc.edit {
				opts.Files = []provider.ImageFile{{Type: "file", Data: []byte{1}, MediaType: "image/png"}}
			}
			_, err := model.DoGenerate(context.Background(), opts)
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v", err)
			}
			if pe.Message != tc.message || pe.StatusCode != 400 || !reflect.DeepEqual(pe.Data, tc.data) {
				t.Fatalf("message %q data %#v, want %q %#v", pe.Message, pe.Data, tc.message, tc.data)
			}
		})
	}
}
