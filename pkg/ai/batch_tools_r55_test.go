package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func startBatchWithTools(t *testing.T, a, b []types.Tool, ctxA, ctxB map[string]interface{}) error {
	t.Helper()
	req1 := textBatchRequest("req-1", "m")
	req1.Text.Tools, req1.Text.ToolsContext = a, ctxA
	req2 := textBatchRequest("req-2", "m")
	req2.Text.Tools, req2.Text.ToolsContext = b, ctxB
	mock := &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(context.Context, provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			return &provider.BatchV4StartResult{BatchID: "b"}, nil
		},
	}
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{Provider: mock, Requests: []BatchRequest{req1, req2}})
	return err
}

// TS validateCompatibleTools compares the prepareTools output
// (isDeepEqualData): resolved descriptions, the JSON input schema, input
// examples, provider options and strict for function tools; id and args for
// provider tools. Title and output schema are not part of it.
func TestStartBatchToolCompatibilityComparesPreparedDefinitions(t *testing.T) {
	obj := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}}
	desc := func(_ context.Context, o types.ToolDescriptionOptions) string { return "search " + o.Context.(string) }
	trueV := true

	cases := []struct {
		name         string
		a, b         types.Tool
		ctxA, ctxB   map[string]interface{}
		incompatible bool
	}{
		{name: "resolved descriptions differ",
			a: types.Tool{Name: "s", DescriptionFunc: desc, Parameters: obj}, b: types.Tool{Name: "s", DescriptionFunc: desc, Parameters: obj},
			ctxA: map[string]interface{}{"s": "web"}, ctxB: map[string]interface{}{"s": "docs"}, incompatible: true},
		{name: "resolved descriptions equal",
			a: types.Tool{Name: "s", DescriptionFunc: desc, Parameters: obj}, b: types.Tool{Name: "s", DescriptionFunc: desc, Parameters: obj},
			ctxA: map[string]interface{}{"s": "web"}, ctxB: map[string]interface{}{"s": "web"}},
		{name: "same JSON schema from a map and a Schema",
			a: types.Tool{Name: "s", Parameters: obj}, b: types.Tool{Name: "s", Parameters: schema.NewSimpleJSONSchema(obj)}},
		{name: "provider options differ",
			a: types.Tool{Name: "s", Parameters: obj, ProviderOptions: map[string]interface{}{"x": map[string]interface{}{"a": 1}}},
			b: types.Tool{Name: "s", Parameters: obj, ProviderOptions: map[string]interface{}{"x": map[string]interface{}{"a": 2}}}, incompatible: true},
		{name: "input examples differ",
			a: types.Tool{Name: "s", Parameters: obj, InputExamples: []types.ToolInputExample{{Input: map[string]interface{}{"q": "a"}}}},
			b: types.Tool{Name: "s", Parameters: obj}, incompatible: true},
		{name: "strict differs",
			a: types.Tool{Name: "s", Parameters: obj, Strict: &trueV}, b: types.Tool{Name: "s", Parameters: obj}, incompatible: true},
		{name: "title and output schema are not compared",
			a: types.Tool{Name: "s", Parameters: obj, Title: "A", OutputSchema: obj}, b: types.Tool{Name: "s", Parameters: obj, Title: "B"}},
		{name: "provider tool args differ",
			a: types.Tool{Name: "ws", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search", ProviderArgs: map[string]interface{}{"n": 1}},
			b: types.Tool{Name: "ws", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search", ProviderArgs: map[string]interface{}{"n": 2}}, incompatible: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := startBatchWithTools(t, []types.Tool{tc.a}, []types.Tool{tc.b}, tc.ctxA, tc.ctxB)
			if tc.incompatible {
				if !isInvalidArgumentError(err) || !strings.Contains(err.Error(), "must have the same definition in every batch request") {
					t.Fatalf("err = %v, want the incompatible tool InvalidArgumentError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

// TS checks each request in turn (standardizePrompt, prepareTools,
// validateCompatibleTools, prepareLanguageModelCallOptions), so an earlier
// request's invalid MaxTokens is reported before a later request's tool
// conflict.
func TestStartBatchValidatesRequestsInOrder(t *testing.T) {
	obj := map[string]interface{}{"type": "object"}
	bad := 0
	req1 := textBatchRequest("req-1", "m")
	req1.Text.Tools = []types.Tool{{Name: "s", Description: "a", Parameters: obj}}
	req1.Text.MaxTokens = &bad
	req2 := textBatchRequest("req-2", "m")
	req2.Text.Tools = []types.Tool{{Name: "s", Description: "b", Parameters: obj}}
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{req1, req2},
	})
	if err == nil || !strings.Contains(err.Error(), "maxOutputTokens must be >= 1") {
		t.Fatalf("err = %v, want the maxOutputTokens error of the first request", err)
	}
}

// TS `request.id.trim()` trims JavaScript whitespace, which includes U+FEFF.
func TestStartBatchRejectsJSWhitespaceID(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{textBatchRequest("\uFEFF", "m")},
	})
	if !isInvalidArgumentError(err) || !strings.Contains(err.Error(), "request IDs must not be empty") {
		t.Fatalf("err = %v, want the empty request ID error", err)
	}
}
