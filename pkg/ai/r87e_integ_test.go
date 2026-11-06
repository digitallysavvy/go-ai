package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/schema"
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

// cr-r87e-stream-to-integrator 2: WithResponseMetadata reports the headers
// beside the stream (TS doStream response.headers) and adds no part; the
// step response still gets them through every wrapper, and the full stream
// carries no Go-only header part.
func TestR87eintegResponseHeadersBesideTheStream(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		inner := providerutils.WithResponseMetadata(testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), http.Header{"X-A": {"1"}}, "m")
		return streaming.NewWarningsStream(inner, []types.Warning{{Type: "other", Message: "w"}}), nil
	}}
	var metaParts int
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x",
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeResponseMetadata {
				metaParts++
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := res.Steps()
	if len(steps) != 1 || steps[0].Response.Headers["X-A"] != "1" {
		t.Fatalf("step response headers = %+v", steps)
	}
	if metaParts != 0 {
		t.Fatalf("response-metadata parts = %d, want none", metaParts)
	}
}

type r87eintegMetaStream struct {
	provider.TextStream
}

func (r87eintegMetaStream) ResponseHeaders() map[string]string { return map[string]string{"X-A": "1"} }
func (r87eintegMetaStream) RequestURL() string                 { return "https://h/v1" }
func (r87eintegMetaStream) RequestBody() interface{}           { return "body" }

// A first step that starts with initial chunks (resumed approvals,
// InitialStreamChunks) keeps the model stream's request and response
// metadata: prefixedTextStream forwards them.
func TestR87eintegPrefixedStreamForwardsMetadata(t *testing.T) {
	var s provider.TextStream = &prefixedTextStream{base: r87eintegMetaStream{testutil.NewMockTextStream(nil)}}
	if streamResponseHeaders(s)["X-A"] != "1" || streamRequestURL(s) != "https://h/v1" || streamRequestBody(s) != "body" {
		t.Fatal("prefixedTextStream hides the model stream's metadata")
	}
}

// TS provider-utils decision-language-model.ts and generate-id.ts throw the
// @ai-sdk/provider InvalidArgumentError: the bare message.
func TestR87eintegProviderUtilsArgumentErrorsAreBare(t *testing.T) {
	if _, err := NewDecisionLanguageModel(DecisionLanguageModelOptions{}); err == nil || err.Error() != "Decision requires a LanguageModel implementation." {
		t.Fatalf("NewDecisionLanguageModel err = %v", err)
	}
	_, err := CreateIDGenerator(CreateIDGeneratorOptions{Prefix: "p", Alphabet: "ab-", Separator: "-"})
	if err == nil || err.Error() != `The separator "-" must not be part of the alphabet "ab-".` {
		t.Fatalf("CreateIDGenerator err = %v", err)
	}
}

// TS decide-events.ts: DecideStartEvent carries maxOutputTokens, reasoning
// and providerOptions; DecideEndEvent adds usage, warnings, rounding,
// providerMetadata and response; DecisionModelCallEndEvent adds warnings,
// rounding and response (cr-r87e-gen-to-carry CR 5).
func TestR87eintegDecideTelemetryEventFields(t *testing.T) {
	questions, model := decideTelemetryFixture()
	digits := 2
	inner := model.doDecide
	model.doDecide = func(ctx context.Context, o provider.DecisionCallOptions) (*provider.DecisionResult, error) {
		r, err := inner(ctx, o)
		r.Warnings = []types.Warning{{Type: "other", Message: "w"}}
		r.Rounding = &provider.DecisionRounding{ProbabilityDecimals: &digits}
		r.Response = &provider.DecisionResponseInfo{ID: "resp-1", ModelID: "m-2"}
		return r, err
	}
	integ := &mockDecideDispatchIntegration{}
	maxOut := 64
	level := types.ReasoningHigh
	popts := map[string]interface{}{"test": map[string]interface{}{"x": 1}}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, State: "s", Questions: questions,
		MaxOutputTokens: &maxOut, Reasoning: &level, ProviderOptions: popts,
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := integ.decideStart
	if s == nil || s.MaxOutputTokens == nil || *s.MaxOutputTokens != 64 || s.Reasoning == nil || *s.Reasoning != types.ReasoningHigh || !reflect.DeepEqual(s.ProviderOptions, popts) {
		t.Fatalf("start = %+v", s)
	}
	e := integ.decideEnd
	if e == nil || e.Usage.InputTokens == nil || *e.Usage.InputTokens != 3 || e.Usage.TotalTokens == nil || *e.Usage.TotalTokens != 10 ||
		len(e.Warnings) != 1 || e.Rounding.(*provider.DecisionRounding).ProbabilityDecimals != &digits ||
		e.ProviderMetadata["test"] == nil || e.Response.(DecideResponse).ID != "resp-1" || e.MaxOutputTokens == nil {
		t.Fatalf("end = %+v", e)
	}
	m := integ.decisionModelEnd
	if m == nil || len(m.Warnings) != 1 || m.Rounding == nil || m.Response.(*provider.DecisionResponseInfo).ModelID != "m-2" {
		t.Fatalf("model end = %+v", m)
	}
}

// A result without rounding or response leaves the telemetry fields nil
// (TS undefined), not a typed nil pointer.
func TestR87eintegDecideTelemetryNilRounding(t *testing.T) {
	questions, model := decideTelemetryFixture()
	integ := &mockDecideDispatchIntegration{}
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, State: "s", Questions: questions,
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
	}); err != nil {
		t.Fatal(err)
	}
	if integ.decideEnd.Rounding != nil || integ.decisionModelEnd.Rounding != nil || integ.decisionModelEnd.Response != nil {
		t.Fatalf("got %#v %#v %#v", integ.decideEnd.Rounding, integ.decisionModelEnd.Rounding, integ.decisionModelEnd.Response)
	}
}

// Coordinator ruling "Undeclared tool input keys": TS zod z.object input
// schemas of provider-defined tools strip undeclared keys (bash_20250124:
// z.object({command, restart})); user JSON-schema tools keep them like TS
// jsonSchema() tools, and unaudited provider tools keep them (S7).
func TestR87eintegProviderToolInputStripsUndeclaredKeys(t *testing.T) {
	var got []map[string]interface{}
	exec := func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		got = append(got, in)
		return "ok", nil
	}
	params := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"command": map[string]interface{}{"type": "string"}, "restart": map[string]interface{}{"type": "boolean"}}, "required": []interface{}{"command"}}
	tools := []types.Tool{
		{Name: "bash", Type: "provider", ProviderID: "anthropic.bash_20250124", Parameters: params, Execute: exec},
		{Name: "user", Parameters: params, Execute: exec},
		{Name: "mem", Type: "provider", ProviderID: "anthropic.memory_20250818", Parameters: params, Execute: exec},
	}
	calls := 0
	model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls > 1 {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		}
		in := func(id, name string) types.ToolCall {
			return types.ToolCall{ID: id, ToolName: name, Arguments: map[string]interface{}{"command": "ls", "extra": 1}}
		}
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{in("1", "bash"), in("2", "user"), in("3", "mem")}}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: tools, StopWhen: []StopCondition{StepCountIs(2)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("executions = %v", got)
	}
	byKeys := map[int]int{}
	for _, in := range got {
		byKeys[len(in)]++
	}
	if byKeys[1] != 1 || byKeys[2] != 2 {
		t.Fatalf("inputs = %v, want the bash input stripped to {command} and the others kept", got)
	}
	if args := res.Steps[0].ToolCalls[0].Arguments; len(args) != 1 || args["command"] != "ls" {
		t.Fatalf("step tool call input = %v", args)
	}
}
