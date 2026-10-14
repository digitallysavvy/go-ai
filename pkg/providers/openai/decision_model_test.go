package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/openai/src/openai-decision.test.ts. The Decisions API
// is exercised against an httptest server instead of a mocked fetch.

const decisionFixture = `{
  "answers": [
    {"name": "department", "type": "choice", "choice": "billing", "confidence": 0.88,
     "probabilities": [{"value": "technical", "probability": 0.08}, {"value": "billing", "probability": 0.92}]},
    {"name": "severity", "type": "score", "score": 0.98, "confidence": 0.79,
     "probabilities": [{"value": 0, "label": "0", "probability": 0.08}, {"value": 1, "label": "1", "probability": 0.86}, {"value": 2, "label": "2", "probability": 0.06}]},
    {"name": "refund", "type": "predicate", "probability": 0.96}
  ]
}`

type decisionRecorder struct {
	mu      sync.Mutex
	path    string
	headers http.Header
	body    map[string]interface{}
	calls   int
}

func newDecisionProvider(t *testing.T, responseBody string, status int) (*Provider, *decisionRecorder) {
	t.Helper()
	rec := &decisionRecorder{}
	url, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.calls++
		rec.path = r.URL.Path
		rec.headers = r.Header.Clone()
		rec.body = nil
		_ = json.NewDecoder(r.Body).Decode(&rec.body)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "req-test")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(closeServer)
	return New(Config{APIKey: "test-key", BaseURL: url + "/v1", Organization: "org-test"}), rec
}

func decisionTestOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart("A billing issue with a workaround.")},
		Questions: map[string]provider.DecisionQuestion{
			"department": {Type: "choice", Instructions: "Pick the team.", Criteria: map[string]interface{}{"technical": "Bugs", "billing": "Charges"}},
			"severity":   {Type: "score", Instructions: "Rate severity.", Criteria: []interface{}{"Low", "Medium", "High"}},
			"refund":     {Type: "boolean", Instructions: "Is a refund requested?"},
		},
	}
}

