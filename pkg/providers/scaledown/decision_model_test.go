package scaledown

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Ports TS packages/scaledown/src/scaledown-decision-model.test.ts
// (c6db718176, final state at a457990a33).

func response() map[string]interface{} {
	return map[string]interface{}{
		"model": "classify-1",
		"answers": map[string]interface{}{
			"category": map[string]interface{}{
				"type": "choice", "choice": "bug", "probabilities": map[string]interface{}{"bug": 0.93, "feedback": 0.07}, "confidence": 0.93,
			},
			"workaround": map[string]interface{}{"type": "noul", "noul": 0.79},
			"severity": map[string]interface{}{
				"type": "score", "score": 1.43, "probabilities": map[string]interface{}{"0": 0.0, "1": 0.57, "2": 0.43}, "confidence": 0.57,
				"legend": map[string]interface{}{"0": "Cosmetic", "1": "Degraded", "2": "Blocking"},
			},
		},
		"usage": map[string]interface{}{"input_tokens": 103.0, "output_tokens": 3.0, "cost": 0.0000044},
	}
}

type recorded struct {
	path    string
	headers http.Header
	body    map[string]interface{}
}

type fake struct {
	mu    sync.Mutex
	calls []recorded
	body  interface{}
	raw   string
	code  int
}

func newFake(t *testing.T, body interface{}, status int) (*fake, string) {
	t.Helper()
	f := &fake{body: body, code: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		f.mu.Lock()
		f.calls = append(f.calls, recorded{path: r.URL.Path, headers: r.Header.Clone(), body: b})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-trace", "trace-1")
		w.WriteHeader(f.code)
		if f.raw != "" {
			_, _ = io.WriteString(w, f.raw)
			return
		}
		_ = json.NewEncoder(w).Encode(f.body)
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

func newModel(t *testing.T) (*DecisionModel, *fake) {
	f, url := newFake(t, response(), 200)
	m, err := New(Config{APIKey: "test-key", BaseURL: url}).DecisionModel("classify-1")
	if err != nil {
		t.Fatal(err)
	}
	return m.(*DecisionModel), f
}

func options() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart("The export crashes in Safari.")},
		Questions: map[string]provider.DecisionQuestion{
			"category":   {Type: "choice", Criteria: map[string]interface{}{"bug": "Broken functionality", "feedback": nil}},
			"workaround": {Type: "boolean", Instructions: "Is there a workaround?"},
			"severity":   {Type: "score", Criteria: []interface{}{"Cosmetic", "Degraded", "Blocking"}},
		},
		QuestionOrder: []string{"category", "workaround", "severity"},
	}
}

func TestMapsAllThreeQuestionTypesNativeResultsUsageAndMetadata(t *testing.T) {
	m, f := newModel(t)
	res, err := m.DoDecide(context.Background(), options())
	if err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.path != "/v1/scaledown" || c.headers.Get("x-api-key") != "test-key" {
		t.Fatalf("path=%s key=%s", c.path, c.headers.Get("x-api-key"))
	}
	want := map[string]interface{}{
		"model": "classify-1",
		"state": map[string]interface{}{"text": "The export crashes in Safari."},
		"questions": map[string]interface{}{
			"category":   map[string]interface{}{"type": "choice", "criteria": map[string]interface{}{"bug": "Broken functionality", "feedback": "feedback"}},
			"workaround": map[string]interface{}{"type": "noul", "instructions": "Is there a workaround?"},
			"severity":   map[string]interface{}{"type": "score", "criteria": []interface{}{"Cosmetic", "Degraded", "Blocking"}},
		},
	}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v\nwant %v", c.body, want)
	}
	workaround, severity := 0.79, 1.43
	wantAnswers := map[string]provider.DecisionAnswer{
		"category":   {Type: "choice", Choice: "bug", Probabilities: map[string]float64{"bug": 0.93, "feedback": 0.07}},
		"workaround": {Type: "boolean", Probability: &workaround},
		"severity":   {Type: "score", Score: &severity, Probabilities: map[string]float64{"0": 0, "1": 0.57, "2": 0.43}},
	}
	if !reflect.DeepEqual(res.Answers, wantAnswers) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	if *res.Usage.InputTokens != 103 || *res.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	wantMeta := map[string]interface{}{"scaledown": map[string]interface{}{
		"confidence": map[string]interface{}{"category": 0.93, "severity": 0.57},
		"legend":     map[string]interface{}{"severity": map[string]string{"0": "Cosmetic", "1": "Degraded", "2": "Blocking"}},
		"cost":       0.0000044,
	}}
	if !reflect.DeepEqual(res.ProviderMetadata, wantMeta) {
		t.Fatalf("metadata = %#v", res.ProviderMetadata)
	}
	if res.Response.ModelID != "classify-1" || !reflect.DeepEqual(res.Response.Body, response()) || res.Response.Headers["x-trace"] != "trace-1" {
		t.Fatalf("response = %+v", res.Response)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestPreservesJSONStateStructuredRubricsAndBooleanCriteriaAsText(t *testing.T) {
	m, f := newModel(t)
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"ticket": "Export is broken"})},
		Questions: map[string]provider.DecisionQuestion{
			"category": {Type: "choice", Instructions: map[string]interface{}{"task": "Route"}, Criteria: map[string]interface{}{"bug": []interface{}{"Broken"}}},
			"workaround": {Type: "boolean", Criteria: map[string]interface{}{
				"true": "Another browser works", "false": "No alternative works",
			}},
		},
		QuestionOrder: []string{"category", "workaround"},
		CriteriaOrder: map[string][]string{"workaround": {"true", "false"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := f.calls[0].body
	if !reflect.DeepEqual(body["state"], map[string]interface{}{"text": `{"ticket":"Export is broken"}`}) {
		t.Fatalf("state = %v", body["state"])
	}
	q := body["questions"].(map[string]interface{})
	cat := q["category"].(map[string]interface{})
	if cat["instructions"] != `{"task":"Route"}` || !reflect.DeepEqual(cat["criteria"], map[string]interface{}{"bug": `["Broken"]`}) {
		t.Fatalf("category = %v", cat)
	}
	wk := q["workaround"].(map[string]interface{})
	if wk["type"] != "noul" || wk["instructions"] != "Answer yes when: Another browser works\nAnswer no when: No alternative works" {
		t.Fatalf("workaround = %v", wk)
	}
}

func TestEncodesDocumentInputAlongsideText(t *testing.T) {
	m, f := newModel(t)
	opts := options()
	opts.State = []provider.DecisionStatePart{
		provider.DecisionTextPart("Classify this invoice."),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}}),
	}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"text": "Classify this invoice.", "document": "AQID", "document_mime_type": "application/pdf"}
	if got := f.calls[0].body["state"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("state = %v", got)
	}
}

