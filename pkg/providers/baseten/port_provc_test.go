package baseten

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS packages/baseten/src/baseten-decision-model.test.ts (3b7c1c3907)
// and the decisionModel case of baseten-provider.unit.test.ts, with the live
// fixtures testdata/decisions.json and testdata/decisions-image.json.

func portprovcFixture(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type portprovcCall struct {
	path    string
	headers http.Header
	raw     string
	body    map[string]interface{}
}

type portprovcServer struct {
	mu    sync.Mutex
	calls []portprovcCall
	srv   *httptest.Server
}

func portprovcSetup(t *testing.T, body interface{}, status int) (*portprovcServer, *DecisionModel) {
	t.Helper()
	s := &portprovcServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		s.mu.Lock()
		s.calls = append(s.calls, portprovcCall{path: r.URL.Path, headers: r.Header.Clone(), raw: string(raw), body: b})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-trace", "trace-1")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.srv.Close)
	p := New(Config{APIKey: "test-key", BaseURL: s.srv.URL + "/v1/", Headers: map[string]string{"x-provider": "custom", "shared": "provider"}})
	m, err := p.DecisionModel("decision-models/baseten-bison-latest")
	if err != nil {
		t.Fatal(err)
	}
	return s, m.(*DecisionModel)
}

const portprovcMessage = "I was charged twice for my subscription. Please refund the duplicate charge today."

func portprovcOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart(portprovcMessage)},
		Questions: map[string]provider.DecisionQuestion{
			"team": {Type: "choice", Instructions: "Which team should handle this?", Criteria: map[string]interface{}{
				"billing": "Charges, refunds, payment methods", "engineering": "Bugs and outages", "account": nil,
			}},
			"refund": {Type: "boolean", Instructions: "Does the customer ask for a refund?"},
			"refund_criteria": {Type: "boolean", Instructions: "Is this a refund request?", Criteria: map[string]interface{}{
				"true": "The customer wants money back", "false": "Anything else",
			}},
			"urgency": {Type: "score", Instructions: "How urgent is this?", Criteria: []interface{}{
				"Can wait", "Handle today", "Time-sensitive", "Blocking revenue",
			}},
		},
		QuestionOrder: []string{"team", "refund", "refund_criteria", "urgency"},
	}
}

var portprovcPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

func portprovcImageQuestions() map[string]provider.DecisionQuestion {
	return map[string]provider.DecisionQuestion{"red": {Type: "boolean", Instructions: "Is this image predominantly red?"}}
}

func portprovcFile(mediaType string, data types.FileData) provider.DecisionStatePart {
	data.MediaType = ""
	return provider.DecisionFilePart(types.FileContent{MediaType: mediaType, FileData: data})
}

func portprovcData(b []byte) types.FileData {
	return types.FileData{Type: types.FileDataTypeData, Data: b}
}

// 'sends one System One JSON request with configured headers'
func TestPortProvcSendsSystemOneRequest(t *testing.T) {
	s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
	opts := portprovcOptions()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if m.SpecificationVersion() != "v4" || m.Provider() != "baseten.decision" || m.ModelID() != "decision-models/baseten-bison-latest" ||
		!reflect.DeepEqual(m.SupportedQuestionTypes(), []string{"choice", "score", "boolean"}) {
		t.Fatalf("model identity wrong: %s %s %v", m.Provider(), m.ModelID(), m.SupportedQuestionTypes())
	}
	if len(s.calls) != 1 {
		t.Fatalf("calls = %d", len(s.calls))
	}
	c := s.calls[0]
	if c.path != "/v1/systemone" {
		t.Fatalf("path = %s", c.path)
	}
	for k, v := range map[string]string{"authorization": "Bearer test-key", "content-type": "application/json", "x-provider": "custom", "shared": "call"} {
		if got := c.headers.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(c.headers.Get("user-agent"), "ai-sdk-baseten/") {
		t.Errorf("user-agent = %q", c.headers.Get("user-agent"))
	}
	want := map[string]interface{}{
		"model": "decision-models/baseten-bison-latest",
		"state": portprovcMessage,
		"questions": map[string]interface{}{
			"team": map[string]interface{}{"type": "choice", "instructions": "Which team should handle this?", "criteria": map[string]interface{}{
				"billing": "Charges, refunds, payment methods", "engineering": "Bugs and outages", "account": nil,
			}},
			"refund": map[string]interface{}{"type": "noul", "instructions": "Does the customer ask for a refund?"},
			"refund_criteria": map[string]interface{}{"type": "noul", "instructions": "Is this a refund request?", "criteria": map[string]interface{}{
				"true": "The customer wants money back", "false": "Anything else",
			}},
			"urgency": map[string]interface{}{"type": "score", "instructions": "How urgent is this?", "criteria": []interface{}{
				"Can wait", "Handle today", "Time-sensitive", "Blocking revenue",
			}},
		},
	}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v\nwant %v", c.body, want)
	}
	// TS body key order: model, state, questions.
	if i, j, k := strings.Index(c.raw, `"model"`), strings.Index(c.raw, `"state"`), strings.Index(c.raw, `"questions"`); i >= j || j >= k {
		t.Fatalf("body key order: %s", c.raw)
	}
}

