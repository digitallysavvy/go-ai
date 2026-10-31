package ai

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// validateInstructionMessages checks that instructions given as messages are
// all system messages (TS standardizePrompt: "instructions must be a string,
// SystemModelMessage, or array of SystemModelMessage").
func validateInstructionMessages(messages []types.Message) error {
	for _, message := range messages {
		if message.Role != types.RoleSystem {
			return invalidInstructionsError()
		}
	}
	return nil
}

// invalidInstructionsError is the error for instructions that are not all
// system messages. TS standardizePrompt throws an InvalidPromptError ("Invalid
// prompt: instructions must be a string, SystemModelMessage, or array of
// SystemModelMessage"). Its Cause is the *InvalidArgumentError (field
// "instructions") that v0.5 returned, so errors.As matches either type.
func invalidInstructionsError() error {
	const message = "instructions must be a string, SystemModelMessage, or array of SystemModelMessage"
	return &providererrors.InvalidPromptError{
		Message: message,
		Cause:   &providererrors.InvalidArgumentError{Field: "instructions", Message: message},
	}
}

// validateStandardizedPromptInput runs the input checks of TS standardizePrompt
// in its order: a prompt or messages must be given, not both; the
// instructions must be system messages; the messages must not be empty.
func validateStandardizedPromptInput(prompt string, messages []types.Message, instructionMessages []types.Message) error {
	err := promptutils.ValidatePromptInput(prompt, messages)
	emptyMessages := err != nil && prompt == "" && messages != nil && len(messages) == 0
	if err != nil && !emptyMessages {
		return err
	}
	if instructionsErr := validateInstructionMessages(instructionMessages); instructionsErr != nil {
		return instructionsErr
	}
	return err
}

// instructionMessagesText concatenates the text parts of system-message
// instructions. It is used where a single text value is needed (telemetry,
// deprecated System fields).
func instructionMessagesText(messages []types.Message) string {
	text := ""
	for _, message := range messages {
		for _, part := range message.Content {
			var partText string
			switch p := part.(type) {
			case types.TextContent:
				partText = p.Text
			case *types.TextContent:
				if p != nil {
					partText = p.Text
				}
			}
			if partText == "" {
				continue
			}
			if text != "" {
				text += "\n"
			}
			text += partText
		}
	}
	return text
}

// cloneInstructionMessages returns a defensive copy of instruction messages.
func cloneInstructionMessages(messages []types.Message) []types.Message {
	if len(messages) == 0 {
		return nil
	}
	return append([]types.Message(nil), messages...)
}

// prependInstructionMessages places system-message instructions before the
// normalized prompt messages (TS convertToLanguageModelPrompt). System
// messages in instructions are always allowed.
func prependInstructionMessages(prompt types.Prompt, messages []types.Message) types.Prompt {
	if len(messages) == 0 {
		return prompt
	}
	combined := make([]types.Message, 0, len(messages)+len(prompt.Messages))
	for _, message := range messages {
		combined = append(combined, types.Message{
			Role:            types.RoleSystem,
			Content:         message.Content,
			ProviderOptions: message.ProviderOptions,
		})
	}
	prompt.Messages = append(combined, prompt.Messages...)
	return prompt
}
