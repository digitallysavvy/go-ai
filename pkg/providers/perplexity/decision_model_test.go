package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/perplexity/src/perplexity-decision-model.test.ts.

type decisionServer struct {
	*httptest.Server
	requests []*http.Request
	bodies   []map[string]interface{}
}

func fixtureBody(t *testing.T) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile("testdata/decisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func newDecisionServer(t *testing.T, body interface{}, status int) *decisionServer {
	t.Helper()
	s := &decisionServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var parsed map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&parsed)
		s.requests = append(s.requests, r)
		s.bodies = append(s.bodies, parsed)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "request-123")
		w.Header().Set("retry-after", "2")
		w.Header().Set("x-ratelimit-remaining", "9")
		w.WriteHeader(status)
		switch b := body.(type) {
		case string:
			_, _ = w.Write([]byte(b))
		default:
			_ = json.NewEncoder(w).Encode(b)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *decisionServer) model(t *testing.T, modelID string) provider.DecisionModel {
	t.Helper()
	m, err := New(Config{APIKey: "test-key", BaseURL: s.URL + "/", Headers: map[string]string{"x-provider": "custom", "shared": "provider"}}).DecisionModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func decisionOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"review": "Great sound, but the battery broke."})},
		Questions: map[string]provider.DecisionQuestion{
			"defect": {Type: "boolean", Instructions: "Does the review report a defect?"},
			"sentiment": {Type: "choice", Instructions: "What is the sentiment?", Criteria: map[string]interface{}{
				"positive": "Satisfied", "mixed": "Praise and complaints", "negative": "Dissatisfied",
			}},
			"severity": {Type: "score", Instructions: "How severe is the problem?", Criteria: []interface{}{"Cosmetic", "Inconvenient", "Product unusable"}},
		},
	}
}

func TestDecisionModel_SendsOneNativeRequestWithConfiguredHeaders(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	model := s.model(t, "pplx-decider-latest")
	opts := decisionOptions()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := model.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if model.SpecificationVersion() != "v4" || model.Provider() != "perplexity.decision" {
		t.Fatalf("model = %s / %s", model.SpecificationVersion(), model.Provider())
	}
	if !reflect.DeepEqual(model.SupportedQuestionTypes(), []string{"choice", "score", "boolean"}) {
		t.Fatalf("types = %v", model.SupportedQuestionTypes())
	}
	if len(s.requests) != 1 {
		t.Fatalf("requests = %d", len(s.requests))
	}
	r := s.requests[0]
	if r.URL.Path != "/v1/decisions" {
		t.Fatalf("path = %s", r.URL.Path)
	}
	for k, want := range map[string]string{
		"Authorization": "Bearer test-key", "Content-Type": "application/json", "X-Provider": "custom",
		"Shared": "call", "X-Pplx-Integration": "vercel-ai-sdk",
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "ai-sdk-perplexity/") {
		t.Errorf("user-agent = %q", ua)
	}
	body := s.bodies[0]
	if body["model"] != "pplx-decider-latest" {
		t.Fatalf("model = %v", body["model"])
	}
	if !reflect.DeepEqual(body["state"], map[string]interface{}{"review": "Great sound, but the battery broke."}) {
		t.Fatalf("state = %#v", body["state"])
	}
	q := body["questions"].(map[string]interface{})
	if q["defect"].(map[string]interface{})["type"] != "noul" || q["sentiment"].(map[string]interface{})["type"] != "choice" {
		t.Fatalf("questions = %#v", q)
	}
}

