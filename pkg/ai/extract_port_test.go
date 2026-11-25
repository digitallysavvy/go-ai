package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Go twins of TS packages/ai/src/extract/extract.test.ts (cb4fa52341,
// d99098a979, 15bfd6b262, 79d2004947, 797dc0254e).

const port2genExtractState = "Invoice INV-2041, total due $1,250.00 by March 3, 2027 at 17:30. Call Ana or Bo within 2 hours."

func port2genP(v float64) *float64 { return &v }
func port2genI(v int) *int         { return &v }
func port2genB(v bool) *bool       { return &v }

func port2genExtractFields() map[string]provider.ExtractionField {
	return map[string]provider.ExtractionField{
		"invoiceNumber": {Type: "text", Instructions: "The invoice number"},
		"total":         {Type: "number", Instructions: "Total due", Unit: "USD", Decimals: port2genI(2)},
		"dueDate":       {Type: "date", Instructions: "Due date"},
		"dueTime":       {Type: "time", Instructions: "Due time"},
		"dueAt":         {Type: "datetime", Instructions: "Due date and time"},
		"callWithin":    {Type: "duration", Instructions: "Call window", Unit: "hours"},
		"contacts":      {Type: "list", Items: "text", Instructions: "Contact names"},
	}
}

func port2genExtractAnswers() map[string]provider.ExtractionAnswer {
	return map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "text", Value: "INV-2041", Confidence: port2genP(0.98), Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 8, End: 16}},
		"total":         {Type: "number", Value: 1250.0, PNull: port2genP(0.01)},
		"dueDate":       {Type: "date", Value: "2027-03-03"},
		"dueTime":       {Type: "time", Value: "17:30"},
		"dueAt":         {Type: "datetime", Value: "2027-03-03T17:30"},
		"callWithin":    {Type: "duration", Value: 2.0},
		"contacts": {Type: "list", Value: []interface{}{"Ana", "Bo"}, Items: []provider.ExtractionListItem{
			{Confidence: port2genP(0.9), Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 70, End: 73}},
			{Confidence: port2genP(0.8)},
		}},
	}
}

func port2genExtractModel(result *provider.ExtractionResult) *testutil.MockExtractionModel {
	if result == nil {
		result = &provider.ExtractionResult{Answers: port2genExtractAnswers()}
	}
	return &testutil.MockExtractionModel{
		DoExtractFunc: func(context.Context, provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
			return result, nil
		},
	}
}

func port2genExtractModelAnswers(answers map[string]provider.ExtractionAnswer) *testutil.MockExtractionModel {
	return port2genExtractModel(&provider.ExtractionResult{Answers: answers})
}

func port2genExtract(t *testing.T, opts ExtractOptions) (*ExtractResult, error) {
	t.Helper()
	return ExperimentalExtract(context.Background(), opts)
}

func TestPort2genExtractPreservesMetadata(t *testing.T) {
	var logged []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) { logged = append(logged, o) })
	t.Cleanup(func() { SetLogWarnings(nil) })

	warning := types.Warning{Type: "other", Message: "Provider note"}
	timestamp := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	model := port2genExtractModel(&provider.ExtractionResult{
		Answers:          port2genExtractAnswers(),
		Warnings:         []types.Warning{warning},
		Usage:            &provider.ExtractionUsage{InputTokens: port2genI(40), OutputTokens: port2genI(12)},
		ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"passes": 1}},
		Response: &provider.ExtractionResponseInfo{
			ID: "response", Timestamp: timestamp, ModelID: "actual-model",
			Headers: map[string]string{"x-request-id": "request"}, Body: map[string]interface{}{"raw": true},
		},
	})
	low := types.ReasoningLow
	fields := port2genExtractFields()
	result, err := port2genExtract(t, ExtractOptions{
		Model: model, State: port2genExtractState, Fields: fields, ReferenceDate: "2026-11-11",
		MaxOutputTokens: port2genI(200), Reasoning: &low,
		Headers:         map[string]string{"custom": "value"},
		ProviderOptions: map[string]interface{}{"test": map[string]interface{}{"option": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.ExtractCalls) != 1 {
		t.Fatalf("calls = %d", len(model.ExtractCalls))
	}
	call := model.ExtractCalls[0]
	if !reflect.DeepEqual(call.State, []provider.DecisionStatePart{provider.DecisionTextPart(port2genExtractState)}) {
		t.Fatalf("state = %#v", call.State)
	}
	if !reflect.DeepEqual(call.Fields, fields) || call.ReferenceDate != "2026-11-11" ||
		*call.MaxOutputTokens != 200 || *call.Reasoning != types.ReasoningLow ||
		!reflect.DeepEqual(call.ProviderOptions, map[string]interface{}{"test": map[string]interface{}{"option": true}}) {
		t.Fatalf("call = %+v", call)
	}
	if call.Headers["custom"] != "value" || !strings.Contains(call.Headers["User-Agent"]+call.Headers["user-agent"], "ai/") {
		t.Fatalf("headers = %v", call.Headers)
	}
	if port2genExtractState[8:16] != "INV-2041" || port2genExtractState[70:73] != "Ana" {
		t.Fatal("fixture offsets are wrong")
	}
	if !reflect.DeepEqual(result.Answers, port2genExtractAnswers()) {
		t.Fatalf("answers = %#v", result.Answers)
	}
	if !reflect.DeepEqual(result.ProviderMetadata, map[string]interface{}{"test": map[string]interface{}{"passes": 1}}) {
		t.Fatalf("metadata = %v", result.ProviderMetadata)
	}
	if *result.Usage.InputTokens != 40 || *result.Usage.OutputTokens != 12 || *result.Usage.TotalTokens != 52 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	want := ExtractResponse{ID: "response", Timestamp: timestamp, ModelID: "actual-model", Headers: map[string]string{"x-request-id": "request"}, Body: map[string]interface{}{"raw": true}}
	if !reflect.DeepEqual(result.Response, want) {
		t.Fatalf("response = %+v", result.Response)
	}
	if !reflect.DeepEqual(result.Warnings, []types.Warning{warning}) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	if len(logged) != 1 || logged[0].Provider != model.Provider() || logged[0].Model != model.ModelID() ||
		!reflect.DeepEqual(logged[0].Warnings, []types.Warning{warning}) {
		t.Fatalf("logged = %+v", logged)
	}
}

func TestPort2genExtractNormalizesEmptyAnswers(t *testing.T) {
	for _, value := range []string{"", " INV-2041 ", "   "} {
		providerAnswers := port2genExtractAnswers()
		providerAnswers["invoiceNumber"] = provider.ExtractionAnswer{
			Type: "text", Value: value, Confidence: port2genP(0.98),
			Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 8, End: 16},
		}
		providerAnswers["dueDate"] = provider.ExtractionAnswer{Type: "date", Value: ""}
		providerAnswers["contacts"] = provider.ExtractionAnswer{
			Type: "list", Value: []interface{}{}, Items: []provider.ExtractionListItem{},
			Confidence: port2genP(0.8), PNull: port2genP(0.9),
		}
		result, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModelAnswers(providerAnswers), State: port2genExtractState, Fields: port2genExtractFields()})
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		inv := result.Answers["invoiceNumber"]
		if value == "" {
			if inv.Value != nil {
				t.Fatalf("%q: value = %#v, want nil", value, inv.Value)
			}
		} else if inv.Value != value {
			t.Fatalf("%q: value = %#v", value, inv.Value)
		}
		if inv.Confidence == nil || *inv.Confidence != 0.98 || inv.Source == nil {
			t.Fatalf("%q: metadata lost: %+v", value, inv)
		}
		if got := result.Answers["dueDate"]; got.Type != "date" || got.Value != nil {
			t.Fatalf("dueDate = %+v", got)
		}
		contacts := result.Answers["contacts"]
		if contacts.Type != "list" || contacts.Value != nil || contacts.Items != nil ||
			*contacts.Confidence != 0.8 || *contacts.PNull != 0.9 {
			t.Fatalf("contacts = %+v", contacts)
		}
		// The provider's own answer is never modified.
		orig := providerAnswers["contacts"]
		if orig.Value == nil || orig.Items == nil {
			t.Fatalf("provider answer was modified: %+v", orig)
		}
		if got := providerAnswers["dueDate"].Value; got != "" {
			t.Fatalf("provider dueDate was modified: %#v", got)
		}
	}
}

