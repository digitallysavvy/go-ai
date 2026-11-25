package liquid

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

// Ports TS packages/liquid/src/liquid-decision-model.test.ts and
// liquid-provider.test.ts (cf5fe9a223) with their fixtures in testdata/.

var (
	png = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	wav = []byte{0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x57, 0x41, 0x56, 0x45}
)

func fixture(t *testing.T, name string) map[string]interface{} {
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

type recorded struct {
	path    string
	headers http.Header
	body    map[string]interface{}
	raw     string
}

type fake struct {
	mu    sync.Mutex
	calls []recorded
}

func (f *fake) last(t *testing.T) recorded {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(f.calls))
	}
	return f.calls[0]
}

func newModel(t *testing.T, body interface{}, status int) (*DecisionModel, *fake, *Provider) {
	t.Helper()
	f := &fake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		f.mu.Lock()
		f.calls = append(f.calls, recorded{path: r.URL.Path, headers: r.Header.Clone(), body: b, raw: string(raw)})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-trace", "trace-1")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/", Headers: map[string]string{"x-provider": "custom", "shared": "provider"}})
	m, err := p.DecisionModel("d1.2")
	if err != nil {
		t.Fatal(err)
	}
	return m.(*DecisionModel), f, p
}

func textAnswers(t *testing.T) map[string]interface{} {
	a := fixture(t, "decisions.json")["answers"].(map[string]interface{})
	return map[string]interface{}{"department": a["department"], "is_urgent": a["is_urgent"], "frustration": a["frustration"]}
}

func baseBody(t *testing.T) map[string]interface{} {
	b := fixture(t, "decisions.json")
	b["answers"] = textAnswers(t)
	return b
}

func callOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart("Help! My payouts have been failing for 3 days.")},
		Questions: map[string]provider.DecisionQuestion{
			"department": {Type: "choice", Instructions: "Which team should handle this?", Criteria: map[string]interface{}{
				"billing": "Payments, invoicing, refunds", "technical": "Bugs, outages, integrations", "sales": "Pricing, upgrades, new accounts",
			}},
			"is_urgent":   {Type: "boolean", Instructions: "Does this convey urgency?"},
			"frustration": {Type: "score", Instructions: "How frustrated is the customer?", Criteria: []interface{}{"Calm", "Frustrated", "Very angry"}},
		},
		QuestionOrder: []string{"department", "is_urgent", "frustration"},
	}
}

func fileData(mediaType string, data []byte) provider.DecisionStatePart {
	return provider.DecisionFilePart(types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, Data: data}})
}

func fileString(mediaType, data string) provider.DecisionStatePart {
	return provider.DecisionFilePart(types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}})
}

func boxOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State:     []provider.DecisionStatePart{provider.DecisionTextPart("A support screenshot."), fileData("image/png", png)},
		Questions: map[string]provider.DecisionQuestion{"subject": {Type: "box", Instructions: "The animal in the picture."}},
	}
}

func TestSendsOneNativeRequestWithConfiguredHeaders(t *testing.T) {
	m, f, _ := newModel(t, baseBody(t), 200)
	opts := callOptions()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := m.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if m.SpecificationVersion() != "v4" || m.Provider() != "liquid.decision" || m.ModelID() != "d1.2" ||
		!reflect.DeepEqual(m.SupportedQuestionTypes(), []string{"choice", "score", "boolean", "box"}) {
		t.Fatalf("model = %s %s %s %v", m.SpecificationVersion(), m.Provider(), m.ModelID(), m.SupportedQuestionTypes())
	}
	c := f.last(t)
	if c.path != "/decisions/v1/systemone" {
		t.Fatalf("path = %s", c.path)
	}
	for k, v := range map[string]string{"authorization": "Bearer test-key", "content-type": "application/json", "x-provider": "custom", "shared": "call"} {
		if got := c.headers.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(c.headers.Get("user-agent"), "ai-sdk-liquid/") {
		t.Errorf("user-agent = %q", c.headers.Get("user-agent"))
	}
	want := map[string]interface{}{
		"model": "d1.2",
		"state": "Help! My payouts have been failing for 3 days.",
		"questions": map[string]interface{}{
			"department": map[string]interface{}{"type": "choice", "instructions": "Which team should handle this?", "criteria": map[string]interface{}{
				"billing": "Payments, invoicing, refunds", "technical": "Bugs, outages, integrations", "sales": "Pricing, upgrades, new accounts",
			}},
			"is_urgent":   map[string]interface{}{"type": "noul", "instructions": "Does this convey urgency?"},
			"frustration": map[string]interface{}{"type": "score", "instructions": "How frustrated is the customer?", "criteria": []interface{}{"Calm", "Frustrated", "Very angry"}},
		},
	}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v\nwant %v", c.body, want)
	}
}

