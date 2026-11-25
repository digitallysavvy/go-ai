package heygen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/heygen/src/heygen-decision-model.test.ts (585caa76cd).

const portprovgDecisionFixture = `{"data":{"id":"decision-0123456789abcdef","request_id":"0123456789abcdef0123456789abcdef","model":"heygen-decision-1","answers":{
"blocked":{"type":"noul","noul":0.96},
"team":{"type":"choice","choice":"video","probabilities":{"video":0.8,"account":0.15,"billing":0.05},"confidence":0.7},
"impact":{"type":"score","score":1.75,"probabilities":{"0":0.05,"1":0.15,"2":0.8},"legend":{"0":"Cosmetic","1":"Slower","2":"Blocked"},"confidence":0.625}},
"usage":{"input_tokens":1200,"output_tokens":0}}}`

type portprovgHeyGen struct {
	server  *httptest.Server
	request *http.Request
	body    map[string]interface{}
	raw     string
	calls   int
	model   provider.DecisionModel
}

func portprovgSetup(t *testing.T, body string, status int) *portprovgHeyGen {
	t.Helper()
	h := &portprovgHeyGen{}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.calls++
		h.request = r
		h.body = nil
		rawBody, _ := io.ReadAll(r.Body)
		h.raw = string(rawBody)
		_ = json.Unmarshal(rawBody, &h.body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-trace", "trace-1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(h.server.Close)
	m, err := New(Config{APIKey: "test-key", BaseURL: h.server.URL + "/", Headers: map[string]string{"x-provider": "custom", "shared": "provider"}}).DecisionModel("heygen-decision-1")
	if err != nil {
		t.Fatal(err)
	}
	h.model = m
	return h
}

func portprovgOptions() provider.DecisionCallOptions {
	return provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart("Video export fails for everyone.")},
		Questions: map[string]provider.DecisionQuestion{
			"blocked": {Type: "boolean", Instructions: "Is the customer unable to work?"},
			"team": {Type: "choice", Instructions: "Which team should handle this request?",
				Criteria: map[string]interface{}{"video": "Rendering", "account": "Access", "billing": "Payments"}},
			"impact": {Type: "score", Instructions: "How much does this affect the customer?", Criteria: []interface{}{"Cosmetic", "Slower", "Blocked"}},
		},
	}
}

func portprovgBooleanQuestion() provider.DecisionQuestion {
	return provider.DecisionQuestion{Type: "boolean", Instructions: "Is the customer unable to work?"}
}

