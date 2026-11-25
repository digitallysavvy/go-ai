package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS: gateway-extraction-model.test.ts fixtures.
var port2provgFields = map[string]provider.ExtractionField{
	"invoiceNumber": {Type: "text", Instructions: "The invoice number"},
	"total":         {Type: "number", Instructions: "Total amount due", Decimals: port2provgIntPtr(2)},
	"contacts":      {Type: "list", Items: "text"},
}

var port2provgFieldsWire = map[string]interface{}{
	"invoiceNumber": map[string]interface{}{"type": "text", "instructions": "The invoice number"},
	"total":         map[string]interface{}{"type": "number", "instructions": "Total amount due", "decimals": float64(2)},
	"contacts":      map[string]interface{}{"type": "list", "items": "text"},
}

const port2provgDummyAnswers = `{
	"invoiceNumber": {"type":"text","value":"INV-2041","confidence":0.98,"pNull":0.01,"source":{"type":"text","part":0,"start":8,"end":16}},
	"total": {"type":"number","value":1250,"guess":1250},
	"contacts": {"type":"list","value":null}
}`

func port2provgIntPtr(v int) *int { return &v }

func port2provgText(s string) []provider.DecisionStatePart {
	return []provider.DecisionStatePart{provider.DecisionTextPart(s)}
}

type port2provgServer struct {
	t       *testing.T
	calls   int
	method  string
	path    string
	headers http.Header
	body    []byte
	status  int
	resp    string
	respHdr map[string]string
	srv     *httptest.Server
}

func newPort2provgServer(t *testing.T, resp string) *port2provgServer {
	s := &port2provgServer{t: t, status: 200, resp: resp}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls++
		s.method, s.path, s.headers = r.Method, r.URL.Path, r.Header.Clone()
		s.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		for k, v := range s.respHdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.resp))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *port2provgServer) model() *ExtractionModel {
	p, err := New(Config{APIKey: "test-key", BaseURL: s.srv.URL})
	if err != nil {
		s.t.Fatal(err)
	}
	return NewExtractionModel(p, "scaledown/extract-1")
}

func (s *port2provgServer) bodyJSON() map[string]interface{} {
	var out map[string]interface{}
	if err := json.Unmarshal(s.body, &out); err != nil {
		s.t.Fatalf("request body: %v (%s)", err, s.body)
	}
	return out
}