func TestPassesCustomSettingsCallHeadersAndModelID(t *testing.T) {
	f, url := newFake(t, response(), 200)
	m, _ := New(Config{APIKey: "custom-key", BaseURL: url + "/", Headers: map[string]string{"shared": "provider"}}).DecisionModel("classify")
	opts := options()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.path != "/v1/scaledown" || c.headers.Get("x-api-key") != "custom-key" || c.headers.Get("shared") != "call" ||
		!strings.Contains(c.headers.Get("user-agent"), "ai-sdk-scaledown/"+version.Version) || c.body["model"] != "classify" {
		t.Fatalf("call = %+v", c)
	}
}

func TestRestoresWorkflowConfigurationAndFallsBackToEnvironmentAuthentication(t *testing.T) {
	t.Setenv("SCALEDOWN_API_KEY", "env-key")
	f, url := newFake(t, response(), 200)
	m, _ := New(Config{APIKey: "test-key", BaseURL: url}).DecisionModel("classify-1")
	serialized := m.(*DecisionModel).Serialize()
	if serialized.Provider != "scaledown.decision" || serialized.ModelID != "classify-1" {
		t.Fatalf("serialized = %+v", serialized)
	}
	for k := range serialized.Config {
		if strings.EqualFold(k, "apikey") {
			t.Fatalf("API key serialized: %v", serialized.Config)
		}
	}
	restored, err := deserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.DoDecide(context.Background(), options()); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].headers.Get("x-api-key") != "env-key" || f.calls[0].path != "/v1/scaledown" {
		t.Fatalf("restored call = %+v", f.calls[0])
	}
	if !reflect.DeepEqual(restored.SupportedQuestionTypes(), []string{"choice", "boolean", "score"}) {
		t.Fatalf("types = %v", restored.SupportedQuestionTypes())
	}
}

func TestWarnsAboutUnsupportedGenerationSettingsAndProviderOptions(t *testing.T) {
	m, _ := newModel(t)
	opts := options()
	ten := 10
	high := types.ReasoningHigh
	opts.MaxOutputTokens = &ten
	opts.Reasoning = &high
	opts.ProviderOptions = map[string]interface{}{"scaledown": map[string]interface{}{"reasoning": true}}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{
		{Type: "unsupported", Feature: "maxOutputTokens"},
		{Type: "unsupported", Feature: "reasoning"},
		{Type: "unsupported", Feature: "providerOptions.scaledown.reasoning"},
	}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

func TestRejectsUnsupportedScoreCriteriaBeforeSendingARequest(t *testing.T) {
	many := make([]interface{}, 11)
	for i := range many {
		many[i] = "Level"
	}
	for name, criteria := range map[string][]interface{}{"one level": {"Only level"}, "eleven levels": many, "a null level": {"Low", nil}} {
		t.Run(name, func(t *testing.T) {
			m, f := newModel(t)
			_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
				State:     options().State,
				Questions: map[string]provider.DecisionQuestion{"severity": {Type: "score", Criteria: criteria}},
			})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) || len(f.calls) != 0 {
				t.Fatalf("err = %v calls = %d", err, len(f.calls))
			}
		})
	}
}