func TestPort2genExtractAcceptsNullValues(t *testing.T) {
	empty := map[string]provider.ExtractionAnswer{}
	for id, f := range port2genExtractFields() {
		empty[id] = provider.ExtractionAnswer{Type: f.Type}
	}
	result, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModelAnswers(empty), State: "", Fields: port2genExtractFields()})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Answers, empty) {
		t.Fatalf("answers = %#v", result.Answers)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil || result.Usage.TotalTokens != nil {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.Response.Timestamp.IsZero() || result.Response.ModelID != "mock-model-id" || result.Response.ID != "" {
		t.Fatalf("response = %+v", result.Response)
	}
}

func TestPort2genExtractRejectsUnsupportedTypesBeforeIO(t *testing.T) {
	model := port2genExtractModel(nil)
	model.FieldTypes = []string{"text"}
	_, err := port2genExtract(t, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields()})
	var target *providererrors.ExtractionUnsupportedFieldTypeError
	if !errors.As(err, &target) || target.FieldID != "callWithin" {
		t.Fatalf("err = %v", err)
	}
	if len(model.ExtractCalls) != 0 {
		t.Fatal("model was called")
	}
	// Insertion order decides the first unsupported field (TS Object.entries).
	_, err = port2genExtract(t, ExtractOptions{
		Model: model, State: port2genExtractState, Fields: port2genExtractFields(),
		FieldOrder: []string{"invoiceNumber", "total", "contacts"},
	})
	if !errors.As(err, &target) || target.FieldID != "total" || target.FieldType != "number" ||
		target.Provider != model.Provider() || target.ModelID != model.ModelID() {
		t.Fatalf("err = %+v", target)
	}
}

func TestPort2genExtractRejectsRefusedFieldsWithoutRetrying(t *testing.T) {
	for _, refused := range [][]string{{"total"}, {"total", "contacts"}} {
		answers := port2genExtractAnswers()
		for _, id := range refused {
			answers[id] = provider.ExtractionAnswer{Type: "refusal"}
		}
		model := port2genExtractModelAnswers(answers)
		_, err := port2genExtract(t, ExtractOptions{
			Model: model, State: port2genExtractState, Fields: port2genExtractFields(),
			FieldOrder: []string{"invoiceNumber", "total", "dueDate", "dueTime", "dueAt", "callWithin", "contacts"},
		})
		var target *providererrors.ExtractionRefusalError
		if !errors.As(err, &target) {
			t.Fatalf("%v: err = %v", refused, err)
		}
		if !reflect.DeepEqual(target.FieldIDs, refused) || target.Provider != model.Provider() || target.ModelID != model.ModelID() {
			t.Fatalf("%v: error = %+v", refused, target)
		}
		if len(model.ExtractCalls) != 1 {
			t.Fatalf("%v: calls = %d", refused, len(model.ExtractCalls))
		}
	}
}

type port2genExtractModelV99 struct{ *testutil.MockExtractionModel }

func (port2genExtractModelV99) SpecificationVersion() string { return "v99" }

func TestPort2genExtractRejectsUnsupportedModelVersion(t *testing.T) {
	inner := port2genExtractModel(nil)
	_, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModelV99{inner}, State: port2genExtractState, Fields: port2genExtractFields()})
	var target *UnsupportedModelVersionError
	if !errors.As(err, &target) || target.Version != "v99" {
		t.Fatalf("err = %v", err)
	}
	if len(inner.ExtractCalls) != 0 {
		t.Fatal("model was called")
	}
}

func TestPort2genExtractInputValidation(t *testing.T) {
	text := map[string]provider.ExtractionField{"name": {Type: "text"}}
	cases := []struct {
		name          string
		fields        map[string]provider.ExtractionField
		referenceDate string
		state         interface{}
	}{
		{"an empty field map", map[string]provider.ExtractionField{}, "", port2genExtractState},
		{"decimals above 6", map[string]provider.ExtractionField{"total": {Type: "number", Decimals: port2genI(7)}}, "", port2genExtractState},
		{"negative decimals", map[string]provider.ExtractionField{"total": {Type: "duration", Decimals: port2genI(-1)}}, "", port2genExtractState},
		{"an unknown duration unit", map[string]provider.ExtractionField{"wait": {Type: "duration", Unit: "fortnights"}}, "", port2genExtractState},
		{"an unknown list item type", map[string]provider.ExtractionField{"names": {Type: "list", Items: "time"}}, "", port2genExtractState},
		{"non-finite instructions", map[string]provider.ExtractionField{"total": {Type: "number", Instructions: map[string]interface{}{"max": math.Inf(1)}}}, "", port2genExtractState},
		{"a malformed reference date", text, "11/11/2026", port2genExtractState},
		{"an impossible reference date", text, "2026-02-30", port2genExtractState},
		{"malformed state parts", text, "", []provider.DecisionStatePart{{Type: "image"}}},
		{"non-finite state JSON", text, "", map[string]interface{}{"x": math.NaN()}},
	}
	for _, tc := range cases {
		model := port2genExtractModel(nil)
		_, err := port2genExtract(t, ExtractOptions{Model: model, State: tc.state, Fields: tc.fields, ReferenceDate: tc.referenceDate})
		var target *providererrors.InvalidArgumentError
		if !errors.As(err, &target) {
			t.Fatalf("%s: err = %v, want InvalidArgumentError", tc.name, err)
		}
		if len(model.ExtractCalls) != 0 {
			t.Fatalf("%s: model was called", tc.name)
		}
	}
}

