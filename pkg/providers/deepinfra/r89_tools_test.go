package deepinfra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func r89toolsImageError(t *testing.T, body string, edit bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL + "/inference"}).ImageModel("stabilityai/sd3.5")
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.ImageGenerateOptions{Prompt: "a cat"}
	if edit {
		opts.Files = []provider.ImageFile{{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"}}
	}
	_, err = model.DoGenerate(context.Background(), opts)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	return perr.Message
}

// TS deepinfra-image-model.ts createJsonErrorResponseHandler: a body that
// does not match deepInfraErrorSchema / deepInfraEditErrorSchema (or a blank
// one) gives the status text; a matching body gives errorToMessage.
func TestR89toolsDeepInfraImageErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		edit bool
		want string
	}{
		{"generate detail", `{"detail":{"error":"invalid model"}}`, false, "invalid model"},
		{"generate empty detail error", `{"detail":{"error":""}}`, false, ""},
		{"generate not json", `upstream failure`, false, "Bad Request"},
		{"generate schema mismatch", `{"error":"x"}`, false, "Bad Request"},
		{"generate blank", `  `, false, "Bad Request"},
		{"edit message", `{"error":{"message":"bad image"}}`, true, "bad image"},
		{"edit no error", `{}`, true, "Unknown error"},
		{"edit null error", `{"error":null}`, true, "Bad Request"},
		{"edit not json", `oops`, true, "Bad Request"},
		{"edit message missing", `{"error":{}}`, true, "Bad Request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r89toolsImageError(t, tc.body, tc.edit); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}