func TestRejectsUnsupportedQuestionsAndFilesBeforeSendingARequest(t *testing.T) {
	m, f := newModel(t)
	opts := options()
	opts.Questions = map[string]provider.DecisionQuestion{"location": {Type: "box"}}
	_, err := m.DoDecide(context.Background(), opts)
	var unsupportedQuestion *providererrors.DecisionUnsupportedQuestionTypeError
	if !errors.As(err, &unsupportedQuestion) {
		t.Fatalf("err = %v", err)
	}
	opts = options()
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})}
	_, err = m.DoDecide(context.Background(), opts)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || !strings.Contains(unsupported.Error(), "ScaleDown supports one JPEG, PNG, TIFF, or PDF document.") {
		t.Fatalf("err = %v", err)
	}
	// A second document, and URL file data, are rejected too.
	pdf := provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})
	opts.State = []provider.DecisionStatePart{pdf, pdf}
	if _, err = m.DoDecide(context.Background(), opts); !errors.As(err, &unsupported) {
		t.Fatalf("two documents: err = %v", err)
	}
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.png"}})}
	if _, err = m.DoDecide(context.Background(), opts); !errors.As(err, &unsupported) {
		t.Fatalf("url data: err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("HTTP request sent")
	}
	p := New(Config{})
	for name, check := range map[string]func() error{
		"languageModel":  func() error { _, err := p.LanguageModel("unknown"); return err },
		"embeddingModel": func() error { _, err := p.EmbeddingModel("unknown"); return err },
		"imageModel":     func() error { _, err := p.ImageModel("unknown"); return err },
	} {
		var noSuch *providererrors.NoSuchModelError
		if err := check(); !errors.As(err, &noSuch) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSurfacesAPIErrors(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"string detail": {`{"detail":"Insufficient credits"}`, "Insufficient credits"},
		"array detail":  {`{"detail":[{"msg":"Invalid model"},{"msg":"Bad state"}]}`, "Invalid model, Bad state"},
		"other body":    {`{"error":"x"}`, "Unprocessable Entity"},
	} {
		t.Run(name, func(t *testing.T) {
			f, url := newFake(t, nil, 422)
			f.raw = tc.body
			m, _ := New(Config{APIKey: "k", BaseURL: url}).DecisionModel("classify-1")
			_, err := m.DoDecide(context.Background(), options())
			var apiErr *providererrors.ProviderError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != 422 || apiErr.Message != tc.want {
				t.Fatalf("err = %#v", err)
			}
		})
	}
}

func TestRejectsMalformedSuccessfulResponse(t *testing.T) {
	f, url := newFake(t, map[string]interface{}{"answers": map[string]interface{}{"category": map[string]interface{}{"type": "choice"}}}, 200)
	_ = f
	m, _ := New(Config{APIKey: "k", BaseURL: url}).DecisionModel("classify-1")
	_, err := m.DoDecide(context.Background(), options())
	var apiErr *providererrors.ProviderError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "Invalid JSON response") {
		t.Fatalf("err = %#v", err)
	}
}

func TestMissingAPIKeyIsReportedWhenADecisionIsMade(t *testing.T) {
	t.Setenv("SCALEDOWN_API_KEY", "")
	m, _ := New(Config{}).DecisionModel("classify-1")
	_, err := m.DoDecide(context.Background(), options())
	if err == nil || !strings.Contains(err.Error(), "SCALEDOWN_API_KEY") || !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBooleanWithoutInstructionsOrCriteriaSendsEmptyInstructions(t *testing.T) {
	m, f := newModel(t)
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     options().State,
		Questions: map[string]provider.DecisionQuestion{"workaround": {Type: "boolean"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := f.calls[0].body["questions"].(map[string]interface{})["workaround"]
	if !reflect.DeepEqual(q, map[string]interface{}{"type": "noul", "instructions": ""}) {
		t.Fatalf("question = %v", q)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDefaultEndpointAndEnvironmentKey(t *testing.T) {
	t.Setenv("SCALEDOWN_API_KEY", "env-key")
	var gotURL, gotKey string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL, gotKey = r.URL.String(), r.Header.Get("x-api-key")
		raw, _ := json.Marshal(response())
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	m, _ := New(Config{HTTPClient: client}).DecisionModel("classify-1")
	if _, err := m.DoDecide(context.Background(), options()); err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://api.scaledown.xyz/v1/scaledown" || gotKey != "env-key" {
		t.Fatalf("url=%s key=%s", gotURL, gotKey)
	}
}
