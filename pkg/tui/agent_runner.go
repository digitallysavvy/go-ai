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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AgentTUIRunnerOptions configures AgentTUIRunner.
type AgentTUIRunnerOptions struct {
	Agent              agent.Agent
	Renderer           AgentTUIRenderer
	Input              io.Reader
	Title              string
	Tools              TerminalPartDisplayMode
	Reasoning          TerminalPartDisplayMode
	ResponseStatistics ResponseStatisticsMode
	ContextSize        int
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
	renderer           AgentTUIRenderer
	input              io.Reader
	title              string
	tools              TerminalPartDisplayMode
	reasoning          TerminalPartDisplayMode
	responseStatistics ResponseStatisticsMode
	contextSize        int
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
	return &AgentTUIRunner{
		agent:              options.Agent,
		renderer:           options.Renderer,
		input:              options.Input,
		title:              options.Title,
		tools:              tools,
		reasoning:          reasoning,
		responseStatistics: stats,
		contextSize:        options.ContextSize,
	}
}

func (r *AgentTUIRunner) Run(ctx context.Context) error {
	if r.agent == nil {
		return fmt.Errorf("agent is required")
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
				return fmt.Errorf("No prompt was provided and the renderer does not support prompt input.")
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
		result, err := r.agent.Stream(streamCtx, agent.AgentStreamOptions{
			AgentGenerateOptions: agent.AgentGenerateOptions{
				Messages: append([]types.Message(nil), messages...),
			},
		})
		if err != nil {
			cancel()
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}

		responseMessages, err := r.renderer.RenderStream(ctx, AgentTUIStreamResult{
			Stream: NewStreamRenderSource(result),
			Abort:  cancel,
		}, TerminalSessionOptions{
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
					return fmt.Errorf("Tool approval was requested, but the renderer does not support tool approval input.")
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
				}
				messages = upsertResponseMessages(messages, responseMessages, streamWithoutPrompt)
				streamWithoutPrompt = true
				continue
			}
			messages = upsertResponseMessages(messages, responseMessages, streamWithoutPrompt)
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

func upsertResponseMessages(messages, responseMessages []types.Message, replaceLast bool) []types.Message {
	if len(responseMessages) == 0 {
		return messages
	}
	if replaceLast && len(messages) > 0 && messages[len(messages)-1].Role == types.RoleAssistant {
		return append(append([]types.Message(nil), messages[:len(messages)-1]...), responseMessages...)
	}
	return append(messages, responseMessages...)
}

func findPendingToolApprovalRequests(messages []types.Message) []AgentTUIToolApprovalRequest {
	var requests []AgentTUIToolApprovalRequest
	for messageIndex, message := range messages {
		for partIndex, part := range message.Content {
			request, ok := part.(types.ToolApprovalRequestContent)
			if !ok || request.IsAutomatic {
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
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID)
	}
	message := &messages[request.MessageIndex]
	if request.PartIndex < 0 || request.PartIndex >= len(message.Content) {
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID)
	}
	part, ok := message.Content[request.PartIndex].(types.ToolApprovalRequestContent)
	if !ok || nonEmpty(part.ToolCallID, part.ToolCall.ID) != request.ToolCallID {
		return fmt.Errorf("Could not find tool approval request %s.", request.ApprovalID)
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