func TestPortprovgHeyGenDecisionSendsOneNativeRequest(t *testing.T) {
	h := portprovgSetup(t, portprovgDecisionFixture, 200)
	opts := portprovgOptions()
	opts.Headers = map[string]string{"shared": "call"}
	if _, err := h.model.DoDecide(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if h.model.SpecificationVersion() != "v4" || h.model.Provider() != "heygen.decision" || h.model.ModelID() != "heygen-decision-1" ||
		!reflect.DeepEqual(h.model.SupportedQuestionTypes(), []string{"choice", "score", "boolean"}) {
		t.Fatalf("model = %s %s %s %v", h.model.SpecificationVersion(), h.model.Provider(), h.model.ModelID(), h.model.SupportedQuestionTypes())
	}
	if h.calls != 1 || h.request.URL.Path != "/v3/models/decisions" {
		t.Fatalf("calls = %d path = %s", h.calls, h.request.URL.Path)
	}
	for k, want := range map[string]string{"X-Api-Key": "test-key", "Content-Type": "application/json", "X-Provider": "custom", "Shared": "call"} {
		if got := h.request.Header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if ua := h.request.Header.Get("User-Agent"); !strings.Contains(ua, "ai-sdk-heygen/") {
		t.Errorf("user-agent = %q", ua)
	}
	want := map[string]interface{}{
		"model": "heygen-decision-1",
		"state": "Video export fails for everyone.",
		"questions": map[string]interface{}{
			"blocked": map[string]interface{}{"type": "noul", "instructions": "Is the customer unable to work?"},
			"team": map[string]interface{}{"type": "choice", "instructions": "Which team should handle this request?",
				"criteria": map[string]interface{}{"video": "Rendering", "account": "Access", "billing": "Payments"}},
			"impact": map[string]interface{}{"type": "score", "instructions": "How much does this affect the customer?", "criteria": []interface{}{"Cosmetic", "Slower", "Blocked"}},
		},
	}
	if !reflect.DeepEqual(h.body, want) {
		t.Fatalf("body = %#v", h.body)
	}
}

func TestPortprovgHeyGenDecisionMapsAnswersAndMetadata(t *testing.T) {
	body := strings.Replace(portprovgDecisionFixture, `"blocked":{"type":"noul","noul":0.96}`, `"blocked":{"type":"noul","noul":0.96,"warning":"Low evidence"}`, 1)
	h := portprovgSetup(t, body, 200)
	result, err := h.model.DoDecide(context.Background(), portprovgOptions())
	if err != nil {
		t.Fatal(err)
	}
	p96, score := 0.96, 1.75
	wantAnswers := map[string]provider.DecisionAnswer{
		"blocked": {Type: "boolean", Probability: &p96},
		"team":    {Type: "choice", Choice: "video", Probabilities: map[string]float64{"video": 0.8, "account": 0.15, "billing": 0.05}},
		"impact":  {Type: "score", Score: &score, Probabilities: map[string]float64{"0": 0.05, "1": 0.15, "2": 0.8}},
	}
	if !reflect.DeepEqual(result.Answers, wantAnswers) {
		t.Fatalf("answers = %#v", result.Answers)
	}
	if result.Rounding != nil || len(result.Warnings) != 0 {
		t.Fatalf("rounding %v warnings %v", result.Rounding, result.Warnings)
	}
	if result.Usage == nil || *result.Usage.InputTokens != 1200 || *result.Usage.OutputTokens != 0 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	wantMeta := map[string]interface{}{"heygen": map[string]interface{}{
		"requestId":      "0123456789abcdef0123456789abcdef",
		"confidence":     map[string]float64{"team": 0.7, "impact": 0.625},
		"legend":         map[string]interface{}{"impact": map[string]interface{}{"0": "Cosmetic", "1": "Slower", "2": "Blocked"}},
		"answerWarnings": map[string]string{"blocked": "Low evidence"},
	}}
	if !reflect.DeepEqual(result.ProviderMetadata, wantMeta) {
		t.Fatalf("metadata = %#v", result.ProviderMetadata)
	}
	if result.Response.ID != "decision-0123456789abcdef" || result.Response.ModelID != "heygen-decision-1" || result.Response.Headers["x-trace"] != "trace-1" || result.Response.Body == nil {
		t.Fatalf("response = %+v", result.Response)
	}
}

func TestPortprovgHeyGenDecisionToleratesAbsentAncillaryFields(t *testing.T) {
	h := portprovgSetup(t, `{"data":{"answers":{"blocked":{"type":"noul","noul":0.96},"team":{"type":"choice","choice":"video","probabilities":{"video":1}},"impact":{"type":"score","score":0,"probabilities":{"0":1}}}}}`, 200)
	result, err := h.model.DoDecide(context.Background(), portprovgOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage != nil || result.Response.ID != "" || result.Response.ModelID != "heygen-decision-1" {
		t.Fatalf("result = %+v / %+v", result.Usage, result.Response)
	}
	if _, has := result.ProviderMetadata["heygen"].(map[string]interface{})["requestId"]; has {
		t.Fatal("requestId present")
	}
}

func portprovgFile(mediaType string, data types.FileData) provider.DecisionStatePart {
	return provider.DecisionFilePart(types.FileContent{MediaType: mediaType, FileData: data})
}

func TestPortprovgHeyGenDecisionSendsFilesAsOrderedAssets(t *testing.T) {
	h := portprovgSetup(t, `{"data":{"answers":{"blocked":{"type":"noul","noul":0.96}}}}`, 200)
	_, err := h.model.DoDecide(context.Background(), provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("Compare the first image with the video."),
			portprovgFile("image", types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}}),
			portprovgFile("video/mp4", types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/clip.mp4"}),
			portprovgFile("image/jpeg", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"heygen": "asset-123"}}),
			provider.DecisionJSONPart(map[string]interface{}{"product": "vase"}),
		},
		Questions: map[string]provider.DecisionQuestion{"blocked": portprovgBooleanQuestion()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.body["state"], []interface{}{"Compare the first image with the video.", map[string]interface{}{"product": "vase"}}) {
		t.Fatalf("state = %#v", h.body["state"])
	}
	wantAssets := map[string]interface{}{
		"images": map[string]interface{}{
			"image-1": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="},
			"image-2": map[string]interface{}{"type": "asset_id", "asset_id": "asset-123"},
		},
		"videos": map[string]interface{}{"video-1": map[string]interface{}{"type": "url", "url": "https://example.com/clip.mp4"}},
	}
	if !reflect.DeepEqual(h.body["assets"], wantAssets) {
		t.Fatalf("assets = %#v", h.body["assets"])
	}
	// Key order on the wire (TS body literal): model, state, assets, questions.
	order := []int{strings.Index(h.raw, `"model"`), strings.Index(h.raw, `"state"`), strings.Index(h.raw, `"assets"`), strings.Index(h.raw, `"questions"`)}
	if !(order[0] == 1 && order[0] < order[1] && order[1] < order[2] && order[2] < order[3]) {
		t.Fatalf("key order = %v in %s", order, h.raw)
	}
}

