package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/google/uuid"
)

// AgentTUIRunnerOptions configures AgentTUIRunner.
//
// Exactly one of Agent or Transport must be set (TS run-agent-tui.ts's
// `agent`/`transport` mutually-exclusive union). When Transport is set, ChatID
// identifies the conversation sent on every SendMessages call; if empty, a
// random per-runner id is generated (mirrors TS AgentTUIRunner's
// `chatId = generateId()`).
type AgentTUIRunnerOptions struct {
	Agent              agent.Agent
	Transport          ai.ChatTransport
	ChatID             string
	Renderer           AgentTUIRenderer
	Input              io.Reader
	Title              string
	Tools              TerminalPartDisplayMode
	Reasoning          TerminalPartDisplayMode
	ResponseStatistics ResponseStatisticsMode
	ContextSize        int
	Sandbox            interface{}
}

// AgentTUIRenderer is the rendering contract used by AgentTUIRunner.
type AgentTUIRenderer interface {
	RenderStream(ctx context.Context, result AgentTUIStreamResult, options TerminalSessionOptions) ([]types.Message, error)
}

type AgentTUIPromptReader interface {
	ReadPrompt(ctx context.Context, options TerminalSessionOptions) (string, bool, error)
}

type AgentTUIToolApprovalReader interface {
	ReadToolApproval(ctx context.Context, request AgentTUIToolApprovalRequest, options TerminalSessionOptions) (AgentTUIToolApprovalResponse, error)
}

type AgentTUIToolApprovalRequest struct {
	ApprovalID       string
	ToolCallID       string
	ToolName         string
	Title            string
	Input            interface{}
	ProviderExecuted bool
	MessageIndex     int
	PartIndex        int
}

type AgentTUIToolApprovalResponse struct {
	Approved bool
	Reason   string
}

// AgentTUIStreamResult is the stream payload passed to the renderer.
type AgentTUIStreamResult struct {
	Stream *StreamRenderSource
	Abort  func()
}

// AgentTUIRunner mirrors the TypeScript AgentTUIRunner loop using Go message types.
type AgentTUIRunner struct {
	agent              agent.Agent
	transport          ai.ChatTransport
	chatID             string
	renderer           AgentTUIRenderer
	input              io.Reader
	title              string
	tools              TerminalPartDisplayMode
	reasoning          TerminalPartDisplayMode
	responseStatistics ResponseStatisticsMode
	contextSize        int
	sandbox            interface{}

	// transportUI is the assistant message of the last ChatTransport turn and
	// transportUICount the number of history messages it was converted to. A
	// resumed turn replaces them with the merged message (TS upsertResponseMessage).
	transportUI      *ai.UIMessage
	transportUICount int
}

func NewAgentTUIRunner(options AgentTUIRunnerOptions) *AgentTUIRunner {
	tools := options.Tools
	if tools == "" {
		tools = defaultTerminalPartDisplayMode
	}
	reasoning := options.Reasoning
	if reasoning == "" {
		reasoning = defaultTerminalPartDisplayMode
	}
	stats := options.ResponseStatistics
	if stats == "" {
		stats = defaultResponseStatisticsMode
	}
	if options.Input == nil {
		options.Input = os.Stdin
	}
	if options.Renderer == nil {
		options.Renderer = NewTerminalRenderer(TerminalRendererOptions{
			Tools:              tools,
			Reasoning:          reasoning,
			ResponseStatistics: stats,
			ContextSize:        options.ContextSize,
		})
	}
	chatID := options.ChatID
	if chatID == "" && options.Transport != nil {
		chatID = uuid.NewString()
	}
	return &AgentTUIRunner{
		agent:              options.Agent,
		transport:          options.Transport,
		chatID:             chatID,
		renderer:           options.Renderer,
		input:              options.Input,
		title:              options.Title,
		tools:              tools,
		reasoning:          reasoning,
		responseStatistics: stats,
		contextSize:        options.ContextSize,
		sandbox:            options.Sandbox,
	}
}

