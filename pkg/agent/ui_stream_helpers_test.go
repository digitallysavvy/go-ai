package agent

import (
	"bytes"
	"context"
	"testing"
)

func TestAgentUIHelpers_RequireAgent(t *testing.T) {
	if _, _, err := CreateAgentUIStream(context.Background(), nil, AgentStreamOptions{}); err == nil {
		t.Fatal("CreateAgentUIStream() expected error for nil agent")
	}
	if _, err := CreateAgentUIStreamResponse(context.Background(), nil, AgentStreamOptions{}); err == nil {
		t.Fatal("CreateAgentUIStreamResponse() expected error for nil agent")
	}
	var buf bytes.Buffer
	if err := PipeAgentUIStreamToResponse(context.Background(), nil, AgentStreamOptions{}, &buf); err == nil {
		t.Fatal("PipeAgentUIStreamToResponse() expected error for nil agent")
	}
}