// 'maps answers, usage, confidence, legends, and response metadata'
func TestPortProvcMapsAnswersUsageAndMetadata(t *testing.T) {
	fixture := portprovcFixture(t, "decisions.json")
	_, m := portprovcSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), portprovcOptions())
	if err != nil {
		t.Fatal(err)
	}
	fa := fixture["answers"].(map[string]interface{})
	probs := func(id string) map[string]float64 {
		out := map[string]float64{}
		for k, v := range fa[id].(map[string]interface{})["probabilities"].(map[string]interface{}) {
			out[k] = v.(float64)
		}
		return out
	}
	num := func(id, key string) float64 { return fa[id].(map[string]interface{})[key].(float64) }
	score := num("urgency", "score")
	refund, refundCriteria := num("refund", "noul"), num("refund_criteria", "noul")
	want := map[string]provider.DecisionAnswer{
		"team":            {Type: "choice", Choice: "billing", Probabilities: probs("team")},
		"refund":          {Type: "boolean", Probability: &refund},
		"refund_criteria": {Type: "boolean", Probability: &refundCriteria},
		"urgency":         {Type: "score", Score: &score, Probabilities: probs("urgency")},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %+v\nwant %+v", res.Answers, want)
	}
	if res.Rounding != nil {
		t.Fatalf("rounding = %+v, want nil (full precision)", res.Rounding)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 295 || *res.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	meta := res.ProviderMetadata["baseten"].(map[string]interface{})
	wantMeta := map[string]interface{}{
		"confidence": map[string]interface{}{"team": num("team", "confidence"), "urgency": num("urgency", "confidence")},
		"legend":     map[string]interface{}{"urgency": fa["urgency"].(map[string]interface{})["legend"]},
	}
	if !reflect.DeepEqual(meta, wantMeta) {
		t.Fatalf("metadata = %v\nwant %v", meta, wantMeta)
	}
	if res.Response.ModelID != "decision-models/baseten-bison-31b-latest" || res.Response.Headers["x-trace"] != "trace-1" ||
		!reflect.DeepEqual(res.Response.Body, any(fixture)) {
		t.Fatalf("response = %+v", res.Response)
	}
}

// 'returns undefined usage when the response has none'
func TestPortProvcNoUsage(t *testing.T) {
	fixture := portprovcFixture(t, "decisions.json")
	delete(fixture, "usage")
	_, m := portprovcSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), portprovcOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage != nil {
		t.Fatalf("usage = %+v, want nil", res.Usage)
	}
}