func TestDecisionModel_PreservesPrecisionConfidenceLegendUsageAndResponse(t *testing.T) {
	fixture := fixtureBody(t)
	s := newDecisionServer(t, fixture, 200)
	result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	answers := fixture["answers"].(map[string]interface{})
	defect := answers["defect"].(map[string]interface{})["noul"].(float64)
	if got := result.Answers["defect"]; got.Type != "boolean" || *got.Probability != defect {
		t.Fatalf("defect = %#v", got)
	}
	if got := result.Answers["sentiment"]; got.Type != "choice" || got.Choice != "mixed" || len(got.Probabilities) != 3 {
		t.Fatalf("sentiment = %#v", got)
	}
	if got := result.Answers["severity"]; got.Type != "score" || *got.Score != answers["severity"].(map[string]interface{})["score"].(float64) {
		t.Fatalf("severity = %#v", got)
	}
	if result.Rounding != nil {
		t.Fatalf("rounding = %#v, want nil", result.Rounding)
	}
	if *result.Usage.InputTokens != 367 || *result.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	meta := result.ProviderMetadata["perplexity"].(map[string]interface{})
	confidence := meta["confidence"].(map[string]float64)
	if len(confidence) != 2 || confidence["sentiment"] != 0.9255246944002182 || confidence["severity"] != 0.7838686319784252 {
		t.Fatalf("confidence = %v", confidence)
	}
	legend := meta["legend"].(map[string]interface{})
	if !reflect.DeepEqual(legend["severity"], map[string]interface{}{"0": "Cosmetic", "1": "Inconvenient", "2": "Product unusable"}) || len(legend) != 1 {
		t.Fatalf("legend = %v", legend)
	}
	if result.Response.ID != "request-123" || result.Response.ModelID != "pplx-decider-latest" ||
		result.Response.Headers["x-request-id"] != "request-123" || result.Response.Headers["x-ratelimit-remaining"] != "9" ||
		!reflect.DeepEqual(result.Response.Body, any(fixture)) {
		t.Fatalf("response = %#v", result.Response)
	}
}

func TestDecisionModel_PreservesStructuredInstructionsAndRubrics(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	opts := decisionOptions()
	opts.Questions = map[string]provider.DecisionQuestion{
		"defect": {Type: "boolean", Instructions: map[string]interface{}{"task": "Check for defects"},
			Criteria: map[string]interface{}{"true": []interface{}{"Broken"}, "false": nil}},
		"sentiment": {Type: "choice", Instructions: []interface{}{"Classify"}, Criteria: map[string]interface{}{
			"positive": map[string]interface{}{"description": "Satisfied"}, "mixed": []interface{}{"Praise", "Complaints"}, "negative": nil}},
		"severity": {Type: "score", Instructions: map[string]interface{}{"task": "Rate"},
			Criteria: []interface{}{map[string]interface{}{"impact": "None"}, []interface{}{"Some"}, "Blocking"}},
	}
	if _, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	q := s.bodies[0]["questions"].(map[string]interface{})
	want := map[string]interface{}{
		"defect":    map[string]interface{}{"type": "noul", "instructions": map[string]interface{}{"task": "Check for defects"}, "criteria": map[string]interface{}{"true": []interface{}{"Broken"}, "false": nil}},
		"sentiment": map[string]interface{}{"type": "choice", "instructions": []interface{}{"Classify"}, "criteria": map[string]interface{}{"positive": map[string]interface{}{"description": "Satisfied"}, "mixed": []interface{}{"Praise", "Complaints"}, "negative": nil}},
		"severity":  map[string]interface{}{"type": "score", "instructions": map[string]interface{}{"task": "Rate"}, "criteria": []interface{}{map[string]interface{}{"impact": "None"}, []interface{}{"Some"}, "Blocking"}},
	}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("questions = %#v\nwant %#v", q, want)
	}
}

func TestDecisionModel_WarnsAboutUnsupportedOptionsWithoutSendingThem(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	opts := decisionOptions()
	opts.ProviderOptions = map[string]interface{}{"perplexity": map[string]interface{}{"temperature": 0, "searchDomainFilter": []interface{}{"example.com"}}}
	result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 2 || result.Warnings[0].Feature != "providerOptions.perplexity.searchDomainFilter" || result.Warnings[1].Feature != "providerOptions.perplexity.temperature" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if len(s.bodies[0]) != 3 {
		t.Fatalf("request keys = %v", s.bodies[0])
	}
}

