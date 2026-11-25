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
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// Ports TS packages/scaledown/src/scaledown-extraction-model.test.ts
// (c6db718176, 4b02148c83; final state at a457990a33).

type extractionCall struct {
	path    string
	headers http.Header
	raw     string
	body    map[string]interface{}
}

type extractionFake struct {
	mu    sync.Mutex
	calls []extractionCall
	body  interface{}
	raw   string
	code  int
}

func newExtractionFake(t *testing.T, body interface{}, status int) (*extractionFake, string) {
	t.Helper()
	f := &extractionFake{body: body, code: status}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		f.mu.Lock()
		f.calls = append(f.calls, extractionCall{path: r.URL.Path, headers: r.Header.Clone(), raw: string(raw), body: b})
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

func extractionContent() map[string]interface{} {
	entities := []interface{}{}
	for _, typ := range []string{"vendor", "contacts", "missing", "empty"} {
		entities = append(entities, map[string]interface{}{"text": "", "type": typ, "confidence": 1.0})
	}
	entities = append(entities,
		map[string]interface{}{"text": "Acme Corp", "type": "vendor", "confidence": 0.97, "start": 0.0, "end": 9.0},
		map[string]interface{}{"text": "Ana", "type": "contacts", "confidence": 0.9, "start": 21.0, "end": 24.0},
		map[string]interface{}{"text": "Bo", "type": "contacts", "confidence": 0.85, "start": 26.0, "end": 28.0},
	)
	return map[string]interface{}{"entities": entities, "structured_result": nil, "input_tokens": 42.0, "ocr_text": nil}
}

func extractionResponse(content interface{}) map[string]interface{} {
	text, ok := content.(string)
	if !ok {
		raw, _ := json.Marshal(content)
		text = string(raw)
	}
	return map[string]interface{}{
		"id": "extract-123", "created": 1700000000.0, "model": "extract-1",
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": text}}},
		"usage":   map[string]interface{}{"prompt_tokens": 42.0, "completion_tokens": 12.0, "total_tokens": 54.0},
	}
}

const extractionStateText = "Acme Corp. Contacts: Ana, Bo."

func extractionOptions() provider.ExtractionCallOptions {
	return provider.ExtractionCallOptions{
		State: []provider.DecisionStatePart{provider.DecisionTextPart(extractionStateText)},
		Fields: map[string]provider.ExtractionField{
			"vendor":   {Type: "text", Instructions: "Company name"},
			"contacts": {Type: "list", Items: "text", Instructions: map[string]interface{}{"find": "Contact names"}},
			"missing":  {Type: "text"},
			"empty":    {Type: "list", Items: "text"},
		},
		FieldOrder: []string{"vendor", "contacts", "missing", "empty"},
	}
}

func newExtractionModel(t *testing.T, body interface{}) (provider.ExtractionModel, *extractionFake) {
	t.Helper()
	f, url := newExtractionFake(t, body, 200)
	m, err := New(Config{APIKey: "test-key", BaseURL: url}).ExtractionModel("extract-1")
	if err != nil {
		t.Fatal(err)
	}
	return m, f
}

func ptr[T any](v T) *T { return &v }

func TestPort2toolsExtractionMapsTextAndListsMissingValuesConfidenceSourcesAndUsage(t *testing.T) {
	m, f := newExtractionModel(t, extractionResponse(extractionContent()))
	opts := extractionOptions()
	opts.ProviderOptions = map[string]interface{}{"scaledown": map[string]interface{}{"threshold": 0.5}}
	res, err := m.DoExtract(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.path != "/v1/chat/completions" || c.headers.Get("x-api-key") != "test-key" {
		t.Fatalf("path=%s key=%s", c.path, c.headers.Get("x-api-key"))
	}
	want := map[string]interface{}{
		"model":    "extract-1",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": extractionStateText}},
		"response_format": map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name": "extraction",
				"schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"vendor":   map[string]interface{}{"type": "string", "description": "Company name", "top_n": 1.0},
						"contacts": map[string]interface{}{"type": "string", "description": `{"find":"Contact names"}`, "top_n": 0.0},
						"missing":  map[string]interface{}{"type": "string", "top_n": 1.0},
						"empty":    map[string]interface{}{"type": "string", "top_n": 0.0},
					},
				},
			},
		},
		"threshold":     0.5,
		"context_chars": 0.0,
	}
	if !reflect.DeepEqual(c.body, want) {
		t.Fatalf("body = %v\nwant %v", c.body, want)
	}
	c9, c85, c97 := 0.9, 0.85, 0.97
	wantAnswers := map[string]provider.ExtractionAnswer{
		"vendor": {Type: "text", Value: "Acme Corp", Confidence: &c97, Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 0, End: 9}},
		"contacts": {Type: "list", Value: []interface{}{"Ana", "Bo"}, Items: []provider.ExtractionListItem{
			{Confidence: &c9, Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 21, End: 24}},
			{Confidence: &c85, Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 26, End: 28}},
		}},
		"missing": {Type: "text"},
		"empty":   {Type: "list", Value: []interface{}{}, Items: []provider.ExtractionListItem{}},
	}
	if !reflect.DeepEqual(res.Answers, wantAnswers) {
		t.Fatalf("answers = %+v", res.Answers)
	}
	if *res.Usage.InputTokens != 42 || *res.Usage.OutputTokens != 12 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.Response.ID != "extract-123" || !res.Response.Timestamp.Equal(time.UnixMilli(1700000000000)) ||
		res.Response.ModelID != "extract-1" || !reflect.DeepEqual(res.Response.Body, extractionResponse(extractionContent())) ||
		res.Response.Headers["x-trace"] != "trace-1" {
		t.Fatalf("response = %+v", res.Response)
	}
	if len(res.Warnings) != 0 || res.ProviderMetadata != nil {
		t.Fatalf("warnings = %v metadata = %v", res.Warnings, res.ProviderMetadata)
	}
}