// describe('state'): it.each of JSON / conversation / joined states.
func TestPortProvcState(t *testing.T) {
	cases := []struct {
		name  string
		state []provider.DecisionStatePart
		want  interface{}
	}{
		{"a single JSON object as structured state",
			[]provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"plan": "business", "message": "Refund me."})},
			map[string]interface{}{"plan": "business", "message": "Refund me."}},
		{"a conversation JSON object as is",
			[]provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"messages": []interface{}{map[string]interface{}{"role": "user", "content": "Refund me."}}})},
			map[string]interface{}{"messages": []interface{}{map[string]interface{}{"role": "user", "content": "Refund me."}}}},
		{"text and JSON parts joined as text",
			[]provider.DecisionStatePart{provider.DecisionTextPart("Ticket:"), provider.DecisionJSONPart(map[string]interface{}{"message": "The API is down."})},
			"Ticket:\n{\"message\":\"The API is down.\"}"},
		// Rules beyond the TS cases: a scalar single value is joined as text.
		{"a single number as text", []provider.DecisionStatePart{provider.DecisionJSONPart(5.0)}, "5"},
		{"a JSON string value as is", []provider.DecisionStatePart{provider.DecisionJSONPart("plain")}, "plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
			opts := portprovcOptions()
			opts.State = tc.state
			if _, err := m.DoDecide(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			if got := s.calls[0].body["state"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("state = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// 'sends images as JSON in one user message, in request order'
func TestPortProvcImagesAsOneUserMessage(t *testing.T) {
	fixture := portprovcFixture(t, "decisions-image.json")
	s, m := portprovcSetup(t, fixture, 200)
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("First screenshot:"),
			portprovcFile("image/png", portprovcData(portprovcPNG)),
			provider.DecisionJSONPart(map[string]interface{}{"page": "checkout"}),
			portprovcFile("image/*", types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw0KGgo="}),
			portprovcFile("image/jpeg", types.FileData{Type: types.FileDataTypeURL, URL: "https://storage.googleapis.com/bucket/photo.jpg"}),
		},
		Questions: portprovcImageQuestions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ct := s.calls[0].headers.Get("content-type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	img := func(url string) interface{} {
		return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}
	}
	want := map[string]interface{}{"messages": []interface{}{map[string]interface{}{"role": "user", "content": []interface{}{
		map[string]interface{}{"type": "text", "text": "First screenshot:"},
		img("data:image/png;base64,iVBORw0KGgo="),
		map[string]interface{}{"type": "text", "text": `{"page":"checkout"}`},
		img("data:image/png;base64,iVBORw0KGgo="),
		img("https://storage.googleapis.com/bucket/photo.jpg"),
	}}}}
	if got := s.calls[0].body["state"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("state = %v\nwant %v", got, want)
	}
}

// 'reports image requests usage, including image tokens'
func TestPortProvcImageUsage(t *testing.T) {
	fixture := portprovcFixture(t, "decisions-image.json")
	_, m := portprovcSetup(t, fixture, 200)
	res, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     []provider.DecisionStatePart{provider.DecisionTextPart("Look at the image."), portprovcFile("image/png", portprovcData(portprovcPNG))},
		Questions: portprovcImageQuestions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	red := fixture["answers"].(map[string]interface{})["red"].(map[string]interface{})["noul"].(float64)
	if !reflect.DeepEqual(res.Answers, map[string]provider.DecisionAnswer{"red": {Type: "boolean", Probability: &red}}) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	if *res.Usage.InputTokens != 729 || *res.Usage.OutputTokens != 1 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

// 'rejects %s before HTTP' (audio files, unsupported image types, file
// references), plus the bare detection failure.
func TestPortProvcRejectsUnsupportedFilesBeforeHTTP(t *testing.T) {
	cases := map[string]provider.DecisionStatePart{
		"audio files":              portprovcFile("audio/wav", portprovcData(portprovcPNG)),
		"unsupported image types":  portprovcFile("image/gif", portprovcData(portprovcPNG)),
		"file references":          portprovcFile("image/png", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"baseten": "file-1"}}),
		"undetectable bare images": portprovcFile("image/*", portprovcData([]byte("not an image"))),
	}
	for name, part := range cases {
		t.Run(name, func(t *testing.T) {
			s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
			_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: []provider.DecisionStatePart{part}, Questions: portprovcImageQuestions()})
			var unsupported *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error = %v (%T), want UnsupportedFunctionalityError", err, err)
			}
			if len(s.calls) != 0 {
				t.Fatalf("calls = %d, want none", len(s.calls))
			}
		})
	}
}

