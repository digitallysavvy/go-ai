package telemetry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r88carryRecorder records what integrations are handed.
type r88carryRecorder struct {
	NoopTelemetryIntegration
	mu        sync.Mutex
	starts    []TelemetryStartEvent
	ends      []TelemetryFinishEvent
	steps     []TelemetryStepFinishEvent
	lmStarts  []LanguageModelCallStartEvent
	lmEnds    []LanguageModelCallEndEvent
	toolStart []TelemetryToolCallStartEvent
	toolEnd   []TelemetryToolCallFinishEvent
	errs      []TelemetryErrorEvent
	aborts    []TelemetryAbortEvent
	embedEnds []EmbeddingModelCallEndEvent
	rrStarts  []RerankingModelCallStartEvent
	dStarts   []DecideStartEvent
	dEnds     []DecideEndEvent
	dmStarts  []DecisionModelCallStartEvent
	dmEnds    []DecisionModelCallEndEvent
	execArgs  []map[string]interface{}
}

func (r *r88carryRecorder) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, e)
	return ctx
}
func (r *r88carryRecorder) OnEnd(_ context.Context, e TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, e)
}
func (r *r88carryRecorder) OnStepEnd(_ context.Context, e TelemetryStepEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, e)
}
func (r *r88carryRecorder) OnLanguageModelCallStart(ctx context.Context, e LanguageModelCallStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lmStarts = append(r.lmStarts, e)
	return ctx
}
func (r *r88carryRecorder) OnLanguageModelCallEnd(_ context.Context, e LanguageModelCallEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lmEnds = append(r.lmEnds, e)
}
func (r *r88carryRecorder) OnToolExecutionStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolStart = append(r.toolStart, e)
	return ctx
}
func (r *r88carryRecorder) OnToolExecutionEnd(_ context.Context, e TelemetryToolCallFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolEnd = append(r.toolEnd, e)
}
func (r *r88carryRecorder) OnError(_ context.Context, e TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, e)
}
func (r *r88carryRecorder) OnAbort(_ context.Context, e TelemetryAbortEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aborts = append(r.aborts, e)
}
func (r *r88carryRecorder) OnEmbedEnd(_ context.Context, e EmbeddingModelCallEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedEnds = append(r.embedEnds, e)
}
func (r *r88carryRecorder) OnRerankStart(_ context.Context, e RerankingModelCallStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rrStarts = append(r.rrStarts, e)
}
func (r *r88carryRecorder) OnDecideStart(ctx context.Context, e DecideStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dStarts = append(r.dStarts, e)
	return ctx
}
func (r *r88carryRecorder) OnDecideEnd(_ context.Context, e DecideEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dEnds = append(r.dEnds, e)
}
func (r *r88carryRecorder) OnDecisionModelCallStart(_ context.Context, e DecisionModelCallStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dmStarts = append(r.dmStarts, e)
}
func (r *r88carryRecorder) OnDecisionModelCallEnd(_ context.Context, e DecisionModelCallEndEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dmEnds = append(r.dmEnds, e)
}
func (r *r88carryRecorder) ExecuteTool(ctx context.Context, _ string, args map[string]interface{}, next func(context.Context, map[string]interface{}) (interface{}, error)) (interface{}, error) {
	r.mu.Lock()
	r.execArgs = append(r.execArgs, args)
	r.mu.Unlock()
	return next(ctx, args)
}

func r88carrySettings(rec TelemetryIntegration, inputs, outputs *bool) *Settings {
	return &Settings{RecordInputs: inputs, RecordOutputs: outputs, Integrations: []TelemetryIntegration{rec}}
}

// TS create-telemetry-dispatcher.test.ts "strips input content from events
// sent to integrations when recordInputs is false".
func TestR88carryFilterStripsInputs(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, Bool(false), nil)
	FireOnStart(context.Background(), TelemetryStartEvent{
		CallID: "call-1", OperationType: "ai.generateText", ModelID: "model-1", Settings: set,
		System:              "secret instructions",
		InstructionMessages: []types.Message{{Role: types.RoleSystem}},
		Messages:            []types.Message{{Role: types.RoleUser}},
		Prompt:              "secret prompt",
	})
	got := rec.starts[0]
	if got.System != "" || got.Prompt != "" || len(got.Messages) != 0 || got.Messages == nil || len(got.InstructionMessages) != 0 {
		t.Errorf("inputs not stripped: %+v", got)
	}
	if got.ModelID != "model-1" || got.Settings.ShouldRecordInputs() {
		t.Errorf("non-content fields changed: %+v", got)
	}
}

