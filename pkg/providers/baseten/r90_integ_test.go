package baseten

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

// provc r90 V061 class (error data = TS schema output): TS Baseten models use
// basetenErrorStructure: {error: string | {message, object?, type?, param?,
// code?}} gives the message and the zod output as data; any other body the
// status text and no data. Go used the generic heuristic and the whole body.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	for body, want := range map[string]struct {
		message string
		data    interface{}
	}{
		`{"error":"check the model","other":1}`:                             {"check the model", map[string]interface{}{"error": "check the model"}},
		`{"error":{"message":"bad","type":"x","extra":1,"code":400},"o":2}`: {"bad", map[string]interface{}{"error": map[string]interface{}{"message": "bad", "type": "x", "code": float64(400)}}},
		`{"foo":1}`:                             {"Bad Request", nil},
		`{"error":{"message":"m","code":true}}`: {"Bad Request", nil},
	} {
		b := body
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(b))
		}))
		m, err := New(Config{APIKey: "k", ModelURL: srv.URL + "/sync/v1"}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want.message || !reflect.DeepEqual(pe.Data, want.data) {
			t.Errorf("%s: err = %#v, want message %q data %#v", body, err, want.message, want.data)
		}
	}
}