func TestPortprovgHeyGenDecisionRejectsBeforeHTTP(t *testing.T) {
	pngData := types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw0KGgo="}
	five := make([]provider.DecisionStatePart, 5)
	for i := range five {
		five[i] = portprovgFile("image/png", pngData)
	}
	two := []provider.DecisionStatePart{
		portprovgFile("video/mp4", types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.mp4"}),
		portprovgFile("video/mp4", types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.mp4"}),
	}
	type kind int
	const (
		invalidArg kind = iota
		unsupported
		noReference
	)
	cases := []struct {
		name  string
		state []provider.DecisionStatePart
		kind  kind
	}{
		{"more than four images", five, invalidArg},
		{"more than one video", two, invalidArg},
		{"non-HTTPS URLs", []provider.DecisionStatePart{portprovgFile("image/png", types.FileData{Type: types.FileDataTypeURL, URL: "http://example.com/a.png"})}, invalidArg},
		{"undetectable media types", []provider.DecisionStatePart{portprovgFile("image", types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}})}, invalidArg},
		{"non-image, non-video media", []provider.DecisionStatePart{portprovgFile("application/pdf", types.FileData{Type: types.FileDataTypeData, DataString: "JVBERi0="})}, unsupported},
		{"inline text files", []provider.DecisionStatePart{portprovgFile("image/png", types.FileData{Type: types.FileDataTypeText, Text: "not an image"})}, unsupported},
		{"references without a HeyGen asset", []provider.DecisionStatePart{portprovgFile("image/png", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"other": "file-1"}})}, noReference},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := portprovgSetup(t, portprovgDecisionFixture, 200)
			opts := portprovgOptions()
			opts.State = tc.state
			_, err := h.model.DoDecide(context.Background(), opts)
			var invalid *providererrors.InvalidArgumentError
			var unsupportedErr *providererrors.UnsupportedFunctionalityError
			var noRef *providererrors.NoSuchProviderReferenceError
			ok := false
			switch tc.kind {
			case invalidArg:
				ok = errors.As(err, &invalid)
			case unsupported:
				ok = errors.As(err, &unsupportedErr)
			case noReference:
				ok = errors.As(err, &noRef)
			}
			if !ok {
				t.Fatalf("err = %v (%T)", err, err)
			}
			if h.calls != 0 {
				t.Fatalf("%d requests were sent", h.calls)
			}
		})
	}
}