// TS "strips output content when recordOutputs is false".
func TestR88carryFilterStripsOutputs(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, nil, Bool(false))
	FireOnEnd(context.Background(), TelemetryFinishEvent{
		CallID: "call-1", OperationType: "ai.generateText", Settings: set,
		Text:        "secret reply",
		ToolCalls:   []types.ToolCall{{ID: "t1"}},
		ToolResults: []types.ToolResult{{ToolCallID: "t1"}},
		Usage:       TelemetryUsage{InputTokens: r88carryInt(1)},
	})
	got := rec.ends[0]
	if got.Text != "" || len(got.ToolCalls) != 0 || len(got.ToolResults) != 0 {
		t.Errorf("outputs not stripped: %+v", got)
	}
	if got.Usage.InputTokens == nil || *got.Usage.InputTokens != 1 {
		t.Errorf("usage changed: %+v", got.Usage)
	}
}

func r88carryInt(v int64) *int64 { return &v }

// TS "keeps content when the flags are not set".
func TestR88carryFilterKeepsContentWithoutFlags(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, nil, nil)
	FireOnStart(context.Background(), TelemetryStartEvent{CallID: "c", Settings: set, System: "secret instructions", Prompt: "p"})
	if rec.starts[0].System != "secret instructions" || rec.starts[0].Prompt != "p" {
		t.Errorf("content dropped: %+v", rec.starts[0])
	}
}

// TS "does not mutate the original event object".
func TestR88carryFilterDoesNotMutateOriginal(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, Bool(false), nil)
	args := map[string]interface{}{"q": "secret"}
	original := TelemetryToolCallStartEvent{Settings: set, CallID: "c", ToolCallID: "t", ToolName: "search", Args: args}
	FireOnToolCallStart(context.Background(), original)
	if original.Args["q"] != "secret" {
		t.Errorf("original mutated: %v", original.Args)
	}
	if rec.toolStart[0].Args != nil || rec.toolStart[0].ToolName != "search" {
		t.Errorf("delivered = %+v", rec.toolStart[0])
	}
	steps := []types.StepResult{{Text: "keep", Request: types.StepRequest{Body: "b"}}}
	FireOnAbort(context.Background(), TelemetryAbortEvent{Settings: r88carrySettings(rec, nil, Bool(false)), Steps: steps})
	if steps[0].Text != "keep" || steps[0].Request.Body != "b" {
		t.Errorf("caller steps mutated: %+v", steps[0])
	}
}

// TS "treats text as input for speech and as output elsewhere".
func TestR88carryFilterSpeechText(t *testing.T) {
	rec := &r88carryRecorder{}
	FireOnStart(context.Background(), TelemetryStartEvent{Settings: r88carrySettings(rec, Bool(false), nil), CallID: "c", OperationType: "ai.generateSpeech", Text: "secret narration"})
	if rec.starts[0].Text != "" {
		t.Errorf("speech text = %q", rec.starts[0].Text)
	}
	FireOnEnd(context.Background(), TelemetryFinishEvent{Settings: r88carrySettings(rec, Bool(false), nil), CallID: "c", OperationType: "ai.generateText", Text: "generated reply"})
	if rec.ends[0].Text != "generated reply" {
		t.Errorf("text call text = %q", rec.ends[0].Text)
	}
	FireOnEnd(context.Background(), TelemetryFinishEvent{Settings: r88carrySettings(rec, nil, Bool(false)), CallID: "c", OperationType: "ai.generateSpeech", Text: "narration"})
	if rec.ends[1].Text != "narration" {
		t.Errorf("speech end text = %q (speech text is an input)", rec.ends[1].Text)
	}
}