// 'fills a missing yes/no outcome and omits absent instructions'
func TestPortProvcFillsMissingYesNoOutcome(t *testing.T) {
	fixture := portprovcFixture(t, "decisions.json")
	fixture["answers"] = map[string]interface{}{"refund": fixture["answers"].(map[string]interface{})["refund"]}
	s, m := portprovcSetup(t, fixture, 200)
	opts := portprovcOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Criteria: map[string]interface{}{"true": "Wants money back"}}}
	opts.QuestionOrder = nil
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"refund": map[string]interface{}{"type": "noul", "criteria": map[string]interface{}{"true": "Wants money back", "false": nil}}}
	if got := s.calls[0].body["questions"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("questions = %v, want %v", got, want)
	}
	if !strings.Contains(s.calls[0].raw, `"criteria":{"true":"Wants money back","false":null}`) {
		t.Fatalf("criteria order: %s", s.calls[0].raw)
	}
}

// 'sends null Score levels'
func TestPortProvcSendsNullScoreLevels(t *testing.T) {
	fixture := portprovcFixture(t, "decisions.json")
	fixture["answers"] = map[string]interface{}{"urgency": fixture["answers"].(map[string]interface{})["urgency"]}
	s, m := portprovcSetup(t, fixture, 200)
	opts := portprovcOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"urgency": {Type: "score", Criteria: []interface{}{"Low", nil, "High"}}}
	opts.QuestionOrder = nil
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	got := s.calls[0].body["questions"].(map[string]interface{})["urgency"].(map[string]interface{})["criteria"]
	if !reflect.DeepEqual(got, []interface{}{"Low", nil, "High"}) {
		t.Fatalf("criteria = %v", got)
	}
}

// 'rejects Box questions before HTTP'
func TestPortProvcRejectsBoxBeforeHTTP(t *testing.T) {
	s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
	opts := portprovcOptions()
	opts.Questions = map[string]provider.DecisionQuestion{"signature": {Type: "box", Instructions: "Signature."}}
	opts.QuestionOrder = nil
	_, err := m.DoDecide(context.Background(), opts)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || len(s.calls) != 0 {
		t.Fatalf("error = %v, calls = %d", err, len(s.calls))
	}
}

// describe('request limits'): rejects %s before HTTP.
func TestPortProvcRequestLimits(t *testing.T) {
	choices := func(n int) map[string]interface{} {
		out := map[string]interface{}{}
		for i := 0; i < n; i++ {
			out[strconv.Itoa(i)] = nil
		}
		return out
	}
	levels := func(n int) []interface{} {
		out := make([]interface{}, n)
		for i := range out {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	manyBooleans := map[string]provider.DecisionQuestion{}
	for i := 0; i < 257; i++ {
		manyBooleans[strconv.Itoa(i)] = provider.DecisionQuestion{Type: "boolean", Instructions: "Does the customer ask for a refund?"}
	}
	manyImages := make([]provider.DecisionStatePart, 17)
	for i := range manyImages {
		manyImages[i] = portprovcFile("image/png", portprovcData(portprovcPNG))
	}
	cases := []struct {
		name      string
		questions map[string]provider.DecisionQuestion
		state     []provider.DecisionStatePart
	}{
		{"no questions", map[string]provider.DecisionQuestion{}, nil},
		{"no Choice options", map[string]provider.DecisionQuestion{"team": {Type: "choice", Criteria: map[string]interface{}{}}}, nil},
		{"more than 255 Choice options", map[string]provider.DecisionQuestion{"team": {Type: "choice", Criteria: choices(256)}}, nil},
		{"no Score levels", map[string]provider.DecisionQuestion{"urgency": {Type: "score", Criteria: []interface{}{}}}, nil},
		{"more than 10 Score levels", map[string]provider.DecisionQuestion{"urgency": {Type: "score", Criteria: levels(11)}}, nil},
		{"more than 512 options across questions", manyBooleans, nil},
		{"more than 16 images", portprovcImageQuestions(), manyImages},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
			state := tc.state
			if state == nil {
				state = []provider.DecisionStatePart{provider.DecisionTextPart(portprovcMessage)}
			}
			_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: state, Questions: tc.questions})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v (%T), want InvalidArgumentError", err, err)
			}
			if len(s.calls) != 0 {
				t.Fatalf("calls = %d, want none", len(s.calls))
			}
		})
	}
	// The boundaries themselves are accepted: 256 yes/no questions are 512
	// options.
	t.Run("512 options accepted", func(t *testing.T) {
		fixture := portprovcFixture(t, "decisions.json")
		answers := map[string]interface{}{}
		qs := map[string]provider.DecisionQuestion{}
		for i := 0; i < 256; i++ {
			answers[strconv.Itoa(i)] = map[string]interface{}{"type": "noul", "noul": 0.5}
			qs[strconv.Itoa(i)] = provider.DecisionQuestion{Type: "boolean"}
		}
		fixture["answers"] = answers
		s, m := portprovcSetup(t, fixture, 200)
		if _, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: portprovcOptions().State, Questions: qs}); err != nil {
			t.Fatal(err)
		}
		if len(s.calls) != 1 {
			t.Fatalf("calls = %d", len(s.calls))
		}
	})
}

