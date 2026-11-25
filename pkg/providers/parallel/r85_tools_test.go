package parallel

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
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS packages/parallel/src/parallel-decision-model.test.ts
// (6a648d45ff) with its fixture testdata/decisions.json.

func r85toolsFixture(t *testing.T) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile("testdata/decisions.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type r85toolsCall struct {
	path    string
	headers http.Header
	body    map[string]interface{}
}

type r85toolsServer struct {
	mu    sync.Mutex
	calls []r85toolsCall
	srv   *httptest.Server
}

func r85toolsSetup(t *testing.T, body interface{}, status int) (*r85toolsServer, *DecisionModel, *Provider) {
	t.Helper()
	s := &r85toolsServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		s.mu.Lock()
		s.calls = append(s.calls, r85toolsCall{path: r.URL.Path, headers: r.Header.Clone(), body: b})
		s.mu.Unlock()
		w.Header().Set("retry-after", "2")
		w.Header().Set("x-request-id", "request-123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.srv.Close)
	p := New(Config{APIKey: "test-key", BaseURL: s.srv.URL + "/v1/", Headers: map[string]string{"x-provider": "custom"}})
	m, err := p.DecisionModel("fast")
	if err != nil {
		t.Fatal(err)
	}
	return s, m.(*DecisionModel), p
}

func r85toolsOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"company": "Parallel", "website": "https://parallel.ai"})},
		Questions: map[string]provider.DecisionQuestion{
			"hasSearch": {Type: "boolean", Instructions: "Does it offer web search?"},
			"availability": {Type: "choice", Instructions: "Is the API available?", Criteria: map[string]interface{}{
				"available": "Publicly available", "unknown": "Insufficient evidence",
			}},
			"fit": {Type: "score", Instructions: "How well does it fit?", Criteria: []interface{}{"No fit", "Partial fit", "Full fit"}},
		},
		QuestionOrder: []string{"hasSearch", "availability", "fit"},
	}
}

func TestR85toolsParallelSendsNativeContract(t *testing.T) {
	s, m, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
	opts := r85toolsOptions()
	opts.Headers = map[string]string{"x-call": "custom"}
	opts.ProviderOptions = map[string]interface{}{"parallel": map[string]interface{}{"includeExcerpts": false}}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 1 {
		t.Fatalf("calls = %d", len(s.calls))
	}
	c := s.calls[0]
	if c.path != "/v1/decisions" {
		t.Fatalf("path = %s", c.path)
	}
	for k, v := range map[string]string{"x-api-key": "test-key", "x-provider": "custom", "x-call": "custom", "content-type": "application/json"} {
		if got := c.headers.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if c.headers.Get("authorization") != "" || !strings.Contains(c.headers.Get("user-agent"), "ai-sdk-parallel/") {
		t.Fatalf("auth/user-agent headers = %v", c.headers)
	}
	want := map[string]interface{}{
		"mode":             "fast",
		"include_excerpts": false,
		"state":            map[string]interface{}{"company": "Parallel", "website": "https://parallel.ai"},
		"questions": map[string]interface{}{
			"hasSearch":    map[string]interface{}{"type": "noul", "instructions": "Does it offer web search?"},
			"availability": map[string]interface{}{"type": "choice", "instructions": "Is the API available?", "criteria": map[string]interface{}{"available": "Publicly available", "unknown": "Insufficient evidence"}},
			"fit":          map[string]interface{}{"type": "score", "instructions": "How well does it fit?", "criteria": []interface{}{"No fit", "Partial fit", "Full fit"}},
		},
	}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v\nwant %v", c.body, want)
	}
}

func TestR85toolsParallelResult(t *testing.T) {
	fixture := r85toolsFixture(t)
	_, m, _ := r85toolsSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), r85toolsOptions())
	if err != nil {
		t.Fatal(err)
	}
	p, s := 0.9992643798509062, 1.7123456789
	answers := fixture["answers"].(map[string]interface{})
	probs := func(id string) map[string]float64 {
		out := map[string]float64{}
		for k, v := range answers[id].(map[string]interface{})["probabilities"].(map[string]interface{}) {
			out[k] = v.(float64)
		}
		return out
	}
	wantAnswers := map[string]provider.DecisionAnswer{
		"hasSearch":    {Type: "boolean", Probability: &p},
		"availability": {Type: "choice", Choice: "available", Probabilities: probs("availability")},
		"fit":          {Type: "score", Score: &s, Probabilities: probs("fit")},
	}
	if !reflect.DeepEqual(res.Answers, wantAnswers) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	gotMeta, _ := json.Marshal(res.ProviderMetadata)
	wantMeta, _ := json.Marshal(map[string]interface{}{"parallel": map[string]interface{}{
		"citations":  fixture["citations"],
		"confidence": map[string]interface{}{"availability": 0.87654321, "fit": 0.7654321},
		"legend":     map[string]interface{}{"fit": answers["fit"].(map[string]interface{})["legend"]},
		"decisionId": fixture["decision_id"],
		"mode":       "fast",
	}})
	if string(gotMeta) != string(wantMeta) {
		t.Fatalf("metadata = %s\nwant %s", gotMeta, wantMeta)
	}
	if res.Rounding != nil || *res.Usage.InputTokens != 4212 || *res.Usage.OutputTokens != 0 {
		t.Fatalf("rounding/usage = %v %+v", res.Rounding, res.Usage)
	}
	if res.Response.ID != fixture["decision_id"] || res.Response.ModelID != "fast" || !reflect.DeepEqual(res.Response.Body, fixture) || res.Response.Headers["x-request-id"] != "request-123" {
		t.Fatalf("response = %+v", res.Response)
	}
}

