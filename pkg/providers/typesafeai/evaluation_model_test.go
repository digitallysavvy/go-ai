package typesafeai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from packages/typesafe-ai/src/typesafe-ai-evaluation-model.test.ts,
// using inline fixtures equivalent to __fixtures__/evaluation-{request,response}.json.

func nativeQuestions() map[string]provider.EvaluationQuestion {
	return map[string]provider.EvaluationQuestion{
		"department": {
			Type:         "choice",
			Instructions: map[string]interface{}{"question": "Which team should handle this?"},
			Criteria: map[string]interface{}{
				"billing":   map[string]interface{}{"includes": []interface{}{"Charges", "Invoices", "Refunds"}},
				"technical": []interface{}{"Bugs", "Outages"},
				"other":     nil,
			},
		},
		"severity": {
			Type:         "score",
			Instructions: []interface{}{"How severe is the issue?"},
			Criteria: []interface{}{
				map[string]interface{}{"meaning": "Cosmetic; functionality works"},
				"Functionality impaired; workaround exists",
				"Blocking; no workaround",
			},
		},
		"requestsRefund": {
			Type:         "boolean",
			Instructions: "Is the customer requesting a refund?",
			Criteria: map[string]interface{}{
				"true":  map[string]interface{}{"meaning": "Explicit request for money back"},
				"false": nil,
			},
		},
	}
}

const nativeResponseJSON = `{
  "model": "jev-1.13.0",
  "answers": {
    "department": {"type":"choice","choice":"billing","confidence":1.0,"probabilities":{"technical":0.0,"other":0.0,"billing":1.0}},
    "severity": {"type":"score","score":0.97,"confidence":0.64,"probabilities":{"0":0.13,"1":0.76,"2":0.11}},
    "requestsRefund": {"type":"noul","noul":0.99}
  },
  "usage": {"input_tokens": 471, "output_tokens": 71}
}`

func newTestServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestEvaluationModel_SendsAllThreeQuestionTypes(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nativeResponseJSON))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, err := p.EvaluationModel("jev-latest")
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}

	_, err = model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State:     map[string]interface{}{"message": "I was charged twice. Please refund the duplicate.", "order": map[string]interface{}{"amount": float64(49)}},
		Questions: nativeQuestions(),
	})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if gotPath != "/systemone" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBody["model"] != "jev-latest" {
		t.Fatalf("model = %v", gotBody["model"])
	}
	questions, _ := gotBody["questions"].(map[string]interface{})
	refund, _ := questions["requestsRefund"].(map[string]interface{})
	if refund["type"] != "noul" {
		t.Fatalf("boolean question should be sent as noul, got %v", refund["type"])
	}
}

func TestEvaluationModel_PreservesScoresProbabilitiesConfidenceUsage(t *testing.T) {
	server := newTestServer(t, nativeResponseJSON)
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")

	result, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State:     "test",
		Questions: nativeQuestions(),
	})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}

	dept := result.Answers["department"]
	if dept.Type != "choice" || dept.Choice != "billing" || dept.Probabilities["billing"] != 1 {
		t.Fatalf("department answer = %+v", dept)
	}
	sev := result.Answers["severity"]
	if sev.Type != "score" || sev.Score == nil || *sev.Score != 0.97 {
		t.Fatalf("severity answer = %+v", sev)
	}
	refund := result.Answers["requestsRefund"]
	if refund.Type != "boolean" || refund.Probability == nil || *refund.Probability != 0.99 {
		t.Fatalf("requestsRefund answer = %+v", refund)
	}
	if result.Rounding == nil || *result.Rounding.ProbabilityDecimals != 2 || *result.Rounding.ScoreDecimals != 2 {
		t.Fatalf("rounding = %+v", result.Rounding)
	}
	meta, _ := result.ProviderMetadata["typesafe"].(map[string]interface{})
	confidence, _ := meta["confidence"].(map[string]float64)
	if confidence["department"] != 1 || confidence["severity"] != 0.64 {
		t.Fatalf("confidence = %+v", confidence)
	}
	if result.Usage == nil || *result.Usage.InputTokens != 471 || *result.Usage.OutputTokens != 71 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.Response == nil || result.Response.ModelID != "jev-1.13.0" {
		t.Fatalf("response = %+v", result.Response)
	}
}

func TestEvaluationModel_CustomBaseURLHeadersAndFutureModelIDs(t *testing.T) {
	var gotAuth, gotCustom, gotShared string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("custom")
		gotShared = r.Header.Get("shared")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nativeResponseJSON))
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "custom-key",
		BaseURL: server.URL,
		Headers: map[string]string{"custom": "provider", "shared": "provider"},
	})
	model, _ := p.EvaluationModel("jev-future")
	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State:     "test",
		Questions: nativeQuestions(),
		Headers:   map[string]string{"shared": "call"},
	})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if gotAuth != "Bearer custom-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotCustom != "provider" {
		t.Fatalf("custom header = %q", gotCustom)
	}
	if gotShared != "call" {
		t.Fatalf("shared header should be overridden by call headers, got %q", gotShared)
	}
}