func TestPort2genExtractInvalidMaxOutputTokens(t *testing.T) {
	model := port2genExtractModel(nil)
	_, err := port2genExtract(t, ExtractOptions{Model: model, State: "x", Fields: port2genExtractFields(), MaxOutputTokens: port2genI(0)})
	var target *providererrors.InvalidArgumentError
	if !errors.As(err, &target) || len(model.ExtractCalls) != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestPort2genExtractResponseValidation(t *testing.T) {
	state := port2genExtractState
	cases := []struct {
		name     string
		override map[string]provider.ExtractionAnswer
		remove   string
	}{
		{name: "a missing answer", remove: "total"},
		{name: "an extra answer", override: map[string]provider.ExtractionAnswer{"extra": {Type: "text", Value: "x"}}},
		{name: "a wrong answer type", override: map[string]provider.ExtractionAnswer{"total": {Type: "text", Value: "1250"}}},
		{name: "a string number", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: "1250"}}},
		{name: "a non-finite number", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: math.Inf(1)}}},
		{name: "too many decimals", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: 1250.005}}},
		{name: "a malformed date", override: map[string]provider.ExtractionAnswer{"dueDate": {Type: "date", Value: "03/03/2027"}}},
		{name: "an impossible date", override: map[string]provider.ExtractionAnswer{"dueDate": {Type: "date", Value: "2027-02-29"}}},
		{name: "an impossible time", override: map[string]provider.ExtractionAnswer{"dueTime": {Type: "time", Value: "24:00"}}},
		{name: "a datetime without a time", override: map[string]provider.ExtractionAnswer{"dueAt": {Type: "datetime", Value: "2027-03-03"}}},
		{name: "a datetime with an impossible date", override: map[string]provider.ExtractionAnswer{"dueAt": {Type: "datetime", Value: "2027-13-03T17:30Z"}}},
		{name: "a negative duration", override: map[string]provider.ExtractionAnswer{"callWithin": {Type: "duration", Value: -2.0}}},
		{name: "a list with a wrong item type", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Value: []interface{}{"Ana", 2.0}}}},
		{name: "a list that is not an array", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Value: "Ana, Bo"}}},
		{name: "list item entries that do not match the values", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Value: []interface{}{"Ana"}, Items: []provider.ExtractionListItem{{}, {}}}}},
		{name: "list item entries without values", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Items: []provider.ExtractionListItem{}}}},
		{name: "an item confidence above 1", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Value: []interface{}{"Ana"}, Items: []provider.ExtractionListItem{{Confidence: port2genP(2)}}}}},
		{name: "an item source outside the state", override: map[string]provider.ExtractionAnswer{"contacts": {Type: "list", Value: []interface{}{"Ana"}, Items: []provider.ExtractionListItem{{Source: &provider.ExtractionSource{Type: "text", Part: 3, Start: 0, End: 1}}}}}},
		{name: "a pNull above 1", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: 1250.0, PNull: port2genP(1.5)}}},
		{name: "a NaN pNull", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: 1250.0, PNull: port2genP(math.NaN())}}},
		{name: "a negative confidence", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Value: 1250.0, Confidence: port2genP(-0.1)}}},
		{name: "an invalid guess", override: map[string]provider.ExtractionAnswer{"total": {Type: "number", Guess: "x"}}},
		{name: "a source past the end of the text", override: map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 8, End: len(state) + 1}}}},
		{name: "an empty source span", override: map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 8, End: 8}}}},
		{name: "a source part outside the state", override: map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "text", Part: 1, Start: 0, End: 1}}}},
		{name: "a file source on a text part", override: map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "file", Part: 0}}}},
		{name: "an unknown source type", override: map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "audio", Part: 0}}}},
	}
	for _, tc := range cases {
		merged := port2genExtractAnswers()
		for id, a := range tc.override {
			merged[id] = a
		}
		if tc.remove != "" {
			delete(merged, tc.remove)
		}
		_, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModelAnswers(merged), State: state, Fields: port2genExtractFields()})
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Fatalf("%s: err = %v, want InvalidResponseDataError", tc.name, err)
		}
	}
}

func TestPort2genExtractEnforcesIntegerFieldsAndListItemNumberRules(t *testing.T) {
	fields := map[string]provider.ExtractionField{
		"count":  {Type: "number", Integer: port2genB(true)},
		"scores": {Type: "list", Items: "number", Decimals: port2genI(1)},
	}
	run := func(count float64, scores []float64) error {
		_, err := port2genExtract(t, ExtractOptions{
			Model: port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{
				"count": {Type: "number", Value: count}, "scores": {Type: "list", Value: scores},
			}),
			State: port2genExtractState, Fields: fields,
		})
		return err
	}
	if err := run(3, []float64{1.5, 2}); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{"fractional integer": run(3.5, []float64{1.5}), "too many list decimals": run(3, []float64{1.55})} {
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestPort2genExtractAcceptsDatetimesWithSecondsFractionsAndOffsets(t *testing.T) {
	for _, value := range []string{"2027-03-03T17:30:00Z", "2027-03-03T17:30:00.123+01:00", "2027-03-03T17:30-08:00"} {
		_, err := port2genExtract(t, ExtractOptions{
			Model:  port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"dueAt": {Type: "datetime", Value: value}}),
			State:  port2genExtractState,
			Fields: map[string]provider.ExtractionField{"dueAt": port2genExtractFields()["dueAt"]},
		})
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
}

func TestPort2genExtractAcceptsSourcesInLaterTextPartsAndFileParts(t *testing.T) {
	model := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "text", Value: "INV-2041", Source: &provider.ExtractionSource{Type: "text", Part: 2, Start: 0, End: 8}},
		"total":         {Type: "number", Value: 1250.0, Source: &provider.ExtractionSource{Type: "file", Part: 1}},
	})
	fields := port2genExtractFields()
	_, err := port2genExtract(t, ExtractOptions{
		Model: model,
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("Header"),
			provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{0x25, 0x50, 0x44, 0x46}}}),
			provider.DecisionTextPart("INV-2041"),
		},
		Fields: map[string]provider.ExtractionField{"invoiceNumber": fields["invoiceNumber"], "total": fields["total"]},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPort2genExtractSourceOffsetsAreUTF16(t *testing.T) {
	// "😀" is two UTF-16 code units, so a span ending at 4 fits "😀ab" (TS
	// string length) even though the text has three runes.
	field := map[string]provider.ExtractionField{"v": {Type: "text"}}
	ok := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"v": {Type: "text", Value: "ab", Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 2, End: 4}}})
	if _, err := port2genExtract(t, ExtractOptions{Model: ok, State: "😀ab", Fields: field}); err != nil {
		t.Fatal(err)
	}
	bad := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"v": {Type: "text", Value: "ab", Source: &provider.ExtractionSource{Type: "text", Part: 0, Start: 2, End: 5}}})
	if _, err := port2genExtract(t, ExtractOptions{Model: bad, State: "😀ab", Fields: field}); err == nil {
		t.Fatal("expected a source past the end")
	}
}