func TestDecisionModel_PassesModelIDsThrough(t *testing.T) {
	for _, id := range []string{"pplx-decider-v1.2", "pplx-decider-flash-v1.2", "pplx-decider-latest", "pplx-decider-flash-latest",
		"perplexity/pplx-decider-latest", "perplexity/pplx-decider-flash-latest", "pplx-decider-v1.1-27b", "pplx-decider-v1-27b", "future-model"} {
		fixture := fixtureBody(t)
		fixture["model"] = id
		s := newDecisionServer(t, fixture, 200)
		result, err := s.model(t, id).DoDecide(context.Background(), decisionOptions())
		if err != nil {
			t.Fatal(err)
		}
		if s.bodies[0]["model"] != id || result.Response.ModelID != id {
			t.Errorf("%s: model = %v / %s", id, s.bodies[0]["model"], result.Response.ModelID)
		}
	}
}

func TestDecisionModel_LoadsEnvironmentAuthenticationLazily(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	model, _ := New(Config{BaseURL: s.URL}).DecisionModel("pplx-decider-latest")
	t.Setenv("PERPLEXITY_API_KEY", "environment-key")
	if _, err := model.DoDecide(context.Background(), decisionOptions()); err != nil {
		t.Fatal(err)
	}
	if got := s.requests[0].Header.Get("Authorization"); got != "Bearer environment-key" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestDecisionModel_Serialization(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	model := s.model(t, "pplx-decider-latest").(*DecisionModel)
	serialized := model.Serialize()
	if serialized.Provider != "perplexity.decision" || serialized.ModelID != "pplx-decider-latest" {
		t.Fatalf("serialized = %#v", serialized)
	}
	restored, err := provider.DeserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Provider() != "perplexity.decision" || restored.ModelID() != "pplx-decider-latest" {
		t.Fatalf("restored = %s / %s", restored.Provider(), restored.ModelID())
	}
	restored2, err := deserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PERPLEXITY_API_KEY", "k")
	if _, err := restored2.DoDecide(context.Background(), decisionOptions()); err != nil {
		t.Fatal(err)
	}
	if got := s.requests[0].URL.Path; got != "/v1/decisions" {
		t.Fatalf("restored model path = %s", got)
	}
}

func TestDecisionModel_RequestLimits(t *testing.T) {
	defect := decisionOptions().Questions["defect"]
	tooMany := map[string]provider.DecisionQuestion{}
	for i := 0; i < 129; i++ {
		tooMany[fmt.Sprint(i)] = defect
	}
	manyOptions := map[string]interface{}{}
	for i := 0; i < 256; i++ {
		manyOptions[fmt.Sprint(i)] = nil
	}
	levels := make([]interface{}, 11)
	for i := range levels {
		levels[i] = fmt.Sprint(i)
	}
	cases := map[string]map[string]provider.DecisionQuestion{
		"empty":         {},
		"empty id":      {"": defect},
		"129 questions": tooMany,
		"empty choice":  {"sentiment": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{}}},
		"256 options":   {"sentiment": {Type: "choice", Instructions: "i", Criteria: manyOptions}},
		"empty score":   {"severity": {Type: "score", Instructions: "i", Criteria: []interface{}{}}},
		"null level":    {"severity": {Type: "score", Instructions: "i", Criteria: []interface{}{"Low", nil}}},
		"11 levels":     {"severity": {Type: "score", Instructions: "i", Criteria: levels}},
	}
	for name, questions := range cases {
		s := newDecisionServer(t, fixtureBody(t), 200)
		opts := decisionOptions()
		opts.Questions = questions
		_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts)
		var target *providererrors.InvalidArgumentError
		if !errors.As(err, &target) {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
		if len(s.requests) != 0 {
			t.Errorf("%s: HTTP request was sent", name)
		}
	}
}

