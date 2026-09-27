package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS: gateway-evaluation-model.test.ts createTestModel / basic request shape
func TestGatewayEvaluationModel_DoEvaluate_RequestShapeAndHeaders(t *testing.T) {
	var gotPath string
	var gotHeaders http.Header
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"answers": {"correct": {"type": "boolean", "probability": 0.97}}
		}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.EvaluationModel("typesafe-ai/jev-latest")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}

	result, err := model.DoEvaluate(context.Background(), provider.EvaluationCallOptions{
		State: "The capital of France is Paris.",
		Questions: map[string]provider.EvaluationQuestion{
			"correct": {Type: "boolean", Instructions: "Is the statement factually correct?"},
		},
		ProviderOptions: map[string]interface{}{"typesafe-ai": map[string]interface{}{"strict": true}},
	})
	if err != nil {
		t.Fatalf("DoEvaluate() error = %v", err)
	}

	if gotPath != "/evaluation-model" {
		t.Fatalf("path = %q, want /evaluation-model", gotPath)
	}
	if got := gotHeaders.Get("ai-model-id"); got != "typesafe-ai/jev-latest" {
		t.Fatalf("ai-model-id = %q", got)
	}
	if got := gotHeaders.Get("ai-evaluation-model-specification-version"); got != "4" {
		t.Fatalf("ai-evaluation-model-specification-version = %q, want 4", got)
	}
	if gotBody["state"] != "The capital of France is Paris." {
		t.Fatalf("state = %v", gotBody["state"])
	}
	questions, _ := gotBody["questions"].(map[string]interface{})
	correctQ, _ := questions["correct"].(map[string]interface{})
	if correctQ["type"] != "boolean" {
		t.Fatalf("question type = %v, want boolean", correctQ["type"])
	}
	providerOptions, _ := gotBody["providerOptions"].(map[string]interface{})
	if providerOptions["typesafe-ai"] == nil {
		t.Fatalf("providerOptions not forwarded: %+v", gotBody)
	}

	if model.Provider() != "gateway" {
		t.Fatalf("Provider() = %q, want gateway", model.Provider())
	}
	if model.ModelID() != "typesafe-ai/jev-latest" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}
	if result.Answers["correct"].Probability == nil || *result.Answers["correct"].Probability != 0.97 {
		t.Fatalf("Answers[correct] = %+v", result.Answers["correct"])
	}
}

// TS: gateway-evaluation-model.test.ts includes criteria only when the
// question defines it (boolean questions may omit criteria entirely).
func TestGatewayEvaluationModel_DoEvaluate_OmitsNilCriteria(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"answers": {}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.EvaluationModel("m")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}

	_, err = model.DoEvaluate(context.Background(), provider.EvaluationCallOptions{
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"q": {Type: "boolean", Instructions: "i"},
		},
	})
	if err != nil {
		t.Fatalf("DoEvaluate() error = %v", err)
	}

	questions, _ := gotBody["questions"].(map[string]interface{})
	q, _ := questions["q"].(map[string]interface{})
	if _, present := q["criteria"]; present {
		t.Fatalf("criteria should be omitted for a boolean question with nil Criteria: %+v", q)
	}
}

// TS: gateway-evaluation-model.test.ts maps rounding/usage/warnings/providerMetadata/response
func TestGatewayEvaluationModel_DoEvaluate_MapsFullResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"answers": {
				"quality": {"type": "score", "score": 1.8, "probabilities": {"0": 0.05, "1": 0.1, "2": 0.85}}
			},
			"rounding": {"probabilityDecimals": 2, "scoreDecimals": 1},
			"usage": {"inputTokens": 12, "outputTokens": 3},
			"warnings": [{"type": "other", "message": "note"}],
			"providerMetadata": {"gateway": {"routing": {"provider": "typesafe-ai"}}}
		}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.EvaluationModel("m")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}

	result, err := model.DoEvaluate(context.Background(), provider.EvaluationCallOptions{
		State: "s",
		Questions: map[string]provider.EvaluationQuestion{
			"quality": {Type: "score", Instructions: "Rate the quality.", Criteria: []interface{}{"Poor.", "Acceptable.", "Excellent."}},
		},
	})
	if err != nil {
		t.Fatalf("DoEvaluate() error = %v", err)
	}

	if result.Rounding == nil || result.Rounding.ProbabilityDecimals == nil || *result.Rounding.ProbabilityDecimals != 2 {
		t.Fatalf("Rounding = %+v", result.Rounding)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 12 {
		t.Fatalf("Usage = %+v", result.Usage)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Message != "note" {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
	if result.ProviderMetadata == nil {
		t.Fatalf("ProviderMetadata missing")
	}
	if result.Response == nil || result.Response.ModelID != "m" || result.Response.Headers["X-Request-Id"] != "req-123" {
		t.Fatalf("Response = %+v", result.Response)
	}
	qualityAnswer := result.Answers["quality"]
	if qualityAnswer.Score == nil || *qualityAnswer.Score != 1.8 {
		t.Fatalf("Answers[quality] = %+v", qualityAnswer)
	}
}

func TestGatewayEvaluationModel_SupportedQuestionTypes(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.EvaluationModel("m")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}
	types := model.SupportedQuestionTypes()
	if len(types) != 3 {
		t.Fatalf("SupportedQuestionTypes() = %v, want 3 entries", types)
	}
	if model.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", model.SpecificationVersion())
	}
}

func TestGatewayEvaluationModel_DoEvaluate_ErrorMapsToTypedGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad question","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model, err := p.EvaluationModel("m")
	if err != nil {
		t.Fatalf("EvaluationModel error = %v", err)
	}

	_, err = model.DoEvaluate(context.Background(), provider.EvaluationCallOptions{
		State:     "s",
		Questions: map[string]provider.EvaluationQuestion{"q": {Type: "boolean", Instructions: "i"}},
	})
	var invalidReq *gatewayerrors.GatewayInvalidRequestError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, ok := err.(*gatewayerrors.GatewayInvalidRequestError); ok {
		invalidReq = e
	}
	if invalidReq == nil {
		t.Fatalf("err = %v (%T), want GatewayInvalidRequestError", err, err)
	}
}
