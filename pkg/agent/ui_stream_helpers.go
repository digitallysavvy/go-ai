package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// CreateAgentUIStream starts an agent stream and converts it to UI message chunks.
func CreateAgentUIStream(ctx context.Context, agent *ToolLoopAgent, opts AgentStreamOptions) (<-chan ai.UIMessageChunk, <-chan error, error) {
	if agent == nil {
		return nil, nil, fmt.Errorf("agent is required")
	}
	stream, err := agent.Stream(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	chunks, errs := ai.CreateUIMessageStream(ctx, stream)
	return chunks, errs, nil
}

// CreateAgentUIStreamResponse starts an agent stream and returns an HTTP response body.
func CreateAgentUIStreamResponse(ctx context.Context, agent *ToolLoopAgent, opts AgentStreamOptions) (*http.Response, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent is required")
	}
	stream, err := agent.Stream(ctx, opts)
	if err != nil {
		return nil, err
	}
	return ai.CreateUIMessageStreamResponse(ctx, stream)
}

// PipeAgentUIStreamToResponse starts an agent stream and writes UI chunks to writer.
func PipeAgentUIStreamToResponse(ctx context.Context, agent *ToolLoopAgent, opts AgentStreamOptions, w io.Writer) error {
	if agent == nil {
		return fmt.Errorf("agent is required")
	}
	stream, err := agent.Stream(ctx, opts)
	if err != nil {
		return err
	}
	return ai.PipeUIMessageStreamToResponse(ctx, stream, w)
}
