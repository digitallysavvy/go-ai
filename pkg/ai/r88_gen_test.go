package ai

import (
	"context"
	"reflect"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 88 gen tests (TS 0f3a85f92d).

// TS steering.test.ts "should deliver messages sent before the run starts in
// order" (1a1da8d68c): resolveTakenMessages returns { id, message } pairs.
func TestR88genSteeringResolveReturnsIDs(t *testing.T) {
	s := NewSteering()
	first := s.Steer("first", WithSteerID("steer-1"))
	second, err := s.SteerMessage(types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "second"}}}, WithSteerID("steer-2"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := startSteeringRun(s)
	if err != nil {
		t.Fatal(err)
	}
	taken := run.takeMessages()
	if len(taken) != 2 {
		t.Fatalf("taken = %d", len(taken))
	}
	got := run.resolveTakenMessages(0, taken)
	want := []steeringDelivered{{ID: "steer-1", Message: taken[0]}, {ID: "steer-2", Message: taken[1]}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivered = %+v, want %+v", got, want)
	}
	if o := first.Outcome(); !o.Delivered || o.ID != "steer-1" || o.StepNumber != 0 {
		t.Fatalf("first outcome = %+v", o)
	}
	if o := second.Outcome(); !o.Delivered || o.ID != "steer-2" || o.StepNumber != 0 {
		t.Fatalf("second outcome = %+v", o)
	}
}

// TS steering.test.ts "should not deliver messages that are not sent to the
// model" (1a1da8d68c).
func TestR88genSteeringResolveOnlySentWithIDs(t *testing.T) {
	s := NewSteering()
	kept := s.Steer("kept", WithSteerID("steer-1"))
	removed := s.Steer("removed", WithSteerID("steer-2"))
	run, err := startSteeringRun(s)
	if err != nil {
		t.Fatal(err)
	}
	keptMessage := run.takeMessages()[0]
	got := run.resolveTakenMessages(1, []types.Message{keptMessage})
	want := []steeringDelivered{{ID: "steer-1", Message: keptMessage}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivered = %+v, want %+v", got, want)
	}
	if o := kept.Outcome(); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("kept outcome = %+v", o)
	}
	if o := removed.Outcome(); o.Delivered || o.ID != "steer-2" || o.Reason != SteeringRemovedByPrepareStep {
		t.Fatalf("removed outcome = %+v", o)
	}
	if msgs := steeringDeliveredMessages(got); !reflect.DeepEqual(msgs, []types.Message{keptMessage}) {
		t.Fatalf("messages = %+v", msgs)
	}
	if steeringDeliveredMessages(nil) != nil {
		t.Fatal("no delivered messages must stay nil")
	}
}

// TS decide.test.ts "forwards optional Box instructions: %j" (c70adf670b):
// `{}`, `{instructions: undefined}` and `{instructions: null}` are all a nil
// Instructions in Go and reach the model unchanged.
func TestR88genDecideForwardsOptionalBoxInstructions(t *testing.T) {
	want := provider.DecisionBox{Top: 0.25, Bottom: 0.82, Left: 0.44, Right: 0.84}
	model := boxMock(&want)
	questions := map[string]provider.DecisionQuestion{"target": {Type: "box"}}
	res, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: "", Questions: questions})
	if err != nil {
		t.Fatalf("box without instructions: %v", err)
	}
	if len(model.calls) != 1 || !reflect.DeepEqual(model.calls[0].Questions, questions) {
		t.Fatalf("questions sent = %+v", model.calls)
	}
	if got := res.Answers["target"]; got.Type != "box" || got.Box == nil || *got.Box != want {
		t.Fatalf("answer = %#v", got)
	}
}

// TS decide.test.ts "forwards null Boolean criteria with instructions"
// (c70adf670b).
func TestR88genDecideForwardsNullBooleanCriteria(t *testing.T) {
	p := 0.9
	answer := provider.DecisionAnswer{Type: "boolean", Probability: &p}
	model := &mockDecisionModel{
		providerName: "test", modelID: "m",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"refund": answer}}, nil
		},
	}
	questions := map[string]provider.DecisionQuestion{"refund": {Type: "boolean", Instructions: "Refund?", Criteria: nil}}
	res, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, State: "text", Questions: questions})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.calls) != 1 || !reflect.DeepEqual(model.calls[0].Questions, questions) {
		t.Fatalf("questions sent = %+v", model.calls)
	}
	if got := res.Answers["refund"]; !reflect.DeepEqual(got, answer) {
		t.Fatalf("answer = %#v", got)
	}
}

// TS steering.ts 8: default steering ids are createIdGenerator({ prefix:
// 'steer', size: 24 }), which the steering-message stream part and UI part
// now expose (1a1da8d68c).
func TestR88genSteeringDefaultIDShape(t *testing.T) {
	re := regexp.MustCompile(`^steer-[0-9A-Za-z]{24}$`)
	s := NewSteering()
	for _, h := range []*SteerHandle{s.Steer("a"), s.Queue("b")} {
		run, _ := startSteeringRun(s)
		if run != nil {
			run.end(SteeringFinished)
		}
		if id := h.Outcome().ID; !re.MatchString(id) {
			t.Fatalf("id = %q", id)
		}
	}
}
