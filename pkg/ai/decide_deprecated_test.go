//nolint:staticcheck // exercises the deprecated aliases on purpose
package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Ports TS packages/ai/src/decide/deprecated-aliases.test.ts.

// legacyEvaluationModel only implements the pre-rename DoEvaluate contract.
type legacyEvaluationModel struct {
	mu    sync.Mutex
	calls int
}

func (m *legacyEvaluationModel) SpecificationVersion() string     { return "v4" }
func (m *legacyEvaluationModel) Provider() string                 { return "legacy" }
func (m *legacyEvaluationModel) ModelID() string                  { return "legacy-model" }
func (m *legacyEvaluationModel) SupportedQuestionTypes() []string { return []string{"choice"} }
func (m *legacyEvaluationModel) DoEvaluate(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"route": {Type: "choice", Choice: "billing"}}}, nil
}

type legacyEvaluationProvider struct{ model provider.EvaluationModel }

func (p legacyEvaluationProvider) EvaluationModel(id string) (provider.EvaluationModel, error) {
	return p.model, nil
}

func legacyQuestions() map[string]provider.EvaluationQuestion {
	return map[string]provider.EvaluationQuestion{
		"route": {Type: "choice", Instructions: "Team?", Criteria: map[string]interface{}{"billing": nil, "support": nil}},
	}
}

func TestExperimentalEvaluate_DelegatesAndAcceptsLegacyModel(t *testing.T) {
	model := &legacyEvaluationModel{}
	out, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{Model: model, State: "message", Questions: legacyQuestions()})
	if err != nil {
		t.Fatal(err)
	}
	if out.Answers["route"].Choice != "billing" || model.calls != 1 {
		t.Fatalf("answers=%+v calls=%d", out.Answers, model.calls)
	}
	// The same legacy model also works with the new entry point.
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: "message", Questions: legacyQuestions()}); err != nil {
		t.Fatal(err)
	}
	if model.calls != 2 {
		t.Fatalf("calls = %d, want 2", model.calls)
	}
}

func TestExperimentalDecide_ResolvesLegacyProvider(t *testing.T) {
	model := &legacyEvaluationModel{}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: "model", Provider: legacyEvaluationProvider{model: model}, State: "message", Questions: legacyQuestions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 {
		t.Fatalf("calls = %d", model.calls)
	}
}

// deprecatedOnlyIntegration implements only the pre-rename handlers.
type deprecatedOnlyIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu  sync.Mutex
	ops []string
}

func (i *deprecatedOnlyIntegration) record(op string) {
	i.mu.Lock()
	i.ops = append(i.ops, op)
	i.mu.Unlock()
}
func (i *deprecatedOnlyIntegration) OnEvaluateStart(ctx context.Context, e telemetry.EvaluateStartEvent) context.Context {
	i.record(e.OperationID)
	return ctx
}
func (i *deprecatedOnlyIntegration) OnEvaluateEnd(_ context.Context, e telemetry.EvaluateEndEvent) {
	i.record(e.OperationID)
}
func (i *deprecatedOnlyIntegration) OnEvaluationModelCallStart(_ context.Context, e telemetry.EvaluationModelCallStartEvent) {
	i.record(e.OperationID)
}
func (i *deprecatedOnlyIntegration) OnEvaluationModelCallEnd(_ context.Context, e telemetry.EvaluationModelCallEndEvent) {
	i.record(e.OperationID)
}

// bothIntegration implements current and deprecated handlers.
type bothIntegration struct {
	deprecatedOnlyIntegration
	current int
}

