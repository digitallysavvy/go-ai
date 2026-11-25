package providerutils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func portcarryBool(b bool) *bool  { return &b }
func portcarryInt(i int) *int     { return &i }
func portcarryI64(i int64) *int64 { return &i }

func portcarryFields() (map[string]provider.ExtractionField, []string) {
	return map[string]provider.ExtractionField{
		"invoice number": {Type: "text", Instructions: "The invoice number"},
		"total":          {Type: "number", Unit: "USD", Decimals: portcarryInt(2)},
		"count":          {Type: "number", Integer: portcarryBool(true)},
		"dueDate":        {Type: "date"},
		"dueTime":        {Type: "time"},
		"dueAt":          {Type: "datetime"},
		"wait":           {Type: "duration", Unit: "hours"},
		"contacts":       {Type: "list", Items: "text"},
		"scores":         {Type: "list", Items: "number", Integer: portcarryBool(true)},
	}, []string{"invoice number", "total", "count", "dueDate", "dueTime", "dueAt", "wait", "contacts", "scores"}
}

func portcarryOptions() provider.ExtractionCallOptions {
	fields, order := portcarryFields()
	return provider.ExtractionCallOptions{
		State:         []provider.DecisionStatePart{provider.DecisionTextPart("Invoice INV-2041, total $1,250.00.")},
		Fields:        fields,
		FieldOrder:    order,
		ReferenceDate: "2026-11-11",
	}
}

const portcarryValues = `{"f0":"INV-2041","f1":1250,"f2":null,"f3":"2027-03-03","f4":"17:30","f5":"2027-03-03T17:30:00","f6":2,"f7":["Ana","Bo"],"f8":null}`

type portcarrySetup struct {
	lm    *testutil.MockLanguageModel
	model *ExtractionLanguageModel
	mu    sync.Mutex
	calls []*provider.GenerateOptions
}

func portcarryResult(text string) *types.GenerateResult {
	return &types.GenerateResult{
		Text:             text,
		FinishReason:     types.FinishReasonStop,
		Usage:            types.Usage{InputTokens: portcarryI64(20), OutputTokens: portcarryI64(12)},
		Warnings:         []types.Warning{{Type: "other", Message: "test warning"}},
		ProviderMetadata: map[string]interface{}{"test": map[string]interface{}{"original": true}},
		ResponseMetadata: &types.ResponseMetadata{ID: "response", ModelID: "resolved"},
	}
}

func portcarryNew(t *testing.T, maxFields *int, gen func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error)) *portcarrySetup {
	t.Helper()
	s := &portcarrySetup{}
	s.lm = &testutil.MockLanguageModel{ProviderName: "test.language", ModelName: "test-model"}
	if gen == nil {
		gen = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return portcarryResult(portcarryValues), nil
		}
	}
	s.lm.DoGenerateFunc = func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		s.mu.Lock()
		s.calls = append(s.calls, o)
		s.mu.Unlock()
		return gen(ctx, o)
	}
	m, err := NewExtractionLanguageModel(ExtractionLanguageModelOptions{Model: s.lm, MaxFieldsPerCall: maxFields})
	if err != nil {
		t.Fatal(err)
	}
	s.model = m
	return s
}

// TS extraction-language-model.test.ts: maps internal keys back to caller
// field ids in one call.
func TestPortcarryExtractionLMMapsKeysBack(t *testing.T) {
	s := portcarryNew(t, nil, nil)
	res, err := s.model.DoExtract(context.Background(), portcarryOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Answers)
	want := `{"contacts":{"type":"list","value":["Ana","Bo"]},"count":{"type":"number","value":null},"dueAt":{"type":"datetime","value":"2027-03-03T17:30:00"},"dueDate":{"type":"date","value":"2027-03-03"},"dueTime":{"type":"time","value":"17:30"},"invoice number":{"type":"text","value":"INV-2041"},"scores":{"type":"list","value":null},"total":{"type":"number","value":1250},"wait":{"type":"duration","value":2}}`
	if string(b) != want {
		t.Fatalf("answers = %s", b)
	}
	if *res.Usage.InputTokens != 20 || *res.Usage.OutputTokens != 12 || len(res.Warnings) != 1 {
		t.Fatalf("metadata: %+v", res)
	}
	if res.Response.ID != "response" || res.Response.ModelID != "resolved" {
		t.Fatalf("response: %+v", res.Response)
	}
	if len(s.calls) != 1 || s.model.Provider() != "test.language.extraction" || s.model.ModelID() != "test-model" {
		t.Fatal("call count / identity")
	}
}