func TestPortprovgHeyGenDecisionRequestLimits(t *testing.T) {
	many := map[string]provider.DecisionQuestion{}
	for i := 0; i < 401; i++ {
		many[fmt.Sprint(i)] = portprovgBooleanQuestion()
	}
	bigChoice := map[string]interface{}{}
	for i := 0; i < 256; i++ {
		bigChoice[fmt.Sprint(i)] = nil
	}
	bigScore := make([]interface{}, 11)
	for i := range bigScore {
		bigScore[i] = fmt.Sprint(i)
	}
	team := func(criteria interface{}) map[string]provider.DecisionQuestion {
		return map[string]provider.DecisionQuestion{"team": {Type: "choice", Instructions: "x", Criteria: criteria}}
	}
	impact := func(criteria interface{}) map[string]provider.DecisionQuestion {
		return map[string]provider.DecisionQuestion{"impact": {Type: "score", Instructions: "x", Criteria: criteria}}
	}
	cases := map[string]map[string]provider.DecisionQuestion{
		"no questions":       {},
		"empty name":         {"": portprovgBooleanQuestion()},
		"padded name":        {" padded": portprovgBooleanQuestion()},
		"colon name":         {"a:b": portprovgBooleanQuestion()},
		"newline name":       {"a\nb": portprovgBooleanQuestion()},
		"401 questions":      many,
		"empty choice":       team(map[string]interface{}{}),
		"256 choice options": team(bigChoice),
		"empty score":        impact([]interface{}{}),
		"null score level":   impact([]interface{}{"Low", nil}),
		"11 score levels":    impact(bigScore),
	}
	for name, questions := range cases {
		t.Run(name, func(t *testing.T) {
			h := portprovgSetup(t, portprovgDecisionFixture, 200)
			opts := portprovgOptions()
			opts.Questions = questions
			_, err := h.model.DoDecide(context.Background(), opts)
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) || h.calls != 0 {
				t.Fatalf("err = %v (%T), calls = %d", err, err, h.calls)
			}
		})
	}

	t.Run("accepts the maximum question count and a single-level score", func(t *testing.T) {
		questions := map[string]provider.DecisionQuestion{"single": {Type: "score", Instructions: "Rate", Criteria: []interface{}{"Only"}}}
		answers := []string{`"single":{"type":"score","score":0,"probabilities":{"0":1}}`}
		for i := 0; i < 399; i++ {
			questions[fmt.Sprint(i)] = portprovgBooleanQuestion()
			answers = append(answers, fmt.Sprintf(`"%d":{"type":"noul","noul":0.96}`, i))
		}
		h := portprovgSetup(t, `{"data":{"answers":{`+strings.Join(answers, ",")+`}}}`, 200)
		opts := portprovgOptions()
		opts.Questions = questions
		if _, err := h.model.DoDecide(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		if h.calls != 1 || len(h.body["questions"].(map[string]interface{})) != 400 {
			t.Fatalf("calls = %d", h.calls)
		}
	})

	t.Run("rejects box questions", func(t *testing.T) {
		h := portprovgSetup(t, portprovgDecisionFixture, 200)
		opts := portprovgOptions()
		opts.Questions = map[string]provider.DecisionQuestion{"box": {Type: "box", Instructions: "Find the product"}}
		_, err := h.model.DoDecide(context.Background(), opts)
		var target *providererrors.DecisionUnsupportedQuestionTypeError
		if !errors.As(err, &target) || h.calls != 0 {
			t.Fatalf("err = %v (%T)", err, err)
		}
	})
}

func TestPortprovgHeyGenDecisionWarnsAboutUnsupportedSettings(t *testing.T) {
	h := portprovgSetup(t, portprovgDecisionFixture, 200)
	opts := portprovgOptions()
	ten, low := 10, types.ReasoningLow
	opts.MaxOutputTokens, opts.Reasoning = &ten, &low
	opts.ProviderOptions = map[string]interface{}{"heygen": map[string]interface{}{"temperature": 0}}
	result, err := h.model.DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Type+":"+w.Feature)
	}
	if !reflect.DeepEqual(features, []string{"unsupported:maxOutputTokens", "unsupported:reasoning", "unsupported:providerOptions.heygen.temperature"}) {
		t.Fatalf("warnings = %v", features)
	}
	if len(h.body) != 3 {
		t.Fatalf("request keys = %v", h.body)
	}
}