func TestDecisionModel_RejectsBoxQuestions(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	opts := decisionOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"box": {Type: "box", Instructions: "Find the product"}}
	_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts)
	var target *providererrors.DecisionUnsupportedQuestionTypeError
	if !errors.As(err, &target) || len(s.requests) != 0 {
		t.Fatalf("err = %v, requests = %d", err, len(s.requests))
	}
}

func TestDecisionModel_AcceptsMaximumCounts(t *testing.T) {
	defect := decisionOptions().Questions["defect"]
	questions := map[string]provider.DecisionQuestion{}
	answers := map[string]interface{}{}
	for i := 0; i < 126; i++ {
		questions[fmt.Sprint(i)] = defect
		answers[fmt.Sprint(i)] = map[string]interface{}{"type": "noul", "noul": 0.5}
	}
	choiceCriteria := map[string]interface{}{}
	choiceProbs := map[string]interface{}{}
	for i := 0; i < 255; i++ {
		choiceCriteria[fmt.Sprint(i)] = nil
		choiceProbs[fmt.Sprint(i)] = 0
	}
	choiceProbs["0"] = 1
	scoreCriteria := make([]interface{}, 10)
	scoreProbs := map[string]interface{}{}
	for i := range scoreCriteria {
		scoreCriteria[i] = fmt.Sprint(i)
		scoreProbs[fmt.Sprint(i)] = 0
	}
	scoreProbs["0"] = 1
	questions["choice"] = provider.DecisionQuestion{Type: "choice", Instructions: "Pick", Criteria: choiceCriteria}
	questions["score"] = provider.DecisionQuestion{Type: "score", Instructions: "Rate", Criteria: scoreCriteria}
	answers["choice"] = map[string]interface{}{"type": "choice", "choice": "0", "probabilities": choiceProbs}
	answers["score"] = map[string]interface{}{"type": "score", "score": 0, "probabilities": scoreProbs}
	s := newDecisionServer(t, map[string]interface{}{"answers": answers}, 200)
	opts := decisionOptions()
	opts.Questions = questions
	if _, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 1 || len(s.bodies[0]["questions"].(map[string]interface{})) != 128 {
		t.Fatalf("requests = %d", len(s.requests))
	}
}

func TestDecisionModel_RejectsMissingAndUnexpectedAnswers(t *testing.T) {
	fixture := fixtureBody(t)
	answers := fixture["answers"].(map[string]interface{})
	extra := map[string]interface{}{"extra": answers["defect"]}
	for k, v := range answers {
		extra[k] = v
	}
	missing := map[string]interface{}{"sentiment": answers["sentiment"], "severity": answers["severity"]}
	for name, a := range map[string]interface{}{"empty": map[string]interface{}{}, "extra": extra, "missing": missing} {
		s := newDecisionServer(t, map[string]interface{}{"answers": a}, 200)
		_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Errorf("%s: err = %v, want InvalidResponseDataError", name, err)
		}
	}
}

func TestDecisionModel_RejectsMalformedNativeResponses(t *testing.T) {
	fixture := fixtureBody(t)
	answers := func(key string, value interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for k, v := range fixture["answers"].(map[string]interface{}) {
			out[k] = v
		}
		out[key] = value
		return out
	}
	cases := map[string]map[string]interface{}{
		"noul > 1":        answers("defect", map[string]interface{}{"type": "noul", "noul": 1.01}),
		"choice no probs": answers("sentiment", map[string]interface{}{"type": "choice", "choice": "mixed"}),
		"negative prob":   answers("severity", map[string]interface{}{"type": "score", "score": 1, "probabilities": map[string]interface{}{"0": -1}}),
		"refusal":         answers("defect", map[string]interface{}{"type": "refusal"}),
	}
	for name, a := range cases {
		s := newDecisionServer(t, map[string]interface{}{"answers": a}, 200)
		_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
		var target *providererrors.ProviderError
		if !errors.As(err, &target) || target.Message != providerutilsInvalidJSON() {
			t.Errorf("%s: err = %v, want an Invalid JSON response error", name, err)
		}
	}
}