func TestEvaluationModel_LoadsAPIKeyLazilyFromEnvironment(t *testing.T) {
	server := newTestServer(t, nativeResponseJSON)
	p := New(Config{BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")

	t.Setenv("TYPESAFE_AI_API_KEY", "environment-key")

	var gotAuth string
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nativeResponseJSON))
	})

	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if gotAuth != "Bearer environment-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestEvaluationModel_MissingAPIKey(t *testing.T) {
	old := os.Getenv("TYPESAFE_AI_API_KEY")
	_ = os.Unsetenv("TYPESAFE_AI_API_KEY")
	defer func() { _ = os.Setenv("TYPESAFE_AI_API_KEY", old) }()

	p := New(Config{})
	model, _ := p.EvaluationModel("jev-latest")
	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
	if err == nil {
		t.Fatal("expected an error when no API key is configured")
	}
}

func TestEvaluationModel_ReportsUnsupportedProviderOptions(t *testing.T) {
	server := newTestServer(t, nativeResponseJSON)
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")

	result, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State:           "test",
		Questions:       nativeQuestions(),
		ProviderOptions: map[string]interface{}{"typesafe": map[string]interface{}{"temperature": float64(0)}},
	})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "providerOptions.typesafe.temperature" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

func TestEvaluationModel_RejectsProviderLimitsBeforeHTTP(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")

	criteria := make(map[string]interface{}, 256)
	for i := 0; i < 256; i++ {
		criteria[string(rune('a'+i%26))+string(rune(i))] = nil
	}
	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State: "test",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "choice", Instructions: "Pick", Criteria: criteria},
		},
	})
	if err == nil {
		t.Fatal("expected an InvalidArgumentError")
	}
	if _, ok := err.(*providererrors.InvalidArgumentError); !ok {
		t.Fatalf("err type = %T", err)
	}
	if called {
		t.Fatal("HTTP request should not have been made")
	}

	levels := make([]interface{}, 11)
	for i := range levels {
		levels[i] = "level"
	}
	_, err = model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{
		State: "test",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "score", Instructions: "Rate", Criteria: levels},
		},
	})
	if err == nil {
		t.Fatal("expected an InvalidArgumentError")
	}
}

func TestEvaluationModel_HTTPErrors(t *testing.T) {
	cases := []struct {
		status      int
		body        string
		wantMessage string
	}{
		{401, `{"detail":"Provider error"}`, "Provider error"},
		{422, `{"error":{"message":"Overloaded"}}`, "Overloaded"},
		{422, `{"error":"Overloaded"}`, "Overloaded"},
		{422, `{"message":"Overloaded"}`, "Overloaded"},
		{400, `{"error_type":"max_tokens_exceeded"}`, "max_tokens_exceeded"},
		{400, `{"error_type":"max_tokens_exceeded","message":"Input exceeds the model context window"}`, "Input exceeds the model context window"},
		{400, `{"unrecognised":"shape"}`, "TypeSafe request failed"},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		p := New(Config{APIKey: "k", BaseURL: server.URL})
		model, _ := p.EvaluationModel("jev-latest")
		_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
		server.Close()
		if err == nil {
			t.Fatalf("status %d: expected an error", tc.status)
		}
		pe, ok := err.(*providererrors.ProviderError)
		if !ok {
			t.Fatalf("status %d: err type = %T", tc.status, err)
		}
		if pe.StatusCode != tc.status {
			t.Fatalf("status %d: got status %d", tc.status, pe.StatusCode)
		}
		if pe.Message != tc.wantMessage {
			t.Fatalf("status %d: message = %q, want %q", tc.status, pe.Message, tc.wantMessage)
		}
	}
}

func TestEvaluationModel_RejectsMalformedSuccessfulResponse(t *testing.T) {
	server := newTestServer(t, `{"answers":{"test":{"type":"noul"}}}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")
	_, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !providererrors.IsInvalidResponseDataError(err) {
		t.Fatalf("err type = %T", err)
	}
}

func TestEvaluationModel_AcceptsNullOptionalMetadata(t *testing.T) {
	server := newTestServer(t, `{"model":null,"usage":null,"answers":{"topic":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":null}}}`)
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.EvaluationModel("jev-latest")
	result, err := model.DoEvaluate(t.Context(), provider.EvaluationCallOptions{State: "test", Questions: nativeQuestions()})
	if err != nil {
		t.Fatalf("DoEvaluate: %v", err)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.Response.ModelID != "jev-latest" {
		t.Fatalf("modelID = %q", result.Response.ModelID)
	}
	meta, _ := result.ProviderMetadata["typesafe"].(map[string]interface{})
	confidence, _ := meta["confidence"].(map[string]float64)
	if len(confidence) != 0 {
		t.Fatalf("confidence = %+v", confidence)
	}
}