func TestPortprovgHeyGenDecisionRejectsMissingAndUnexpectedAnswers(t *testing.T) {
	blocked := `"blocked":{"type":"noul","noul":0.96}`
	team := `"team":{"type":"choice","choice":"video","probabilities":{"video":1}}`
	impact := `"impact":{"type":"score","score":0,"probabilities":{"0":1}}`
	for name, answers := range map[string]string{
		"none":       ``,
		"extra":      strings.Join([]string{blocked, team, impact, `"extra":{"type":"noul","noul":0.5}`}, ","),
		"one absent": strings.Join([]string{team, impact}, ","),
	} {
		t.Run(name, func(t *testing.T) {
			h := portprovgSetup(t, `{"data":{"answers":{`+answers+`}}}`, 200)
			_, err := h.model.DoDecide(context.Background(), portprovgOptions())
			var target *providererrors.InvalidResponseDataError
			if !errors.As(err, &target) {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
}

func TestPortprovgHeyGenDecisionRejectsMalformedNativeResponses(t *testing.T) {
	team := `"team":{"type":"choice","choice":"video","probabilities":{"video":1}}`
	impact := `"impact":{"type":"score","score":0,"probabilities":{"0":1}}`
	for name, blocked := range map[string]string{
		"noul above one":   `"blocked":{"type":"noul","noul":1.01}`,
		"refusal":          `"blocked":{"type":"refusal"}`,
		"noul not a float": `"blocked":{"type":"noul","noul":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := portprovgSetup(t, `{"data":{"answers":{`+blocked+`,`+team+`,`+impact+`}}}`, 200)
			_, err := h.model.DoDecide(context.Background(), portprovgOptions())
			var target *providererrors.ProviderError
			if !errors.As(err, &target) || target.Message != "Invalid JSON response" {
				t.Fatalf("err = %v (%T)", err, err)
			}
		})
	}
	t.Run("choice without probabilities", func(t *testing.T) {
		h := portprovgSetup(t, `{"data":{"answers":{"blocked":{"type":"noul","noul":0.9},"team":{"type":"choice","choice":"video"},`+impact+`}}}`, 200)
		_, err := h.model.DoDecide(context.Background(), portprovgOptions())
		var target *providererrors.ProviderError
		if !errors.As(err, &target) {
			t.Fatalf("err = %v (%T)", err, err)
		}
	})
}

func TestPortprovgHeyGenDecisionPreservesErrorCodesAndParameters(t *testing.T) {
	h := portprovgSetup(t, `{"error":{"code":"invalid_parameter","message":"Invalid question.","param":"questions.team.criteria"}}`, 400)
	_, err := h.model.DoDecide(context.Background(), portprovgOptions())
	var target *providererrors.ProviderError
	if !errors.As(err, &target) || target.StatusCode != 400 || target.Message != "Invalid question. (invalid_parameter) [questions.team.criteria]" {
		t.Fatalf("err = %#v", err)
	}
}

func TestPortprovgHeyGenDecisionLoadsEnvironmentAuthenticationLazily(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "heygen-decision-1-turbo" {
			t.Errorf("model = %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(portprovgDecisionFixture))
	}))
	defer server.Close()
	model, _ := New(Config{BaseURL: server.URL}).DecisionModel(string(DecisionModelHeyGenDecision1Turbo))
	t.Setenv("HEYGEN_API_KEY", "environment-key")
	if _, err := model.DoDecide(context.Background(), portprovgOptions()); err != nil {
		t.Fatal(err)
	}
	if gotKey != "environment-key" {
		t.Fatalf("x-api-key = %q", gotKey)
	}
	if DefaultBaseURL != "https://api.heygen.com" {
		t.Fatalf("default base URL = %q", DefaultBaseURL)
	}
}

func TestPortprovgHeyGenDecisionWorkflowSerialization(t *testing.T) {
	h := portprovgSetup(t, portprovgDecisionFixture, 200)
	serializable, ok := h.model.(provider.SerializableModel)
	if !ok {
		t.Fatalf("%T is not serializable", h.model)
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "heygen.decision" || serialized.ModelID != "heygen-decision-1" {
		t.Fatalf("serialized = %+v", serialized)
	}
	raw, _ := json.Marshal(serialized.Config)
	if strings.Contains(string(raw), "test-key") {
		t.Fatalf("credentials were serialized: %s", raw)
	}
	t.Setenv("HEYGEN_API_KEY", "restored-key")
	restored, err := provider.DeserializeDecisionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.DoDecide(context.Background(), portprovgOptions()); err != nil {
		t.Fatal(err)
	}
	if h.request.URL.Path != "/v3/models/decisions" || h.request.Header.Get("x-api-key") != "restored-key" {
		t.Fatalf("restored request: %s key %q", h.request.URL.Path, h.request.Header.Get("x-api-key"))
	}
}
