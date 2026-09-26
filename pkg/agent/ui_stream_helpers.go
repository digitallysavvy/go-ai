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

// CreateAgentUIStreamFromUIMessagesOptions configures
// CreateAgentUIStreamFromUIMessages.
type CreateAgentUIStreamFromUIMessagesOptions struct {
	// UIMessages are the input UI messages (for example []ai.UIMessage,
	// decoded JSON, or raw JSON bytes/string). Validated against the agent's
	// tools via ai.ValidateUIMessagesForAgent before conversion.
	UIMessages interface{}

	// ExperimentalRefineToolInput reconstructs approved tool input from an
	// approval's inputSchemaInput during validation. Optional.
	ExperimentalRefineToolInput map[string]ai.ToolInputRefiner

	// AgentOptions carries the remaining agent stream options (RuntimeContext,
	// ToolsContext, CallOptions, AbortSignal-equivalents, etc). Its Prompt and
	// Messages fields are ignored: the converted UI messages are used instead.
	AgentOptions AgentStreamOptions

	// UIMessageStream carries ToUIMessageStream/CreateUIMessageStream options
	// (message metadata, ID generation, callbacks, etc). Tools defaults to
	// agent.Tools() when nil. OriginalMessages defaults to the validated UI
	// messages (converted to UIMessageChunk) when nil, kept for backwards
	// compatibility with callers that already computed their own chunks (TS
	// `originalMessages` escape hatch).
	UIMessageStream ai.UIMessageStreamResultOptions
}

// CreateAgentUIStreamFromUIMessages runs the agent from a list of UI messages
// (for example persisted chat history from a frontend) and streams the
// output as UI message chunks. Mirrors TS createAgentUIStream: it validates
// the UI messages against the agent's tools (ValidateUIMessagesForAgent),
// converts them to model messages (ConvertToModelMessages), runs the agent,
// and converts the resulting stream back to UI message chunks
// (ToUIMessageStream) with originalMessages set to the validated input so
// the reducer can correctly diff/update existing tool parts.
func CreateAgentUIStreamFromUIMessages(ctx context.Context, agent *ToolLoopAgent, opts CreateAgentUIStreamFromUIMessagesOptions) (<-chan ai.UIMessageChunk, <-chan error, error) {
	if agent == nil {
		return nil, nil, fmt.Errorf("agent is required")
	}

	validated, err := ai.ValidateUIMessagesForAgent(ctx, ai.ValidateUIMessagesOptions{
		Messages:                    opts.UIMessages,
		Tools:                       agent.Tools(),
		ExperimentalRefineToolInput: opts.ExperimentalRefineToolInput,
	})
	if err != nil {
		return nil, nil, err
	}

	modelMessages, err := ai.ConvertToModelMessages(ctx, validated, ai.ConvertToModelMessagesOptions{
		Tools: agent.Tools(),
	})
	if err != nil {
		return nil, nil, err
	}

	streamOptions := opts.AgentOptions
	streamOptions.Prompt = ""
	streamOptions.Messages = modelMessages
	// UI conversations can include a system-role message (from a UI system
	// message); the agent's own system/instructions are combined separately.
	streamOptions.AllowSystemInMessages = true

	result, err := agent.Stream(ctx, streamOptions)
	if err != nil {
		return nil, nil, err
	}

	uiOptions := opts.UIMessageStream
	if uiOptions.Tools == nil {
		uiOptions.Tools = agent.Tools()
	}
	if uiOptions.OriginalMessages == nil {
		originalMessages := make([]ai.UIMessageChunk, 0, len(validated))
		for _, message := range validated {
			chunk, chunkErr := message.ToChunk()
			if chunkErr != nil {
				return nil, nil, chunkErr
			}
			originalMessages = append(originalMessages, chunk)
		}
		uiOptions.OriginalMessages = originalMessages
	}

	chunks, errs := ai.CreateUIMessageStream(ctx, result, uiOptions)
	return chunks, errs, nil
}
