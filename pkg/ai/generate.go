package ai

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateTextOptions contains options for text generation
// Updated in v6.0 with Output system and Context flow
type GenerateTextOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt string
	Messages []types.Message
	System string

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int

	// Tools available for the model to call
	Tools []types.Tool
	ToolChoice types.ToolChoice

	// Maximum number of tool calling steps (default: 10)
	MaxSteps *int

	// ========================================================================
	// Timeout Configuration (v6.0.41 - NEW)
	// ========================================================================

	// Timeout provides granular timeout controls
	// Supports total timeout, per-step timeout, and per-chunk timeout (for streaming)
	Timeout *TimeoutConfig

	// ========================================================================
	// Output Specification (v6.0 - NEW)
	// ========================================================================

	// Output specifies how to handle and parse model output
	// Use TextOutput(), ObjectOutput(), ArrayOutput(), ChoiceOutput(), or JSONOutput()
	// If nil, defaults to text output
	Output interface{} // Output[any, any]

	// ResponseFormat (for structured output) - DEPRECATED: Use Output instead
	// Kept for backward compatibility
	ResponseFormat *provider.ResponseFormat

	// ========================================================================
	// Context Flow (v6.0 - NEW)
	// ========================================================================

	// ExperimentalContext is user-defined context that flows through the conversation
	// This context is passed to:
	// - Tool execution functions (via ToolExecutionOptions)
	// - PrepareStep callback
	// - OnStepFinish callback
	// - OnFinish callback
	ExperimentalContext interface{}

	// ========================================================================
	// Retention Settings (v6.0.60 - NEW)
	// ========================================================================

	// ExperimentalRetention controls what data is retained from LLM requests/responses.
	// Useful for reducing memory consumption with images or large contexts.
	// Default (nil) retains everything for backwards compatibility.
	//
	// Example:
	//   retention := &types.RetentionSettings{
	//       RequestBody:  types.BoolPtr(false),  // Don't retain request
	//       ResponseBody: types.BoolPtr(false),  // Don't retain response
	//   }
	//
	// This can reduce memory consumption by 50-80% for image-heavy workloads.
	ExperimentalRetention *types.RetentionSettings

	// ========================================================================
	// Callbacks (Updated signatures in v6.0)
	// ========================================================================

	// PrepareStep is called before each generation step
	// Allows modification of options before the next step
	// Receives the user context if ExperimentalContext is set
	PrepareStep func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions

	// OnStepFinish is called after each generation step completes
	// Receives the user context if ExperimentalContext is set
	OnStepFinish func(ctx context.Context, step types.StepResult, userContext interface{})

	// OnFinish is called when generation completes
	// Receives the user context if ExperimentalContext is set
	OnFinish func(ctx context.Context, result *GenerateTextResult, userContext interface{})
}

// PrepareStepOptions contains options that can be modified before each step
type PrepareStepOptions struct {
	// Messages for the next step
	Messages []types.Message

	// User context (from ExperimentalContext)
	UserContext interface{}

	// Current step number
	StepNumber int

	// Accumulated usage so far
	AccumulatedUsage types.Usage
}

// GenerateTextResult contains the result of text generation
type GenerateTextResult struct {
	// Generated text content
	Text string

	// Tool calls made during generation
	ToolCalls []types.ToolCall

	// Tool results from executed tools
	ToolResults []types.ToolResult

	// Steps taken during generation (for multi-step tool calling)
	Steps []types.StepResult

	// Reason why generation finished
	FinishReason types.FinishReason

	// Token usage information
	Usage types.Usage

	// Warnings from the provider
	Warnings []types.Warning

	// Raw request/response (for debugging)
	RawRequest  interface{}
	RawResponse interface{}
}

