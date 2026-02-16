package agent

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/google/uuid"
)

// Context keys for run tracking
type contextKey string

const (
	runIDKey       contextKey = "agent_run_id"
	parentRunIDKey contextKey = "agent_parent_run_id"
	tagsKey        contextKey = "agent_tags"
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

	// Initialize skills registry if not provided
	if config.Skills == nil {
		config.Skills = NewSkillRegistry()
	}

	// Initialize subagents registry if not provided
	if config.Subagents == nil {
		config.Subagents = NewSubagentRegistry()
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

	// Initialize run tracking in context if not already present
	// Generate a new run ID if one doesn't exist
	if ctx.Value(runIDKey) == nil {
		runID := uuid.New().String()
		ctx = context.WithValue(ctx, runIDKey, runID)
	}

	// Extract input for OnChainStart callback
	input := ""
	if len(messages) > 0 {
		for _, part := range messages[0].Content {
			if textPart, ok := part.(types.TextContent); ok {
				input = textPart.Text
				break
			}
		}
	}

	// Call OnChainStart callback
	if a.config.OnChainStart != nil {
		a.config.OnChainStart(input, messages)
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
		Delegations: []SubagentDelegation{},
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
			// Call OnChainError callback
			if a.config.OnChainError != nil {
				a.config.OnChainError(err)
			}
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
			// Call OnAgentAction callback for each tool call
			if a.config.OnAgentAction != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				for _, toolCall := range stepResult.ToolCalls {
					action := AgentAction{
						ToolCall:    toolCall,
						StepNumber:  stepNum,
						Reasoning:   stepResult.Text, // Include any reasoning text from the step
						RunID:       runID,
						ParentRunID: parentRunID,
						Tags:        tags,
					}
					a.config.OnAgentAction(action)
				}
			}
			toolResults, err := a.executeTools(ctx, stepResult.ToolCalls)
			if err != nil {
				// Call OnChainError callback
				if a.config.OnChainError != nil {
					a.config.OnChainError(err)
				}
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

			// Call OnAgentFinish callback when agent reaches final answer
			if a.config.OnAgentFinish != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				finish := AgentFinish{
					Output:       stepResult.Text,
					StepNumber:   stepNum,
					FinishReason: stepResult.FinishReason,
					Metadata: map[string]interface{}{
						"total_steps": stepNum,
						"usage":       result.Usage,
					},
					RunID:       runID,
					ParentRunID: parentRunID,
					Tags:        tags,
				}
				a.config.OnAgentFinish(finish)
			}
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

			// Call OnAgentFinish callback when hitting max steps
			if a.config.OnAgentFinish != nil {
				// Extract run tracking from context
				runID, _ := ctx.Value(runIDKey).(string)
				parentRunID, _ := ctx.Value(parentRunIDKey).(string)
				tags, _ := ctx.Value(tagsKey).([]string)

				finish := AgentFinish{
					Output:       stepResult.Text,
					StepNumber:   stepNum,
					FinishReason: types.FinishReasonLength,
					Metadata: map[string]interface{}{
						"total_steps":   stepNum,
						"usage":         result.Usage,
						"max_steps_hit": true,
					},
					RunID:       runID,
					ParentRunID: parentRunID,
					Tags:        tags,
				}
				a.config.OnAgentFinish(finish)
			}
			break
		}
	}

	// Call OnChainEnd callback (successful completion)
	if a.config.OnChainEnd != nil {
		a.config.OnChainEnd(result)
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

	// Build response message for this step
	responseMsg := types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentPart{},
	}
	if genResult.Text != "" {
		responseMsg.Content = append(responseMsg.Content, types.TextContent{Text: genResult.Text})
	}

	// Extract raw finish reason if available
	rawFinishReason := ""
	if genResult.RawResponse != nil {
		if respMap, ok := genResult.RawResponse.(map[string]interface{}); ok {
			if fr, ok := respMap["finish_reason"].(string); ok {
				rawFinishReason = fr
			}
		}
	}

	// Create step result
	stepResult := &types.StepResult{
		StepNumber:       stepNum,
		Text:             genResult.Text,
		ToolCalls:        genResult.ToolCalls,
		ToolResults:      []types.ToolResult{},
		FinishReason:     genResult.FinishReason,
		RawFinishReason:  rawFinishReason,
		Usage:            genResult.Usage,
		Warnings:         genResult.Warnings,
		ResponseMessages: []types.Message{responseMsg},
	}

	// Determine if we should continue
	shouldContinue := genResult.FinishReason == types.FinishReasonToolCalls && len(genResult.ToolCalls) > 0

	return stepResult, shouldContinue, callConfig.CustomData, nil
}