// TS: sends a nullable schema per field, the field options, and the state.
func TestPortcarryExtractionLMPrompt(t *testing.T) {
	s := portcarryNew(t, nil, nil)
	o := portcarryOptions()
	o.State = []provider.DecisionStatePart{
		provider.DecisionTextPart("Invoice INV-2041, total $1,250.00."),
		provider.DecisionJSONPart(map[string]interface{}{"po": "PO-7"}),
		provider.DecisionFilePart(types.FileContent{MediaType: "application/pdf", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "JVBERi0="}}),
	}
	if _, err := s.model.DoExtract(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c := s.calls[0]
	if c.Prompt.System != extractionLanguageModelSystemPrompt || !strings.HasPrefix(c.Prompt.System, "Extract every field from the shared state") {
		t.Fatal("system prompt")
	}
	if c.Reasoning == nil || *c.Reasoning != types.ReasoningNone {
		t.Fatalf("reasoning = %v", c.Reasoning)
	}
	content := c.Prompt.Messages[0].Content
	if len(content) != 5 {
		t.Fatalf("content parts = %d", len(content))
	}
	wantHeader := `{"fields":{"f0":{"id":"invoice number","type":"text","instructions":"The invoice number"},"f1":{"id":"total","type":"number","unit":"USD","decimals":2},"f2":{"id":"count","type":"number","integer":true},"f3":{"id":"dueDate","type":"date"},"f4":{"id":"dueTime","type":"time"},"f5":{"id":"dueAt","type":"datetime"},"f6":{"id":"wait","type":"duration","unit":"hours"},"f7":{"id":"contacts","type":"list","items":"text"},"f8":{"id":"scores","type":"list","items":"number","integer":true}},"referenceDate":"2026-11-11"}`
	if got := content[0].(types.TextContent).Text; got != wantHeader {
		t.Fatalf("header:\n got %s\nwant %s", got, wantHeader)
	}
	if content[1].(types.TextContent).Text != "Shared state:" || content[2].(types.TextContent).Text != "Invoice INV-2041, total $1,250.00." || content[3].(types.TextContent).Text != `{"po":"PO-7"}` {
		t.Fatal("state parts")
	}
	if _, ok := content[4].(types.FileContent); !ok {
		t.Fatalf("file part is %T", content[4])
	}
	schemaJSON, _ := json.Marshal(c.ResponseFormat.Schema)
	wantSchema := `{"additionalProperties":false,"properties":{"f0":{"anyOf":[{"description":"Text copied from the state.","type":"string"},{"type":"null"}]},"f1":{"anyOf":[{"description":"A finite number.","type":"number"},{"type":"null"}]},"f2":{"anyOf":[{"description":"A finite number.","type":"integer"},{"type":"null"}]},"f3":{"anyOf":[{"description":"A calendar date formatted as YYYY-MM-DD.","type":"string"},{"type":"null"}]},"f4":{"anyOf":[{"description":"A time of day formatted as HH:MM or HH:MM:SS.","type":"string"},{"type":"null"}]},"f5":{"anyOf":[{"description":"An ISO 8601 date and time such as 2027-03-03T17:30:00, with a Z or +HH:MM offset only when the state gives one.","type":"string"},{"type":"null"}]},"f6":{"anyOf":[{"description":"A nonnegative number of the given unit, seconds by default.","type":"number"},{"type":"null"}]},"f7":{"anyOf":[{"description":"Every matching value in the order it appears in the state.","items":{"description":"Text copied from the state.","type":"string"},"type":"array"},{"type":"null"}]},"f8":{"anyOf":[{"description":"Every matching value in the order it appears in the state.","items":{"description":"A finite number.","type":"integer"},"type":"array"},{"type":"null"}]}},"required":["f0","f1","f2","f3","f4","f5","f6","f7","f8"],"type":"object"}`
	if string(schemaJSON) != wantSchema || c.ResponseFormat.Name != "extraction" || c.ResponseFormat.Type != "json" {
		t.Fatalf("schema = %s", schemaJSON)
	}
}

func TestPortcarryExtractionLMRejectsBadOutput(t *testing.T) {
	for name, text := range map[string]string{
		"missing values": `{"f0":"x"}`,
		"an extra value": strings.Replace(portcarryValues, `"f8":null`, `"f8":null,"f9":1`, 1),
		"an array":       `[]`,
		"invalid JSON":   `not json`,
	} {
		s := portcarryNew(t, nil, func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return portcarryResult(text), nil
		})
		_, err := s.model.DoExtract(context.Background(), portcarryOptions())
		var target *providererrors.InvalidResponseDataError
		if !errors.As(err, &target) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	s := portcarryNew(t, nil, func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		r := portcarryResult(portcarryValues)
		r.FinishReason = types.FinishReasonLength
		return r, nil
	})
	_, err := s.model.DoExtract(context.Background(), portcarryOptions())
	if err == nil || err.Error() != "Extraction did not complete: length." {
		t.Fatalf("incomplete: %v", err)
	}
}