func port2provgJSON(t *testing.T, s string) interface{} {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func port2provgResp(answers string, extra string) string {
	if answers == "" {
		answers = port2provgDummyAnswers
	}
	if extra != "" {
		extra = "," + extra
	}
	return `{"answers":` + answers + extra + `}`
}

// TS: 'should report every field type as supported'
func TestPortprovgExtractionSupportsEveryFieldType(t *testing.T) {
	m := &ExtractionModel{}
	want := []string{"text", "number", "date", "time", "datetime", "duration", "list"}
	if !reflect.DeepEqual(m.SupportedFieldTypes(), want) {
		t.Fatalf("got %v", m.SupportedFieldTypes())
	}
}

// TS: 'should create the model from both provider factories'
func TestPortprovgExtractionProviderFactories(t *testing.T) {
	p, err := New(Config{APIKey: "test-token", BaseURL: "https://api.test.com"})
	if err != nil {
		t.Fatal(err)
	}
	var _ provider.ExtractionModelProvider = p
	for name, make := range map[string]func(string) (provider.ExtractionModel, error){"ExtractionModel": p.ExtractionModel, "Extraction": p.Extraction} {
		model, err := make("scaledown/extract-1")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := model.(*ExtractionModel); !ok || model.Provider() != "gateway" || model.ModelID() != "scaledown/extract-1" || model.SpecificationVersion() != "v4" {
			t.Fatalf("%s: %#v", name, model)
		}
	}
}

// TS: 'sends $name as ordered state parts' (3 rows)
func TestPortprovgExtractionStateParts(t *testing.T) {
	cases := []struct {
		name  string
		state []provider.DecisionStatePart
		want  string
	}{
		{"a text part", port2provgText("Inspect."), `[{"type":"text","text":"Inspect."}]`},
		{"a JSON part", []provider.DecisionStatePart{{Type: "json", Value: map[string]interface{}{"product": "vase", "history": []interface{}{"new"}}}}, `[{"type":"json","value":{"product":"vase","history":["new"]}}]`},
		{"mixed parts", []provider.DecisionStatePart{provider.DecisionTextPart("Inspect."), {Type: "json", Value: map[string]interface{}{"product": "vase"}}, provider.DecisionTextPart("Again.")},
			`[{"type":"text","text":"Inspect."},{"type":"json","value":{"product":"vase"}},{"type":"text","text":"Again."}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newPort2provgServer(t, port2provgResp("", ""))
			if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: tc.state, Fields: port2provgFields}); err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"stateParts": port2provgJSON(t, tc.want), "fields": port2provgFieldsWire}
			if got := s.bodyJSON(); !reflect.DeepEqual(got, want) {
				t.Fatalf("body = %v", got)
			}
		})
	}
}

func port2provgFilePart(data types.FileData) provider.DecisionStatePart {
	return provider.DecisionStatePart{Type: "file", File: &types.FileContent{MediaType: "application/pdf", Filename: "invoice.pdf", FileData: data}}
}

// TS: 'sends file bytes as base64 in ordered state parts'
func TestPortprovgExtractionFileBytesAsBase64(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{
		State:  []provider.DecisionStatePart{provider.DecisionTextPart("Read this invoice."), port2provgFilePart(types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}})},
		Fields: port2provgFields,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := port2provgJSON(t, `[{"type":"text","text":"Read this invoice."},{"type":"file","mediaType":"application/pdf","filename":"invoice.pdf","data":{"type":"data","data":"AQID"}}]`)
	if got := s.bodyJSON()["stateParts"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stateParts = %v", got)
	}
}

// TS: 'sends base64 file strings unchanged'
func TestPortprovgExtractionBase64StringsUnchanged(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	part := provider.DecisionStatePart{Type: "file", File: &types.FileContent{MediaType: "image/jpeg", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}}}
	if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: []provider.DecisionStatePart{part}, Fields: port2provgFields}); err != nil {
		t.Fatal(err)
	}
	want := port2provgJSON(t, `[{"type":"file","mediaType":"image/jpeg","data":{"type":"data","data":"AQID"}}]`)
	if got := s.bodyJSON()["stateParts"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stateParts = %v", got)
	}
}

// TS: 'sends file references unchanged'
func TestPortprovgExtractionFileReferencesUnchanged(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	part := provider.DecisionStatePart{Type: "file", File: &types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"gateway": "file_123"}}}}
	if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: []provider.DecisionStatePart{part}, Fields: port2provgFields}); err != nil {
		t.Fatal(err)
	}
	want := port2provgJSON(t, `[{"type":"file","mediaType":"application/pdf","data":{"type":"reference","reference":{"gateway":"file_123"}}}]`)
	if got := s.bodyJSON()["stateParts"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("stateParts = %v", got)
	}
}

// The decision model still rejects references (shared helper, TS
// toGatewayStateParts allowReferences defaults to false).
func TestPortprovgDecisionStillRejectsReferences(t *testing.T) {
	part := provider.DecisionStatePart{Type: "file", File: &types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"gateway": "f"}}}}
	_, err := encodeGatewayDecisionState([]provider.DecisionStatePart{part})
	var ufe *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &ufe) || ufe.Functionality != "Gateway decision file input: reference data" {
		t.Fatalf("err = %v", err)
	}
}

// TS: 'rejects $type file data before sending a Gateway request' (2 rows)
func TestPortprovgExtractionRejectsURLAndTextFileData(t *testing.T) {
	cases := map[string]types.FileData{
		"url":  {Type: types.FileDataTypeURL, URL: "https://example.com/a.pdf"},
		"text": {Type: types.FileDataTypeText},
	}
	for kind, data := range cases {
		t.Run(kind, func(t *testing.T) {
			s := newPort2provgServer(t, port2provgResp("", ""))
			_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{
				State: []provider.DecisionStatePart{port2provgFilePart(data)}, Fields: port2provgFields,
			})
			var ufe *providererrors.UnsupportedFunctionalityError
			if !errors.As(err, &ufe) || ufe.Functionality != "Gateway extraction file input: "+kind+" data" {
				t.Fatalf("err = %v", err)
			}
			if s.calls != 0 {
				t.Fatalf("calls = %d", s.calls)
			}
		})
	}
}

// TS: 'should post to the /extraction-model endpoint'
func TestPortprovgExtractionPostsToEndpoint(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields}); err != nil {
		t.Fatal(err)
	}
	if s.method != http.MethodPost || s.path != "/extraction-model" {
		t.Fatalf("%s %s", s.method, s.path)
	}
}

// TS: 'should send every call option in the request body'
func TestPortprovgExtractionSendsEveryCallOption(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	low := types.ReasoningLevel("low")
	_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{
		State: port2provgText("Invoice"), Fields: port2provgFields,
		ReferenceDate: "2026-11-11", MaxOutputTokens: port2provgIntPtr(512), Reasoning: &low,
		ProviderOptions: map[string]interface{}{
			"gateway":   map[string]interface{}{"models": []interface{}{"openai/gpt-5.4-nano"}, "user": "user-123"},
			"scaledown": map[string]interface{}{"threshold": 0.4},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"stateParts": port2provgJSON(t, `[{"type":"text","text":"Invoice"}]`), "fields": port2provgFieldsWire,
		"referenceDate": "2026-11-11", "maxOutputTokens": float64(512), "reasoning": "low",
		"providerOptions": port2provgJSON(t, `{"gateway":{"models":["openai/gpt-5.4-nano"],"user":"user-123"},"scaledown":{"threshold":0.4}}`),
	}
	if got := s.bodyJSON(); !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %v", got)
	}
	// TS key order: stateParts, fields, referenceDate, maxOutputTokens,
	// reasoning, providerOptions.
	last := -1
	for _, k := range []string{"stateParts", "fields", "referenceDate", "maxOutputTokens", "reasoning", "providerOptions"} {
		i := strings.Index(string(s.body), `"`+k+`"`)
		if i <= last {
			t.Fatalf("key %s out of order in %s", k, s.body)
		}
		last = i
	}
}

// TS: 'should omit options that are not set'
func TestPortprovgExtractionOmitsUnsetOptions(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields}); err != nil {
		t.Fatal(err)
	}
	got := s.bodyJSON()
	if len(got) != 2 || got["stateParts"] == nil || got["fields"] == nil {
		t.Fatalf("body = %v", got)
	}
}

// TS: 'should pass headers correctly'
func TestPortprovgExtractionHeaders(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	t.Setenv("VERCEL_REGION", "iad1")
	ctx := context.Background()
	if _, err := s.model().DoExtract(ctx, provider.ExtractionCallOptions{
		State: port2provgText("x"), Fields: port2provgFields, Headers: map[string]string{"Custom-Header": "test-value"},
	}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Authorization": "Bearer test-key", "Custom-Header": "test-value",
		"Ai-Extraction-Model-Specification-Version": "4", "Ai-Model-Id": "scaledown/extract-1", "Ai-O11y-Region": "iad1",
	}
	for k, v := range want {
		if got := s.headers.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func port2provgF(v float64) *float64 { return &v }

// TS: 'should return answers of every type'
func TestPortprovgExtractionAnswersOfEveryType(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp(`{
		"text":{"type":"text","value":"INV-2041","guess":"INV-2041","pNull":0.01,"confidence":0.98,"source":{"type":"text","part":0,"start":8,"end":16}},
		"number":{"type":"number","value":1250},
		"date":{"type":"date","value":"2026-03-03"},
		"time":{"type":"time","value":"17:30"},
		"datetime":{"type":"datetime","value":"2026-03-03T17:30:00Z"},
		"duration":{"type":"duration","value":120},
		"missing":{"type":"text","value":null,"guess":"PO-1","pNull":0.9},
		"fromFile":{"type":"text","value":"Acme","source":{"type":"file","part":1}},
		"texts":{"type":"list","value":["Ana","Bo"],"confidence":0.9,"items":[{"confidence":0.95,"source":{"type":"text","part":0,"start":1,"end":4}},{}]},
		"numbers":{"type":"list","value":[1,2.5]},
		"emptyList":{"type":"list","value":[]},
		"missingList":{"type":"list","value":null,"pNull":0.8}
	}`, ""))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	src := &provider.ExtractionSource{Type: "text", Part: 0, Start: 8, End: 16}
	want := map[string]provider.ExtractionAnswer{
		"text":     {Type: "text", Value: "INV-2041", Guess: "INV-2041", PNull: port2provgF(0.01), Confidence: port2provgF(0.98), Source: src},
		"number":   {Type: "number", Value: float64(1250)},
		"date":     {Type: "date", Value: "2026-03-03"},
		"time":     {Type: "time", Value: "17:30"},
		"datetime": {Type: "datetime", Value: "2026-03-03T17:30:00Z"},
		"duration": {Type: "duration", Value: float64(120)},
		"missing":  {Type: "text", Value: nil, Guess: "PO-1", PNull: port2provgF(0.9)},
		"fromFile": {Type: "text", Value: "Acme", Source: &provider.ExtractionSource{Type: "file", Part: 1}},
		"texts": {Type: "list", Value: []interface{}{"Ana", "Bo"}, Confidence: port2provgF(0.9), Items: []provider.ExtractionListItem{
			{Confidence: port2provgF(0.95), Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 1, End: 4}}, {}}},
		"numbers":     {Type: "list", Value: []interface{}{float64(1), 2.5}},
		"emptyList":   {Type: "list", Value: []interface{}{}},
		"missingList": {Type: "list", Value: nil, PNull: port2provgF(0.8)},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %#v", res.Answers)
	}
}

// TS: 'should return refusal answers'
func TestPortprovgExtractionRefusalAnswers(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp(`{"invoiceNumber":{"type":"refusal"},"total":{"type":"number","value":1250},"contacts":{"type":"refusal"}}`, ""))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "refusal"}, "total": {Type: "number", Value: float64(1250)}, "contacts": {Type: "refusal"},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %#v", res.Answers)
	}
}

// TS: 'should return usage, warnings, and provider metadata'
func TestPortprovgExtractionUsageWarningsMetadata(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", `"usage":{"inputTokens":120,"outputTokens":30},
		"warnings":[{"type":"unsupported","feature":"reasoning","details":"Ignored."},{"type":"other","message":"Something else."}],
		"providerMetadata":{"gateway":{"cost":"0.002"}}`))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 120 || *res.Usage.OutputTokens != 30 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	wantW := []types.Warning{{Type: "unsupported", Feature: "reasoning", Details: "Ignored."}, {Type: "other", Message: "Something else."}}
	if !reflect.DeepEqual(res.Warnings, wantW) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
	if !reflect.DeepEqual(res.ProviderMetadata, map[string]interface{}{"gateway": map[string]interface{}{"cost": "0.002"}}) {
		t.Fatalf("metadata = %v", res.ProviderMetadata)
	}
}

// TS: 'should omit usage and default warnings when absent'
func TestPortprovgExtractionOmitsUsageAndDefaultsWarnings(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage != nil || res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("usage = %+v, warnings = %#v", res.Usage, res.Warnings)
	}
}

// TS: 'should drop null optional fields from the response'
func TestPortprovgExtractionDropsNullOptionalFields(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp(`{
		"invoiceNumber":{"type":"text","value":null,"guess":null,"pNull":0.9,"confidence":null,"source":null},
		"total":{"type":"number","value":1250,"guess":null,"source":null},
		"contacts":{"type":"list","value":["Ana"],"pNull":null,"items":[{"confidence":null,"source":null}]}
	}`, `"model":null,"usage":{"inputTokens":120,"outputTokens":null},"warnings":null,"providerMetadata":null`))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "text", PNull: port2provgF(0.9)},
		"total":         {Type: "number", Value: float64(1250)},
		"contacts":      {Type: "list", Value: []interface{}{"Ana"}, Items: []provider.ExtractionListItem{{}}},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %#v", res.Answers)
	}
	if res.Usage == nil || *res.Usage.InputTokens != 120 || res.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Warnings) != 0 || res.ProviderMetadata != nil || res.Response.ModelID != "scaledown/extract-1" {
		t.Fatalf("%+v %v %q", res.Warnings, res.ProviderMetadata, res.Response.ModelID)
	}
	// The marshalled answer carries value:null and none of the dropped keys.
	b, _ := json.Marshal(res.Answers["invoiceNumber"])
	if string(b) != `{"type":"text","value":null,"pNull":0.9}` {
		t.Fatalf("marshalled = %s", b)
	}
}

// TS: 'should keep an unknown warning as an other warning'
func TestPortprovgExtractionKeepsUnknownWarningAsOther(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", `"warnings":[{"type":"other","message":"Known."},{"type":"future","detail":"New."},{"type":"unsupported","feature":"f","details":null}]`))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	want := []types.Warning{
		{Type: "other", Message: "Known."},
		{Type: "other", Message: `Unknown warning: {"type":"future","detail":"New."}`},
		{Type: "other", Message: `Unknown warning: {"type":"unsupported","feature":"f","details":null}`},
	}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// TS: 'should return response metadata'
func TestPortprovgExtractionResponseMetadata(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", `"model":"scaledown/extract-1-fallback"`))
	s.respHdr = map[string]string{"x-request-id": "req-1"}
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.ModelID != "scaledown/extract-1-fallback" || res.Response.Headers["X-Request-Id"] != "req-1" {
		t.Fatalf("response = %+v", res.Response)
	}
	want := map[string]interface{}{"answers": port2provgJSON(t, port2provgDummyAnswers), "model": "scaledown/extract-1-fallback"}
	if !reflect.DeepEqual(res.Response.Body, want) {
		t.Fatalf("body = %v", res.Response.Body)
	}
}

// TS: 'should attribute the response to the requested model when none is returned'
func TestPortprovgExtractionAttributesToRequestedModel(t *testing.T) {
	s := newPort2provgServer(t, port2provgResp("", ""))
	res, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.ModelID != "scaledown/extract-1" {
		t.Fatalf("modelID = %q", res.Response.ModelID)
	}
}

func port2provgErrBody(errJSON string) string { return `{"error":` + errJSON + `}` }

// TS: 'should throw ExtractionUnsupportedFieldTypeError for an unsupported field type'
func TestPortprovgExtractionUnsupportedFieldTypeError(t *testing.T) {
	s := newPort2provgServer(t, port2provgErrBody(`{"message":"Field \"total\" has type \"number\", which is not supported by provider \"upstream.extraction\" and model \"extract-1\".","type":"invalid_request_error","param":{"name":"AI_ExtractionUnsupportedFieldTypeError","fieldId":"total","fieldType":"number","statusCode":400}}`))
	s.status = 400
	_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	var ufe *providererrors.ExtractionUnsupportedFieldTypeError
	if !errors.As(err, &ufe) {
		t.Fatalf("err = %T %v", err, err)
	}
	if ufe.FieldID != "total" || ufe.FieldType != "number" || ufe.Provider != "gateway" || ufe.ModelID != "scaledown/extract-1" ||
		ufe.Error() != `Field "total" has type "number", which is not supported by provider "gateway" and model "scaledown/extract-1".` {
		t.Fatalf("err = %+v / %s", ufe, ufe.Error())
	}
}

// TS: 'should keep the list item type of an unsupported list field'
func TestPortprovgExtractionUnsupportedListFieldType(t *testing.T) {
	s := newPort2provgServer(t, port2provgErrBody(`{"message":"Unsupported field type.","type":"invalid_request_error","param":{"name":"AI_ExtractionUnsupportedFieldTypeError","fieldId":"contacts","fieldType":"list<number>"}}`))
	s.status = 400
	_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	var ufe *providererrors.ExtractionUnsupportedFieldTypeError
	if !errors.As(err, &ufe) || ufe.FieldID != "contacts" || ufe.FieldType != "list<number>" {
		t.Fatalf("err = %v", err)
	}
}

// TS: 'should throw GatewayInvalidRequestError on a 400 with $name' (3 rows)
func TestPortprovgExtractionInvalidRequestOnOtherBadRequests(t *testing.T) {
	cases := map[string]string{
		"no param":            `{"message":"Invalid fields format","type":"invalid_request_error"}`,
		"another error class": `{"message":"Invalid fields format","type":"invalid_request_error","param":{"name":"AI_InvalidArgumentError","fieldId":"a","fieldType":"b"}}`,
		"no field":            `{"message":"Invalid fields format","type":"invalid_request_error","param":{"name":"AI_ExtractionUnsupportedFieldTypeError"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPort2provgServer(t, port2provgErrBody(body))
			s.status = 400
			_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
			var ire *gatewayerrors.GatewayInvalidRequestError
			if !errors.As(err, &ire) || ire.GetStatusCode() != 400 || ire.Error() != "Invalid fields format" {
				t.Fatalf("err = %T %v", err, err)
			}
		})
	}
}

// TS: 'should throw GatewayInternalServerError on 500'
func TestPortprovgExtractionInternalServerError(t *testing.T) {
	s := newPort2provgServer(t, port2provgErrBody(`{"message":"Internal server error","type":"internal_server_error"}`))
	s.status = 500
	_, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields})
	var ise *gatewayerrors.GatewayInternalServerError
	if !errors.As(err, &ise) || ise.GetStatusCode() != 500 {
		t.Fatalf("err = %T %v", err, err)
	}
}

// A malformed answer fails the successful response (zod discriminated union).
func TestPortprovgExtractionRejectsMalformedAnswer(t *testing.T) {
	s := newPort2provgServer(t, `{"answers":{"a":{"type":"text","value":5}}}`)
	if _, err := s.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields}); err == nil {
		t.Fatal("expected a validation error")
	}
	s2 := newPort2provgServer(t, `{"answers":{"a":{"type":"list","value":["a",1]}}}`)
	if _, err := s2.model().DoExtract(context.Background(), provider.ExtractionCallOptions{State: port2provgText("x"), Fields: port2provgFields}); err == nil {
		t.Fatal("mixed list must fail")
	}
}
