package typesafeai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS typesafe-ai-decision-model.test.ts (c70adf670b).
func TestR88provcTypeSafeOpenAPIParity(t *testing.T) {
	var gotBody map[string]interface{}
	response := `{"model":"jev-latest","answers":{"severity":{"type":"score","score":1,"probabilities":{"0":0,"1":1},"confidence":0.9,"legend":{"0":"low","1":{"meaning":"high"}}},"department":{"type":"choice","choice":"billing","probabilities":{"billing":1}}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	model, _ := New(Config{APIKey: "k", BaseURL: server.URL}).DecisionModel("jev-latest")

	t.Run("criteria limits", func(t *testing.T) {
		levels := make([]interface{}, 11)
		for i := range levels {
			levels[i] = "l"
		}
		for _, tc := range []struct {
			q    provider.DecisionQuestion
			want string
		}{
			{provider.DecisionQuestion{Type: "choice", Instructions: "x", Criteria: map[string]interface{}{}}, "TypeSafe Choice questions require 1 to 255 options."},
			{provider.DecisionQuestion{Type: "score", Instructions: "x", Criteria: []interface{}{}}, "TypeSafe Score questions require 1 to 10 non-null levels."},
			{provider.DecisionQuestion{Type: "score", Instructions: "x", Criteria: levels}, "TypeSafe Score questions require 1 to 10 non-null levels."},
			{provider.DecisionQuestion{Type: "score", Instructions: "x", Criteria: []interface{}{"a", nil}}, "TypeSafe Score questions require 1 to 10 non-null levels."},
		} {
			gotBody = nil
			_, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{State: "s", Questions: map[string]provider.DecisionQuestion{"q": tc.q}})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || ia.Field != "questions.q.criteria" || err.Error() != tc.want || gotBody != nil {
				t.Fatalf("%+v: err = %v", tc.q, err)
			}
		}
	})

	t.Run("a primitive JSON state is sent as text; objects, arrays and strings natively", func(t *testing.T) {
		for _, tc := range []struct {
			state interface{}
			want  interface{}
		}{
			{42.0, "42"}, {true, "true"}, {nil, "null"}, {"text", "text"},
			{map[string]interface{}{"a": 1.0}, map[string]interface{}{"a": 1.0}}, {[]interface{}{1.0}, []interface{}{1.0}},
		} {
			_, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{
				State:     []provider.DecisionStatePart{provider.DecisionJSONPart(tc.state)},
				Questions: nativeQuestions(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotBody["state"], tc.want) {
				t.Fatalf("%v: state = %#v", tc.state, gotBody["state"])
			}
		}
	})

	t.Run("score legends reach provider metadata", func(t *testing.T) {
		res, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{State: "s", Questions: nativeQuestions()})
		if err != nil {
			t.Fatal(err)
		}
		meta := res.ProviderMetadata["typesafe"].(map[string]interface{})
		want := map[string]interface{}{"severity": map[string]interface{}{"0": "low", "1": map[string]interface{}{"meaning": "high"}}}
		if !reflect.DeepEqual(meta["legend"], want) {
			t.Fatalf("legend = %#v", meta["legend"])
		}
	})
}