func TestMapsAnswersAndPreservesMetadata(t *testing.T) {
	body := baseBody(t)
	m, _, _ := newModel(t, body, 200)
	res, err := m.DoDecide(context.Background(), callOptions())
	if err != nil {
		t.Fatal(err)
	}
	answers := body["answers"].(map[string]interface{})
	probs := func(id string) map[string]float64 {
		out := map[string]float64{}
		for k, v := range answers[id].(map[string]interface{})["probabilities"].(map[string]interface{}) {
			out[k] = v.(float64)
		}
		return out
	}
	urgent := answers["is_urgent"].(map[string]interface{})["noul"].(float64)
	score := answers["frustration"].(map[string]interface{})["score"].(float64)
	want := map[string]provider.DecisionAnswer{
		"department":  {Type: "choice", Choice: "billing", Probabilities: probs("department")},
		"is_urgent":   {Type: "boolean", Probability: &urgent},
		"frustration": {Type: "score", Score: &score, Probabilities: probs("frustration")},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	if res.Rounding != nil || len(res.Warnings) != 0 {
		t.Fatalf("rounding/warnings = %v %v", res.Rounding, res.Warnings)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 612 || *res.Usage.OutputTokens != 0 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	wantMeta := map[string]interface{}{"liquid": map[string]interface{}{
		"cost": body["usage"].(map[string]interface{})["cost"],
		"confidence": map[string]interface{}{
			"department":  answers["department"].(map[string]interface{})["confidence"],
			"frustration": answers["frustration"].(map[string]interface{})["confidence"],
		},
		"legend": map[string]interface{}{"frustration": answers["frustration"].(map[string]interface{})["legend"]},
		"found":  map[string]interface{}{},
	}}
	if !reflect.DeepEqual(res.ProviderMetadata, wantMeta) {
		t.Fatalf("metadata = %#v\nwant %#v", res.ProviderMetadata, wantMeta)
	}
	if res.Response.ModelID != "d1.2" || res.Response.Headers["x-trace"] != "trace-1" || !reflect.DeepEqual(res.Response.Body, body) {
		t.Fatalf("response = %+v", res.Response)
	}
}

func TestSendsBoxQuestionWithImageAndMapsBox(t *testing.T) {
	subject := fixture(t, "decisions.json")["answers"].(map[string]interface{})["subject"].(map[string]interface{})
	m, f, _ := newModel(t, map[string]interface{}{"model": "d1.2", "answers": map[string]interface{}{"subject": subject}}, 200)
	res, err := m.DoDecide(context.Background(), boxOptions())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"model":     "d1.2",
		"state":     "A support screenshot.",
		"images":    []interface{}{"data:image/png;base64,iVBORw0KGgo="},
		"questions": map[string]interface{}{"subject": map[string]interface{}{"type": "box", "instructions": "The animal in the picture."}},
	}
	if c := f.last(t); !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v", c.body)
	}
	box := subject["box"].(map[string]interface{})
	wantBox := &provider.DecisionBox{Top: box["top"].(float64), Bottom: box["bottom"].(float64), Left: box["left"].(float64), Right: box["right"].(float64)}
	if got := res.Answers["subject"]; got.Type != "box" || !reflect.DeepEqual(got.Box, wantBox) {
		t.Fatalf("answer = %+v", got)
	}
	meta := res.ProviderMetadata["liquid"].(map[string]interface{})
	if meta["confidence"].(map[string]interface{})["subject"] != subject["confidence"] || meta["found"].(map[string]interface{})["subject"] != 1.0 {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestReturnsRefusalWhenBoxObjectNotFound(t *testing.T) {
	body := fixture(t, "decisions-box-not-found.json")
	m, _, _ := newModel(t, body, 200)
	res, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:     boxOptions().State.([]provider.DecisionStatePart),
		Questions: map[string]provider.DecisionQuestion{"car": {Type: "box", Instructions: "The red sports car."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Answers, map[string]provider.DecisionAnswer{"car": {Type: "refusal"}}) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	meta := res.ProviderMetadata["liquid"].(map[string]interface{})
	wantFound := body["answers"].(map[string]interface{})["car"].(map[string]interface{})["found"]
	if meta["found"].(map[string]interface{})["car"] != wantFound {
		t.Fatalf("found = %v", meta["found"])
	}
	// confidence 0 is a present confidence.
	if meta["confidence"].(map[string]interface{})["car"] != 0.0 {
		t.Fatalf("confidence = %v", meta["confidence"])
	}
}

func TestSendsImagesAndAudioAsOrderedTopLevelDataURLs(t *testing.T) {
	isUrgent := fixture(t, "decisions.json")["answers"].(map[string]interface{})["is_urgent"]
	m, f, _ := newModel(t, map[string]interface{}{"answers": map[string]interface{}{"is_urgent": isUrgent}}, 200)
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("The voice note belongs to this ticket."),
			fileData("audio", wav),
			fileData("image", png),
			fileString("image/webp", "UklGRg=="),
			provider.DecisionJSONPart(map[string]interface{}{"plan": "enterprise"}),
		},
		Questions: map[string]provider.DecisionQuestion{"is_urgent": callOptions().Questions["is_urgent"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	if c.body["state"] != "The voice note belongs to this ticket.\n{\"plan\":\"enterprise\"}" {
		t.Errorf("state = %v", c.body["state"])
	}
	if !reflect.DeepEqual(c.body["images"], []interface{}{"data:image/png;base64,iVBORw0KGgo=", "data:image/webp;base64,UklGRg=="}) {
		t.Errorf("images = %v", c.body["images"])
	}
	if !reflect.DeepEqual(c.body["audio"], []interface{}{"data:audio/wav;base64,UklGRgAAAABXQVZF"}) {
		t.Errorf("audio = %v", c.body["audio"])
	}
	// images and audio precede the questions, after the state.
	img, aud, q := strings.Index(c.raw, `"images"`), strings.Index(c.raw, `"audio"`), strings.Index(c.raw, `"questions"`)
	if !(strings.Index(c.raw, `"state"`) < img && img < aud && aud < q) {
		t.Errorf("body key order: %s", c.raw[:80])
	}
}

func TestSendsSingleJSONObjectAndMediaOnlyStateNatively(t *testing.T) {
	isUrgent := fixture(t, "decisions.json")["answers"].(map[string]interface{})["is_urgent"]
	for name, tc := range map[string]struct {
		state []provider.DecisionStatePart
		want  interface{}
	}{
		"a single JSON object as structured state": {[]provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"message": "Refund me"})}, map[string]interface{}{"message": "Refund me"}},
		"media-only state as null":                 {[]provider.DecisionStatePart{fileString("audio/mpeg", "SUQz")}, nil},
		"a single JSON array":                      {[]provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{1.0, "a"})}, []interface{}{1.0, "a"}},
		"a single JSON number as text":             {[]provider.DecisionStatePart{provider.DecisionJSONPart(7.0)}, "7"},
		"a single JSON null as text":               {[]provider.DecisionStatePart{provider.DecisionJSONPart(nil)}, "null"},
	} {
		t.Run(name, func(t *testing.T) {
			m, f, _ := newModel(t, map[string]interface{}{"answers": map[string]interface{}{"is_urgent": isUrgent}}, 200)
			if _, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
				State: tc.state, Questions: map[string]provider.DecisionQuestion{"is_urgent": callOptions().Questions["is_urgent"]},
			}); err != nil {
				t.Fatal(err)
			}
			if got := f.last(t).body["state"]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("state = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func nineOf(part provider.DecisionStatePart) []provider.DecisionStatePart {
	parts := make([]provider.DecisionStatePart, 9)
	for i := range parts {
		parts[i] = part
	}
	return parts
}

func TestRejectsBadStateBeforeHTTP(t *testing.T) {
	url := provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.png"}})
	ref := provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference}})
	for name, tc := range map[string]struct {
		state      []provider.DecisionStatePart
		invalidArg bool
	}{
		"more than eight images":      {nineOf(fileData("image/png", png)), true},
		"more than eight audio clips": {nineOf(fileData("audio/wav", wav)), true},
		"unsupported image types":     {[]provider.DecisionStatePart{fileString("image/gif", "R0lGOD==")}, false},
		"undetectable media types":    {[]provider.DecisionStatePart{fileData("audio", []byte{1, 2, 3})}, false},
		"non-image, non-audio media":  {[]provider.DecisionStatePart{fileString("video/mp4", "AAAA")}, false},
		"URL file data":               {[]provider.DecisionStatePart{url}, false},
		"provider references":         {[]provider.DecisionStatePart{ref}, false},
	} {
		t.Run(name, func(t *testing.T) {
			m, f, _ := newModel(t, baseBody(t), 200)
			opts := callOptions()
			opts.State = tc.state
			_, err := m.DoDecide(context.Background(), opts)
			var invalid *providererrors.InvalidArgumentError
			var unsupported *providererrors.UnsupportedFunctionalityError
			switch {
			case tc.invalidArg && !errors.As(err, &invalid):
				t.Fatalf("err = %v, want InvalidArgumentError", err)
			case !tc.invalidArg && !errors.As(err, &unsupported):
				t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("HTTP request sent")
			}
		})
	}
}