func TestPort2genExtractHandlesFieldIDsThatMatchObjectPrototypeProperties(t *testing.T) {
	fields := map[string]provider.ExtractionField{"__proto__": {Type: "text"}, "constructor": {Type: "number"}}
	answers := map[string]provider.ExtractionAnswer{"__proto__": {Type: "text", Value: "x"}, "constructor": {Type: "number", Value: 1.0}}
	result, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModelAnswers(answers), State: port2genExtractState, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Answers) != 2 || result.Answers["__proto__"].Value != "x" || result.Answers["constructor"].Value != 1.0 {
		t.Fatalf("answers = %#v", result.Answers)
	}
}

func port2genRetryable(status int) error {
	return &providererrors.ProviderError{Provider: "mock", StatusCode: status, Message: "boom", ResponseHeaders: map[string]string{"retry-after-ms": "0"}}
}

func TestPort2genExtractRetriesTransientErrorsWithConfiguredLimit(t *testing.T) {
	calls := 0
	model := &testutil.MockExtractionModel{DoExtractFunc: func(context.Context, provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
		calls++
		if calls == 1 {
			return nil, port2genRetryable(429)
		}
		return &provider.ExtractionResult{Answers: port2genExtractAnswers()}, nil
	}}
	if _, err := port2genExtract(t, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields(), MaxRetries: port2genI(1)}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestPort2genExtractHonorsMaxRetriesZero(t *testing.T) {
	boom := port2genRetryable(529)
	calls := 0
	model := &testutil.MockExtractionModel{DoExtractFunc: func(context.Context, provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
		calls++
		return nil, boom
	}}
	_, err := port2genExtract(t, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields(), MaxRetries: port2genI(0)})
	if err != boom || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}

func TestPort2genExtractRejectsNegativeMaxRetries(t *testing.T) {
	_, err := port2genExtract(t, ExtractOptions{Model: port2genExtractModel(nil), State: "x", Fields: port2genExtractFields(), MaxRetries: port2genI(-1)})
	var target *providererrors.InvalidArgumentError
	if !errors.As(err, &target) {
		t.Fatalf("err = %v", err)
	}
}

func TestPort2genExtractDoesNotCallProviderWhenAlreadyAborted(t *testing.T) {
	model := port2genExtractModel(nil)
	reason := errors.New("Cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	_, err := ExperimentalExtract(ctx, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields()})
	if err != reason {
		t.Fatalf("err = %v, want the abort reason", err)
	}
	if len(model.ExtractCalls) != 0 {
		t.Fatal("model was called")
	}
}

func TestPort2genExtractDoesNotReturnResultAfterCancellationDuringCall(t *testing.T) {
	reason := errors.New("Cancelled")
	ctx, cancel := context.WithCancelCause(context.Background())
	model := &testutil.MockExtractionModel{DoExtractFunc: func(context.Context, provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
		cancel(reason)
		return &provider.ExtractionResult{Answers: port2genExtractAnswers()}, nil
	}}
	result, err := ExperimentalExtract(ctx, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields()})
	if err != reason || result != nil {
		t.Fatalf("result = %v, err = %v", result, err)
	}
}

func TestPort2genExtractSendsJSONStateAsJSONPart(t *testing.T) {
	model := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041"}})
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, State: map[string]interface{}{"invoice": "INV-2041"},
		Fields: map[string]provider.ExtractionField{"invoiceNumber": port2genExtractFields()["invoiceNumber"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"invoice": "INV-2041"})}
	if !reflect.DeepEqual(model.ExtractCalls[0].State, want) {
		t.Fatalf("state = %#v", model.ExtractCalls[0].State)
	}
}

func port2genURLFile(data types.FileContent) []provider.DecisionStatePart {
	data.MediaType = "application/pdf"
	return []provider.DecisionStatePart{provider.DecisionFilePart(data)}
}

func TestPort2genExtractRejectsRemoteFileStateWithoutDownloadHook(t *testing.T) {
	const url = "https://attacker.example/invoice.pdf"
	forms := map[string]types.FileContent{
		"tagged url": {FileData: types.FileData{Type: types.FileDataTypeURL, URL: url}},
		"legacy url": {URL: url},
	}
	for name, file := range forms {
		model := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041"}})
		_, err := port2genExtract(t, ExtractOptions{
			Model:  model,
			Fields: map[string]provider.ExtractionField{"invoiceNumber": port2genExtractFields()["invoiceNumber"]},
			State:  port2genURLFile(file),
		})
		var target *providererrors.DownloadError
		if !errors.As(err, &target) || !strings.Contains(err.Error(), "experimental_download") {
			t.Fatalf("%s: err = %v, want DownloadError naming experimental_download", name, err)
		}
		if len(model.ExtractCalls) != 0 {
			t.Fatalf("%s: model was called", name)
		}
	}
}

func TestPort2genExtractStopsBeforeProviderIOWhenFileDownloadFails(t *testing.T) {
	model := port2genExtractModel(nil)
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, Fields: port2genExtractFields(),
		// Loopback is blocked by the download guard: no network access.
		ExperimentalDownload: CreateDownload(nil),
		State:                port2genURLFile(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "http://127.0.0.1/invoice.pdf"}}),
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want the download guard's loopback error", err)
	}
	if len(model.ExtractCalls) != 0 {
		t.Fatal("model was called")
	}
}

