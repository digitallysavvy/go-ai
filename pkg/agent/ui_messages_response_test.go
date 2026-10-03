package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

const helloUIMessages = `[{"id":"u1","role":"user","parts":[{"type":"text","text":"hello"}]}]`

func TestPipeAgentUIStreamFromUIMessagesToResponse(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: simpleStreamModel()})

	var buf bytes.Buffer
	err := PipeAgentUIStreamFromUIMessagesToResponse(context.Background(), a, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: []byte(helloUIMessages),
	}, &buf)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "data: [DONE]\n\n") {
		t.Fatalf("missing [DONE] terminator:\n%s", buf.String())
	}
	chunks, err := ai.ReadUIMessageStream(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var text string
	for _, c := range chunks {
		if c["type"] == "text-delta" {
			text += c["delta"].(string)
		}
	}
	if text != "response" {
		t.Fatalf("streamed text = %q, want %q", text, "response")
	}
}

func TestPipeAgentUIStreamFromUIMessagesToResponse_InvalidMessages(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: simpleStreamModel()})
	var buf bytes.Buffer
	err := PipeAgentUIStreamFromUIMessagesToResponse(context.Background(), a, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: []byte(`not json`),
	}, &buf)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if buf.Len() != 0 {
		t.Fatalf("nothing should be written on a validation error, got %q", buf.String())
	}
}

func TestCreateAgentUIStreamResponseFromUIMessages(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: simpleStreamModel()})
	resp, err := CreateAgentUIStreamResponseFromUIMessages(context.Background(), a, CreateAgentUIStreamFromUIMessagesOptions{
		UIMessages: []byte(helloUIMessages),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Vercel-AI-UI-Message-Stream"); got != "v1" {
		t.Fatalf("X-Vercel-AI-UI-Message-Stream = %q, want v1", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"delta":"response"`) || !strings.HasSuffix(string(body), "data: [DONE]\n\n") {
		t.Fatalf("unexpected body:\n%s", body)
	}
}