func TestRequestLimitsRejectBeforeHTTP(t *testing.T) {
	many := map[string]provider.DecisionQuestion{}
	var order []string
	for i := range 129 {
		id := fmt.Sprint(i)
		many[id] = callOptions().Questions["is_urgent"]
		order = append(order, id)
	}
	manyOptions := map[string]interface{}{}
	for i := range 256 {
		manyOptions[fmt.Sprint(i)] = nil
	}
	levels := make([]interface{}, 11)
	for i := range levels {
		levels[i] = fmt.Sprint(i)
	}
	q := callOptions().Questions
	type tc struct {
		questions map[string]provider.DecisionQuestion
		order     []string
		state     []provider.DecisionStatePart
	}
	cases := map[string]tc{
		"no questions":                    {questions: map[string]provider.DecisionQuestion{}},
		"more than 128 questions":         {questions: many, order: order},
		"missing instructions":            {questions: map[string]provider.DecisionQuestion{"is_urgent": {Type: "boolean", Criteria: map[string]interface{}{"true": "Urgent", "false": "Not urgent"}}}},
		"a single Choice option":          {questions: map[string]provider.DecisionQuestion{"department": {Type: "choice", Instructions: "x", Criteria: map[string]interface{}{"billing": nil}}}},
		"256 Choice options":              {questions: map[string]provider.DecisionQuestion{"department": {Type: "choice", Instructions: "x", Criteria: manyOptions}}},
		"a single Score level":            {questions: map[string]provider.DecisionQuestion{"frustration": {Type: "score", Instructions: "x", Criteria: []interface{}{"Calm"}}}},
		"more than 10 Score level":        {questions: map[string]provider.DecisionQuestion{"frustration": {Type: "score", Instructions: "x", Criteria: levels}}},
		"null Score levels":               {questions: map[string]provider.DecisionQuestion{"frustration": {Type: "score", Instructions: "x", Criteria: []interface{}{"Calm", nil}}}},
		"a Box question without an image": {questions: boxOptions().Questions, state: callOptions().State.([]provider.DecisionStatePart)},
		"a Box question with two images":  {questions: boxOptions().Questions, state: append(append([]provider.DecisionStatePart{}, boxOptions().State.([]provider.DecisionStatePart)...), boxOptions().State.([]provider.DecisionStatePart)[1])},
	}
	_ = q
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, f, _ := newModel(t, baseBody(t), 200)
			state := c.state
			if state == nil {
				state = boxOptions().State.([]provider.DecisionStatePart)
			}
			_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{State: state, Questions: c.questions, QuestionOrder: c.order})
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want InvalidArgumentError", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("HTTP request sent")
			}
		})
	}
}

