package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ChatTransport sends a chat's message history to a remote or embedded
// backend and receives a stream of UI message chunks describing the
// response. It mirrors the TypeScript SDK's `ChatTransport<UIMessage>`
// interface used by runAgentTUI's `transport` option (ai/packages/tui/src/
// run-agent-tui.ts, agent-tui-runner.ts).
//
// This is the cross-package core concept referenced by pkg/tui's
// RunAgentTUIOptions.Transport. Concrete transports in this SDK — for
// example pkg/workflow.WorkflowChatTransport and
// pkg/langchain.LangSmithDeploymentTransport — implement the same shape
// (a SendMessages method that returns a channel of UIMessageChunk plus an
// error channel) without necessarily satisfying this interface verbatim,
// since each adapts a different remote protocol.
type ChatTransport interface {
	// SendMessages submits the current message history and returns a
	// channel of UI message chunks describing the response, plus an error
	// channel for transport-level failures. Both channels are closed when
	// the exchange completes. Implementations should stop sending on ctx
	// cancellation.
	SendMessages(ctx context.Context, req ChatTransportSendMessagesRequest) (<-chan UIMessageChunk, <-chan error)
}

// ChatTransportSendMessagesRequest is the request payload passed to
// ChatTransport.SendMessages. Field names mirror
// pkg/workflow.SendMessagesOptions (ChatID, MessageID, Trigger, Messages);
// Messages uses []types.Message — the same parameter type as
// pkg/langchain.LangSmithDeploymentTransport.SendMessages — since that is
// this SDK's canonical, provider-agnostic message history representation
// (the TypeScript SDK's ChatTransport instead carries []UIMessage, but the
// Go SDK has no cross-package UI-message-native history type used for
// conversation state; types.Message is the established convention).
type ChatTransportSendMessagesRequest struct {
	// ChatID identifies the conversation. Callers that run a single
	// conversation per process (such as pkg/tui's AgentTUIRunner) typically
	// generate this once per runner instance.
	ChatID string

	// MessageID is the id of the message being responded to, when known.
	MessageID string

	// Trigger describes why SendMessages was called (for example
	// "submit-message" or "regenerate-message"), matching the TypeScript
	// ChatTransport's `trigger` field.
	Trigger string

	// Messages is the full message history sent to the transport.
	Messages []types.Message
}
