package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DefaultInstructionsOptions configures DefaultInstructionsMiddleware.
type DefaultInstructionsOptions struct {
	// Instructions are plain-string default instructions, applied as
	// Prompt.System when the call has none.
	Instructions string

	// InstructionMessages supplies the default instructions as system
	// messages instead, preserving each message's ProviderOptions. Takes
	// precedence over Instructions when non-empty.
	InstructionMessages []types.Message
}

// DefaultInstructionsMiddleware applies default instructions to a language
// model call when it does not already contain a system message. Mirrors the
// TypeScript SDK's defaultInstructionsMiddleware
// (middleware/default-instructions-middleware.ts).
//
// A call already has a system message when Prompt.System is non-empty, or
// any message in Prompt.Messages has the system role — either one means the
// call already carries its own instructions (e.g. an earlier middleware, an
// explicit System/Instructions option, or a trusted system message in a
// message history) and the defaults are left out entirely, matching TS's
// "call-level instructions take precedence" and "a system message later in
// the prompt still counts" behavior.
func DefaultInstructionsMiddleware(opts DefaultInstructionsOptions) *LanguageModelMiddleware {
	// Precompute once, mirroring TS resolving defaultSystemMessages outside
	// transformParams.
	instructionMessages := opts.InstructionMessages
	instructions := opts.Instructions
	if len(instructionMessages) > 0 {
		instructions = ""
	}

	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",
		TransformParams: func(_ context.Context, _ string, params *provider.GenerateOptions, _ provider.LanguageModel) (*provider.GenerateOptions, error) {
			if instructions == "" && len(instructionMessages) == 0 {
				return params, nil
			}
			if params.Prompt.System != "" {
				return params, nil
			}
			for _, message := range params.Prompt.Messages {
				if message.Role == types.RoleSystem {
					return params, nil
				}
			}

			cloned := *params
			if len(instructionMessages) > 0 {
				combined := make([]types.Message, 0, len(instructionMessages)+len(params.Prompt.Messages))
				for _, message := range instructionMessages {
					combined = append(combined, types.Message{
						Role:            types.RoleSystem,
						Content:         message.Content,
						ProviderOptions: message.ProviderOptions,
					})
				}
				combined = append(combined, params.Prompt.Messages...)
				cloned.Prompt.Messages = combined
			} else {
				cloned.Prompt.System = instructions
			}
			return &cloned, nil
		},
	}
}