// 'warns about unsupported settings without adding fields to the request'
func TestPortProvcWarnsAboutUnsupportedSettings(t *testing.T) {
	s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
	opts := portprovcOptions()
	ten := 10
	low := types.ReasoningLow
	opts.MaxOutputTokens = &ten
	opts.Reasoning = &low
	opts.ProviderOptions = map[string]interface{}{"baseten": map[string]interface{}{"serviceTier": "flex"}}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{
		{Type: "unsupported", Feature: "maxOutputTokens"},
		{Type: "unsupported", Feature: "reasoning"},
		{Type: "unsupported", Feature: "providerOptions.baseten.serviceTier"},
	}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if len(s.calls[0].body) != 3 {
		t.Fatalf("request keys = %v", s.calls[0].body)
	}
}

// 'rejects missing and unexpected answers'
func TestPortProvcRejectsMissingAndUnexpectedAnswers(t *testing.T) {
	fixture := portprovcFixture(t, "decisions.json")
	fa := fixture["answers"].(map[string]interface{})
	withExtra := map[string]interface{}{"extra": fa["refund"]}
	for k, v := range fa {
		withExtra[k] = v
	}
	cases := map[string]map[string]interface{}{
		"empty":      {},
		"extra":      withExtra,
		"two absent": {"team": fa["team"], "urgency": fa["urgency"]},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			f := portprovcFixture(t, "decisions.json")
			f["answers"] = answers
			_, m := portprovcSetup(t, f, 200)
			_, err := m.DoDecide(context.Background(), portprovcOptions())
			var invalid *providererrors.InvalidResponseDataError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v (%T), want InvalidResponseDataError", err, err)
			}
		})
	}
}

// 'rejects malformed native responses'
func TestPortProvcRejectsMalformedResponses(t *testing.T) {
	fa := portprovcFixture(t, "decisions.json")["answers"].(map[string]interface{})
	override := func(key string, value interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for k, v := range fa {
			out[k] = v
		}
		out[key] = value
		return out
	}
	cases := map[string]map[string]interface{}{
		"probability above 1": override("refund", map[string]interface{}{"type": "noul", "noul": 1.01}),
		"choice without data": override("team", map[string]interface{}{"type": "choice", "choice": "billing"}),
		"box answer":          override("refund", map[string]interface{}{"type": "box", "box": nil}),
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			f := portprovcFixture(t, "decisions.json")
			f["answers"] = answers
			_, m := portprovcSetup(t, f, 200)
			_, err := m.DoDecide(context.Background(), portprovcOptions())
			var api *providererrors.ProviderError
			if !errors.As(err, &api) || !strings.Contains(err.Error(), "Invalid JSON response") {
				t.Fatalf("error = %v (%T), want an Invalid JSON response APICallError", err, err)
			}
		})
	}
}

