package typesafeai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS sends the request body as model, state, questions with the questions
// and choice criteria in object insertion order; QuestionOrder and
// CriteriaOrder carry that order to Go.
func TestDecisionModel_HonorsQuestionAndCriteriaOrder(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1","answers":{"z":{"type":"choice","choice":"omega","probabilities":{"omega":1,"alpha":0}},"a":{"type":"noul","noul":0.5}}}`))
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).DecisionModel("jev-latest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"a": {Type: "boolean", Instructions: "A?"},
			"z": {Type: "choice", Instructions: "Z?", Criteria: map[string]interface{}{"alpha": nil, "omega": nil}},
		},
		QuestionOrder: []string{"z", "a"},
		CriteriaOrder: map[string][]string{"z": {"omega", "alpha"}},
	}); err != nil {
		t.Fatal(err)
	}
	want := `{"model":"jev-latest","state":"s","questions":{"z":{"type":"choice","instructions":"Z?","criteria":{"omega":null,"alpha":null}},"a":{"type":"noul","instructions":"A?"}}}`
	if got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

// The provider-limit check reports the first violating question in
// QuestionOrder, as TS reports the first in insertion order.
func TestDecisionModel_LimitErrorFollowsQuestionOrder(t *testing.T) {
	model, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).DecisionModel("jev-latest")
	if err != nil {
		t.Fatal(err)
	}
	levels := make([]interface{}, 11)
	for i := range levels {
		levels[i] = "level"
	}
	_, err = model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "s",
		Questions: map[string]provider.DecisionQuestion{
			"a": {Type: "score", Instructions: "A?", Criteria: levels},
			"b": {Type: "score", Instructions: "B?", Criteria: levels},
		},
		QuestionOrder: []string{"b", "a"},
	})
	ia, ok := err.(*providererrors.InvalidArgumentError)
	if !ok || ia.Field != "questions.b.criteria" {
		t.Fatalf("err = %v, want the questions.b.criteria limit error", err)
	}
}