func (i *bothIntegration) OnDecideStart(ctx context.Context, _ telemetry.DecideStartEvent) context.Context {
	i.mu.Lock()
	i.current++
	i.mu.Unlock()
	return ctx
}
func (i *bothIntegration) OnDecideEnd(context.Context, telemetry.DecideEndEvent) {
	i.mu.Lock()
	i.current++
	i.mu.Unlock()
}
func (i *bothIntegration) OnDecisionModelCallStart(context.Context, telemetry.DecisionModelCallStartEvent) {
	i.mu.Lock()
	i.current++
	i.mu.Unlock()
}
func (i *bothIntegration) OnDecisionModelCallEnd(context.Context, telemetry.DecisionModelCallEndEvent) {
	i.mu.Lock()
	i.current++
	i.mu.Unlock()
}

func TestDecide_DeprecatedTelemetryHandlersCalledOnceWithDecisionOperationNames(t *testing.T) {
	integ := &deprecatedOnlyIntegration{}
	_, err := ExperimentalEvaluate(context.Background(), EvaluateOptions{
		Model: &legacyEvaluationModel{}, State: "message", Questions: legacyQuestions(),
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
	})
	if err != nil {
		t.Fatal(err)
	}
	integ.mu.Lock()
	defer integ.mu.Unlock()
	want := []string{"ai.decide", "ai.decide.doDecide", "ai.decide.doDecide", "ai.decide"}
	if len(integ.ops) != 4 {
		t.Fatalf("ops = %v", integ.ops)
	}
	for i := range want {
		if integ.ops[i] != want[i] {
			t.Fatalf("ops = %v, want %v", integ.ops, want)
		}
	}
}

func TestDecide_CurrentTelemetryHandlerPreferredOverDeprecated(t *testing.T) {
	integ := &bothIntegration{}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: &legacyEvaluationModel{}, State: "message", Questions: legacyQuestions(),
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
	})
	if err != nil {
		t.Fatal(err)
	}
	integ.mu.Lock()
	defer integ.mu.Unlock()
	if integ.current != 4 || len(integ.ops) != 0 {
		t.Fatalf("current=%d deprecated=%v", integ.current, integ.ops)
	}
}

// stateRecordingLegacyModel records the State a DoEvaluate-only model sees.
type stateRecordingLegacyModel struct {
	legacyEvaluationModel
	state interface{}
}

func (m *stateRecordingLegacyModel) DoEvaluate(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	m.mu.Lock()
	m.state = opts.State
	m.mu.Unlock()
	return m.legacyEvaluationModel.DoEvaluate(ctx, opts)
}

func TestExperimentalDecide_LegacyModelKeepsPreMultimodalStateShape(t *testing.T) {
	cases := []struct {
		name  string
		state interface{}
		want  interface{}
	}{
		{"string", "hello", "hello"},
		{"object", map[string]interface{}{"a": 1.0}, map[string]interface{}{"a": 1.0}},
		{"array", []interface{}{1.0, 2.0}, []interface{}{1.0, 2.0}},
		{"single text part", []provider.DecisionStatePart{provider.DecisionTextPart("hi")}, "hi"},
	}
	for _, tc := range cases {
		for _, entry := range []string{"evaluate", "decide"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				model := &stateRecordingLegacyModel{}
				opts := DecideOptions{Model: model, State: tc.state, Questions: legacyQuestions()}
				var err error
				if entry == "evaluate" {
					_, err = ExperimentalEvaluate(context.Background(), opts)
				} else {
					_, err = ExperimentalDecide(context.Background(), opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(model.state, tc.want) {
					t.Fatalf("legacy model saw %#v (%T), want %#v", model.state, model.state, tc.want)
				}
			})
		}
	}

	t.Run("multi-part state is passed as parts", func(t *testing.T) {
		model := &stateRecordingLegacyModel{}
		parts := []provider.DecisionStatePart{provider.DecisionTextPart("a"), provider.DecisionJSONPart(map[string]interface{}{"b": 1.0})}
		if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: parts, Questions: legacyQuestions()}); err != nil {
			t.Fatal(err)
		}
		got, ok := model.state.([]provider.DecisionStatePart)
		if !ok || len(got) != 2 {
			t.Fatalf("state = %#v, want 2 parts", model.state)
		}
	})
}
