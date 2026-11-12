package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Round 92 gen tests (TS 2ebc78b654).

// create-agent-ui-stream.test.ts 410 'should reject many malformed parts
// without running the agent' (11323f9894): 2500 malformed parts fail
// validation with one TypeValidationError whose message carries the value
// once, and the model is never called.
func TestR92genAgentUIStreamRejectsManyMalformedParts(t *testing.T) {
	streamCalls := 0
	model := &functionalAgentLanguageModel{doStream: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		streamCalls++
		return nil, errors.New("not called")
	}}
	parts := make([]map[string]interface{}, 2500)
	for i := range parts {
		parts[i] = map[string]interface{}{}
	}
	raw, err := json.Marshal([]map[string]interface{}{{"id": "user-1", "role": "user", "parts": parts}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateAgentUIStreamResponseFromUIMessages(context.Background(), NewToolLoopAgent(AgentConfig{Model: model}), CreateAgentUIStreamFromUIMessagesOptions{UIMessages: json.RawMessage(raw)})
	var validationErr *providererrors.TypeValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %v (%T), want a TypeValidationError", err, err)
	}
	if n := len(err.Error()); n >= len(raw)+1000 {
		t.Fatalf("error message is %d bytes, want under %d", n, len(raw)+1000)
	}
	if strings.Count(err.Error(), `"user-1"`) > 1 {
		t.Fatal("the value appears more than once in the message")
	}
	if streamCalls != 0 {
		t.Fatalf("doStream calls = %d, want 0", streamCalls)
	}
}
