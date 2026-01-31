package agent

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolLoopAgent is an agent that loops through tool calls until task completion
type ToolLoopAgent struct {
	config AgentConfig
}

// NewToolLoopAgent creates a new ToolLoopAgent with the given configuration
func NewToolLoopAgent(config AgentConfig) *ToolLoopAgent {
	// Set defaults if not provided
	if config.MaxSteps == 0 {
		config.MaxSteps = 10
	}

	return &ToolLoopAgent{
		config: config,
	}
}

// Execute runs the agent with a simple text prompt
func (a *ToolLoopAgent) Execute(ctx context.Context, prompt string) (*AgentResult, error) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: prompt},
			},
		},
	}

	return a.ExecuteWithMessages(ctx, messages)
}

// ExecuteWithMessages runs the agent with a message history
func (a *ToolLoopAgent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*AgentResult, error) {
	// Validate configuration
	if a.config.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Apply total timeout if configured
	var cancel context.CancelFunc
	if a.config.Timeout != nil && a.config.Timeout.HasTotal() {
		ctx, cancel = a.config.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Initialize result
	result := &AgentResult{
		Steps:       []types.StepResult{},
		ToolResults: []types.ToolResult{},
	}

	// Current conversation state
	currentMessages := make([]types.Message, len(messages))
	copy(currentMessages, messages)

	// Custom data for PrepareCall (persists across steps)
	var customData interface{}

	// Execute agent loop
	for stepNum := 1; stepNum <= a.config.MaxSteps; stepNum++ {
		// Call step start callback
		if a.config.OnStepStart != nil {
			a.config.OnStepStart(stepNum)
		}

		// Execute one step with custom data
		stepResult, shouldContinue, newCustomData, err := a.executeStep(ctx, stepNum, currentMessages, result.Usage, customData)
		customData = newCustomData
		if err != nil {
			return nil, fmt.Errorf("step %d failed: %w", stepNum, err)
		}

		// Add step to results
		result.Steps = append(result.Steps, *stepResult)
		result.Usage = result.Usage.Add(stepResult.Usage)
		result.Warnings = append(result.Warnings, stepResult.Warnings...)

		// Call step finish callback
		if a.config.OnStepFinish != nil {
			a.config.OnStepFinish(*stepResult)
		}

		// Update conversation with assistant response
		assistantMsg := types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentPart{},
		}
		if stepResult.Text != "" {
			assistantMsg.Content = append(assistantMsg.Content, types.TextContent{Text: stepResult.Text})
		}
		currentMessages = append(currentMessages, assistantMsg)

		// If there are tool calls, execute them
		if len(stepResult.ToolCalls) > 0 {
			toolResults, err := a.executeTools(ctx, stepResult.ToolCalls)
			if err != nil {
				return nil, fmt.Errorf("tool execution failed at step %d: %w", stepNum, err)
			}

			result.ToolResults = append(result.ToolResults, toolResults...)

			// Add tool results to conversation
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
		}

		// Check if we should continue
		if !shouldContinue {
			result.Text = stepResult.Text
			result.FinishReason = stepResult.FinishReason
			break
		}

		// Check if we've hit max steps
		if stepNum == a.config.MaxSteps {
			result.Text = stepResult.Text
			result.FinishReason = types.FinishReasonLength
			result.Warnings = append(result.Warnings, types.Warning{
				Type:    "max_steps_reached",
				Message: fmt.Sprintf("Agent reached maximum steps (%d)", a.config.MaxSteps),
			})
			break
		}
	}

	// Call finish callback
	if a.config.OnFinish != nil {
		a.config.OnFinish(result)
	}

	return result, nil
}