// GenerateText performs non-streaming text generation with optional tool calling
func GenerateText(ctx context.Context, opts GenerateTextOptions) (*GenerateTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Apply total timeout if configured
	var cancel context.CancelFunc
	if opts.Timeout != nil && opts.Timeout.HasTotal() {
		ctx, cancel = opts.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Initialize result
	result := &GenerateTextResult{
		Steps: []types.StepResult{},
	}

	// Set default max steps
	maxSteps := 10
	if opts.MaxSteps != nil {
		maxSteps = *opts.MaxSteps
	}

	// Current messages for conversation history
	currentMessages := prompt.Messages

	// Execute generation loop (for tool calling)
	for stepNum := 1; stepNum <= maxSteps; stepNum++ {
		// Apply per-step timeout if configured
		stepCtx := ctx
		var stepCancel context.CancelFunc
		if opts.Timeout != nil && opts.Timeout.HasPerStep() {
			stepCtx, stepCancel = opts.Timeout.CreateTimeoutContext(ctx, "step")
			defer stepCancel()
		}

		// Build generate options
		genOpts := &provider.GenerateOptions{
			Prompt: types.Prompt{
				Messages: currentMessages,
				System:   prompt.System,
			},
			Temperature:      opts.Temperature,
			MaxTokens:        opts.MaxTokens,
			TopP:             opts.TopP,
			TopK:             opts.TopK,
			FrequencyPenalty: opts.FrequencyPenalty,
			PresencePenalty:  opts.PresencePenalty,
			StopSequences:    opts.StopSequences,
			Seed:             opts.Seed,
			Tools:            opts.Tools,
			ToolChoice:       opts.ToolChoice,
			ResponseFormat:   opts.ResponseFormat,
		}

		// Call the model with step context
		genResult, err := opts.Model.DoGenerate(stepCtx, genOpts)
		if err != nil {
			return nil, fmt.Errorf("generation failed at step %d: %w", stepNum, err)
		}

		// Create step result
		stepResult := types.StepResult{
			StepNumber:   stepNum,
			Text:         genResult.Text,
			ToolCalls:    genResult.ToolCalls,
			ToolResults:  []types.ToolResult{},
			FinishReason: genResult.FinishReason,
			Usage:        genResult.Usage,
			Warnings:     genResult.Warnings,
		}

		// Update accumulated usage
		result.Usage = result.Usage.Add(genResult.Usage)

		// Check if there are tool calls to execute
		if len(genResult.ToolCalls) > 0 && len(opts.Tools) > 0 {
			// Execute tools with context flow (v6.0)
			toolResults, err := executeTools(ctx, genResult.ToolCalls, opts.Tools, opts.ExperimentalContext, &result.Usage)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed at step %d: %w", stepNum, err)
			}

			stepResult.ToolResults = toolResults
			result.ToolResults = append(result.ToolResults, toolResults...)

			// Add assistant message with tool calls to history
			assistantMsg := types.Message{
				Role:    types.RoleAssistant,
				Content: []types.ContentPart{},
			}
			if genResult.Text != "" {
				assistantMsg.Content = append(assistantMsg.Content, types.TextContent{Text: genResult.Text})
			}
			currentMessages = append(currentMessages, assistantMsg)

			// Add tool results to history
			for _, tr := range toolResults {
				toolMsg := types.Message{
					Role: types.RoleTool,
					Content: []types.ContentPart{
						types.ToolResultContent{
							ToolCallID: tr.ToolCallID,
							ToolName:   tr.ToolName,
							Result:     tr.Result,
						},
					},
				}
				currentMessages = append(currentMessages, toolMsg)
			}
		} else {
			// No more tool calls, we're done
			result.Text = genResult.Text
			result.FinishReason = genResult.FinishReason
			result.ToolCalls = genResult.ToolCalls
			result.Warnings = append(result.Warnings, genResult.Warnings...)
			result.RawRequest = genResult.RawRequest
			result.RawResponse = genResult.RawResponse
		}

		// Add step to results
		result.Steps = append(result.Steps, stepResult)

		// Call step finish callback (v6.0: with user context)
		if opts.OnStepFinish != nil {
			opts.OnStepFinish(ctx, stepResult, opts.ExperimentalContext)
		}

		// Check if we should continue
		if genResult.FinishReason != types.FinishReasonToolCalls {
			break
		}
	}

	// Call finish callback (v6.0: with user context)
	if opts.OnFinish != nil {
		opts.OnFinish(ctx, result, opts.ExperimentalContext)
	}

	// Apply retention settings (v6.0.60)
	// Exclude request/response bodies based on retention settings
	if opts.ExperimentalRetention != nil {
		if !opts.ExperimentalRetention.ShouldRetainRequestBody() {
			result.RawRequest = nil
		}
		if !opts.ExperimentalRetention.ShouldRetainResponseBody() {
			result.RawResponse = nil
		}
	}

	return result, nil
}

// executeTools executes a list of tool calls
// Updated in v6.0 to pass ToolExecutionOptions with ToolCallID and UserContext
func executeTools(ctx context.Context, toolCalls []types.ToolCall, availableTools []types.Tool, userContext interface{}, usage *types.Usage) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(toolCalls))

	for i, call := range toolCalls {
		// Find the tool
		var tool *types.Tool
		for j := range availableTools {
			if availableTools[j].Name == call.ToolName {
				tool = &availableTools[j]
				break
			}
		}

		if tool == nil {
			results[i] = types.ToolResult{
				ToolCallID: call.ID,
				ToolName:   call.ToolName,
				Error:      fmt.Errorf("tool not found: %s", call.ToolName),
			}
			continue
		}

		// Prepare execution options (v6.0)
		execOptions := types.ToolExecutionOptions{
			ToolCallID:  call.ID,
			UserContext: userContext,
			Usage:       usage,
			Metadata:    make(map[string]interface{}),
		}

		// Execute the tool with new signature
		result, err := tool.Execute(ctx, call.Arguments, execOptions)
		results[i] = types.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Result:     result,
			Error:      err,
		}
	}

	return results, nil
}

// buildPrompt builds a unified Prompt from various input formats
func buildPrompt(promptText string, messages []types.Message, system string) types.Prompt {
	if len(messages) > 0 {
		return types.Prompt{
			Messages: messages,
			System:   system,
		}
	}

	if promptText != "" {
		return types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: promptText},
					},
				},
			},
			System: system,
		}
	}

	return types.Prompt{}
}