func TestPort2genExtractUsesConfiguredDownloadAndForwardsCancellation(t *testing.T) {
	model := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041"}})
	bytes := []byte("Invoice INV-2041")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gotCtx context.Context
	var requests []DownloadRequest
	_, err := ExperimentalExtract(ctx, ExtractOptions{
		Model:  model,
		Fields: map[string]provider.ExtractionField{"invoiceNumber": port2genExtractFields()["invoiceNumber"]},
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart("Read the invoice."),
			port2genURLFile(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/invoice.pdf"}})[0],
		},
		ExperimentalDownload: func(c context.Context, reqs []DownloadRequest) ([]*DownloadResult, error) {
			gotCtx, requests = c, reqs
			return []*DownloadResult{{Data: bytes, MediaType: "application/pdf"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].URL != "https://example.com/invoice.pdf" {
		t.Fatalf("requests = %+v", requests)
	}
	cancel()
	if gotCtx.Err() == nil {
		t.Fatal("the download did not get the caller's cancellation")
	}
	got := model.ExtractCalls[0].State.([]provider.DecisionStatePart)
	if len(got) != 2 || got[0].Text != "Read the invoice." {
		t.Fatalf("state = %#v", got)
	}
	mt, data, err := got[1].InlineData()
	if err != nil || mt != "application/pdf" || !reflect.DeepEqual(data, bytes) {
		t.Fatalf("file = %q %v %v", mt, data, err)
	}
}

func TestPort2genExtractStopsBeforeProviderIOWhenDownloadIsBlocked(t *testing.T) {
	model := port2genExtractModel(nil)
	reason := errors.New("Blocked private DNS address")
	calls := 0
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, Fields: port2genExtractFields(),
		State: port2genURLFile(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://attacker.example/invoice.pdf"}}),
		ExperimentalDownload: func(context.Context, []DownloadRequest) ([]*DownloadResult, error) {
			calls++
			return nil, &providererrors.DownloadError{URL: "https://attacker.example/invoice.pdf", Cause: reason}
		},
	})
	var target *providererrors.DownloadError
	if !errors.As(err, &target) || !errors.Is(err, reason) {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 || len(model.ExtractCalls) != 0 {
		t.Fatalf("download calls = %d, model calls = %d", calls, len(model.ExtractCalls))
	}
}

// TS 797dc0254e caller side: the model sees part provider options and the
// caller's state is untouched; only the telemetry copies drop them.
func TestPort2genExtractModelSeesPartProviderOptionsAndCallerStateIsUntouched(t *testing.T) {
	providerOptions := map[string]interface{}{"anthropic": map[string]interface{}{"title": "private-title"}}
	file := types.FileContent{
		MediaType:       "application/pdf",
		FileData:        types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/invoice.pdf?X-Amz-Signature=secret"},
		ProviderOptions: providerOptions,
	}
	state := []provider.DecisionStatePart{provider.DecisionFilePart(file)}
	model := port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041"}})
	mock := &port2genExtractIntegration{}
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, State: state,
		Fields: map[string]provider.ExtractionField{"invoiceNumber": port2genExtractFields()["invoiceNumber"]},
		ExperimentalDownload: func(context.Context, []DownloadRequest) ([]*DownloadResult, error) {
			return []*DownloadResult{{Data: []byte{1, 2, 3}, MediaType: "application/pdf"}}, nil
		},
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), RecordInputs: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{mock}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := model.ExtractCalls[0].State.([]provider.DecisionStatePart)
	if !reflect.DeepEqual(seen[0].File.ProviderOptions, providerOptions) {
		t.Fatalf("model state lost part provider options: %+v", seen[0].File)
	}
	if state[0].File.FileData.Type != types.FileDataTypeURL || !reflect.DeepEqual(state[0].File.ProviderOptions, providerOptions) {
		t.Fatalf("caller state was modified: %+v", state[0].File)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.states) != 4 {
		t.Fatalf("telemetry events = %d, want 4", len(mock.states))
	}
	for i, s := range mock.states {
		parts := s.([]provider.DecisionStatePart)
		if parts[0].File.ProviderOptions != nil {
			t.Fatalf("event %d kept part provider options", i)
		}
		if strings.Contains(port2genJSON(t, parts), "secret") || strings.Contains(port2genJSON(t, parts), "private") {
			t.Fatalf("event %d leaked: %s", i, port2genJSON(t, parts))
		}
	}
}

// ---- telemetry ----

// port2genExtractIntegration records the extract events in order and
// whether the generic start/end handlers were called.
type port2genExtractIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu sync.Mutex

	events   []string
	genericC int
	starts   []telemetry.ExtractStartEvent
	ends     []telemetry.ExtractEndEvent
	mcStarts []telemetry.ExtractionModelCallStartEvent
	mcEnds   []telemetry.ExtractionModelCallEndEvent
	errs     []telemetry.TelemetryErrorEvent
	states   []interface{}
}

func (m *port2genExtractIntegration) OnStart(ctx context.Context, _ telemetry.TelemetryStartEvent) context.Context {
	m.mu.Lock()
	m.genericC++
	m.mu.Unlock()
	return ctx
}
func (m *port2genExtractIntegration) OnEnd(context.Context, telemetry.TelemetryFinishEvent) {
	m.mu.Lock()
	m.genericC++
	m.mu.Unlock()
}
func (m *port2genExtractIntegration) OnExtractStart(ctx context.Context, e telemetry.ExtractStartEvent) context.Context {
	m.mu.Lock()
	m.events = append(m.events, "experimental_onExtractStart")
	m.starts = append(m.starts, e)
	m.states = append(m.states, e.State)
	m.mu.Unlock()
	return ctx
}
func (m *port2genExtractIntegration) OnExtractEnd(_ context.Context, e telemetry.ExtractEndEvent) {
	m.mu.Lock()
	m.events = append(m.events, "experimental_onExtractEnd")
	m.ends = append(m.ends, e)
	m.states = append(m.states, e.State)
	m.mu.Unlock()
}
func (m *port2genExtractIntegration) OnExtractionModelCallStart(_ context.Context, e telemetry.ExtractionModelCallStartEvent) {
	m.mu.Lock()
	m.events = append(m.events, "experimental_onExtractionModelCallStart")
	m.mcStarts = append(m.mcStarts, e)
	m.states = append(m.states, e.State)
	m.mu.Unlock()
}
func (m *port2genExtractIntegration) OnExtractionModelCallEnd(_ context.Context, e telemetry.ExtractionModelCallEndEvent) {
	m.mu.Lock()
	m.events = append(m.events, "experimental_onExtractionModelCallEnd")
	m.mcEnds = append(m.mcEnds, e)
	m.states = append(m.states, e.State)
	m.mu.Unlock()
}
func (m *port2genExtractIntegration) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	m.mu.Lock()
	m.events = append(m.events, "onError")
	m.errs = append(m.errs, e)
	m.mu.Unlock()
}

func port2genTelemetry(mock *port2genExtractIntegration, mut func(*telemetry.Options)) *telemetry.Options {
	o := &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{mock}}
	if mut != nil {
		mut(o)
	}
	return o
}