func portcarryMany() (map[string]provider.ExtractionField, []string) {
	f := map[string]provider.ExtractionField{}
	var order []string
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("price %d", i)
		f[id] = provider.ExtractionField{Type: "number"}
		order = append(order, id)
	}
	return f, order
}

func portcarryRespond(o *provider.GenerateOptions) *types.GenerateResult {
	header := o.Prompt.Messages[0].Content[0].(types.TextContent).Text
	var parsed struct {
		Fields map[string]struct{ ID string } `json:"fields"`
	}
	_ = json.Unmarshal([]byte(header), &parsed)
	out := map[string]interface{}{}
	for k, v := range parsed.Fields {
		var idx int
		if _, err := fmt.Sscanf(strings.TrimPrefix(v.ID, "price "), "%d", &idx); err != nil {
			panic(err)
		}
		if idx%2 == 0 {
			out[k] = idx
		} else {
			out[k] = nil
		}
	}
	b, _ := json.Marshal(out)
	r := portcarryResult(string(b))
	r.Warnings = []types.Warning{{Type: "other", Message: "call warning"}}
	r.ProviderMetadata = map[string]interface{}{"test": map[string]interface{}{"call": true}}
	return r
}

func TestPortcarryExtractionLMMaxFieldsPerCall(t *testing.T) {
	fields, order := portcarryMany()
	o := portcarryOptions()
	o.Fields, o.FieldOrder = fields, order

	// sends every field in one call by default
	one := portcarryNew(t, nil, func(_ context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		return portcarryRespond(g), nil
	})
	res, err := one.model.DoExtract(context.Background(), o)
	if err != nil || len(one.calls) != 1 {
		t.Fatalf("one call: %v %d", err, len(one.calls))
	}
	if len(one.calls[0].ResponseFormat.Schema.(map[string]interface{})["properties"].(map[string]interface{})) != 40 {
		t.Fatal("properties")
	}
	if res.Answers["price 38"].Value != float64(38) {
		t.Fatalf("price 38 = %v", res.Answers["price 38"].Value)
	}

	// splits fields into parallel nullable calls and merges the answers
	sp := portcarryNew(t, portcarryInt(16), func(_ context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		return portcarryRespond(g), nil
	})
	res, err = sp.model.DoExtract(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[int]int{}
	for _, c := range sp.calls {
		props := c.ResponseFormat.Schema.(map[string]interface{})["properties"].(map[string]interface{})
		for _, p := range props {
			if len(p.(map[string]interface{})["anyOf"].([]interface{})) != 2 {
				t.Fatal("anyOf size")
			}
		}
		sizes[len(props)]++
	}
	if len(sp.calls) != 3 || sizes[16] != 2 || sizes[8] != 1 {
		t.Fatalf("sizes %v", sizes)
	}
	if len(res.Answers) != 40 {
		t.Fatalf("answers = %d", len(res.Answers))
	}
	for i := 0; i < 40; i++ {
		var want interface{}
		if i%2 == 0 {
			want = float64(i)
		}
		got := res.Answers[fmt.Sprintf("price %d", i)]
		if got.Type != "number" || got.Value != want {
			t.Fatalf("price %d = %+v", i, got)
		}
	}
	if *res.Usage.InputTokens != 60 || *res.Usage.OutputTokens != 36 || len(res.Warnings) != 3 {
		t.Fatalf("merged metadata %+v", res.Usage)
	}
	if !reflect.DeepEqual(res.ProviderMetadata, map[string]interface{}{"test": map[string]interface{}{"call": true}}) || res.Response.ID != "response" {
		t.Fatal("first-call metadata")
	}

	// reports unknown usage when any call does not report it
	var once sync.Once
	un := portcarryNew(t, portcarryInt(16), func(_ context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		r := portcarryRespond(g)
		once.Do(func() { r.Usage.InputTokens = nil })
		return r, nil
	})
	res, err = un.model.DoExtract(context.Background(), o)
	if err != nil || res.Usage.InputTokens != nil || *res.Usage.OutputTokens != 36 {
		t.Fatalf("unknown usage: %v %+v", err, res.Usage)
	}
}

func TestPortcarryExtractionLMKeepsPrototypeFieldIDs(t *testing.T) {
	s := portcarryNew(t, portcarryInt(1), func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return portcarryResult(`{"f0":"x"}`), nil
	})
	o := portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{"__proto__": {Type: "text"}, "constructor": {Type: "text"}}
	o.FieldOrder = []string{"__proto__", "constructor"}
	res, err := s.model.DoExtract(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Answers["__proto__"]; !ok || len(res.Answers) != 2 {
		t.Fatalf("answers %v", res.Answers)
	}
}

func TestPortcarryExtractionLMFailureAbortsSiblings(t *testing.T) {
	boom := errors.New("first call failed")
	var mu sync.Mutex
	n := 0
	var ctxs []context.Context
	s := portcarryNew(t, portcarryInt(1), func(ctx context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		n++
		first := n == 1
		ctxs = append(ctxs, ctx)
		mu.Unlock()
		if first {
			return nil, boom
		}
		<-ctx.Done()
		return nil, context.Cause(ctx)
	})
	o := portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{"a": {Type: "text"}, "b": {Type: "text"}, "c": {Type: "text"}}
	o.FieldOrder = []string{"a", "b", "c"}
	_, err := s.model.DoExtract(context.Background(), o)
	if err != boom {
		t.Fatalf("err = %v", err)
	}
	if len(ctxs) != 3 {
		t.Fatalf("calls = %d", len(ctxs))
	}
	for _, c := range ctxs {
		if c.Err() == nil {
			t.Fatal("sibling context not aborted")
		}
	}
}

func TestPortcarryExtractionLMCallerAbortReachesEveryCall(t *testing.T) {
	reason := errors.New("stopped by user")
	ctx, cancel := context.WithCancelCause(context.Background())
	var mu sync.Mutex
	started := 0
	ready := make(chan struct{})
	s := portcarryNew(t, portcarryInt(1), func(c context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		started++
		if started == 2 {
			close(ready)
		}
		mu.Unlock()
		<-c.Done()
		return nil, context.Cause(c)
	})
	o := portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{"a": {Type: "text"}, "b": {Type: "text"}}
	o.FieldOrder = []string{"a", "b"}
	done := make(chan error, 1)
	go func() { _, err := s.model.DoExtract(ctx, o); done <- err }()
	<-ready
	cancel(reason)
	if err := <-done; err != reason {
		t.Fatalf("err = %v", err)
	}
}

func TestPortcarryExtractionLMRejectsBadMaxFieldsPerCall(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, err := NewExtractionLanguageModel(ExtractionLanguageModelOptions{Model: &testutil.MockLanguageModel{}, MaxFieldsPerCall: &n})
		var target *providererrors.InvalidArgumentError
		if !errors.As(err, &target) {
			t.Errorf("maxFieldsPerCall %d: err = %v", n, err)
		}
	}
	if _, err := NewExtractionLanguageModel(ExtractionLanguageModelOptions{}); err == nil {
		t.Error("nil model accepted")
	}
}

