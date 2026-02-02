package anthropic

// ModelOptions contains optional configuration for Anthropic language models.
// These options can be passed when creating a model instance to configure
// provider-specific features.
type ModelOptions struct {
	// ContextManagement enables automatic cleanup of conversation history
	// to prevent context window overflow in long conversations.
	//
	// This is a beta feature that requires Claude 4.5+ models.
	// When enabled, Anthropic will automatically remove or truncate old
	// content based on the configured strategies.
	//
	// Example:
	//   options := anthropic.ModelOptions{
	//       ContextManagement: &anthropic.ContextManagement{
	//           Strategies: []string{anthropic.StrategyClearToolUses},
	//       },
	//   }
	//
	// See ContextManagement for available strategies.
	ContextManagement *ContextManagement `json:"context_management,omitempty"`
}
