package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// ---------------------------------------------------------------------------
// Ports of ai/packages/ai/src/agent/create-agent-ui-stream.test.ts (#101):
// CreateAgentUIStreamFromUIMessages is the Go equivalent of TS
// createAgentUIStream (validateUIMessagesForAgent -> convertToModelMessages
// -> agent.Stream -> ToUIMessageStream with originalMessages).
// ---------------------------------------------------------------------------

func currentTestTool() types.Tool {
	return types.Tool{
		Name: "current",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"current": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"current"},
		},
		OutputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"result": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"result"},
		},
	}
}

func simpleStreamModel() *functionalAgentLanguageModel {
	return &functionalAgentLanguageModel{
		doStream: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "response"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func drainUIChunks(t *testing.T, chunks <-chan ai.UIMessageChunk, errs <-chan error) []ai.UIMessageChunk {
	t.Helper()
	var got []ai.UIMessageChunk
	for c := range chunks {
		got = append(got, c)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	}
	return got
}

// ports "should reject stale terminal input for a currently available tool"
func TestCreateAgentUIStreamFromUIMessages_RejectsStaleTerminalInput(t *testing.T) {
	toolLoopAgent := NewToolLoopAgent(AgentConfig{
		Model: simpleStreamModel(),
		Tools: []types.Tool{currentTestTool()},
	})

	_, _, err := CreateAgentUIStreamFromUIMessages(context.Background(), toolLoopAgent, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: json.RawMessage(`[{
			"id": "assistant-1",
			"role": "assistant",
			"parts": [{
				"type": "tool-current",
				"toolCallId": "call-1",
				"state": "output-available",
				"input": {"previous": "value"},
				"output": {"result": "done"}
			}]
		}]`),
	})
	if err == nil {
		t.Fatal("expected a validation error for stale terminal input, got nil")
	}
}

// ports "should expose unavailable terminal tools as dynamic parts to callbacks"
func TestCreateAgentUIStreamFromUIMessages_ExposesUnavailableTerminalToolsAsDynamic(t *testing.T) {
	toolLoopAgent := NewToolLoopAgent(AgentConfig{
		Model: simpleStreamModel(),
		Tools: []types.Tool{currentTestTool()},
	})

	var endMessages []ai.UIMessageChunk
	chunks, errs, err := CreateAgentUIStreamFromUIMessages(context.Background(), toolLoopAgent, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: json.RawMessage(`[
			{
				"id": "assistant-1",
				"role": "assistant",
				"parts": [{
					"type": "tool-removed",
					"toolCallId": "call-1",
					"state": "output-available",
					"input": {"previous": "value"},
					"output": {"result": "done"}
				}]
			},
			{
				"id": "user-1",
				"role": "user",
				"parts": [{"type": "text", "text": "continue"}]
			}
		]`),
		UIMessageStream: ai.UIMessageStreamResultOptions{
			OnEnd: func(ctx map[string]interface{}) {
				if msgs, ok := ctx["messages"].([]ai.UIMessageChunk); ok {
					endMessages = msgs
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	drainUIChunks(t, chunks, errs)

	if len(endMessages) == 0 {
		t.Fatal("expected OnEnd to be called with messages")
	}
	firstParts, _ := endMessages[0]["parts"].([]interface{})
	if len(firstParts) == 0 {
		t.Fatalf("expected parts on first message, got: %#v", endMessages[0])
	}
	part, ok := firstParts[0].(ai.UIMessageChunk)
	if !ok {
		if m, mapOK := firstParts[0].(map[string]interface{}); mapOK {
			part = ai.UIMessageChunk(m)
		}
	}
	if part["type"] != "dynamic-tool" {
		t.Fatalf("part.type = %v, want dynamic-tool: %#v", part["type"], part)
	}
	if part["toolName"] != "removed" {
		t.Fatalf("part.toolName = %v, want removed", part["toolName"])
	}
	if part["toolCallId"] != "call-1" {
		t.Fatalf("part.toolCallId = %v, want call-1", part["toolCallId"])
	}
	if part["state"] != "output-available" {
		t.Fatalf("part.state = %v, want output-available", part["state"])
	}
}

// ports "should expose terminal tool history as dynamic parts when tools are omitted"
func TestCreateAgentUIStreamFromUIMessages_ExposesTerminalHistoryAsDynamicWhenToolsOmitted(t *testing.T) {
	toolLoopAgent := NewToolLoopAgent(AgentConfig{
		Model: simpleStreamModel(),
		// No Tools registered at all.
	})

	var endMessages []ai.UIMessageChunk
	chunks, errs, err := CreateAgentUIStreamFromUIMessages(context.Background(), toolLoopAgent, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: json.RawMessage(`[
			{
				"id": "assistant-1",
				"role": "assistant",
				"parts": [{
					"type": "tool-removed",
					"toolCallId": "call-1",
					"state": "output-available",
					"input": {"previous": "value"},
					"output": {"result": "done"}
				}]
			},
			{
				"id": "user-1",
				"role": "user",
				"parts": [{"type": "text", "text": "continue"}]
			}
		]`),
		UIMessageStream: ai.UIMessageStreamResultOptions{
			OnEnd: func(ctx map[string]interface{}) {
				if msgs, ok := ctx["messages"].([]ai.UIMessageChunk); ok {
					endMessages = msgs
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	drainUIChunks(t, chunks, errs)

	if len(endMessages) == 0 {
		t.Fatal("expected OnEnd to be called with messages")
	}
	firstParts, _ := endMessages[0]["parts"].([]interface{})
	if len(firstParts) == 0 {
		t.Fatalf("expected parts on first message, got: %#v", endMessages[0])
	}
	part, ok := firstParts[0].(ai.UIMessageChunk)
	if !ok {
		if m, mapOK := firstParts[0].(map[string]interface{}); mapOK {
			part = ai.UIMessageChunk(m)
		}
	}
	if part["type"] != "dynamic-tool" || part["toolName"] != "removed" {
		t.Fatalf("unexpected part: %#v", part)
	}
}

func TestCreateAgentUIStreamFromUIMessages_RequiresAgent(t *testing.T) {
	if _, _, err := CreateAgentUIStreamFromUIMessages(context.Background(), nil, CreateAgentUIStreamFromUIMessagesOptions{}); err == nil {
		t.Fatal("expected error for nil agent")
	}
}