// 'preserves Baseten validation error messages'
func TestPortProvcPreservesValidationErrorMessages(t *testing.T) {
	_, m := portprovcSetup(t, map[string]interface{}{"error": "Question type must be choice, noul or score", "error_type": "Validation"}, 422)
	_, err := m.DoDecide(context.Background(), portprovcOptions())
	var api *providererrors.ProviderError
	if !errors.As(err, &api) {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if api.Message != "Question type must be choice, noul or score" || api.StatusCode != 422 {
		t.Fatalf("message = %q, status = %d", api.Message, api.StatusCode)
	}
	// Dedicated-deployment envelope: {error: {message}}.
	_, m = portprovcSetup(t, map[string]interface{}{"error": map[string]interface{}{"message": "bad things", "type": "x"}}, 400)
	_, err = m.DoDecide(context.Background(), portprovcOptions())
	if !errors.As(err, &api) || api.Message != "bad things" {
		t.Fatalf("object envelope: %v", err)
	}
}

// 'restores the endpoint and authentication across workflow serialization':
// the endpoint and static headers survive; the API key is never serialized
// (Go drops credentials), so the restored model reads BASETEN_API_KEY.
func TestPortProvcWorkflowSerialization(t *testing.T) {
	s, m := portprovcSetup(t, portprovcFixture(t, "decisions.json"), 200)
	serialized := m.Serialize()
	if serialized.Provider != "baseten.decision" || serialized.ModelID != "decision-models/baseten-bison-latest" {
		t.Fatalf("serialized = %+v", serialized)
	}
	if _, leaked := serialized.Config["APIKey"]; leaked {
		t.Fatalf("API key serialized: %v", serialized.Config)
	}
	t.Setenv("BASETEN_API_KEY", "env-key")
	restored, err := provider.DeserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.DoDecide(context.Background(), portprovcOptions()); err != nil {
		t.Fatal(err)
	}
	c := s.calls[0]
	if c.path != "/v1/systemone" || c.headers.Get("authorization") != "Bearer env-key" || c.headers.Get("x-provider") != "custom" {
		t.Fatalf("call = %s %v", c.path, c.headers)
	}
}

// baseten-provider.unit.test.ts 'should call the Model APIs System One
// endpoint and ignore modelURL'.
func TestPortProvcProviderIgnoresModelURL(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"refund":{"type":"noul","noul":0.9}},"usage":{"input_tokens":12,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p := New(Config{
		APIKey:   "k",
		BaseURL:  srv.URL + "/v1",
		ModelURL: "https://model-123.api.baseten.co/environments/production/sync/v1",
	})
	model, err := p.DecisionModel(DecisionModelGroundhogLatest)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := model.(*DecisionModel); !ok || model.Provider() != "baseten.decision" {
		t.Fatalf("model = %T %s", model, model.Provider())
	}
	if _, err := model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     []provider.DecisionStatePart{provider.DecisionTextPart("Please refund me.")},
		Questions: map[string]provider.DecisionQuestion{"refund": {Type: "boolean"}},
	}); err != nil {
		t.Fatal(err)
	}
	if gotURL != "/v1/systemone" {
		t.Fatalf("path = %q", gotURL)
	}
	// The default base URL is the Model APIs one.
	if d := New(Config{APIKey: "k"}).decisionBaseURL; d != "https://inference.baseten.co/v1" {
		t.Fatalf("default decision base URL = %q", d)
	}
}

func TestPortProvcMissingAPIKey(t *testing.T) {
	t.Setenv("BASETEN_API_KEY", "")
	m, _ := New(Config{}).DecisionModel("x")
	_, err := m.DoDecide(context.Background(), portprovcOptions())
	if !providererrors.IsLoadAPIKeyError(err) || !strings.HasPrefix(err.Error(), "Baseten API key API key is missing") {
		t.Fatalf("error = %v", err)
	}
}