func TestDecisionModel_ToleratesAbsentOrNullAncillaryFields(t *testing.T) {
	for _, absent := range []bool{true, false} {
		fixture := fixtureBody(t)
		answers := fixture["answers"].(map[string]interface{})
		set := func(m map[string]interface{}, key string) {
			if absent {
				delete(m, key)
			} else {
				m[key] = nil
			}
		}
		set(fixture, "model")
		set(fixture, "usage")
		set(answers["sentiment"].(map[string]interface{}), "confidence")
		set(answers["severity"].(map[string]interface{}), "confidence")
		set(answers["severity"].(map[string]interface{}), "legend")
		s := newDecisionServer(t, fixture, 200)
		result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
		if err != nil {
			t.Fatal(err)
		}
		meta := result.ProviderMetadata["perplexity"].(map[string]interface{})
		if result.Usage != nil || result.Response.ModelID != "pplx-decider-latest" ||
			len(meta["confidence"].(map[string]float64)) != 0 || len(meta["legend"].(map[string]interface{})) != 0 {
			t.Fatalf("absent=%v result = %#v meta=%v", absent, result, meta)
		}
	}
}

func TestDecisionModel_PreservesZeroUsageAndConfidence(t *testing.T) {
	fixture := fixtureBody(t)
	fixture["usage"] = map[string]interface{}{"input_tokens": 0, "output_tokens": nil}
	fixture["answers"].(map[string]interface{})["sentiment"].(map[string]interface{})["confidence"] = 0
	s := newDecisionServer(t, fixture, 200)
	result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 0 || result.Usage.OutputTokens != nil {
		t.Fatalf("usage = %#v", result.Usage)
	}
	confidence := result.ProviderMetadata["perplexity"].(map[string]interface{})["confidence"].(map[string]float64)
	if v, ok := confidence["sentiment"]; !ok || v != 0 {
		t.Fatalf("confidence = %v", confidence)
	}
}

func TestDecisionModel_PreservesErrorMessagesWithAnyCode(t *testing.T) {
	for _, code := range []interface{}{nil, "400", 401} {
		s := newDecisionServer(t, map[string]interface{}{"error": map[string]interface{}{
			"code": code, "message": "Invalid model", "type": "invalid_request_error", "param": nil,
		}}, 400)
		_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
		var target *providererrors.ProviderError
		if !errors.As(err, &target) || target.Message != "Invalid model" || target.StatusCode != 400 || target.IsRetryable() {
			t.Errorf("code %v: err = %v", code, err)
		}
	}
}

func TestDecisionModel_PreservesHTTPStatusAndRetryHeadersForNonJSONErrors(t *testing.T) {
	for _, status := range []int{404, 405, 413, 429, 500, 502, 503, 504} {
		body := ""
		if status == 504 {
			body = "<html>Gateway Timeout</html>"
		}
		s := newDecisionServer(t, body, status)
		_, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
		var target *providererrors.ProviderError
		if !errors.As(err, &target) || target.StatusCode != status || target.ResponseHeaders["retry-after"] != "2" ||
			target.IsRetryable() != (status == 429 || status >= 500) || target.Message != http.StatusText(status) {
			t.Errorf("status %d: err = %#v", status, err)
		}
	}
}