// Either flag off: provider metadata, raw errors and bodies never reach
// integrations or diagnostic subscribers (TS filterTelemetryEvent "Raw
// provider payloads and errors").
func TestR88carryFilterMetadataErrorsAndDiagnostics(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, Bool(false), nil)
	var diag []interface{}
	var dmu sync.Mutex
	unsub := SubscribeDiagnostic(func(_ context.Context, m DiagnosticMessage) error {
		dmu.Lock()
		defer dmu.Unlock()
		diag = append(diag, m.Event)
		return nil
	})
	defer unsub()

	secret := errors.New("provider said: secret prompt")
	FireOnLanguageModelCallEnd(context.Background(), LanguageModelCallEndEvent{Settings: set, CallID: "c-diag", Content: []types.ContentPart{types.TextContent{Text: "reply"}}, ProviderMetadata: map[string]interface{}{"p": "x"}})
	FireOnError(context.Background(), TelemetryErrorEvent{Settings: set, CallID: "c-diag", Error: secret})
	FireOnEmbedEnd(context.Background(), EmbeddingModelCallEndEvent{Settings: set, CallID: "c-diag", Values: []string{"secret"}, Embeddings: [][]float64{{1}}, Error: secret})

	lm := rec.lmEnds[0]
	if lm.ProviderMetadata != nil {
		t.Errorf("provider metadata kept: %v", lm.ProviderMetadata)
	}
	if parts, _ := lm.Content.([]types.ContentPart); len(parts) != 1 {
		t.Errorf("content hidden with only recordInputs false: %v", lm.Content)
	}
	if rec.errs[0].Error == nil || rec.errs[0].Error.Error() != "AI SDK operation failed" || errors.Is(rec.errs[0].Error, secret) {
		t.Errorf("error = %v", rec.errs[0].Error)
	}
	ee := rec.embedEnds[0]
	if len(ee.Values) != 0 || len(ee.Embeddings) != 1 || ee.Error.Error() != "AI SDK operation failed" {
		t.Errorf("embed end = %+v", ee)
	}

	dmu.Lock()
	defer dmu.Unlock()
	for _, ev := range diag {
		switch e := ev.(type) {
		case LanguageModelCallEndEvent:
			if e.CallID == "c-diag" && e.ProviderMetadata != nil {
				t.Errorf("diagnostic subscriber saw provider metadata")
			}
		case TelemetryErrorEvent:
			if e.CallID == "c-diag" && strings.Contains(e.Error.Error(), "secret") {
				t.Errorf("diagnostic subscriber saw the raw error")
			}
		case EmbeddingModelCallEndEvent:
			if e.CallID == "c-diag" && len(e.Values) != 0 {
				t.Errorf("diagnostic subscriber saw embedding values")
			}
		}
	}
}

// Abort steps are filtered element by element (TS `steps.map(filter)`):
// outputs, response messages, provider metadata and bodies.
func TestR88carryFilterAbortSteps(t *testing.T) {
	rec := &r88carryRecorder{}
	FireOnAbort(context.Background(), TelemetryAbortEvent{Settings: r88carrySettings(rec, Bool(false), Bool(false)), CallID: "c", Steps: []types.StepResult{{
		StepNumber: 2, Text: "out", ReasoningText: "r", Content: []types.ContentPart{types.TextContent{Text: "out"}},
		ToolCalls: []types.ToolCall{{ID: "t"}}, ResponseMessages: []types.Message{{Role: types.RoleAssistant}},
		ExperimentalSteeringMessages: []types.Message{{Role: types.RoleUser}},
		ProviderMetadata:             map[string]interface{}{"a": 1},
		Request:                      types.StepRequest{Body: "req", Messages: []types.Message{{}}, URL: "https://x"},
		Response:                     types.StepResponse{ID: "id", Body: "res", Messages: []types.Message{{}}},
	}}})
	s := rec.aborts[0].Steps[0]
	if s.StepNumber != 2 || s.Text != "" || s.ReasoningText != "" || len(s.Content) != 0 || len(s.ToolCalls) != 0 ||
		len(s.ResponseMessages) != 0 || len(s.ExperimentalSteeringMessages) != 0 || s.ProviderMetadata != nil {
		t.Errorf("step not filtered: %+v", s)
	}
	if s.Request.Body != nil || len(s.Request.Messages) != 0 || s.Request.URL != "https://x" ||
		s.Response.Body != nil || len(s.Response.Messages) != 0 || s.Response.ID != "id" {
		t.Errorf("step metadata = %+v / %+v", s.Request, s.Response)
	}
}

type r88carryDecideResponse struct {
	ID   string
	Body interface{}
}

