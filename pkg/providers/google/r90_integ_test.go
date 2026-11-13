package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provg r90 open item: TS sends response_modalities whenever the option is
// not null (google-interactions-language-model.ts 455-460), so an empty
// list is sent as []. Go dropped it (omitempty). An absent option stays out.
func TestR90integInteractionsEmptyResponseModalities(t *testing.T) {
	for name, tc := range map[string]struct {
		options interface{}
		want    interface{}
		present bool
	}{
		"typed empty":  {GoogleInteractionsProviderOptions{ResponseModalities: []string{}}, []interface{}{}, true},
		"map empty":    {map[string]interface{}{"responseModalities": []interface{}{}}, []interface{}{}, true},
		"typed absent": {GoogleInteractionsProviderOptions{}, nil, false},
	} {
		var mu sync.Mutex
		var captured map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&captured)
			mu.Unlock()
			_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"ok"}]}]}`)
		}))
		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		model, err := p.Interactions(InteractionsModelGemini3ProImage)
		if err != nil {
			t.Fatal(err)
		}
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hi"},
			ProviderOptions: map[string]interface{}{"google": tc.options},
		})
		server.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		mu.Lock()
		got, ok := captured["response_modalities"]
		mu.Unlock()
		if ok != tc.present || (tc.present && !reflect.DeepEqual(got, tc.want)) {
			t.Errorf("%s: response_modalities = %#v (present %v), want %#v (present %v)", name, got, ok, tc.want, tc.present)
		}
	}
}
