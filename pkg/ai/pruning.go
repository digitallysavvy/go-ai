package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MessagePruneFunc is a function that prunes messages to fit within token limits
type MessagePruneFunc func(ctx context.Context, messages []types.Message, maxTokens int) ([]types.Message, error)

// PruneOptions contains options for message pruning
type PruneOptions struct {
	// MaxTokens is the maximum number of tokens allowed
	MaxTokens int

	// PreserveSystemMessage keeps the system message even if pruning is needed
	PreserveSystemMessage bool

	// PreserveLastN keeps the last N messages
	PreserveLastN int

	// PruneFunc is the custom pruning function
	PruneFunc MessagePruneFunc
}

// DefaultPruneOptions returns default pruning options
func DefaultPruneOptions(maxTokens int) PruneOptions {
	return PruneOptions{
		MaxTokens:             maxTokens,
		PreserveSystemMessage: true,
		PreserveLastN:         5,
		PruneFunc:             DefaultMessagePrune,
	}
}

// DefaultMessagePrune is the default message pruning function
// It keeps the system message and the last N messages, removing older messages as needed
func DefaultMessagePrune(ctx context.Context, messages []types.Message, maxTokens int) ([]types.Message, error) {
	// Simple token estimation (4 chars ≈ 1 token)
	estimateTokens := func(msg types.Message) int {
		totalChars := 0
		for _, content := range msg.Content {
			if content.ContentType() == "text" {
				if textContent, ok := content.(types.TextContent); ok {
					totalChars += len(textContent.Text)
				}
			}
		}
		return totalChars / 4
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += estimateTokens(msg)
	}

	// If within limits, return as-is
	if totalTokens <= maxTokens {
		return messages, nil
	}

	// Prune from the middle, keeping system message and recent messages
	pruned := make([]types.Message, 0, len(messages))

	// Always keep system message if it exists
	systemMsg := types.Message{}
	hasSystem := false
	if len(messages) > 0 && messages[0].Role == "system" {
		systemMsg = messages[0]
		hasSystem = true
		pruned = append(pruned, systemMsg)
		messages = messages[1:]
	}

	// Keep last N messages
	preserveCount := 5
	if len(messages) <= preserveCount {
		pruned = append(pruned, messages...)
		return pruned, nil
	}

	// Take the last N messages
	startIdx := len(messages) - preserveCount
	pruned = append(pruned, messages[startIdx:]...)

	// Recalculate tokens
	newTotal := 0
	for _, msg := range pruned {
		newTotal += estimateTokens(msg)
	}

	// If still over limit, remove more aggressively
	if newTotal > maxTokens && len(pruned) > (preserveCount/2) {
		if hasSystem {
			pruned = append([]types.Message{systemMsg}, pruned[len(pruned)-(preserveCount/2):]...)
		} else {
			pruned = pruned[len(pruned)-(preserveCount/2):]
		}
	}

	return pruned, nil
}

// PruneMessages prunes messages to fit within token limits
func PruneMessages(ctx context.Context, messages []types.Message, opts PruneOptions) ([]types.Message, error) {
	if opts.PruneFunc == nil {
		opts.PruneFunc = DefaultMessagePrune
	}

	if opts.MaxTokens <= 0 {
		// No pruning needed
		return messages, nil
	}

	return opts.PruneFunc(ctx, messages, opts.MaxTokens)
}

// PruneToFitContext prunes messages to fit within a model's context window
func PruneToFitContext(ctx context.Context, messages []types.Message, contextWindow int, reserveTokens int) ([]types.Message, error) {
	maxTokens := contextWindow - reserveTokens
	if maxTokens <= 0 {
		maxTokens = contextWindow / 2 // Use half the context window as fallback
	}

	opts := DefaultPruneOptions(maxTokens)
	return PruneMessages(ctx, messages, opts)
}