// d99098a979: empty strings and lists pass through for core to normalize.
func TestPortcarryExtractionLMPassesEmptyValuesThrough(t *testing.T) {
	s := portcarryNew(t, nil, func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return portcarryResult(strings.NewReplacer(`"f0":"INV-2041"`, `"f0":""`, `"f3":"2027-03-03"`, `"f3":""`, `"f7":["Ana","Bo"]`, `"f7":[]`).Replace(portcarryValues)), nil
	})
	res, err := s.model.DoExtract(context.Background(), portcarryOptions())
	if err != nil {
		t.Fatal(err)
	}
	if res.Answers["invoice number"].Value != "" || res.Answers["dueDate"].Value != "" {
		t.Fatalf("empty strings: %+v", res.Answers)
	}
	if l, ok := res.Answers["contacts"].Value.([]interface{}); !ok || len(l) != 0 {
		t.Fatalf("empty list: %#v", res.Answers["contacts"].Value)
	}
}

func TestPortcarryExtractionLMRoundsToDecimals(t *testing.T) {
	s := portcarryNew(t, nil, func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return portcarryResult(`{"f0":1250.004,"f1":0.30000000000000004,"f2":[1.256,2]}`), nil
	})
	o := portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{
		"total":  {Type: "number", Decimals: portcarryInt(2)},
		"wait":   {Type: "duration", Decimals: portcarryInt(1)},
		"prices": {Type: "list", Items: "number", Decimals: portcarryInt(2)},
	}
	o.FieldOrder = []string{"total", "wait", "prices"}
	res, err := s.model.DoExtract(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Answers)
	if string(b) != `{"prices":{"type":"list","value":[1.26,2]},"total":{"type":"number","value":1250},"wait":{"type":"duration","value":0.3}}` {
		t.Fatalf("answers = %s", b)
	}
}