func TestSendsBooleanCriteriaAndNullChoiceDescriptions(t *testing.T) {
	answers := textAnswers(t)
	dept := map[string]interface{}{}
	for k, v := range answers["department"].(map[string]interface{}) {
		dept[k] = v
	}
	dept["probabilities"] = map[string]interface{}{"billing": 0.4, "technical": 0.6}
	m, f, _ := newModel(t, map[string]interface{}{"answers": map[string]interface{}{"department": dept, "is_urgent": answers["is_urgent"]}}, 200)
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: callOptions().State.([]provider.DecisionStatePart),
		Questions: map[string]provider.DecisionQuestion{
			"department": {Type: "choice", Instructions: "Which team?", Criteria: map[string]interface{}{"billing": nil, "technical": nil}},
			"is_urgent":  {Type: "boolean", Instructions: "Urgent?", Criteria: map[string]interface{}{"true": "Needs action today", "false": "Can wait"}},
		},
		QuestionOrder: []string{"department", "is_urgent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"department": map[string]interface{}{"type": "choice", "instructions": "Which team?", "criteria": map[string]interface{}{"billing": nil, "technical": nil}},
		"is_urgent":  map[string]interface{}{"type": "noul", "instructions": "Urgent?", "criteria": map[string]interface{}{"true": "Needs action today", "false": "Can wait"}},
	}
	if got := f.last(t).body["questions"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("questions = %v", got)
	}
}

