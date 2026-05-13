package ai

import (
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestCallbackEventStructsCarryStepAndResponseFields(t *testing.T) {
	step := types.StepResult{
		StepNumber:    1,
		Text:          "hello",
		ReasoningText: "think",
	}
	resp := GenerateStepResponse{
		ID:        "resp-1",
		Timestamp: time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC),
		ModelID:   "m1",
		Headers:   map[string]string{"x-request-id": "req-1"},
	}
	event := OnFinishEvent{
		CallID:          "call-1",
		StepNumber:      1,
		Text:            "hello",
		ReasoningText:   "think",
		Steps:           []types.StepResult{step},
		ResponseHeaders: resp.Headers,
		Response:        resp,
	}

	if event.CallID != "call-1" || event.StepNumber != 1 {
		t.Fatalf("unexpected basic fields: %+v", event)
	}
	if len(event.Steps) != 1 || event.Steps[0].ReasoningText != "think" {
		t.Fatalf("unexpected step payload: %+v", event.Steps)
	}
	if event.Response.ID != "resp-1" || event.ResponseHeaders["x-request-id"] != "req-1" {
		t.Fatalf("unexpected response metadata: response=%+v headers=%+v", event.Response, event.ResponseHeaders)
	}
}