// JS toFixed rounds an exact tie up, away from zero for negatives (strconv
// rounds half to even): mutation target for extractionToFixed.
func TestPortcarryExtractionToFixedTies(t *testing.T) {
	cases := []struct {
		in       float64
		decimals int
		want     float64
	}{{2.5, 0, 3}, {0.5, 0, 1}, {-2.5, 0, -3}, {1.5, 0, 2}, {0.125, 2, 0.13}, {-0.125, 2, -0.13}, {1.005, 2, 1}, {5e-324, 2, 0}}
	for _, c := range cases {
		if got := extractionToFixed(c.in, c.decimals); got != c.want {
			t.Errorf("toFixed(%v,%d) = %v want %v", c.in, c.decimals, got, c.want)
		}
	}
}

func TestPortcarryExtractionLMRefusals(t *testing.T) {
	s := portcarryNew(t, portcarryInt(1), func(_ context.Context, g *provider.GenerateOptions) (*types.GenerateResult, error) {
		if strings.Contains(g.Prompt.Messages[0].Content[0].(types.TextContent).Text, `"id":"a"`) {
			r := portcarryResult("")
			r.FinishReason = types.FinishReasonContentFilter
			return r, nil
		}
		return portcarryResult(`{"f0":"B"}`), nil
	})
	o := portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{"a": {Type: "text"}, "b": {Type: "text"}}
	o.FieldOrder = []string{"a", "b"}
	res, err := s.model.DoExtract(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Answers)
	if string(b) != `{"a":{"type":"refusal"},"b":{"type":"text","value":"B"}}` {
		t.Fatalf("answers = %s", b)
	}
}

func TestPortcarryExtractionLMOmitsUnsetReferenceDate(t *testing.T) {
	s := portcarryNew(t, nil, nil)
	o := portcarryOptions()
	o.ReferenceDate = ""
	if _, err := s.model.DoExtract(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.calls[0].Prompt.Messages[0].Content[0].(types.TextContent).Text, "referenceDate") {
		t.Fatal("referenceDate present")
	}
}

func TestPortcarryExtractionLMAbortReason(t *testing.T) {
	reason := errors.New("aborted")
	s := portcarryNew(t, nil, nil)
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(reason)
	if _, err := s.model.DoExtract(ctx, portcarryOptions()); err != reason || len(s.calls) != 0 {
		t.Fatalf("pre-aborted: %v calls=%d", err, len(s.calls))
	}
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	s2 := portcarryNew(t, nil, func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		cancel2(reason)
		return portcarryResult(portcarryValues), nil
	})
	if _, err := s2.model.DoExtract(ctx2, portcarryOptions()); err != reason {
		t.Fatalf("aborted while responding: %v", err)
	}
}

func TestPortcarryExtractionLMPassesCallOptionsThrough(t *testing.T) {
	s := portcarryNew(t, nil, nil)
	o := portcarryOptions()
	low := types.ReasoningLow
	o.Reasoning = &low
	o.MaxOutputTokens = portcarryInt(100)
	o.Headers = map[string]string{"custom": "value"}
	o.ProviderOptions = map[string]interface{}{"test": map[string]interface{}{"option": true}}
	if _, err := s.model.DoExtract(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	c := s.calls[0]
	if *c.Reasoning != types.ReasoningLow || *c.MaxTokens != 100 || c.Headers["custom"] != "value" || c.ProviderOptions["test"] == nil {
		t.Fatalf("options not forwarded: %+v", c)
	}
}

func TestPortcarryExtractionLMRejectsBadCalls(t *testing.T) {
	s := portcarryNew(t, nil, nil)
	o := portcarryOptions()
	o.State = []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "audio/wav", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "UklGRg=="}})}
	_, err := s.model.DoExtract(context.Background(), o)
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "Extraction file media type: audio/wav") {
		t.Fatalf("media type: %v", err)
	}
	o = portcarryOptions()
	o.Fields, o.FieldOrder = nil, nil
	_, err = s.model.DoExtract(context.Background(), o)
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || err.Error() != "Extraction requires at least one field." {
		t.Fatalf("empty fields: %v", err)
	}
	o = portcarryOptions()
	o.Fields = map[string]provider.ExtractionField{"x": {Type: "bogus"}}
	o.FieldOrder = []string{"x"}
	_, err = s.model.DoExtract(context.Background(), o)
	if !providererrors.IsExtractionUnsupportedFieldTypeError(err) {
		t.Fatalf("field type: %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatal("model called")
	}
}