func TestWarnsAboutUnsupportedSettingsWithoutRequestFields(t *testing.T) {
	m, f, _ := newModel(t, baseBody(t), 200)
	opts := callOptions()
	ten := 10
	low := types.ReasoningLow
	opts.MaxOutputTokens = &ten
	opts.Reasoning = &low
	opts.ProviderOptions = map[string]interface{}{"liquid": map[string]interface{}{"temperature": 0}}
	res, err := m.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{
		{Type: "unsupported", Feature: "maxOutputTokens"},
		{Type: "unsupported", Feature: "reasoning"},
		{Type: "unsupported", Feature: "providerOptions.liquid.temperature"},
	}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	c := f.last(t)
	keys := []string{}
	for _, k := range []string{"model", "state", "questions"} {
		if _, ok := c.body[k]; ok {
			keys = append(keys, k)
		}
	}
	if len(c.body) != 3 || len(keys) != 3 {
		t.Fatalf("body keys = %v", c.body)
	}
}

func TestRejectsMissingAndUnexpectedAnswers(t *testing.T) {
	a := textAnswers(t)
	withExtra := map[string]interface{}{"department": a["department"], "is_urgent": a["is_urgent"], "frustration": a["frustration"], "extra": a["is_urgent"]}
	for name, answers := range map[string]map[string]interface{}{
		"empty":   {},
		"extra":   withExtra,
		"missing": {"department": a["department"], "frustration": a["frustration"]},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := newModel(t, map[string]interface{}{"answers": answers}, 200)
			_, err := m.DoDecide(context.Background(), callOptions())
			var bad *providererrors.InvalidResponseDataError
			if !errors.As(err, &bad) || bad.Message != "Liquid must return exactly one answer for every question." {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRejectsMalformedNativeResponses(t *testing.T) {
	a := textAnswers(t)
	with := func(id string, v interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for k, val := range a {
			out[k] = val
		}
		out[id] = v
		return out
	}
	for name, answers := range map[string]map[string]interface{}{
		"probability above one":        with("is_urgent", map[string]interface{}{"type": "noul", "noul": 1.01}),
		"choice without probabilities": with("department", map[string]interface{}{"type": "choice", "choice": "technical"}),
		"unknown answer type":          with("is_urgent", map[string]interface{}{"type": "text", "text": "yes"}),
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := newModel(t, map[string]interface{}{"answers": answers}, 200)
			_, err := m.DoDecide(context.Background(), callOptions())
			var apiErr *providererrors.ProviderError
			if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "Invalid JSON response") {
				t.Fatalf("err = %#v", err)
			}
		})
	}
}