// executeTools executes a list of tool calls with optional approval
// Updated in v6.0.57 to handle provider-executed (deferrable) tools
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
				rejectionErr := fmt.Errorf("tool call rejected by user")
				results[i] = types.ToolResult{
					ToolCallID:       call.ID,
					ToolName:         call.ToolName,
					Error:            rejectionErr,
					ProviderExecuted: false,
				}

				// Call OnToolError for rejected tools
				if a.config.OnToolError != nil {
					a.config.OnToolError(call, rejectionErr)
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
			notFoundErr := fmt.Errorf("tool not found: %s", call.ToolName)
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Error:            notFoundErr,
				ProviderExecuted: false,
			}

			// Call OnToolError for tool not found
			if a.config.OnToolError != nil {
				a.config.OnToolError(call, notFoundErr)
			}
			continue
		}

		// Check if this is a provider-executed tool
		providerExecuted := isProviderExecutedTool(tool)

		if providerExecuted {
			// Provider-executed tool: result will come from provider in next response
			// Call OnToolStart for provider-executed tools
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Result:           nil,
				Error:            nil,
				ProviderExecuted: true,
			}

			// Call tool result callback with pending result
			if a.config.OnToolResult != nil {
				a.config.OnToolResult(results[i])
			}

			// Call OnToolEnd for provider-executed tools (they're deferred but considered started)
			if a.config.OnToolEnd != nil {
				a.config.OnToolEnd(results[i])
			}
		} else {
			// Locally-executed tool: execute now
			// Call OnToolStart before execution
			if a.config.OnToolStart != nil {
				a.config.OnToolStart(call)
			}

			execOptions := types.ToolExecutionOptions{
				ToolCallID: call.ID,
			}
			result, err := tool.Execute(ctx, call.Arguments, execOptions)
			results[i] = types.ToolResult{
				ToolCallID:       call.ID,
				ToolName:         call.ToolName,
				Result:           result,
				Error:            err,
				ProviderExecuted: false,
			}

			// Call tool result callback
			if a.config.OnToolResult != nil {
				a.config.OnToolResult(results[i])
			}

			// Call OnToolEnd or OnToolError based on execution result
			if err != nil {
				if a.config.OnToolError != nil {
					a.config.OnToolError(call, err)
				}
			} else {
				if a.config.OnToolEnd != nil {
					a.config.OnToolEnd(results[i])
				}
			}
		}
	}

	return results, nil
}