func TestDecisionModel_ForwardsCancellation(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.model(t, "pplx-decider-latest").DoDecide(ctx, decisionOptions())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDecisionModel_CriteriaOnlyQuestionsOnTheWire(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	opts := decisionOptions()
	opts.Questions = map[string]provider.DecisionQuestion{
		"defect":    {Type: "boolean", Criteria: map[string]interface{}{"true": []interface{}{"Broken"}, "false": nil}},
		"sentiment": {Type: "choice", Criteria: decisionOptions().Questions["sentiment"].Criteria},
		"severity":  {Type: "score", Criteria: decisionOptions().Questions["severity"].Criteria},
	}
	if _, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	for id, q := range s.bodies[0]["questions"].(map[string]interface{}) {
		if _, ok := q.(map[string]interface{})["instructions"]; ok {
			t.Errorf("%s: instructions must be omitted", id)
		}
	}
}

func TestDecisionModel_SingleScoreLevel(t *testing.T) {
	s := newDecisionServer(t, map[string]interface{}{
		"model": "pplx-decider-latest",
		"answers": map[string]interface{}{"severity": map[string]interface{}{
			"type": "score", "score": 0, "probabilities": map[string]interface{}{"0": 1}, "confidence": 1,
			"legend": map[string]interface{}{"0": "Product unusable"},
		}},
	}, 200)
	opts := decisionOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"severity": {Type: "score", Criteria: []interface{}{"Product unusable"}}}
	result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Answers["severity"]
	if got.Type != "score" || *got.Score != 0 || got.Probabilities["0"] != 1 {
		t.Fatalf("answer = %#v", got)
	}
	meta := result.ProviderMetadata["perplexity"].(map[string]interface{})
	if meta["confidence"].(map[string]float64)["severity"] != 1 {
		t.Fatalf("meta = %v", meta)
	}
	q := s.bodies[0]["questions"].(map[string]interface{})["severity"].(map[string]interface{})
	if !reflect.DeepEqual(q, map[string]interface{}{"type": "score", "criteria": []interface{}{"Product unusable"}}) {
		t.Fatalf("question = %#v", q)
	}
}

func providerutilsInvalidJSON() string { return "Invalid JSON response" }

// TS #23590.
func TestDecisionModel_WarnsAboutUnsupportedMaxOutputTokensAndReasoning(t *testing.T) {
	s := newDecisionServer(t, fixtureBody(t), 200)
	opts := decisionOptions()
	n, level := 512, types.ReasoningHigh
	opts.MaxOutputTokens, opts.Reasoning = &n, &level
	result, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 2 || result.Warnings[0].Feature != "maxOutputTokens" || result.Warnings[1].Feature != "reasoning" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if len(s.bodies[0]) != 3 {
		t.Fatalf("request keys = %v", s.bodies[0])
	}
}

// QuestionOrder and CriteriaOrder keep the TS object order in the request
// (the questions and choice criteria are JSON objects).
func TestDecisionModel_KeepsTSQuestionAndCriteriaOrder(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixtureBody(t))
	}))
	t.Cleanup(srv.Close)
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).DecisionModel("pplx-decider-latest")
	if err != nil {
		t.Fatal(err)
	}
	opts := decisionOptions()
	opts.QuestionOrder = []string{"severity", "sentiment", "defect"}
	opts.CriteriaOrder = map[string][]string{"sentiment": {"positive", "negative", "mixed"}}
	_, _ = m.DoDecide(context.Background(), opts)
	body := string(raw)
	if !(strings.Index(body, `"severity"`) < strings.Index(body, `"sentiment"`) && strings.Index(body, `"sentiment"`) < strings.Index(body, `"defect"`)) {
		t.Fatalf("question order not kept: %s", body)
	}
	if !(strings.Index(body, `"positive"`) < strings.Index(body, `"negative"`) && strings.Index(body, `"negative"`) < strings.Index(body, `"mixed"`)) {
		t.Fatalf("criteria order not kept: %s", body)
	}
}

// TS `modelId: response.model ?? this.modelId` only falls back on null.
func TestDecisionModel_EmptyResponseModelIsKept(t *testing.T) {
	body := fixtureBody(t)
	body["model"] = ""
	s := newDecisionServer(t, body, 200)
	res, err := s.model(t, "pplx-decider-latest").DoDecide(context.Background(), decisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.ModelID != "" {
		t.Fatalf("ModelID = %q", res.Response.ModelID)
	}
}
