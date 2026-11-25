package cerebras

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provc r90 V061 class (error data = TS schema output): TS cerebras errors
// use createJsonErrorResponseHandler({errorSchema: cerebrasErrorSchema}): a
// matching body gives message and data {message, type, param, code}
// (undeclared keys stripped); any other body the status text and no data.
// Go kept the OpenAI-compatible heuristic message and the whole body.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	for body, want := range map[string]struct {
		message string
		data    interface{}
	}{
		`{"error":{"message":"bad","type":"x"},"other":2}`:               {"Bad Request", nil},
		`{"message":"m","type":"t","param":"p","code":"c","extra":true}`: {"m", map[string]interface{}{"message": "m", "type": "t", "param": "p", "code": "c"}},
	} {
		b := body
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(b))
		}))
		m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("llama3.1-8b")
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want.message || !reflect.DeepEqual(pe.Data, want.data) || pe.StatusCode != 400 {
			t.Errorf("%s: err = %#v, want message %q data %#v", body, err, want.message, want.data)
		}
	}
}
