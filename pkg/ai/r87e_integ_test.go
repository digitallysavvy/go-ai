package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Coordinator ruling (round 87e): a single Embed whose provider sends no
// usage (Google never does) returns the v0.5.2 zero usage, which marshals,
// and the root span drops ai.usage.tokens where TS's `usage ?? {tokens:
// NaN}` is NaN.
func TestR87eintegEmbedWithoutUsageKeepsZeroAndDropsAttribute(t *testing.T) {
	model := &testutil.MockEmbeddingModel{
		DoEmbedFunc: func(context.Context, string, *provider.EmbedModelOptions) (*types.EmbeddingResult, error) {
			return &types.EmbeddingResult{Embedding: []float64{0.1}}, nil
		},
	}
	var res *EmbedResult
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		var err error
		res, err = Embed(context.Background(), EmbedOptions{Model: model, Input: "a", Telemetry: settings})
		if err != nil {
			t.Fatal(err)
		}
	})
	if res.Usage.Tokens != 0 || res.Usage.Reported {
		t.Fatalf("Usage = %+v, want the v0.5.2 zero", res.Usage)
	}
	if _, err := json.Marshal(res.Usage); err != nil {
		t.Fatalf("Usage does not marshal: %v", err)
	}
	if v, ok := got["ai.embed ai.usage.tokens"]; ok {
		t.Fatalf("root ai.usage.tokens = %v, want it dropped; all: %v", v, got)
	}
}

// Every EmbedMany batch reported: the sum is recorded on the root span.
func TestR87eintegEmbedManyAllReportedRecordsSum(t *testing.T) {
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings: 1,
		DoEmbedManyFunc: func(context.Context, []string, *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			return &types.EmbeddingsResult{Embeddings: [][]float64{{0.1}}, Usage: types.EmbeddingUsage{Tokens: 2, Reported: true}}, nil
		},
	}
	got := r80carryUsageAttrs(t, func(settings *TelemetrySettings) {
		if _, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, Telemetry: settings}); err != nil {
			t.Fatal(err)
		}
	})
	if v, ok := got["ai.embedMany ai.usage.tokens"]; !ok || v.(int64) != 4 {
		t.Fatalf("root ai.usage.tokens = %v (set %v), want 4", v, ok)
	}
}

// TS UnsupportedModelVersionError interpolates provider and model between
// literal quotes; %q would escape them.
func TestR87eintegUnsupportedModelVersionTextUnescaped(t *testing.T) {
	err := &UnsupportedModelVersionError{Version: "v3", Provider: `a"b`, ModelID: `m\1`}
	want := `Unsupported model version v3 for provider "a"b" and model "m\1". AI SDK 5 only supports models that implement specification version "v2".`
	if err.Error() != want {
		t.Fatalf("Error() = %s\nwant      %s", err.Error(), want)
	}
}

// cr-r87e-gen-to-stream 2: a streamed provider tool result flagged isError
// without a value is a tool-error whose error is undefined (TS
// runToolsTransformation): the step content's Error is nil, the response
// message replays error-json null, and the UI tool-output-error has no
// errorText (to-ui-message-chunk.ts JSON.stringify(undefined)).
func TestR87eintegStreamProviderToolErrorWithoutValue(t *testing.T) {
	t.Parallel()
	isErr := true
	newModel := func() *testutil.MockLanguageModel {
		return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "ws", ToolName: "web_search", IsError: &isErr, ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
	}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: newModel(), Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	var errPart *types.ToolErrorContent
	for _, part := range res.Content() {
		if p, ok := part.(types.ToolErrorContent); ok {
			errPart = &p
		}
	}
	if errPart == nil || errPart.Error != nil || errPart.Err != nil {
		t.Fatalf("tool-error part = %+v, want a nil Error and Err", errPart)
	}
	var replayed *types.ToolResultContent
	for _, m := range res.ResponseMessages() {
		for _, c := range m.Content {
			if p, ok := c.(types.ToolResultContent); ok {
				replayed = &p
			}
		}
	}
	if replayed == nil || replayed.Output == nil || replayed.Output.Type != types.ToolResultOutputErrorJSON || replayed.Output.Value != nil {
		t.Fatalf("replayed tool result = %+v, want error-json null", replayed)
	}

	res2, err := StreamText(context.Background(), StreamTextOptions{Model: newModel(), Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := res2.ToUIMessageStream(context.Background())
	var outErr UIMessageChunk
	for c := range chunks {
		if c["type"] == "tool-output-error" {
			outErr = c
		}
	}
	if e := <-errs; e != nil {
		t.Fatal(e)
	}
	if outErr == nil {
		t.Fatal("no tool-output-error chunk")
	}
	if v, ok := outErr["errorText"]; ok {
		t.Fatalf("tool-output-error errorText = %v, want the key absent", v)
	}
}

// cr-r87e-stream-to-integrator 3: TS executeToolsFromStream enqueues a
// tool result after executeToolCall's notify([onToolExecutionEnd,
// telemetry.onToolExecutionEnd]) settled, so the user end, then the
// telemetry end, then the result part.
func TestR87eintegLiveToolResultAfterTelemetryEnd(t *testing.T) {
	var events r87cstreambEvents
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		},
		[]provider.StreamChunk{{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}},
	)
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "x", Tools: []types.Tool{tool},
		StopWhen:           []StopCondition{StepCountIs(2)},
		OnToolExecutionEnd: func(context.Context, OnToolCallFinishEvent) { events.add("user-end") },
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeToolResult {
				events.add("result")
			}
		},
		Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{&r87cstreambIntegration{rec: events.add,
			names: r87cstreambNames("onToolExecutionEnd", "telemetry-end")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	if got, want := events.get(), []string{"user-end", "telemetry-end", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// r87eintegHeaderStream is a provider stream that returns its response
// headers beside the stream (TS doStream response.headers) and sends no
// header-only response-metadata part.
type r87eintegHeaderStream struct {
	provider.TextStream
	headers map[string]string
}

func (s *r87eintegHeaderStream) ResponseHeaders() map[string]string { return s.headers }

// cr-r87e-stream-to-gen 2: StreamObject takes the response headers from a
// stream that exposes them when no response-metadata part carries any.
func TestR87eintegStreamObjectResponseHeadersFromStream(t *testing.T) {
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &r87eintegHeaderStream{TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: `{"value":"a"}`},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), headers: map[string]string{"x-test": "1"}}, nil
		},
	}
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
	})
	var finish ObjectOnFinishEvent
	res, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "hi", Schema: sch,
		OnFinishEvent: func(_ context.Context, e ObjectOnFinishEvent) { finish = e }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.Headers["x-test"] != "1" || finish.Response.Headers["x-test"] != "1" {
		t.Fatalf("headers: result %v, finish %v; want x-test 1", res.Response.Headers, finish.Response.Headers)
	}
}
