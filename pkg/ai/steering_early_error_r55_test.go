package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText / startStreamText start the steering run before any
// validation and end it with 'error' when the call fails before its loop
// starts, so a waiting steering message is resolved as not delivered.
func TestSteeringEndsWhenCallFailsValidation(t *testing.T) {
	negative := -1
	for _, call := range []struct {
		name string
		run  func(*Steering) error
	}{
		{"GenerateText", func(s *Steering) error {
			_, err := GenerateText(context.Background(), GenerateTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "q", MaxRetries: &negative, ExperimentalSteering: s})
			return err
		}},
		{"StreamText", func(s *Steering) error {
			_, err := StreamText(context.Background(), StreamTextOptions{Model: &testutil.MockLanguageModel{}, Prompt: "q", MaxRetries: &negative, ExperimentalSteering: s})
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			steering := NewSteering()
			h := steering.Steer("hello")
			if err := call.run(steering); err == nil || !strings.Contains(err.Error(), "maxRetries must be >= 0") {
				t.Fatalf("err = %v, want the maxRetries error", err)
			}
			o := waitOutcome(t, h)
			if o.Delivered || o.Reason != SteeringError {
				t.Fatalf("outcome = %+v, want not delivered with reason error", o)
			}
			// The steering object is used up, and that error comes before
			// any other validation.
			if err := call.run(steering); err == nil || !strings.Contains(err.Error(), "already used by a generation") {
				t.Fatalf("second call err = %v, want the already-used steering error", err)
			}
		})
	}
}