func decide(t *testing.T, p *Provider, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	t.Helper()
	m, err := p.DecisionModel("gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	return m.DoDecide(context.Background(), opts)
}

func TestDecisionModel_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k", Name: "custom"})
	m, _ := p.DecisionModel("gpt-6-luna")
	if m.SpecificationVersion() != "v4" || m.Provider() != "custom.decision" || m.ModelID() != "gpt-6-luna" {
		t.Fatalf("metadata = %s %s %s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
	if !reflect.DeepEqual(m.SupportedQuestionTypes(), []string{"choice", "score", "boolean"}) {
		t.Fatalf("types = %v", m.SupportedQuestionTypes())
	}
	if New(Config{APIKey: "k"}).Name()+".decision" != "openai.decision" {
		t.Fatal("default provider name")
	}
}

func TestDecisionModel_SendsAllPrimitivesDirectlyToDecisions(t *testing.T) {
	p, rec := newDecisionProvider(t, decisionFixture, 200)
	opts := decisionTestOptions()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := decide(t, p, opts); err != nil {
		t.Fatal(err)
	}
	if rec.path != "/v1/decisions" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.headers.Get("Authorization") != "Bearer test-key" || rec.headers.Get("OpenAI-Organization") != "org-test" || rec.headers.Get("shared") != "call" {
		t.Fatalf("headers = %v", rec.headers)
	}
	if !strings.Contains(rec.headers.Get("User-Agent"), "ai-sdk-openai/") {
		t.Fatalf("user agent = %q", rec.headers.Get("User-Agent"))
	}
	// Go maps are unordered, so questions and choices are sent sorted by name.
	want := map[string]interface{}{
		"model": "gpt-6-luna",
		"input": []interface{}{map[string]interface{}{
			"role":    "user",
			"content": []interface{}{map[string]interface{}{"type": "input_text", "text": "A billing issue with a workaround."}},
		}},
		"questions": []interface{}{
			map[string]interface{}{"type": "choice", "name": "department", "instructions": "Pick the team.", "choices": []interface{}{
				map[string]interface{}{"value": "billing", "description": "Charges"},
				map[string]interface{}{"value": "technical", "description": "Bugs"},
			}},
			map[string]interface{}{"type": "predicate", "name": "refund", "instructions": "Is a refund requested?"},
			map[string]interface{}{"type": "score", "name": "severity", "instructions": "Rate severity.", "levels": []interface{}{
				map[string]interface{}{"label": "0", "description": "Low"},
				map[string]interface{}{"label": "1", "description": "Medium"},
				map[string]interface{}{"label": "2", "description": "High"},
			}},
		},
	}
	if !reflect.DeepEqual(rec.body, want) {
		got, _ := json.MarshalIndent(rec.body, "", " ")
		t.Fatalf("body = %s", got)
	}
}

func TestDecisionModel_MapsNamedAnswersConfidenceAndMetadata(t *testing.T) {
	// Reverse answer order: results are keyed by name.
	reversed := `{"answers": [
	  {"name": "refund", "type": "predicate", "probability": 0.96},
	  {"name": "severity", "type": "score", "score": 0.98, "confidence": 0.79, "probabilities": [{"value": 0, "probability": 0.08}, {"value": 1, "probability": 0.86}, {"value": 2, "probability": 0.06}]},
	  {"name": "department", "type": "choice", "choice": "billing", "confidence": 0.88, "probabilities": [{"value": "technical", "probability": 0.08}, {"value": "billing", "probability": 0.92}]}
	]}`
	p, _ := newDecisionProvider(t, reversed, 200)
	result, err := decide(t, p, decisionTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	score, prob := 0.98, 0.96
	want := map[string]provider.DecisionAnswer{
		"department": {Type: "choice", Choice: "billing", Probabilities: map[string]float64{"technical": 0.08, "billing": 0.92}},
		"severity":   {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0.08, "1": 0.86, "2": 0.06}},
		"refund":     {Type: "boolean", Probability: &prob},
	}
	if !reflect.DeepEqual(result.Answers, want) {
		t.Fatalf("answers = %+v", result.Answers)
	}
	if !reflect.DeepEqual(result.ProviderMetadata, map[string]interface{}{"openai": map[string]interface{}{"confidence": map[string]interface{}{"department": 0.88, "severity": 0.79}}}) {
		t.Fatalf("metadata = %#v", result.ProviderMetadata)
	}
	if result.Rounding == nil || *result.Rounding.ProbabilityDecimals != 2 || *result.Rounding.ScoreDecimals != 2 {
		t.Fatalf("rounding = %+v", result.Rounding)
	}
	if result.Usage != nil {
		t.Fatalf("usage = %+v, want nil", result.Usage)
	}
	if result.Response.ModelID != "gpt-6-luna" || result.Response.Headers["X-Request-Id"] != "req-test" && result.Response.Headers["x-request-id"] != "req-test" {
		t.Fatalf("response = %+v", result.Response)
	}
}

func TestDecisionModel_SerializesStructuredInputAndRubrics(t *testing.T) {
	p, rec := newDecisionProvider(t, decisionFixture, 200)
	_, err := decide(t, p, provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"ticket": []interface{}{"charged twice"}})},
		Questions: map[string]provider.DecisionQuestion{
			"department": {Type: "choice", Instructions: map[string]interface{}{"task": "route"}, Criteria: map[string]interface{}{"technical": nil, "billing": map[string]interface{}{"rubric": []interface{}{"charges"}}}},
			"severity":   {Type: "score", Instructions: []interface{}{"severity"}, Criteria: []interface{}{nil, map[string]interface{}{"meaning": "high"}}},
			"refund": {Type: "boolean", Instructions: "Refund?", Criteria: map[string]interface{}{
				"true":  map[string]interface{}{"meaning": "Explicit request for money back"},
				"false": "The customer only asks about refund status.",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := rec.body["input"].([]interface{})[0].(map[string]interface{})["content"]
	if !reflect.DeepEqual(input, []interface{}{map[string]interface{}{"type": "input_text", "text": `{"ticket":["charged twice"]}`}}) {
		t.Fatalf("input = %v", input)
	}
	questions := rec.body["questions"].([]interface{})
	department := questions[0].(map[string]interface{})
	if department["instructions"] != `{"task":"route"}` {
		t.Fatalf("department = %v", department)
	}
	choices := department["choices"].([]interface{})
	if !reflect.DeepEqual(choices[0], map[string]interface{}{"value": "billing", "description": `{"rubric":["charges"]}`}) ||
		!reflect.DeepEqual(choices[1], map[string]interface{}{"value": "technical"}) {
		t.Fatalf("choices = %v", choices)
	}
	refund := questions[1].(map[string]interface{})
	if refund["instructions"] != "Refund?\n\nCriteria for true:\n{\"meaning\":\"Explicit request for money back\"}\n\nCriteria for false:\nThe customer only asks about refund status." {
		t.Fatalf("refund = %v", refund["instructions"])
	}
	severity := questions[2].(map[string]interface{})
	levels := severity["levels"].([]interface{})
	if severity["instructions"] != `["severity"]` || !reflect.DeepEqual(levels[0], map[string]interface{}{"label": "0"}) ||
		!reflect.DeepEqual(levels[1], map[string]interface{}{"label": "1", "description": `{"meaning":"high"}`}) {
		t.Fatalf("severity = %v", severity)
	}
}

func TestDecisionModel_FormatsOnlySuppliedBooleanCriteria(t *testing.T) {
	cases := []struct {
		criteria interface{}
		expected string
	}{
		{nil, "Refund?"},
		{map[string]interface{}{}, "Refund?"},
		{map[string]interface{}{"true": nil, "false": nil}, "Refund?"},
		{map[string]interface{}{"true": "Money back"}, "Refund?\n\nCriteria for true:\nMoney back"},
		{map[string]interface{}{"false": []interface{}{"Status request"}}, "Refund?\n\nCriteria for false:\n[\"Status request\"]"},
	}
	for _, tc := range cases {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		opts.Questions["refund"] = provider.DecisionQuestion{Type: "boolean", Instructions: "Refund?", Criteria: tc.criteria}
		if _, err := decide(t, p, opts); err != nil {
			t.Fatal(err)
		}
		got := rec.body["questions"].([]interface{})[1].(map[string]interface{})["instructions"]
		if got != tc.expected {
			t.Fatalf("criteria %v: instructions = %q, want %q", tc.criteria, got, tc.expected)
		}
	}
}

func TestDecisionModel_WarnsAboutUnsupportedOptionsWithoutSendingThem(t *testing.T) {
	p, rec := newDecisionProvider(t, decisionFixture, 200)
	opts := decisionTestOptions()
	opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"reasoningEffort": "high", "safetyIdentifier": "user-123"}}
	result, err := decide(t, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Type != "unsupported" || result.Warnings[0].Feature != "providerOptions.openai.reasoningEffort" {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	if rec.body["safety_identifier"] != "user-123" {
		t.Fatalf("safety_identifier = %v", rec.body["safety_identifier"])
	}
	if _, ok := rec.body["reasoning"]; ok {
		t.Fatal("reasoning must not be sent")
	}
	if _, ok := rec.body["reasoningEffort"]; ok {
		t.Fatal("reasoningEffort must not be sent")
	}
}

func TestDecisionModel_SafetyIdentifierBoundaries(t *testing.T) {
	for _, id := range []string{"", "user-123", strings.Repeat("x", 128)} {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": id}}
		if _, err := decide(t, p, opts); err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		if rec.body["safety_identifier"] != id {
			t.Fatalf("%q: sent %v", id, rec.body["safety_identifier"])
		}
	}
	for _, bad := range []interface{}{123.0, nil, strings.Repeat("x", 129)} {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": bad}}
		_, err := decide(t, p, opts)
		var target *providererrors.InvalidArgumentError
		if !errors.As(err, &target) {
			t.Fatalf("%v: err = %v", bad, err)
		}
		if rec.calls != 0 {
			t.Fatalf("%v: HTTP request was sent", bad)
		}
	}
}

func TestDecisionModel_RejectsMissingDuplicateAndUnexpectedAnswerNames(t *testing.T) {
	a := `{"name": "department", "type": "choice", "choice": "billing", "probabilities": [{"value": "billing", "probability": 1}]}`
	c := `{"name": "refund", "type": "predicate", "probability": 0.5}`
	d := `{"name": "severity", "type": "score", "score": 1, "probabilities": [{"value": 1, "probability": 1}]}`
	unknown := `{"name": "unknown", "type": "predicate", "probability": 0.5}`
	for name, answers := range map[string]string{
		"empty":      ``,
		"extra":      a + "," + c + "," + d + "," + a,
		"duplicate":  a + "," + a + "," + d,
		"unexpected": a + "," + c + "," + unknown,
	} {
		p, _ := newDecisionProvider(t, `{"answers": [`+answers+`]}`, 200)
		_, err := decide(t, p, decisionTestOptions())
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Fatalf("%s: err = %v, want InvalidResponseDataError", name, err)
		}
	}
}

func TestDecisionModel_RejectsDuplicateDistributionValues(t *testing.T) {
	body := `{"answers": [{"name": "department", "type": "choice", "choice": "billing", "probabilities": [{"value": "billing", "probability": 0.5}, {"value": "billing", "probability": 0.5}]},
	  {"name": "refund", "type": "predicate", "probability": 0.5},
	  {"name": "severity", "type": "score", "score": 1, "probabilities": [{"value": 1, "probability": 1}]}]}`
	p, _ := newDecisionProvider(t, body, 200)
	_, err := decide(t, p, decisionTestOptions())
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecisionModel_RejectsMalformedSuccessfulResponses(t *testing.T) {
	for _, body := range []string{
		`{"answers": [{"type": "predicate", "name": "refund", "probability": 1.1}]}`,
		`{"answers": [{"type": "choice", "name": "department", "choice": "billing"}]}`,
		`{"answers": [{"type": "score", "name": "severity", "score": 1, "probabilities": [{"value": 0.5, "probability": 1}]}]}`,
		`{"answers": null}`,
	} {
		p, _ := newDecisionProvider(t, body, 200)
		if _, err := decide(t, p, decisionTestOptions()); err == nil {
			t.Fatalf("expected an error for %s", body)
		}
	}
}

func TestDecisionModel_UsesStandardOpenAIAPIErrorHandler(t *testing.T) {
	p, _ := newDecisionProvider(t, `{"error": {"message": "Decisions access not enabled", "type": "invalid_request_error"}}`, 403)
	_, err := decide(t, p, decisionTestOptions())
	if err == nil || !strings.Contains(err.Error(), "Decisions access not enabled") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecisionModel_MapsUsageAndNativeCounts(t *testing.T) {
	usage := `{"input_tokens": 387, "output_tokens": 3, "total_tokens": 390,
	  "input_tokens_details": {"cached_tokens": 50, "cache_write_tokens": 20}, "output_tokens_details": {"reasoning_tokens": 2}}`
	body := strings.Replace(decisionFixture, `"answers"`, `"model": "gpt-6-luna-resolved", "usage": `+usage+`, "answers"`, 1)
	p, _ := newDecisionProvider(t, body, 200)
	result, err := decide(t, p, decisionTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || *result.Usage.InputTokens != 387 || *result.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	var wantUsage interface{}
	_ = json.Unmarshal([]byte(usage), &wantUsage)
	got := result.ProviderMetadata["openai"].(map[string]interface{})["usage"]
	if !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("metadata usage = %v", got)
	}
	if result.Response.ModelID != "gpt-6-luna-resolved" {
		t.Fatalf("model = %q", result.Response.ModelID)
	}
}

func TestDecisionModel_AcceptsAbsentOrNullUsageAndModel(t *testing.T) {
	for _, extra := range []string{``, `"usage": null, "model": null,`} {
		p, _ := newDecisionProvider(t, strings.Replace(decisionFixture, `"answers"`, extra+`"answers"`, 1), 200)
		result, err := decide(t, p, decisionTestOptions())
		if err != nil {
			t.Fatal(err)
		}
		if result.Usage != nil {
			t.Fatalf("usage = %+v", result.Usage)
		}
		if _, ok := result.ProviderMetadata["openai"].(map[string]interface{})["usage"]; ok {
			t.Fatal("usage must be absent from metadata")
		}
		if result.Response.ModelID != "gpt-6-luna" {
			t.Fatalf("model = %q", result.Response.ModelID)
		}
	}
}

func TestDecisionModel_AcceptsPartialUsageAndPreservesZero(t *testing.T) {
	usage := `{"input_tokens": 0, "output_tokens": null, "input_tokens_details": null, "output_tokens_details": null, "total_tokens": null}`
	p, _ := newDecisionProvider(t, strings.Replace(decisionFixture, `"answers"`, `"usage": `+usage+`, "answers"`, 1), 200)
	result, err := decide(t, p, decisionTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 0 || result.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestDecisionModel_ReturnsRefusalAnswersAndKeepsTheOthers(t *testing.T) {
	for _, refused := range []string{"department", "severity", "refund"} {
		var fixture map[string]interface{}
		_ = json.Unmarshal([]byte(decisionFixture), &fixture)
		answers := fixture["answers"].([]interface{})
		for i, a := range answers {
			if a.(map[string]interface{})["name"] == refused {
				answers[i] = map[string]interface{}{"type": "refusal", "name": refused}
			}
		}
		body, _ := json.Marshal(fixture)
		p, _ := newDecisionProvider(t, string(body), 200)
		result, err := decide(t, p, decisionTestOptions())
		if err != nil {
			t.Fatal(err)
		}
		if result.Answers[refused].Type != "refusal" || len(result.Answers) != 3 {
			t.Fatalf("%s: answers = %+v", refused, result.Answers)
		}
		confidence := result.ProviderMetadata["openai"].(map[string]interface{})["confidence"].(map[string]interface{})
		if _, ok := confidence[refused]; ok {
			t.Fatalf("%s: refused question must not have confidence", refused)
		}
	}
}

func TestDecisionModel_FailsWhenUnnamedQuestionIsRefused(t *testing.T) {
	body := strings.Replace(decisionFixture, `{"name": "refund", "type": "predicate", "probability": 0.96}`, `{"type": "refusal", "name": null}`, 1)
	p, _ := newDecisionProvider(t, body, 200)
	_, err := decide(t, p, decisionTestOptions())
	var target *providererrors.InvalidResponseDataError
	if !errors.As(err, &target) || !strings.Contains(err.Error(), "OpenAI Decisions refused an unnamed question.") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecisionModel_MapsOrderedStatePartsToTextAndInlineImages(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	for _, data := range []types.FileData{
		{Type: types.FileDataTypeData, Data: png},
		{Type: types.FileDataTypeData, DataString: "iVBORw=="},
	} {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		opts.State = []provider.DecisionStatePart{
			provider.DecisionTextPart("Inspect this package."),
			provider.DecisionJSONPart([]interface{}{"glass vase", nil}),
			provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: data}),
			provider.DecisionTextPart("Look for cracks."),
		}
		if _, err := decide(t, p, opts); err != nil {
			t.Fatal(err)
		}
		content := rec.body["input"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
		wantURL := "data:image/png;base64,iVBORw=="
		if len(data.Data) > 0 {
			wantURL = "data:image/png;base64,iVBORw=="
		}
		want := []interface{}{
			map[string]interface{}{"type": "input_text", "text": "Inspect this package."},
			map[string]interface{}{"type": "input_text", "text": `["glass vase",null]`},
			map[string]interface{}{"type": "input_image", "image_url": wantURL},
			map[string]interface{}{"type": "input_text", "text": "Look for cracks."},
		}
		if !reflect.DeepEqual(content, want) {
			t.Fatalf("content = %v", content)
		}
	}
}

func TestDecisionModel_SendsImageDetailFromFilePartProviderOptions(t *testing.T) {
	for _, detail := range []string{"low", "high", "auto", "original"} {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		file := types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw=="}}
		withDetail := file
		withDetail.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"imageDetail": detail}}
		opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(withDetail), provider.DecisionFilePart(file)}
		if _, err := decide(t, p, opts); err != nil {
			t.Fatal(err)
		}
		content := rec.body["input"].([]interface{})[0].(map[string]interface{})["content"]
		want := []interface{}{
			map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64,iVBORw==", "detail": detail},
			map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64,iVBORw=="},
		}
		if !reflect.DeepEqual(content, want) {
			t.Fatalf("%s: content = %v", detail, content)
		}
	}
}

func TestDecisionModel_DetectsImageMediaTypeForShorthand(t *testing.T) {
	p, rec := newDecisionProvider(t, decisionFixture, 200)
	opts := decisionTestOptions()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
		MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, Data: png},
	})}
	if _, err := decide(t, p, opts); err != nil {
		t.Fatal(err)
	}
	content := rec.body["input"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	if url := content[0].(map[string]interface{})["image_url"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("image_url = %q", url)
	}
}