func TestR85toolsParallelRefusalAndEmptyEvidence(t *testing.T) {
	fixture := r85toolsFixture(t)
	answers := fixture["answers"].(map[string]interface{})
	answers["hasSearch"] = map[string]interface{}{"type": "refusal"}
	_, m, _ := r85toolsSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), r85toolsOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.Answers["hasSearch"].Type != "refusal" || res.Answers["fit"].Type != "score" {
		t.Fatalf("answers = %+v", res.Answers)
	}
	for _, citations := range []string{`[]`, `[{"url":"https://example.com","excerpts":[]}]`} {
		f := r85toolsFixture(t)
		var c interface{}
		_ = json.Unmarshal([]byte(citations), &c)
		f["citations"] = c
		_, m, _ := r85toolsSetup(t, f, 200)
		res, err := m.DoDecide(context.Background(), r85toolsOptions())
		if err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(res.ProviderMetadata["parallel"].(map[string]interface{})["citations"])
		var gotV interface{}
		_ = json.Unmarshal(got, &gotV)
		if !reflect.DeepEqual(gotV, c) {
			t.Fatalf("citations = %s, want %s", got, citations)
		}
	}
}

func TestR85toolsParallelState(t *testing.T) {
	for _, tc := range []struct {
		state []provider.DecisionStatePart
		want  string
	}{
		{[]provider.DecisionStatePart{}, `[]`},
		{[]provider.DecisionStatePart{provider.DecisionTextPart("A company")}, `"A company"`},
		{[]provider.DecisionStatePart{provider.DecisionJSONPart(nil)}, `null`},
		{[]provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{1, "two"})}, `[1,"two"]`},
		{[]provider.DecisionStatePart{provider.DecisionTextPart("A company"), provider.DecisionJSONPart(map[string]interface{}{"website": "https://example.com"})}, `["A company",{"website":"https://example.com"}]`},
	} {
		s, m, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
		opts := r85toolsOptions()
		opts.State = tc.state
		if _, err := m.DoDecide(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(s.calls[0].body["state"])
		if string(got) != tc.want {
			t.Errorf("state = %s, want %s", got, tc.want)
		}
	}
}

func TestR85toolsParallelRejectsBeforeIO(t *testing.T) {
	s, m, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
	opts := r85toolsOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"box": {Type: "box", Instructions: "Find a box"}}
	opts.QuestionOrder = nil
	var unsupported *providererrors.DecisionUnsupportedQuestionTypeError
	if _, err := m.DoDecide(context.Background(), opts); !errors.As(err, &unsupported) {
		t.Fatalf("box err = %v", err)
	}
	opts = r85toolsOptions()
	opts.State = []provider.DecisionStatePart{{Type: "file"}}
	var unsupportedFn *providererrors.UnsupportedFunctionalityError
	if _, err := m.DoDecide(context.Background(), opts); !errors.As(err, &unsupportedFn) || err.Error() != "'Parallel Decisions file input' functionality not supported." {
		t.Fatalf("file err = %v", err)
	}
	opts = r85toolsOptions()
	opts.ProviderOptions = map[string]interface{}{"parallel": map[string]interface{}{"includeExcerpts": "false"}}
	var invalid *providererrors.InvalidArgumentError
	if _, err := m.DoDecide(context.Background(), opts); !errors.As(err, &invalid) {
		t.Fatalf("options err = %v", err)
	}

	hasSearch := r85toolsOptions().Questions["hasSearch"]
	many := map[string]provider.DecisionQuestion{}
	for i := 0; i < 101; i++ {
		many[fmt.Sprintf("q%d", i)] = hasSearch
	}
	choices := map[string]interface{}{}
	for i := 0; i < 256; i++ {
		choices[fmt.Sprintf("c%d", i)] = "Option"
	}
	levels := make([]interface{}, 11)
	for i := range levels {
		levels[i] = "Level"
	}
	for i, questions := range []map[string]provider.DecisionQuestion{
		{},
		many,
		{"bad name": hasSearch},
		{"choice": {Type: "choice", Instructions: "x", Criteria: map[string]interface{}{"only": "One option"}}},
		{"choice": {Type: "choice", Instructions: "x", Criteria: choices}},
		{"score": {Type: "score", Instructions: "x", Criteria: []interface{}{"One level"}}},
		{"score": {Type: "score", Instructions: "x", Criteria: levels}},
	} {
		o := r85toolsOptions()
		o.Questions, o.QuestionOrder = questions, nil
		if _, err := m.DoDecide(context.Background(), o); !errors.As(err, &invalid) {
			t.Errorf("questions %d: err = %v, want InvalidArgumentError", i, err)
		}
	}
	if len(s.calls) != 0 {
		t.Fatalf("calls = %d, want none", len(s.calls))
	}
}