func (r *AgentTUIRunner) Run(ctx context.Context) error {
	if r.agent == nil && r.transport == nil {
		return fmt.Errorf("agent or transport is required")
	}
	if r.agent != nil && r.transport != nil {
		return fmt.Errorf("agent and transport are mutually exclusive")
	}
	if r.renderer == nil {
		return fmt.Errorf("renderer is required")
	}
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if terminalRenderer, ok := r.renderer.(*TerminalRenderer); ok {
		defer terminalRenderer.Stop()
	}

	var messages []types.Message
	hasRunTurn := false
	streamWithoutPrompt := false

	for {
		var prompt string
		if !streamWithoutPrompt {
			promptReader, ok := r.renderer.(AgentTUIPromptReader)
			if !ok {
				if hasRunTurn {
					return nil
				}
				return fmt.Errorf("No prompt was provided and the renderer does not support prompt input.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			var promptOK bool
			var err error
			prompt, promptOK, err = promptReader.ReadPrompt(ctx, TerminalSessionOptions{Title: r.title, TitleSet: r.title != ""})
			if err != nil {
				if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			if !promptOK {
				return nil
			}
			messages = append(messages, types.Message{
				Role:    types.RoleUser,
				Content: []types.ContentPart{types.TextContent{Text: prompt}},
			})
			hasRunTurn = true
		}

		streamCtx, cancel := context.WithCancel(ctx)
		var streamResult AgentTUIStreamResult
		var err error
		if r.transport != nil {
			var seed *ai.UIMessage
			if streamWithoutPrompt {
				seed = r.transportUI
			}
			streamResult, err = r.streamViaTransport(streamCtx, messages, seed)
		} else {
			var result *ai.StreamTextResult
			result, err = r.agent.Stream(streamCtx, agent.AgentStreamOptions{
				AgentGenerateOptions: agent.AgentGenerateOptions{
					Messages:            append([]types.Message(nil), messages...),
					ExperimentalSandbox: r.sandbox,
				},
			})
			if err == nil {
				streamResult = AgentTUIStreamResult{Stream: NewStreamRenderSource(result)}
			}
		}
		if err != nil {
			cancel()
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		streamResult.Abort = cancel

		responseMessages, err := r.renderer.RenderStream(ctx, streamResult, TerminalSessionOptions{
			Title:              r.title,
			TitleSet:           r.title != "",
			SubmittedPrompt:    prompt,
			SubmittedPromptSet: !streamWithoutPrompt,
			ContinueSession:    rendererSupportsPrompt(r.renderer),
			Tools:              r.tools,
			Reasoning:          r.reasoning,
			ResponseStatistics: r.responseStatistics,
			ContextSize:        r.contextSize,
			WaitForExit:        false,
			WaitForExitSet:     true,
		})
		cancel()
		if err != nil {
			if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if len(responseMessages) > 0 {
			approvalRequests := findPendingToolApprovalRequests(responseMessages)
			if len(approvalRequests) > 0 {
				approvalReader, ok := r.renderer.(AgentTUIToolApprovalReader)
				if !ok {
					return fmt.Errorf("Tool approval was requested, but the renderer does not support tool approval input.") //nolint:staticcheck // matches TS SDK's exact error text
				}
				for _, request := range approvalRequests {
					response, err := approvalReader.ReadToolApproval(ctx, request, TerminalSessionOptions{Title: r.title, TitleSet: r.title != ""})
					if err != nil {
						if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
							return nil
						}
						return err
					}
					if err := applyToolApprovalResponse(responseMessages, request, response); err != nil {
						return err
					}
					applyUIToolApproval(streamResult.Stream.uiMessage, request.ToolCallID, request.ApprovalID, response)
				}
				messages = r.recordResponse(messages, responseMessages, streamResult.Stream, streamWithoutPrompt)
				streamWithoutPrompt = true
				continue
			}
			messages = r.recordResponse(messages, responseMessages, streamResult.Stream, streamWithoutPrompt)
		}
		if !hasRunTurn {
			return nil
		}
		streamWithoutPrompt = false
	}
}

// LinePromptRenderer adapts a renderer to simple line-oriented stdin prompts.
type LinePromptRenderer struct {
	Renderer *TerminalRenderer
	Scanner  *bufio.Scanner
}

func rendererSupportsPrompt(renderer AgentTUIRenderer) bool {
	_, ok := renderer.(AgentTUIPromptReader)
	return ok
}

// upsertResponseMessages adds a turn's response messages to the history.
//
// TS replaces the last assistant UIMessage because the continuation stream
// keeps appending parts to it. Response messages here are deltas: after an
// approval, the resumed stream returns the tool message with the results and
// the new assistant message, not the assistant message that holds the tool
// call. Replacing the last assistant message (which holds that call and its
// approval response) with them would leave a tool result with no call in the
// next request. So they are always appended; replaceLast is kept for callers
// of the signature.
// recordResponse adds a turn's response to the history. A ChatTransport turn
// that resumed the previous assistant message returns that message merged with
// the new parts, so it replaces the messages the previous turn produced.
func (r *AgentTUIRunner) recordResponse(messages, responseMessages []types.Message, stream *StreamRenderSource, resumed bool) []types.Message {
	if r.transport == nil {
		return upsertResponseMessages(messages, responseMessages, resumed)
	}
	if resumed && r.transportUICount > 0 && r.transportUICount <= len(messages) {
		messages = messages[:len(messages)-r.transportUICount]
	}
	r.transportUI = stream.uiMessage
	r.transportUICount = len(responseMessages)
	return append(messages, responseMessages...)
}

func upsertResponseMessages(messages, responseMessages []types.Message, replaceLast bool) []types.Message {
	_ = replaceLast
	if len(responseMessages) == 0 {
		return messages
	}
	return append(messages, responseMessages...)
}

func findPendingToolApprovalRequests(messages []types.Message) []AgentTUIToolApprovalRequest {
	var requests []AgentTUIToolApprovalRequest
	// A request answered elsewhere in the messages (a resumed transport turn
	// returns the whole merged assistant message, with the response in the
	// tool message) is not pending.
	answered := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Content {
			if response, ok := part.(types.ToolApprovalResponseContent); ok {
				answered[response.ApprovalID] = true
			}
		}
	}
	for messageIndex, message := range messages {
		for partIndex, part := range message.Content {
			request, ok := part.(types.ToolApprovalRequestContent)
			if !ok || request.IsAutomatic || answered[request.ApprovalID] {
				continue
			}
			toolCall := request.ToolCall
			requests = append(requests, AgentTUIToolApprovalRequest{
				ApprovalID:       request.ApprovalID,
				ToolCallID:       nonEmpty(request.ToolCallID, toolCall.ID),
				ToolName:         toolCall.ToolName,
				Title:            toolCall.Title,
				Input:            toolCall.Arguments,
				ProviderExecuted: toolCall.ProviderExecuted,
				MessageIndex:     messageIndex,
				PartIndex:        partIndex,
			})
		}
	}
	return requests
}

func applyToolApprovalResponse(messages []types.Message, request AgentTUIToolApprovalRequest, response AgentTUIToolApprovalResponse) error {
	if request.MessageIndex < 0 || request.MessageIndex >= len(messages) {
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	message := &messages[request.MessageIndex]
	if request.PartIndex < 0 || request.PartIndex >= len(message.Content) {
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	part, ok := message.Content[request.PartIndex].(types.ToolApprovalRequestContent)
	if !ok || nonEmpty(part.ToolCallID, part.ToolCall.ID) != request.ToolCallID {
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	message.Content[request.PartIndex] = types.ToolApprovalResponseContent{
		ApprovalID:       request.ApprovalID,
		ToolCallID:       request.ToolCallID,
		ToolCall:         part.ToolCall,
		Approved:         response.Approved,
		Reason:           response.Reason,
		ProviderExecuted: request.ProviderExecuted,
	}
	return nil
}
