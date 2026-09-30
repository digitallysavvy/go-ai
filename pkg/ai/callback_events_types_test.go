package ai

import (
	"encoding/json"
	"errors"
	"strings"
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

// TestToolCallEventDeprecatedFieldsExcludedFromJSON verifies that the legacy
// tool-event fields kept only for Go API compatibility (StepNumber,
// ModelProvider, ModelID, Args, Result, Error, DurationMs) are tagged
// `json:"-"` and therefore never appear in a serialized OnToolCallStartEvent
// or OnToolCallFinishEvent, so a serialized event matches the TypeScript
// event shape exactly (review condition from the P0-3 handoff).
func TestToolCallEventDeprecatedFieldsExcludedFromJSON(t *testing.T) {
	startEvent := OnToolCallStartEvent{
		CallID:        "call-1",
		ToolCallID:    "tc-1",
		ToolName:      "get_weather",
		Args:          map[string]any{"city": "NY"},
		StepNumber:    3,
		ModelProvider: "openai",
		ModelID:       "gpt-5",
	}
	startJSON, err := json.Marshal(startEvent)
	if err != nil {
		t.Fatalf("marshal OnToolCallStartEvent: %v", err)
	}
	for _, deprecated := range []string{"Args", "StepNumber", "ModelProvider", "ModelID"} {
		if strings.Contains(string(startJSON), `"`+deprecated+`"`) {
			t.Errorf("OnToolCallStartEvent JSON must not contain deprecated field %q: %s", deprecated, startJSON)
		}
	}

	finishEvent := OnToolCallFinishEvent{
		CallID:          "call-1",
		ToolCallID:      "tc-1",
		ToolName:        "get_weather",
		Args:            map[string]any{"city": "NY"},
		Result:          "sunny",
		Error:           errors.New("boom"),
		DurationMs:      42,
		StepNumber:      3,
		ModelProvider:   "openai",
		ModelID:         "gpt-5",
		ToolExecutionMs: 42,
	}
	finishJSON, err := json.Marshal(finishEvent)
	if err != nil {
		t.Fatalf("marshal OnToolCallFinishEvent: %v", err)
	}
	for _, deprecated := range []string{"Args", "Result", "Error", "DurationMs", "StepNumber", "ModelProvider", "ModelID"} {
		if strings.Contains(string(finishJSON), `"`+deprecated+`"`) {
			t.Errorf("OnToolCallFinishEvent JSON must not contain deprecated field %q: %s", deprecated, finishJSON)
		}
	}
	// The non-deprecated ToolExecutionMs field must still round-trip.
	if !strings.Contains(string(finishJSON), "ToolExecutionMs") {
		t.Errorf("expected ToolExecutionMs to still be present: %s", finishJSON)
	}
}
