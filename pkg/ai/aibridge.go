package ai

import "github.com/digitallysavvy/go-ai/pkg/internal/aibridge"

// Helpers the legacy agent loop (pkg/agent) shares with GenerateText. They
// are reached through pkg/internal/aibridge so they stay out of the public
// API.
func init() {
	aibridge.ProviderToolResultsFromContent = providerToolResultsFromContent
	aibridge.MessagesForModel = messagesForModel
}
