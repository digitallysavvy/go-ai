package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS create-agent-ui-stream.test.ts 410 (11323f9894): many malformed parts
// are rejected with one bounded validation error, without running the agent.
func TestR92streamAgentUIStreamRejectsManyMalformedParts(t *testing.T) {
	var calls atomic.Int32
	model := &functionalAgentLanguageModel{doStream: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	}}
	parts := make([]interface{}, 2500)
	for i := range parts {
		parts[i] = map[string]interface{}{}
	}
	uiMessages := []interface{}{map[string]interface{}{"id": "user-1", "role": "user", "parts": parts}}
	raw, _ := json.Marshal(uiMessages)
	_, err := CreateAgentUIStreamResponseFromUIMessages(context.Background(), NewToolLoopAgent(AgentConfig{Model: model}), CreateAgentUIStreamFromUIMessagesOptions{UIMessages: uiMessages})
	var verr *providererrors.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %T %v, want *ValidationError", err, err)
	}
	if len(err.Error()) >= len(raw)+1000 {
		t.Fatalf("error length %d, input %d", len(err.Error()), len(raw))
	}
	if calls.Load() != 0 {
		t.Fatalf("doStream calls = %d", calls.Load())
	}
}