func TestDecisionModel_RejectsUnsupportedFilesBeforeTheAPICall(t *testing.T) {
	for name, file := range map[string]types.FileContent{
		"audio":     {MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AAAA"}},
		"svg":       {MediaType: "image/svg+xml", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AAAA"}},
		"reference": {MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"openai": "file-123"}}},
		"url":       {MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}},
	} {
		p, rec := newDecisionProvider(t, decisionFixture, 200)
		opts := decisionTestOptions()
		opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(file)}
		_, err := decide(t, p, opts)
		if !providererrors.IsUnsupportedFunctionalityError(err) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if rec.calls != 0 {
			t.Fatalf("%s: request was sent", name)
		}
	}
}

func TestDecisionModel_DeprecatedEvaluationFactoryIsBackedByDecisions(t *testing.T) {
	p, rec := newDecisionProvider(t, decisionFixture, 200)
	m, err := p.EvaluationModel("gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.DoEvaluate(context.Background(), decisionTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Answers["refund"].Probability == nil || *result.Answers["refund"].Probability != 0.96 || m.Provider() != "openai.decision" {
		t.Fatalf("result = %+v provider = %s", result.Answers, m.Provider())
	}
	if rec.path != "/v1/decisions" {
		t.Fatalf("path = %q", rec.path)
	}
}