func TestPreservesLiquidErrorMessagesAndCodes(t *testing.T) {
	m, _, _ := newModel(t, map[string]interface{}{"error": map[string]interface{}{
		"message": "Invalid API key provided.", "type": "authentication_error", "param": nil, "code": "invalid_api_key",
	}}, 401)
	_, err := m.DoDecide(context.Background(), callOptions())
	var apiErr *providererrors.ProviderError
	if !errors.As(err, &apiErr) || apiErr.Message != "Invalid API key provided. (invalid_api_key)" || apiErr.StatusCode != 401 {
		t.Fatalf("err = %#v", err)
	}
	if !reflect.DeepEqual(apiErr.Data, map[string]interface{}{"error": map[string]interface{}{"message": "Invalid API key provided.", "code": "invalid_api_key"}}) {
		t.Fatalf("data = %#v", apiErr.Data)
	}

	// A null code adds no suffix; a body that is not the error shape gives the status text.
	m2, _, _ := newModel(t, map[string]interface{}{"error": map[string]interface{}{"message": "Boom", "code": nil}}, 500)
	_, err = m2.DoDecide(context.Background(), callOptions())
	if !errors.As(err, &apiErr) || apiErr.Message != "Boom" {
		t.Fatalf("null code: %#v", err)
	}
	m3, _, _ := newModel(t, map[string]interface{}{"detail": "nope"}, 502)
	_, err = m3.DoDecide(context.Background(), callOptions())
	if !errors.As(err, &apiErr) || apiErr.Message != "Bad Gateway" {
		t.Fatalf("status text: %#v", err)
	}
}

func TestSerializationRestoresEndpoint(t *testing.T) {
	m, f, p := newModel(t, baseBody(t), 200)
	_ = f
	serialized := m.Serialize()
	if serialized.Provider != "liquid.decision" || serialized.ModelID != "d1.2" {
		t.Fatalf("serialized = %+v", serialized)
	}
	for k := range serialized.Config {
		if strings.EqualFold(k, "apikey") || strings.EqualFold(k, "httpclient") {
			t.Fatalf("%s serialized: %v", k, serialized.Config)
		}
	}
	if serialized.Config["baseURL"] != p.config.BaseURL {
		t.Fatalf("config = %v", serialized.Config)
	}
	t.Setenv("LIQUID_API_KEY", "env-key")
	restored, err := deserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.DoDecide(context.Background(), callOptions()); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	if c.path != "/decisions/v1/systemone" || c.headers.Get("authorization") != "Bearer env-key" {
		t.Fatalf("restored call: %s %v", c.path, c.headers)
	}
}