func TestR85toolsParallelRejectsMalformedResponses(t *testing.T) {
	mutate := []func(map[string]interface{}){
		func(f map[string]interface{}) {
			f["answers"].(map[string]interface{})["hasSearch"] = map[string]interface{}{"type": "noul", "noul": 2}
		},
		func(f map[string]interface{}) {
			f["answers"] = map[string]interface{}{"hasSearch": f["answers"].(map[string]interface{})["hasSearch"]}
		},
		func(f map[string]interface{}) {
			a := f["answers"].(map[string]interface{})
			a["unexpected"] = a["hasSearch"]
		},
		func(f map[string]interface{}) {
			f["citations"] = []interface{}{map[string]interface{}{"url": "https://example.com", "excerpts": "not-an-array"}}
		},
	}
	for i, mut := range mutate {
		f := r85toolsFixture(t)
		mut(f)
		_, m, _ := r85toolsSetup(t, f, 200)
		_, err := m.DoDecide(context.Background(), r85toolsOptions())
		var apiErr *providererrors.ProviderError
		var invalid *providererrors.InvalidResponseDataError
		if !errors.As(err, &apiErr) && !errors.As(err, &invalid) {
			t.Errorf("case %d: err = %#v", i, err)
		}
	}
}

func TestR85toolsParallelProviderErrors(t *testing.T) {
	for _, status := range []int{400, 401, 422, 429, 500} {
		_, m, _ := r85toolsSetup(t, map[string]interface{}{"type": "error", "error": map[string]interface{}{"message": "Request rejected", "ref_id": "error-123"}}, status)
		_, err := m.DoDecide(context.Background(), r85toolsOptions())
		var apiErr *providererrors.ProviderError
		if !errors.As(err, &apiErr) || apiErr.Message != "Request rejected" || apiErr.StatusCode != status || apiErr.ResponseHeaders["retry-after"] != "2" {
			t.Fatalf("%d: err = %#v", status, err)
		}
	}
}

func TestR85toolsParallelWarnings(t *testing.T) {
	_, m, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
	opts := r85toolsOptions()
	maxTokens := 10
	low := types.ReasoningLevel("low")
	opts.MaxOutputTokens, opts.Reasoning = &maxTokens, &low
	opts.ProviderOptions = map[string]interface{}{"parallel": map[string]interface{}{"unused": true}}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "maxOutputTokens"}, {Type: "unsupported", Feature: "reasoning"}, {Type: "unsupported", Feature: "providerOptions.parallel.unused"}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

func TestR85toolsParallelEnvAuthDefaultsAndFamilies(t *testing.T) {
	t.Setenv("PARALLEL_API_KEY", "environment-key")
	s, _, _ := r85toolsSetup(t, r85toolsFixture(t), 200)
	p := New(Config{BaseURL: s.srv.URL})
	m, _ := p.DecisionModel("future-mode")
	res, err := m.DoDecide(context.Background(), r85toolsOptions())
	if err != nil {
		t.Fatal(err)
	}
	if s.calls[0].headers.Get("x-api-key") != "environment-key" || s.calls[0].path != "/decisions" || s.calls[0].body["mode"] != "future-mode" || res.Response.ModelID != "future-mode" {
		t.Fatalf("call = %+v, response = %+v", s.calls[0], res.Response)
	}
	if New(Config{}).client == nil || DefaultBaseURL != "https://api.parallel.ai/v1" {
		t.Fatal("default base URL")
	}
	if _, err := p.LanguageModel("fast"); err == nil || !strings.Contains(err.Error(), "No such languageModel") {
		t.Fatalf("languageModel err = %v", err)
	}
	if _, err := p.EmbeddingModel("fast"); err == nil {
		t.Fatal("embeddingModel")
	}
	if _, err := p.ImageModel("fast"); err == nil {
		t.Fatal("imageModel")
	}
	if _, err := p.DecisionModel(""); err == nil || !strings.Contains(err.Error(), "No such decisionModel") {
		t.Fatalf("empty decision model err = %v", err)
	}
	t.Setenv("PARALLEL_API_KEY", "")
	if _, err := m.DoDecide(context.Background(), r85toolsOptions()); !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("missing key err = %v", err)
	}
}
