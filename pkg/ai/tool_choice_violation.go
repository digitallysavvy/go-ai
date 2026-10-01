package ai

import (
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolChoiceViolationError is returned when a model response does not
// satisfy an enforced tool choice ("required" or a specific "tool"),
// matching TS error/tool-choice-violation-error.ts (audit rows 8b6b756 /
// 36b3364 / ccf98e7, WG3).
type ToolChoiceViolationError struct {
	// ToolChoice is the enforced tool choice the response did not satisfy.
	// Type is always ToolChoiceRequired or ToolChoiceTool.
	ToolChoice types.ToolChoice

	// FinishReason is why the model finished generating the response.
	FinishReason types.FinishReason

	// Provider that returned the response.
	Provider string

	// ModelID that returned the response.
	ModelID string

	// Content is the normalized content returned by the model, so callers
	// can inspect it to recover a tool call the provider returned as text or
	// reasoning instead of a structured tool call.
	Content []types.ContentPart
}

func (e *ToolChoiceViolationError) Error() string {
	if e.ToolChoice.Type == types.ToolChoiceRequired {
		return "Model response did not contain a tool call even though tool choice was required."
	}
	return fmt.Sprintf("Model response did not contain a call to the required tool '%s'.", e.ToolChoice.ToolName)
}

// IsToolChoiceViolationError reports whether err is a ToolChoiceViolationError.
func IsToolChoiceViolationError(err error) bool {
	var target *ToolChoiceViolationError
	return errors.As(err, &target)
}

// enforcedToolChoice returns choice when its type requires the model to make
// a (specific) tool call, or the zero value otherwise.
func enforcedToolChoice(choice types.ToolChoice) (types.ToolChoice, bool) {
	if choice.Type == types.ToolChoiceRequired || choice.Type == types.ToolChoiceTool {
		return choice, true
	}
	return types.ToolChoice{}, false
}

// toolChoiceSatisfied reports whether toolCalls contains a call matching an
// enforced tool choice: any call for "required", or a call to the named tool
// for "tool". Invalid tool calls still count, mirroring TS (which checks
// toolCall.toolName without excluding invalid calls).
func toolChoiceSatisfied(enforced types.ToolChoice, toolCalls []types.ToolCall) bool {
	if enforced.Type == types.ToolChoiceRequired {
		return len(toolCalls) > 0
	}
	for _, call := range toolCalls {
		if call.ToolName == enforced.ToolName {
			return true
		}
	}
	return false
}

// checkToolChoiceViolation returns a *ToolChoiceViolationError when choice
// enforces a tool call that toolCalls does not satisfy, else nil.
func checkToolChoiceViolation(choice types.ToolChoice, toolCalls []types.ToolCall, finishReason types.FinishReason, provider, modelID string, content []types.ContentPart) error {
	enforced, ok := enforcedToolChoice(choice)
	if !ok {
		return nil
	}
	if toolChoiceSatisfied(enforced, toolCalls) {
		return nil
	}
	return &ToolChoiceViolationError{
		ToolChoice:   enforced,
		FinishReason: finishReason,
		Provider:     provider,
		ModelID:      modelID,
		Content:      content,
	}
}
