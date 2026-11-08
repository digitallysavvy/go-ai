package ai

import (
	"reflect"
	"testing"

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