// TS decide.test.ts "telemetry" with recordInputs / recordOutputs false: the
// integration gets `state: {}` (root) / `state: []` (model call) and
// `questions: {}`; answers, provider metadata and the response body hidden.
func TestR88carryFilterDecideEvents(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, Bool(false), Bool(false))
	state := map[string]interface{}{"a": 1}
	questions := map[string]interface{}{"q": map[string]interface{}{"type": "boolean"}}
	FireOnDecideStart(context.Background(), DecideStartEvent{Settings: set, CallID: "d", State: state, Questions: questions, ProviderOptions: map[string]interface{}{"x": 1}})
	FireOnDecisionModelCallStart(context.Background(), DecisionModelCallStartEvent{Settings: set, CallID: "d", State: []interface{}{map[string]interface{}{"type": "json"}}, Questions: questions})
	FireOnDecisionModelCallEnd(context.Background(), DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: DecisionModelCallStartEvent{Settings: set, CallID: "d", State: "text state", Questions: questions},
		Answers:                     map[string]interface{}{"q": true}, ProviderMetadata: map[string]interface{}{"m": 1},
		Response: &r88carryDecideResponse{ID: "r", Body: "raw"},
	})
	FireOnDecideEnd(context.Background(), DecideEndEvent{DecideStartEvent: DecideStartEvent{Settings: set, CallID: "d", State: state, Questions: questions}, Answers: map[string]interface{}{"q": true}, Response: r88carryDecideResponse{ID: "r", Body: "raw"}})

	ds := rec.dStarts[0]
	if m, ok := ds.State.(map[string]interface{}); !ok || len(m) != 0 {
		t.Errorf("root state = %#v", ds.State)
	}
	if m, ok := ds.Questions.(map[string]interface{}); !ok || len(m) != 0 {
		t.Errorf("root questions = %#v", ds.Questions)
	}
	if len(ds.ProviderOptions) != 0 || ds.ProviderOptions == nil {
		t.Errorf("provider options = %#v", ds.ProviderOptions)
	}
	if l, ok := rec.dmStarts[0].State.([]interface{}); !ok || len(l) != 0 {
		t.Errorf("model call state = %#v", rec.dmStarts[0].State)
	}
	dm := rec.dmEnds[0]
	if dm.State != "" || dm.ProviderMetadata != nil {
		t.Errorf("model call end = %+v", dm)
	}
	if m, ok := dm.Answers.(map[string]interface{}); !ok || len(m) != 0 {
		t.Errorf("answers = %#v", dm.Answers)
	}
	if r := dm.Response.(*r88carryDecideResponse); r.Body != nil || r.ID != "r" {
		t.Errorf("model response = %+v", r)
	}
	if r := rec.dEnds[0].Response.(r88carryDecideResponse); r.Body != nil || r.ID != "r" {
		t.Errorf("decide response = %+v", r)
	}
	if state["a"] != 1 {
		t.Errorf("caller state mutated")
	}
}

