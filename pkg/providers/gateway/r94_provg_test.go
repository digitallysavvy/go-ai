package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs sends the call options as given (minus abortSignal), so a
// caller's empty headers map is in the body as {}.
func TestR94provgEmptyHeadersForwarded(t *testing.T) {
	m := newTestGatewayLanguageModel("openai/gpt-5")
	body, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: map[string]string{}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := body["headers"].(map[string]string); !ok || len(h) != 0 {
		t.Fatalf("headers = %#v (present %v)", body["headers"], ok)
	}
	body, _ = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["headers"]; ok {
		t.Fatal("nil headers should be omitted")
	}
}

func r94provgDecide(t *testing.T, response string, po map[string]interface{}) (*provider.DecisionResult, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.DecisionModel("typesafe-ai/jev")
	if err != nil {
		t.Fatal(err)
	}
	return model.DoDecide(t.Context(), provider.DecisionCallOptions{
		State: "s", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}, ProviderOptions: po,
	})
}

// TS `responseBody.model ?? this.modelId`: a present empty model is kept.
func TestR94provgDecisionEmptyModelKept(t *testing.T) {
	res, err := r94provgDecide(t, `{"answers":{},"model":""}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.ModelID != "" {
		t.Fatalf("modelId = %q, want the response's empty model", res.Response.ModelID)
	}
}

// gatewayDecisionAnswerSchema and gatewayDecisionWarningSchema are
// discriminated unions: each member's zod object drops the keys only another
// member has.
func TestR94provgDecisionAnswerAndWarningKeysStripped(t *testing.T) {
	res, err := r94provgDecide(t, `{"answers":{
		"b":{"type":"boolean","probability":0.5,"choice":"x","score":2,"probabilities":{"x":1}},
		"c":{"type":"choice","choice":"x","probability":0.5,"score":2},
		"s":{"type":"score","score":2,"choice":"x","probability":0.5},
		"r":{"type":"refusal","choice":"x","probability":0.5}},
		"warnings":[
		{"type":"unsupported","feature":"f","details":"d","message":"m","setting":"s"},
		{"type":"deprecated","setting":"s","message":"m","feature":"f","details":"d"},
		{"type":"other","message":"m","feature":"f","details":"d","setting":"s"}]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	half, two := 0.5, 2.0
	want := map[string]provider.DecisionAnswer{
		"b": {Type: "boolean", Probability: &half},
		"c": {Type: "choice", Choice: "x"},
		"s": {Type: "score", Score: &two},
		"r": {Type: "refusal"},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %#v", res.Answers)
	}
	wantWarnings := []types.Warning{
		{Type: "unsupported", Feature: "f", Details: "d"},
		{Type: "deprecated", Setting: "s", Message: "m"},
		{Type: "other", Message: "m"},
	}
	if !reflect.DeepEqual(res.Warnings, wantWarnings) {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
}

// The other gateway models share the warning schema.
func TestR94provgWarningSchemaSweep(t *testing.T) {
	in := []types.Warning{{Type: "compatibility", Feature: "f", Details: "d", Message: "m"}}
	want := []types.Warning{{Type: "compatibility", Feature: "f", Details: "d"}}
	if got := warningsOrEmpty(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("warningsOrEmpty = %#v", got)
	}
	if got := convertWarningData([]warningData{{Type: "compatibility", Feature: "f", Details: "d", Message: "m"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("convertWarningData = %#v", got)
	}
	if got := warningsOrEmpty(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil warnings = %#v, want []", got)
	}
}

// TS parseProviderOptions with z.catchall(z.object(...)): a gateway
// namespace that is not an object fails the call before any request; null
// is no options.
func TestR94provgDecisionNonObjectGatewayOptions(t *testing.T) {
	_, err := r94provgDecide(t, `{"answers":{}}`, map[string]interface{}{"gateway": "x"})
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) || argErr.Message != "invalid gateway provider options" {
		t.Fatalf("err = %T %v", err, err)
	}
	if _, err := r94provgDecide(t, `{"answers":{}}`, map[string]interface{}{"gateway": nil}); err != nil {
		t.Fatalf("null namespace: %v", err)
	}
}