// executeStep executes a single agent step
func (a *ToolLoopAgent) executeStep(ctx context.Context, stepNum int, messages []types.Message, accumulatedUsage types.Usage, customData interface{}) (*types.StepResult, bool, interface{}, error) {
	// Apply per-step timeout if configured
	stepCtx := ctx
	var stepCancel context.CancelFunc
	if a.config.Timeout != nil && a.config.Timeout.HasPerStep() {
		stepCtx, stepCancel = a.config.Timeout.CreateTimeoutContext(ctx, "step")
		defer stepCancel()
	}

	// Prepare call configuration
	callConfig := PrepareCallConfig{
		StepNumber:       stepNum,
		System:           a.config.System,
		Messages:         messages,
		Tools:            a.config.Tools,
		Temperature:      a.config.Temperature,
		MaxTokens:        a.config.MaxTokens,
		AccumulatedUsage: accumulatedUsage,
		CustomData:       customData,
	}

	// Call PrepareCall hook if configured
	if a.config.PrepareCall != nil {
		callConfig = a.config.PrepareCall(ctx, callConfig)
	}

	// Build generate options using potentially modified config
	genOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: callConfig.Messages,
			System:   callConfig.System,
		},
		Temperature: callConfig.Temperature,
		MaxTokens:   callConfig.MaxTokens,
		Tools:       callConfig.Tools,
		ToolChoice:  types.AutoToolChoice(),
	}

	// Call the model with step context
	genResult, err := a.config.Model.DoGenerate(stepCtx, genOpts)
	if err != nil {
		return nil, false, callConfig.CustomData, err
	}

	// Create step result
	stepResult := &types.StepResult{
		StepNumber:   stepNum,
		Text:         genResult.Text,
		ToolCalls:    genResult.ToolCalls,
		ToolResults:  []types.ToolResult{},
		FinishReason: genResult.FinishReason,
		Usage:        genResult.Usage,
		Warnings:     genResult.Warnings,
	}

	// Determine if we should continue
	shouldContinue := genResult.FinishReason == types.FinishReasonToolCalls && len(genResult.ToolCalls) > 0

	return stepResult, shouldContinue, callConfig.CustomData, nil
}

// executeTools executes a list of tool calls with optional approval
func (a *ToolLoopAgent) executeTools(ctx context.Context, toolCalls []types.ToolCall) ([]types.ToolResult, error) {
	results := make([]types.ToolResult, len(toolCalls))

	for i, call := range toolCalls {
		// Call tool call callback
		if a.config.OnToolCall != nil {
			a.config.OnToolCall(call)
		}

		// Check if approval is required
		if a.config.ToolApprovalRequired && a.config.ToolApprover != nil {
			approved := a.config.ToolApprover(call)
			if !approved {
				results[i] = types.ToolResult{
					ToolCallID: call.ID,
					ToolName:   call.ToolName,
					Error:      fmt.Errorf("tool call rejected by user"),
				}
				continue
			}
		}

		// Find the tool
		var tool *types.Tool
		for j := range a.config.Tools {
			if a.config.Tools[j].Name == call.ToolName {
				tool = &a.config.Tools[j]
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

		// Execute the tool
		execOptions := types.ToolExecutionOptions{
			ToolCallID: call.ID,
		}
		result, err := tool.Execute(ctx, call.Arguments, execOptions)
		results[i] = types.ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.ToolName,
			Result:     result,
			Error:      err,
		}

		// Call tool result callback
		if a.config.OnToolResult != nil {
			a.config.OnToolResult(results[i])
		}
	}

	return results, nil
}

// SetSystem updates the system prompt
func (a *ToolLoopAgent) SetSystem(system string) {
	a.config.System = system
}

// AddTool adds a tool to the agent
func (a *ToolLoopAgent) AddTool(tool types.Tool) {
	a.config.Tools = append(a.config.Tools, tool)
}

// RemoveTool removes a tool from the agent by name
func (a *ToolLoopAgent) RemoveTool(toolName string) {
	for i, tool := range a.config.Tools {
		if tool.Name == toolName {
			a.config.Tools = append(a.config.Tools[:i], a.config.Tools[i+1:]...)
			return
		}
	}
}

// SetMaxSteps updates the maximum number of steps
func (a *ToolLoopAgent) SetMaxSteps(maxSteps int) {
	a.config.MaxSteps = maxSteps
}