func TestPort2toolsExtractionPreservesFieldIDsWithSpacesCamelCaseUnicodeAndDistinctCasing(t *testing.T) {
	entities := []interface{}{
		map[string]interface{}{"type": "invoice number", "text": "INV-2041", "confidence": 0.9},
		map[string]interface{}{"type": "dueDate", "text": "2026-12-01", "confidence": 0.9},
		map[string]interface{}{"type": "客户名称", "text": "Zoë Martin", "confidence": 0.9},
		map[string]interface{}{"type": "Vendor", "text": "Acme Corp", "confidence": 0.9},
	}
	m, f := newExtractionModel(t, extractionResponse(map[string]interface{}{"entities": entities}))
	order := []string{"invoice number", "dueDate", "客户名称", "Vendor", "vendor"}
	fields := map[string]provider.ExtractionField{}
	for _, id := range order {
		fields[id] = provider.ExtractionField{Type: "text"}
	}
	res, err := m.DoExtract(context.Background(), provider.ExtractionCallOptions{
		State:      []provider.DecisionStatePart{provider.DecisionTextPart("Invoice INV-2041 issued by Acme Corp to Zoë Martin. Payment is due on 2026-12-01.")},
		Fields:     fields,
		FieldOrder: order,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Property order is the field insertion order.
	var keys []string
	dec := json.NewDecoder(strings.NewReader(f.calls[0].raw))
	_ = dec.Decode(&struct{}{})
	var body struct {
		ResponseFormat struct {
			JSONSchema struct {
				Schema struct {
					Properties json.RawMessage `json:"properties"`
				} `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal([]byte(f.calls[0].raw), &body); err != nil {
		t.Fatal(err)
	}
	pd := json.NewDecoder(strings.NewReader(string(body.ResponseFormat.JSONSchema.Schema.Properties)))
	_, _ = pd.Token()
	for pd.More() {
		k, _ := pd.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		_ = pd.Decode(&skip)
	}
	if !reflect.DeepEqual(keys, order) {
		t.Fatalf("property keys = %v", keys)
	}
	c9 := 0.9
	want := map[string]provider.ExtractionAnswer{
		"invoice number": {Type: "text", Value: "INV-2041", Confidence: &c9},
		"dueDate":        {Type: "text", Value: "2026-12-01", Confidence: &c9},
		"客户名称":           {Type: "text", Value: "Zoë Martin", Confidence: &c9},
		"Vendor":         {Type: "text", Value: "Acme Corp", Confidence: &c9},
		"vendor":         {Type: "text"},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		t.Fatalf("answers = %+v", res.Answers)
	}
}

func TestPort2toolsExtractionConvertsUTF8OffsetsAcrossJSONAndUnicodeTextPartsWithoutInventingSources(t *testing.T) {
	entities := []interface{}{
		map[string]interface{}{"type": "names", "text": "Acmé", "confidence": 0.9, "start": 19.0, "end": 24.0},
		map[string]interface{}{"type": "names", "text": "Zoë", "confidence": 0.8, "start": 29.0, "end": 33.0},
		map[string]interface{}{"type": "note", "text": "é", "confidence": 0.9, "start": 9.0, "end": 11.0},
		map[string]interface{}{"type": "invalid", "text": "Acmé", "confidence": 0.9, "start": 20.0, "end": 25.0},
	}
	m, f := newExtractionModel(t, extractionResponse(map[string]interface{}{"entities": entities}))
	res, err := m.DoExtract(context.Background(), provider.ExtractionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionJSONPart(map[string]interface{}{"note": "é"}),
			provider.DecisionTextPart("🙂 Acmé and Zoë."),
		},
		Fields: map[string]provider.ExtractionField{
			"names":   {Type: "list", Items: "text"},
			"note":    {Type: "text"},
			"invalid": {Type: "text"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := f.calls[0].body["messages"].([]interface{})
	if got := msgs[0].(map[string]interface{})["content"]; got != "{\"note\":\"é\"}\n🙂 Acmé and Zoë." {
		t.Fatalf("content = %q", got)
	}
	c8, c9 := 0.8, 0.9
	wantItems := []provider.ExtractionListItem{
		{Confidence: ptr(0.9), Source: &provider.ExtractionSource{Type: "text", Part: 1, Start: 3, End: 7}},
		{Confidence: &c8, Source: &provider.ExtractionSource{Type: "text", Part: 1, Start: 12, End: 15}},
	}
	if !reflect.DeepEqual(res.Answers["names"].Items, wantItems) {
		t.Fatalf("names items = %+v", res.Answers["names"].Items)
	}
	if !reflect.DeepEqual(res.Answers["note"], provider.ExtractionAnswer{Type: "text", Value: "é", Confidence: &c9}) {
		t.Fatalf("note = %+v", res.Answers["note"])
	}
	if !reflect.DeepEqual(res.Answers["invalid"], provider.ExtractionAnswer{Type: "text", Value: "Acmé", Confidence: &c9}) {
		t.Fatalf("invalid = %+v", res.Answers["invalid"])
	}
}

// A span that ends inside a four-byte character must not produce a source
// (the boundary check of convertScaleDownExtractionSource).
func TestPort2toolsExtractionSourceRejectsOffsetsInsideACharacter(t *testing.T) {
	state := []provider.DecisionStatePart{provider.DecisionTextPart("🙂 ab")}
	cases := map[string]struct {
		entity scaledownExtractionEntity
		want   *provider.ExtractionSource
	}{
		"after emoji":               {scaledownExtractionEntity{Text: "ab", Start: ptr(5.0), End: ptr(7.0)}, &provider.ExtractionSource{Type: "text", Part: 0, Start: 3, End: 5}},
		"starts inside emoji":       {scaledownExtractionEntity{Text: "\xf0\x9f", Start: ptr(1.0), End: ptr(3.0)}, nil},
		"starts inside (3 of 4)":    {scaledownExtractionEntity{Text: "\U0001f642", Start: ptr(0.0), End: ptr(3.0)}, nil},
		"end before start":          {scaledownExtractionEntity{Text: "", Start: ptr(3.0), End: ptr(2.0)}, nil},
		"misaligned but equal text": {scaledownExtractionEntity{Text: "é", Start: ptr(1.0), End: ptr(3.0)}, nil},
		"missing offsets":           {scaledownExtractionEntity{Text: "ab"}, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := convertExtractionSource(tc.entity, state, nil); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("source = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPort2toolsExtractionSendsABase64DocumentAndReturnsFileSourcesAndOCRText(t *testing.T) {
	content := extractionContent()
	content["ocr_text"] = extractionStateText
	resp := extractionResponse(content)
	resp["usage"] = nil
	m, f := newExtractionModel(t, resp)
	opts := extractionOptions()
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}})}
	res, err := m.DoExtract(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	body := f.calls[0].body
	msgs := body["messages"].([]interface{})
	if msgs[0].(map[string]interface{})["content"] != "Extract the requested fields from the document." ||
		body["document"] != "AQID" || body["document_mime_type"] != "application/pdf" {
		t.Fatalf("body = %v", body)
	}
	if !reflect.DeepEqual(res.Answers["vendor"].Source, &provider.ExtractionSource{Type: "file", Part: 0}) {
		t.Fatalf("vendor source = %+v", res.Answers["vendor"].Source)
	}
	if !reflect.DeepEqual(res.ProviderMetadata, map[string]interface{}{"scaledown": map[string]interface{}{"ocrText": extractionStateText}}) {
		t.Fatalf("metadata = %v", res.ProviderMetadata)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 42 || res.Usage.OutputTokens != nil {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

func TestPort2toolsExtractionRejectsUnsupportedFieldsAndFilesBeforeSendingARequest(t *testing.T) {
	m, f := newExtractionModel(t, extractionResponse(extractionContent()))
	for name, field := range map[string]provider.ExtractionField{
		"number":      {Type: "number"},
		"date":        {Type: "date"},
		"list number": {Type: "list", Items: "number"},
		"list date":   {Type: "list", Items: "date"},
	} {
		t.Run(name, func(t *testing.T) {
			opts := extractionOptions()
			opts.Fields = map[string]provider.ExtractionField{"value": field}
			opts.FieldOrder = nil
			_, err := m.DoExtract(context.Background(), opts)
			var unsupported *providererrors.ExtractionUnsupportedFieldTypeError
			if !errors.As(err, &unsupported) || unsupported.FieldID != "value" || unsupported.Provider != "scaledown.extraction" || unsupported.ModelID != "extract-1" {
				t.Fatalf("err = %#v", err)
			}
			if field.Type == "list" && unsupported.FieldType != "list<"+field.Items+">" {
				t.Fatalf("field type = %q", unsupported.FieldType)
			}
		})
	}
	opts := extractionOptions()
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})}
	_, err := m.DoExtract(context.Background(), opts)
	var unsupportedFile *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupportedFile) {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("HTTP request sent")
	}
}

type port2toolsCtxKey struct{}

func TestPort2toolsExtractionPassesCustomConfigurationAndWarnsAboutUnsupportedSettings(t *testing.T) {
	var gotURL string
	var gotHeaders http.Header
	var gotCtx interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL, gotHeaders, gotCtx = r.URL.String(), r.Header.Clone(), r.Context().Value(port2toolsCtxKey{})
		raw, _ := json.Marshal(extractionResponse(extractionContent()))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	m, _ := New(Config{APIKey: "custom-key", BaseURL: "https://custom.example/", Headers: map[string]string{"shared": "provider"}, HTTPClient: client}).ExtractionModel("extract")
	opts := extractionOptions()
	opts.Headers = map[string]string{"shared": "call"}
	opts.ReferenceDate = "2026-11-12"
	high := types.ReasoningHigh
	opts.Reasoning = &high
	opts.MaxOutputTokens = ptr(10)
	res, err := m.DoExtract(context.WithValue(context.Background(), port2toolsCtxKey{}, "ctx"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://custom.example/v1/chat/completions" || gotHeaders.Get("x-api-key") != "custom-key" ||
		gotHeaders.Get("shared") != "call" || gotCtx != "ctx" ||
		!strings.Contains(gotHeaders.Get("user-agent"), "ai-sdk-scaledown/"+version.Version) {
		t.Fatalf("url=%s headers=%v ctx=%v", gotURL, gotHeaders, gotCtx)
	}
	want := []types.Warning{
		{Type: "unsupported", Feature: "referenceDate"},
		{Type: "unsupported", Feature: "maxOutputTokens"},
		{Type: "unsupported", Feature: "reasoning"},
	}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

func TestPort2toolsExtractionRestoresWorkflowConfigurationAndEnvironmentAuthentication(t *testing.T) {
	t.Setenv("SCALEDOWN_API_KEY", "env-key")
	f, url := newExtractionFake(t, extractionResponse(extractionContent()), 200)
	m, _ := New(Config{APIKey: "test-key", BaseURL: url}).ExtractionModel("extract-1")
	serialized := m.(*ExtractionModel).Serialize()
	if serialized.Provider != "scaledown.extraction" || serialized.ModelID != "extract-1" {
		t.Fatalf("serialized = %+v", serialized)
	}
	for k := range serialized.Config {
		if strings.EqualFold(k, "apikey") {
			t.Fatalf("API key serialized: %v", serialized.Config)
		}
	}
	restored, err := DeserializeExtractionModel(serialized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.DoExtract(context.Background(), extractionOptions()); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].headers.Get("x-api-key") != "env-key" {
		t.Fatalf("restored call = %+v", f.calls[0])
	}
	if !reflect.DeepEqual(restored.SupportedFieldTypes(), []string{"text", "list"}) {
		t.Fatalf("types = %v", restored.SupportedFieldTypes())
	}
}

func TestPort2toolsExtractionRejectsMalformedInnerJSONAndSurfacesAPIErrors(t *testing.T) {
	for _, content := range []string{"not JSON", `{"entities":"invalid"}`, `{"entities":[{"text":"a","type":"t","confidence":1,"start":-1}]}`, `{"entities":[],"__proto__":{}}`} {
		m, _ := newExtractionModel(t, extractionResponse(content))
		_, err := m.DoExtract(context.Background(), extractionOptions())
		var invalid *providererrors.InvalidResponseDataError
		if !errors.As(err, &invalid) || invalid.Data != content || invalid.Message != "ScaleDown did not return valid extraction content." {
			t.Fatalf("content %q: err = %#v", content, err)
		}
	}
	f, url := newExtractionFake(t, nil, 402)
	f.raw = `{"detail":"Insufficient credits"}`
	m, _ := New(Config{APIKey: "k", BaseURL: url}).ExtractionModel("extract-1")
	_, err := m.DoExtract(context.Background(), extractionOptions())
	var apiErr *providererrors.ProviderError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 402 || apiErr.Message != "Insufficient credits" || apiErr.Provider != "scaledown.extraction" {
		t.Fatalf("err = %#v", err)
	}
}

func TestPort2toolsExtractionRejectsMalformedSuccessfulResponse(t *testing.T) {
	for name, body := range map[string]interface{}{
		"no choices":   map[string]interface{}{"choices": []interface{}{}},
		"no content":   map[string]interface{}{"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{}}}},
		"choices type": map[string]interface{}{"choices": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := newExtractionModel(t, body)
			_, err := m.DoExtract(context.Background(), extractionOptions())
			var apiErr *providererrors.ProviderError
			if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "Invalid JSON response") {
				t.Fatalf("err = %#v", err)
			}
		})
	}
}

func TestPort2toolsExtractionValidatesThresholdAndSkipsUnknownOptions(t *testing.T) {
	m, f := newExtractionModel(t, extractionResponse(extractionContent()))
	for _, bad := range []interface{}{1.5, -0.1, "high"} {
		opts := extractionOptions()
		opts.ProviderOptions = map[string]interface{}{"scaledown": map[string]interface{}{"threshold": bad}}
		_, err := m.DoExtract(context.Background(), opts)
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) {
			t.Fatalf("threshold %v: err = %v", bad, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatal("request sent for invalid options")
	}
	opts := extractionOptions()
	opts.ProviderOptions = map[string]interface{}{"scaledown": ExtractionModelOptions{Threshold: ptr(0.25)}}
	if _, err := m.DoExtract(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].body["threshold"] != 0.25 {
		t.Fatalf("threshold = %v", f.calls[0].body["threshold"])
	}
	opts.ProviderOptions = map[string]interface{}{"scaledown": map[string]interface{}{"other": true}}
	if _, err := m.DoExtract(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.calls[1].body["threshold"]; ok {
		t.Fatal("threshold sent when unset")
	}
}

func TestPort2toolsExtractionUnsupportedFileWinsOverMissingAPIKey(t *testing.T) {
	t.Setenv("SCALEDOWN_API_KEY", "")
	m, _ := New(Config{}).ExtractionModel("extract-1")
	opts := extractionOptions()
	opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})}
	_, err := m.DoExtract(context.Background(), opts)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v", err)
	}
	_, err = m.DoExtract(context.Background(), extractionOptions())
	if err == nil || !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestPort2toolsScaleDownModelIDsAndProviderFactory(t *testing.T) {
	if !reflect.DeepEqual(ExtractionModelIDs(), []string{"extract", "extract-1"}) {
		t.Fatalf("ids = %v", ExtractionModelIDs())
	}
	var p provider.Provider = New(Config{})
	if _, ok := p.(provider.ExtractionModelProvider); !ok {
		t.Fatal("provider does not implement ExtractionModelProvider")
	}
	m, err := New(Config{}).ExtractionModel("custom-id")
	if err != nil || m.ModelID() != "custom-id" || m.Provider() != "scaledown.extraction" || m.SpecificationVersion() != "v4" {
		t.Fatalf("model = %+v err = %v", m, err)
	}
}

// The span starts inside the first "é" and ends inside the second, so the
// text between the decoded offsets equals the entity text; only the byte
// boundary check rejects it.
func TestPort2toolsExtractionSourceRejectsMisalignedSpanWithEqualText(t *testing.T) {
	state := []provider.DecisionStatePart{provider.DecisionTextPart("éé")}
	entity := scaledownExtractionEntity{Text: "é", Start: ptr(1.0), End: ptr(3.0)}
	if got := convertExtractionSource(entity, state, nil); got != nil {
		t.Fatalf("source = %+v, want nil", got)
	}
	aligned := scaledownExtractionEntity{Text: "é", Start: ptr(2.0), End: ptr(4.0)}
	if got := convertExtractionSource(aligned, state, nil); !reflect.DeepEqual(got, &provider.ExtractionSource{Type: "text", Part: 0, Start: 1, End: 2}) {
		t.Fatalf("aligned source = %+v", got)
	}
}

func TestPort2toolsExtractionTextFieldKeepsTheFirstOfEqualConfidenceMatches(t *testing.T) {
	entities := []interface{}{
		map[string]interface{}{"type": "vendor", "text": "First", "confidence": 0.9},
		map[string]interface{}{"type": "vendor", "text": "Second", "confidence": 0.9},
		map[string]interface{}{"type": "vendor", "text": "Third", "confidence": 0.8},
	}
	m, _ := newExtractionModel(t, extractionResponse(map[string]interface{}{"entities": entities}))
	opts := extractionOptions()
	opts.Fields = map[string]provider.ExtractionField{"vendor": {Type: "text"}}
	opts.FieldOrder = nil
	res, err := m.DoExtract(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Answers["vendor"].Value != "First" {
		t.Fatalf("vendor = %+v", res.Answers["vendor"])
	}
}

func TestPort2toolsExtractionFileSourceNeedsOCRTextContainingTheValue(t *testing.T) {
	for name, ocr := range map[string]interface{}{"absent": nil, "other text": "something else"} {
		t.Run(name, func(t *testing.T) {
			content := extractionContent()
			content["ocr_text"] = ocr
			m, _ := newExtractionModel(t, extractionResponse(content))
			opts := extractionOptions()
			opts.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}})}
			res, err := m.DoExtract(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.Answers["vendor"].Source != nil {
				t.Fatalf("vendor source = %+v, want none", res.Answers["vendor"].Source)
			}
		})
	}
}