func TestPort2genExtractTelemetryEmitsOperationAndModelCallLifecycleEvents(t *testing.T) {
	mock := &port2genExtractIntegration{}
	answers := port2genExtractAnswers()
	answers["contacts"] = provider.ExtractionAnswer{Type: "list", Value: []interface{}{}}
	model := port2genExtractModel(&provider.ExtractionResult{
		Answers: answers,
		Usage:   &provider.ExtractionUsage{InputTokens: port2genI(40), OutputTokens: port2genI(12)},
	})
	var callbackStart ExtractOnStartEvent
	var callbackEnd ExtractOnEndEvent
	fields := port2genExtractFields()
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, State: port2genExtractState, Fields: fields, ReferenceDate: "2026-11-18",
		ExperimentalTelemetry: port2genTelemetry(mock, func(o *telemetry.Options) {
			o.FunctionID = "extract-test"
			o.RecordInputs = telemetry.Bool(false)
			o.RecordOutputs = telemetry.Bool(true)
			o.IncludeRuntimeContext = map[string]bool{}
		}),
		RuntimeContext: map[string]interface{}{"requestId": "request-1", "secret": "hidden"},
		OnStart: func(e ExtractOnStartEvent) {
			mock.mu.Lock()
			mock.events = append(mock.events, "extractOnStart")
			mock.mu.Unlock()
			callbackStart = e
		},
		OnEnd: func(e ExtractOnEndEvent) {
			mock.mu.Lock()
			mock.events = append(mock.events, "extractOnEnd")
			mock.mu.Unlock()
			callbackEnd = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.genericC != 0 {
		t.Fatalf("generic start/end handlers were called %d times", mock.genericC)
	}
	want := []string{"extractOnStart", "experimental_onExtractStart", "experimental_onExtractionModelCallStart", "experimental_onExtractionModelCallEnd", "extractOnEnd", "experimental_onExtractEnd"}
	if !reflect.DeepEqual(mock.events, want) {
		t.Fatalf("events = %v", mock.events)
	}
	start := mock.starts[0]
	if start.OperationID != "ai.extract" || start.ModelProvider != "mock-provider" || start.ModelID != "mock-model-id" ||
		start.MaxRetries != 2 || start.CallID == "" || start.AIVersion == "" || start.Settings.FunctionID != "extract-test" ||
		len(start.RuntimeContext) != 0 {
		t.Fatalf("start = %+v", start)
	}
	mcStart, mcEnd, end := mock.mcStarts[0], mock.mcEnds[0], mock.ends[0]
	if mcStart.CallID != start.CallID || mcStart.OperationID != "ai.extract.doExtract" || mcEnd.CallID != start.CallID || end.CallID != start.CallID {
		t.Fatalf("call ids / operation ids differ: %+v %+v %+v", mcStart, mcEnd, end)
	}
	wantAnswers := port2genExtractAnswers()
	wantAnswers["contacts"] = provider.ExtractionAnswer{Type: "list"}
	if !reflect.DeepEqual(mcEnd.Answers, wantAnswers) || !reflect.DeepEqual(end.Answers, wantAnswers) {
		t.Fatalf("answers = %#v / %#v", mcEnd.Answers, end.Answers)
	}
	if *mcEnd.Usage.InputTokens != 40 || *mcEnd.Usage.OutputTokens != 12 || *end.Usage.TotalTokens != 52 {
		t.Fatalf("usage = %+v %+v", mcEnd.Usage, end.Usage)
	}
	// recordInputs false: no inputs reach integrations.
	for i, s := range []interface{}{start.State, mcStart.State, mcEnd.State, end.State} {
		wantState := interface{}("")
		if i == 1 || i == 2 {
			wantState = []provider.DecisionStatePart{}
		}
		if !reflect.DeepEqual(s, wantState) {
			t.Fatalf("event %d state = %#v, want %#v", i, s, wantState)
		}
	}
	for i, f := range []interface{}{start.Fields, mcStart.Fields, mcEnd.Fields, end.Fields} {
		if m, ok := f.(map[string]interface{}); !ok || len(m) != 0 {
			t.Fatalf("event %d fields = %#v", i, f)
		}
	}
	if start.ReferenceDate != "" || mcStart.ReferenceDate != "" || end.ReferenceDate != "" {
		t.Fatal("reference date reached integrations")
	}
	// The lifecycle callbacks get the unredacted events.
	if callbackStart.OperationID != "ai.extract" || callbackStart.State != port2genExtractState || !reflect.DeepEqual(callbackStart.Fields, fields) ||
		callbackStart.ReferenceDate != "2026-11-18" ||
		callbackStart.RuntimeContext.(map[string]interface{})["secret"] != "hidden" {
		t.Fatalf("callbackStart = %+v", callbackStart)
	}
	if callbackEnd.OperationID != "ai.extract" || callbackEnd.RuntimeContext.(map[string]interface{})["secret"] != "hidden" ||
		!reflect.DeepEqual(callbackEnd.Answers, wantAnswers) || *callbackEnd.Usage.TotalTokens != 52 {
		t.Fatalf("callbackEnd = %+v", callbackEnd)
	}
}

func TestPort2genExtractTelemetryHidesAnswersWhenOutputsAreNotRecorded(t *testing.T) {
	mock := &port2genExtractIntegration{}
	var end ExtractOnEndEvent
	fields := port2genExtractFields()
	_, err := port2genExtract(t, ExtractOptions{
		Model: port2genExtractModel(nil), State: port2genExtractState, Fields: fields,
		ExperimentalTelemetry: port2genTelemetry(mock, func(o *telemetry.Options) { o.RecordOutputs = telemetry.Bool(false) }),
		OnEnd:                 func(e ExtractOnEndEvent) { end = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for name, answers := range map[string]interface{}{"model call end": mock.mcEnds[0].Answers, "extract end": mock.ends[0].Answers} {
		if m, ok := answers.(map[string]provider.ExtractionAnswer); ok && len(m) != 0 {
			t.Fatalf("%s answers = %v", name, answers)
		} else if m, ok := answers.(map[string]interface{}); ok && len(m) != 0 {
			t.Fatalf("%s answers = %v", name, answers)
		}
	}
	if mf, ok := mock.ends[0].Fields.(map[string]interface{}); !ok || len(mf) != len(fields) {
		t.Fatalf("fields = %#v", mock.ends[0].Fields)
	}
	if !reflect.DeepEqual(end.Answers, port2genExtractAnswers()) {
		t.Fatalf("callback answers = %#v", end.Answers)
	}
}

func TestPort2genExtractTelemetryKeepsHeadersAndProviderOptionsOutOfIntegrations(t *testing.T) {
	for _, recordInputs := range []bool{true, false} {
		mock := &port2genExtractIntegration{}
		headers := map[string]string{"authorization": "Bearer secret-token"}
		providerOptions := map[string]interface{}{"gateway": map[string]interface{}{"byok": map[string]interface{}{"openai": []interface{}{map[string]interface{}{"apiKey": "secret-key"}}}}}
		var start ExtractOnStartEvent
		_, err := port2genExtract(t, ExtractOptions{
			Model: port2genExtractModel(nil), State: port2genExtractState, Fields: port2genExtractFields(),
			Headers: headers, ProviderOptions: providerOptions,
			ExperimentalTelemetry: port2genTelemetry(mock, func(o *telemetry.Options) { o.RecordInputs = telemetry.Bool(recordInputs) }),
			OnStart:               func(e ExtractOnStartEvent) { start = e },
		})
		if err != nil {
			t.Fatal(err)
		}
		mock.mu.Lock()
		events := []telemetry.ExtractStartEvent{mock.starts[0], mock.ends[0].ExtractStartEvent}
		mock.mu.Unlock()
		for i, e := range events {
			if e.Headers != nil || len(e.ProviderOptions) != 0 {
				t.Fatalf("recordInputs=%v event %d: headers=%v options=%v", recordInputs, i, e.Headers, e.ProviderOptions)
			}
			if strings.Contains(port2genJSON(t, []interface{}{e.State, e.Fields, e.Headers, e.ProviderOptions}), "secret") {
				t.Fatalf("recordInputs=%v event %d leaked: %s", recordInputs, i, port2genJSON(t, []interface{}{e.State, e.Fields, e.Headers, e.ProviderOptions}))
			}
		}
		if !reflect.DeepEqual(start.Headers, headers) || !reflect.DeepEqual(start.ProviderOptions, providerOptions) {
			t.Fatalf("callback start = %+v", start)
		}
	}
}

func TestPort2genExtractTelemetryRedactsFileURLsAndReferences(t *testing.T) {
	mock := &port2genExtractIntegration{}
	const url = "https://files.example.com/invoice.pdf?X-Amz-Signature=secret"
	state := []provider.DecisionStatePart{
		provider.DecisionTextPart("Invoice"),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeURL, URL: url}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", URL: url}),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: map[string]string{"openai": "secret-reference"}}}),
	}
	var start ExtractOnStartEvent
	_, err := port2genExtract(t, ExtractOptions{
		Model:  port2genExtractModelAnswers(map[string]provider.ExtractionAnswer{"invoiceNumber": {Type: "text", Value: "INV-2041"}}),
		State:  state,
		Fields: map[string]provider.ExtractionField{"invoiceNumber": port2genExtractFields()["invoiceNumber"]},
		ExperimentalDownload: func(_ context.Context, reqs []DownloadRequest) ([]*DownloadResult, error) {
			out := make([]*DownloadResult, len(reqs))
			for i := range reqs {
				out[i] = &DownloadResult{Data: []byte{1, 2, 3}, MediaType: "application/pdf"}
			}
			return out, nil
		},
		ExperimentalTelemetry: port2genTelemetry(mock, nil),
		OnStart:               func(e ExtractOnStartEvent) { start = e },
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	got := mock.starts[0].State.([]provider.DecisionStatePart)
	if strings.Contains(port2genJSON(t, []interface{}{mock.starts[0].State, mock.starts[0].Fields, mock.starts[0].Headers, mock.starts[0].ProviderOptions}), "secret") {
		t.Fatalf("event leaked: %s", port2genJSON(t, []interface{}{mock.starts[0].State, mock.starts[0].Fields, mock.starts[0].Headers, mock.starts[0].ProviderOptions}))
	}
	if len(got) != 4 || got[0].Text != "Invoice" {
		t.Fatalf("state = %#v", got)
	}
	for i := 1; i < 4; i++ {
		if got[i].File == nil || got[i].File.FileData.DataString != "[redacted]" {
			t.Fatalf("part %d not redacted: %+v", i, got[i].File)
		}
	}
	// The callback gets the caller's state unchanged.
	if cs, ok := start.State.([]provider.DecisionStatePart); !ok || cs[1].File.FileData.URL != url {
		t.Fatalf("callback state = %#v", start.State)
	}
}

func TestPort2genExtractTelemetryRedactsFailedDownloadURL(t *testing.T) {
	mock := &port2genExtractIntegration{}
	const url = "https://files.example.com/invoice.pdf?X-Amz-Signature=secret"
	model := port2genExtractModel(nil)
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, Fields: port2genExtractFields(),
		State: port2genURLFile(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: url}}),
		ExperimentalDownload: func(context.Context, []DownloadRequest) ([]*DownloadResult, error) {
			return nil, &providererrors.DownloadError{URL: url, Message: "Download of " + url + " exceeded maximum size of 10 bytes."}
		},
		ExperimentalTelemetry: port2genTelemetry(mock, nil),
	})
	var raw *providererrors.DownloadError
	if !errors.As(err, &raw) || raw.URL != url {
		t.Fatalf("caller error = %v", err)
	}
	if len(model.ExtractCalls) != 0 {
		t.Fatal("model was called")
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.errs) != 1 {
		t.Fatalf("errors = %d", len(mock.errs))
	}
	var delivered *providererrors.DownloadError
	if !errors.As(mock.errs[0].Error, &delivered) || strings.Contains(delivered.URL+" "+delivered.Error(), "secret") {
		t.Fatalf("delivered = %v", mock.errs[0].Error)
	}
}

func TestPort2genExtractTelemetryIncludesOnlySelectedRuntimeContextFields(t *testing.T) {
	mock := &port2genExtractIntegration{}
	_, err := port2genExtract(t, ExtractOptions{
		Model: port2genExtractModel(nil), State: port2genExtractState, Fields: port2genExtractFields(),
		ExperimentalTelemetry: port2genTelemetry(mock, func(o *telemetry.Options) { o.IncludeRuntimeContext = map[string]bool{"requestId": true} }),
		RuntimeContext:        map[string]interface{}{"requestId": "request-1", "secret": "hidden"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	for name, rc := range map[string]map[string]interface{}{"start": mock.starts[0].RuntimeContext, "end": mock.ends[0].RuntimeContext} {
		if !reflect.DeepEqual(rc, map[string]interface{}{"requestId": "request-1"}) {
			t.Fatalf("%s runtime context = %v", name, rc)
		}
	}
}

func TestPort2genExtractTelemetryEmitsErrorEventWhenExtractionFails(t *testing.T) {
	mock := &port2genExtractIntegration{}
	boom := errors.New("extraction failed")
	model := &testutil.MockExtractionModel{DoExtractFunc: func(context.Context, provider.ExtractionCallOptions) (*provider.ExtractionResult, error) {
		return nil, boom
	}}
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, State: port2genExtractState, Fields: port2genExtractFields(), MaxRetries: port2genI(0),
		ExperimentalTelemetry: port2genTelemetry(mock, nil),
	})
	if err != boom {
		t.Fatalf("err = %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.errs) != 1 || mock.errs[0].CallID != mock.starts[0].CallID || mock.errs[0].AIVersion == "" {
		t.Fatalf("errors = %+v", mock.errs)
	}
	if mock.errs[0].Error != boom {
		t.Fatalf("error = %v", mock.errs[0].Error)
	}
	if len(mock.ends) != 0 || len(mock.mcEnds) != 0 {
		t.Fatal("end events fired on failure")
	}
}

func TestPort2genExtractTelemetryEmitsErrorEventAndNoEndEventsWhenFieldIsRefused(t *testing.T) {
	mock := &port2genExtractIntegration{}
	answers := port2genExtractAnswers()
	answers["total"] = provider.ExtractionAnswer{Type: "refusal"}
	endCalled := false
	_, err := port2genExtract(t, ExtractOptions{
		Model: port2genExtractModelAnswers(answers), State: port2genExtractState, Fields: port2genExtractFields(),
		ExperimentalTelemetry: port2genTelemetry(mock, nil),
		OnEnd:                 func(ExtractOnEndEvent) { endCalled = true },
	})
	if !providererrors.IsExtractionRefusalError(err) {
		t.Fatalf("err = %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.errs) != 1 || mock.errs[0].CallID != mock.starts[0].CallID {
		t.Fatalf("errors = %+v", mock.errs)
	}
	if len(mock.mcEnds) != 0 || len(mock.ends) != 0 || endCalled {
		t.Fatal("end events fired on refusal")
	}
}

func TestPort2genExtractTelemetryEmitsNoEventsWhenFieldTypeIsUnsupported(t *testing.T) {
	mock := &port2genExtractIntegration{}
	model := port2genExtractModel(nil)
	model.FieldTypes = []string{"text"}
	startCalled := false
	_, err := port2genExtract(t, ExtractOptions{
		Model: model, State: port2genExtractState, Fields: port2genExtractFields(),
		ExperimentalTelemetry: port2genTelemetry(mock, nil),
		OnStart:               func(ExtractOnStartEvent) { startCalled = true },
	})
	if !providererrors.IsExtractionUnsupportedFieldTypeError(err) {
		t.Fatalf("err = %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.events) != 0 || startCalled {
		t.Fatalf("events = %v, startCalled = %v", mock.events, startCalled)
	}
}

func port2genJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TS input validation 'rejects state with array holes' (extract.test.ts 369):
// Go slices have no holes; the analog is a zero-valued part, which is not a
// text, file or json part.
func TestPort2genExtractRejectsStateWithZeroValuedParts(t *testing.T) {
	model := port2genExtractModel(nil)
	_, err := port2genExtract(t, ExtractOptions{
		Model:  model,
		State:  []provider.DecisionStatePart{{}, provider.DecisionTextPart("b")},
		Fields: port2genExtractFields(),
	})
	var target *providererrors.InvalidArgumentError
	if !errors.As(err, &target) || len(model.ExtractCalls) != 0 {
		t.Fatalf("err = %v, calls = %d", err, len(model.ExtractCalls))
	}
}

// Go twins of ai/src/error/extraction-refusal-error.test.ts (14, 28, 39, 47)
// and extraction-unsupported-field-type-error.test.ts (15, 29, 38) through
// the errors ExperimentalExtract returns.
func TestPort2genExtractionRefusalErrorFromExtract(t *testing.T) {
	answers := port2genExtractAnswers()
	answers["total"] = provider.ExtractionAnswer{Type: "refusal"}
	model := port2genExtractModelAnswers(answers)
	model.ProviderName, model.ModelName = "mock-provider", "mock-model-id"
	// extraction-refusal-error.test.ts 14: identifies the refused fields and model.
	_, err := port2genExtract(t, ExtractOptions{Model: model, State: port2genExtractState, Fields: port2genExtractFields()})
	var target *providererrors.ExtractionRefusalError
	if !errors.As(err, &target) || !reflect.DeepEqual(target.FieldIDs, []string{"total"}) || target.Provider != "mock-provider" || target.ModelID != "mock-model-id" {
		t.Fatalf("err = %#v", err)
	}
	if want := `Extraction model "mock-model-id" from provider "mock-provider" refused field "total".`; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
	// extraction-refusal-error.test.ts 28: lists every refused field.
	answers["dueDate"] = provider.ExtractionAnswer{Type: "refusal"}
	_, err = port2genExtract(t, ExtractOptions{
		Model: port2genExtractModelAnswers(answers), State: port2genExtractState, Fields: port2genExtractFields(),
		FieldOrder: []string{"total", "dueDate"},
	})
	if want := `Extraction model "mock-model-id" from provider "mock-provider" refused fields "total", "dueDate".`; err == nil || !strings.Contains(err.Error(), `refused fields "total", "dueDate".`) {
		t.Fatalf("message = %v, want %q", err, want)
	}
	// extraction-refusal-error.test.ts 39: the marker is recognized through wrapping.
	if !providererrors.IsExtractionRefusalError(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("wrapped refusal error not recognized")
	}
	// extraction-refusal-error.test.ts 47: values without the marker.
	for _, v := range []error{nil, errors.New("Unrelated error"), &providererrors.ExtractionUnsupportedFieldTypeError{}} {
		if providererrors.IsExtractionRefusalError(v) {
			t.Fatalf("%v recognized as a refusal error", v)
		}
	}
}

func TestPort2genExtractionUnsupportedFieldTypeErrorFromExtract(t *testing.T) {
	model := port2genExtractModel(nil)
	model.ProviderName, model.ModelName = "mock-provider", "mock-model-id"
	model.FieldTypes = []string{"text"}
	fields := map[string]provider.ExtractionField{"total": {Type: "number"}}
	// extraction-unsupported-field-type-error.test.ts 15
	_, err := port2genExtract(t, ExtractOptions{Model: model, State: port2genExtractState, Fields: fields})
	var target *providererrors.ExtractionUnsupportedFieldTypeError
	if !errors.As(err, &target) || target.FieldID != "total" || target.FieldType != "number" || target.Provider != "mock-provider" || target.ModelID != "mock-model-id" {
		t.Fatalf("err = %#v", err)
	}
	if want := `Field "total" has type "number", which is not supported by provider "mock-provider" and model "mock-model-id".`; err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
	// extraction-unsupported-field-type-error.test.ts 29: recognized through wrapping.
	if !providererrors.IsExtractionUnsupportedFieldTypeError(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("wrapped error not recognized")
	}
	// extraction-unsupported-field-type-error.test.ts 38: values without the marker.
	for _, v := range []error{nil, errors.New("Unrelated error"), &providererrors.ExtractionRefusalError{}} {
		if providererrors.IsExtractionUnsupportedFieldTypeError(v) {
			t.Fatalf("%v recognized", v)
		}
	}
}