// Ruling 5: the executeTool wrappers see the filtered input; the tool runs
// with the real one (TS execute takes no arguments).
func TestR88carryExecuteToolFilteredArgs(t *testing.T) {
	rec := &r88carryRecorder{}
	set := r88carrySettings(rec, Bool(false), nil)
	var toolGot map[string]interface{}
	_, err := FireExecuteToolWithSettings(context.Background(), set, "search", map[string]interface{}{"q": "secret"},
		func(_ context.Context, args map[string]interface{}) (interface{}, error) {
			toolGot = args
			return "ok", nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if rec.execArgs[0] != nil {
		t.Errorf("integration saw args %v", rec.execArgs[0])
	}
	if toolGot["q"] != "secret" {
		t.Errorf("tool got %v", toolGot)
	}
	// Recorded inputs: unchanged pass-through.
	rec2 := &r88carryRecorder{}
	_, _ = FireExecuteToolWithSettings(context.Background(), r88carrySettings(rec2, nil, Bool(false)), "search", map[string]interface{}{"q": "v"},
		func(context.Context, map[string]interface{}) (interface{}, error) { return nil, nil })
	if rec2.execArgs[0]["q"] != "v" {
		t.Errorf("args hidden with recordInputs on: %v", rec2.execArgs[0])
	}
}

// Built-in spans with a recording flag off: a failed call records the
// generic error text, and a hidden tool error records an error status with
// no message (TS recordErrorOnSpan / recordSpanError with `undefined`).
func TestR88carrySpansUseFilteredErrors(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		var integ TelemetryIntegration
		if legacy {
			integ = NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		} else {
			integ = NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}
		set := &Settings{RecordOutputs: Bool(false), Integrations: []TelemetryIntegration{integ}}
		id := t.Name()
		ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "p"})
		ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
		tctx := FireOnToolCallStart(ctx, TelemetryToolCallStartEvent{Settings: set, CallID: id, ToolCallID: "t1", ToolName: "search"})
		FireOnToolCallFinish(tctx, TelemetryToolCallFinishEvent{Settings: set, CallID: id, ToolCallID: "t1", ToolName: "search", Error: errors.New("tool leaked secret")})
		FireOnError(ctx, TelemetryErrorEvent{Settings: set, CallID: id, Error: errors.New("secret reply in error")})
		_ = tp.Shutdown(context.Background())
		var sawTool, sawRoot bool
		for _, s := range rec.Ended() {
			for _, ev := range s.Events() {
				for _, kv := range ev.Attributes {
					if strings.Contains(kv.Value.Emit(), "secret") {
						t.Errorf("legacy=%v span %s event leaks %v", legacy, s.Name(), kv)
					}
				}
			}
			if strings.Contains(s.Status().Description, "secret") {
				t.Errorf("legacy=%v span %s status %q", legacy, s.Name(), s.Status().Description)
			}
			isTool := strings.Contains(s.Name(), "toolCall") || strings.HasPrefix(s.Name(), "execute_tool")
			if isTool {
				sawTool = true
				if s.Status().Code != codes.Error || s.Status().Description != "" || len(s.Events()) != 0 {
					t.Errorf("legacy=%v tool span status = %+v events %d", legacy, s.Status(), len(s.Events()))
				}
			}
			if s.Name() == "ai.generateText" || s.Name() == "invoke_agent" || strings.HasPrefix(s.Name(), "invoke_agent") {
				if s.Status().Description == "AI SDK operation failed" {
					sawRoot = true
				}
			}
		}
		if !sawTool || !sawRoot {
			t.Errorf("legacy=%v: tool span %v, root span with generic error %v; spans: %v", legacy, sawTool, sawRoot, r88carrySpanNames(rec.Ended()))
		}
	}
}

func r88carrySpanNames(spans []sdktrace.ReadOnlySpan) []string {
	var out []string
	for _, s := range spans {
		out = append(out, s.Name()+"="+s.Status().Description)
	}
	return out
}

// Rerank start: query and documents are inputs (TS `query: ''`,
// `documents: []`).
func TestR88carryFilterRerankStart(t *testing.T) {
	rec := &r88carryRecorder{}
	FireOnRerankStart(context.Background(), RerankingModelCallStartEvent{Settings: r88carrySettings(rec, Bool(false), nil), CallID: "r", Query: "secret", Documents: []string{"doc"}, ModelID: "m"})
	got := rec.rrStarts[0]
	if docs, ok := got.Documents.([]string); got.Query != "" || !ok || len(docs) != 0 || got.ModelID != "m" {
		t.Errorf("rerank start = %+v", got)
	}
}

// Generic FilterEvent over a caller-defined struct (the pkg/ai callback
// events published on the diagnostic channel): rankings lose their
// documents, Responses their bodies, and a non-tool Result is cleared.
func TestR88carryFilterEventGenericShapes(t *testing.T) {
	type item struct {
		Score    float64
		Document interface{}
	}
	type ev struct {
		Values    []string
		Ranking   []item
		Responses []types.EmbeddingResponse
		Result    *struct{ X int }
	}
	in := ev{Values: []string{"v"}, Ranking: []item{{1, "doc"}, {2, map[string]interface{}{"k": "v"}}}, Responses: []types.EmbeddingResponse{{Body: "b", Headers: map[string]string{"h": "1"}}}, Result: &struct{ X int }{1}}
	out := FilterEvent(in, &Options{RecordInputs: Bool(false)})
	if len(out.Values) != 0 || out.Ranking[0].Document != "" || len(out.Ranking[1].Document.(map[string]interface{})) != 0 || out.Ranking[0].Score != 1 {
		t.Errorf("inputs: %+v", out)
	}
	if out.Responses[0].Body != nil || out.Responses[0].Headers["h"] != "1" || out.Result != nil {
		t.Errorf("metadata: %+v", out)
	}
	if in.Ranking[0].Document != "doc" || in.Responses[0].Body != "b" {
		t.Errorf("input mutated: %+v", in)
	}
	out = FilterEvent(in, &Options{RecordOutputs: Bool(false)})
	if len(out.Ranking) != 0 || len(out.Values) != 1 {
		t.Errorf("outputs: %+v", out)
	}
}