// isProviderExecutedTool determines if a tool is executed by the provider
// Provider-executed tools include:
// - Anthropic: tool-search-bm25, tool-search-regex, web-search, web-fetch, code-execution
// - xAI: file-search, mcp-server
// - OpenAI: MCP tools (with approval)
func isProviderExecutedTool(tool *types.Tool) bool {
	// Check for common provider-executed tool names
	providerTools := map[string]bool{
		// Anthropic built-in tools
		"tool-search-bm25":  true,
		"tool-search-regex": true,
		"web-search":        true,
		"web-fetch":         true,
		"code-execution":    true,
		// xAI tools
		"file-search": true,
		"mcp-server":  true,
	}

	return providerTools[tool.Name]
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

// ========================================================================
// Skills Management
// ========================================================================

// AddSkill adds a skill to the agent
func (a *ToolLoopAgent) AddSkill(skill *Skill) error {
	if a.config.Skills == nil {
		a.config.Skills = NewSkillRegistry()
	}
	return a.config.Skills.Register(skill)
}

// RemoveSkill removes a skill from the agent by name
func (a *ToolLoopAgent) RemoveSkill(name string) {
	if a.config.Skills != nil {
		a.config.Skills.Unregister(name)
	}
}

// GetSkill retrieves a skill by name
func (a *ToolLoopAgent) GetSkill(name string) (*Skill, bool) {
	if a.config.Skills == nil {
		return nil, false
	}
	return a.config.Skills.Get(name)
}

// ListSkills returns all registered skills
func (a *ToolLoopAgent) ListSkills() []*Skill {
	if a.config.Skills == nil {
		return []*Skill{}
	}
	return a.config.Skills.List()
}

// ExecuteSkill runs a skill by name with the given input
func (a *ToolLoopAgent) ExecuteSkill(ctx context.Context, name string, input string) (string, error) {
	if a.config.Skills == nil {
		return "", fmt.Errorf("no skills registry configured")
	}
	return a.config.Skills.Execute(ctx, name, input)
}

// ========================================================================
// Subagent Management
// ========================================================================

// AddSubagent registers a subagent with the given name
func (a *ToolLoopAgent) AddSubagent(name string, subagent Agent) error {
	if a.config.Subagents == nil {
		a.config.Subagents = NewSubagentRegistry()
	}
	return a.config.Subagents.Register(name, subagent)
}

// RemoveSubagent removes a subagent from the agent by name
func (a *ToolLoopAgent) RemoveSubagent(name string) {
	if a.config.Subagents != nil {
		a.config.Subagents.Unregister(name)
	}
}

// GetSubagent retrieves a subagent by name
func (a *ToolLoopAgent) GetSubagent(name string) (Agent, bool) {
	if a.config.Subagents == nil {
		return nil, false
	}
	return a.config.Subagents.Get(name)
}

// ListSubagents returns all registered subagent names
func (a *ToolLoopAgent) ListSubagents() []string {
	if a.config.Subagents == nil {
		return []string{}
	}
	return a.config.Subagents.List()
}

// DelegateToSubagent delegates execution to a named subagent
func (a *ToolLoopAgent) DelegateToSubagent(ctx context.Context, name string, prompt string) (*AgentResult, error) {
	if a.config.Subagents == nil {
		return nil, fmt.Errorf("no subagents registry configured")
	}
	return a.config.Subagents.Execute(ctx, name, prompt)
}

// DelegateToSubagentWithMessages delegates execution to a named subagent with message history
func (a *ToolLoopAgent) DelegateToSubagentWithMessages(ctx context.Context, name string, messages []types.Message) (*AgentResult, error) {
	if a.config.Subagents == nil {
		return nil, fmt.Errorf("no subagents registry configured")
	}
	return a.config.Subagents.ExecuteWithMessages(ctx, name, messages)
}

// ========================================================================
// Run Tracking Helpers (v6.0.61+)
// ========================================================================

// WithRunID adds a run ID to the context for tracking agent execution
// If a run ID already exists in the context, it is preserved and this has no effect
// Use this to provide a custom run ID or to manually initialize run tracking
func WithRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, runIDKey, runID)
}

// WithParentRunID adds a parent run ID to the context for nested/subagent executions
// Use this when delegating to subagents to maintain the execution hierarchy
func WithParentRunID(ctx context.Context, parentRunID string) context.Context {
	return context.WithValue(ctx, parentRunIDKey, parentRunID)
}

// WithTags adds tags to the context for categorizing agent runs
// Tags can be used for filtering, grouping, or labeling runs in monitoring systems
// Example: WithTags(ctx, []string{"production", "user:123", "session:abc"})
func WithTags(ctx context.Context, tags []string) context.Context {
	return context.WithValue(ctx, tagsKey, tags)
}

// GetRunID retrieves the run ID from the context
// Returns empty string if no run ID is present
func GetRunID(ctx context.Context) string {
	runID, _ := ctx.Value(runIDKey).(string)
	return runID
}

// GetParentRunID retrieves the parent run ID from the context
// Returns empty string if no parent run ID is present
func GetParentRunID(ctx context.Context) string {
	parentRunID, _ := ctx.Value(parentRunIDKey).(string)
	return parentRunID
}

// GetTags retrieves the tags from the context
// Returns nil if no tags are present
func GetTags(ctx context.Context) []string {
	tags, _ := ctx.Value(tagsKey).([]string)
	return tags
}
