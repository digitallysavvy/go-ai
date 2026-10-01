package ai

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// validateInstructionMessages checks that instructions given as messages are
// all system messages (TS standardizePrompt: "instructions must be a string,
// SystemModelMessage, or array of SystemModelMessage").
func validateInstructionMessages(messages []types.Message) error {
	for _, message := range messages {
		if message.Role != types.RoleSystem {
			return &providererrors.InvalidArgumentError{
				Field:   "instructions",
				Message: "instructions must be a string, SystemModelMessage, or array of SystemModelMessage",
			}
		}
	}
	return nil
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
