package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// cr-r87e-stream-to-gen 3: TS pipeAgentUIStreamToResponse /
// createAgentUIStreamResponse end an errored stream without "data: [DONE]".
// A UI callback that panics fails the agent's UI stream.
func TestR87eintegAgentUIHelpersEndFailedStreamWithoutDone(t *testing.T) {
	opts := func() CreateAgentUIStreamFromUIMessagesOptions {
		return CreateAgentUIStreamFromUIMessagesOptions{
			UIMessages: []byte(helloUIMessages),
			UIMessageStream: ai.UIMessageStreamResultOptions{
				MessageMetadata: func(map[string]interface{}) map[string]interface{} { panic("metadata failed") },
			},
		}
	}
	a := NewToolLoopAgent(AgentConfig{Model: simpleStreamModel()})
	var buf bytes.Buffer
	err := PipeAgentUIStreamFromUIMessagesToResponse(context.Background(), a, opts(), &buf)
	if err == nil {
		t.Fatalf("pipe: want the callback failure, body:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "data: [DONE]") {
		t.Fatalf("failed stream ended with [DONE]:\n%s", buf.String())
	}

	resp, err := CreateAgentUIStreamResponseFromUIMessages(context.Background(), a, opts())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("failed response ended with [DONE]:\n%s", body)
	}
}
