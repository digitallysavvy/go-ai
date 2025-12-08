package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestPruneMessages_UnderLimit(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi!"}}},
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: 1000, // Much higher than needed
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 2 {
		t.Errorf("expected 2 messages, got %d", len(pruned))
	}
}

func TestPruneMessages_OverLimit(t *testing.T) {
	t.Parallel()

	// Create many messages
	messages := make([]types.Message, 20)
	for i := 0; i < 20; i++ {
		messages[i] = types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "This is a somewhat longer message to use up tokens."}},
		}
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens:     50, // Very low limit
		PreserveLastN: 3,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) > len(messages) {
		t.Errorf("pruned should not be larger than original")
	}
	// Should have fewer messages due to pruning
	if len(pruned) >= 20 {
		t.Errorf("expected pruning, got %d messages", len(pruned))
	}
}

func TestPruneMessages_PreserveSystemMessage(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: "system", Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Long message 1 " + string(make([]byte, 500))}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Long message 2 " + string(make([]byte, 500))}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Long message 3 " + string(make([]byte, 500))}}},
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens:             100,
		PreserveSystemMessage: true,
		PreserveLastN:         2,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First message should be system message
	if len(pruned) > 0 && pruned[0].Role != "system" {
		t.Error("expected system message to be preserved as first message")
	}
}

func TestPruneMessages_PreserveLastN(t *testing.T) {
	t.Parallel()

	messages := make([]types.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: "Message " + string(rune('0'+i))}},
		}
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens:     10,
		PreserveLastN: 5,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should preserve at least some of the last messages
	if len(pruned) == 0 {
		t.Error("expected some messages to be preserved")
	}
}

func TestPruneMessages_ZeroMaxTokens(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: 0, // Zero means no pruning
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message (no pruning), got %d", len(pruned))
	}
}

func TestPruneMessages_NegativeMaxTokens(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: -100, // Negative means no pruning
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message (no pruning), got %d", len(pruned))
	}
}

func TestPruneMessages_CustomPruneFunc(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi!"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "How are you?"}}},
	}

	customPruneCalled := false
	customPrune := func(ctx context.Context, msgs []types.Message, maxTokens int) ([]types.Message, error) {
		customPruneCalled = true
		// Custom pruning: just return first message
		return msgs[:1], nil
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: 10,
		PruneFunc: customPrune,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !customPruneCalled {
		t.Error("expected custom prune function to be called")
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message from custom prune, got %d", len(pruned))
	}
}

func TestPruneMessages_CustomPruneFuncError(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	expectedErr := errors.New("custom prune error")
	customPrune := func(ctx context.Context, msgs []types.Message, maxTokens int) ([]types.Message, error) {
		return nil, expectedErr
	}

	_, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: 10,
		PruneFunc: customPrune,
	})

	if err == nil {
		t.Fatal("expected error from custom prune function")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected custom error, got: %v", err)
	}
}

func TestPruneToFitContext_Basic(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	pruned, err := PruneToFitContext(context.Background(), messages, 1000, 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message, got %d", len(pruned))
	}
}

func TestPruneToFitContext_NegativeReserve(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	// Reserve more than context window
	pruned, err := PruneToFitContext(context.Background(), messages, 100, 200)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still work with fallback
	if pruned == nil {
		t.Error("expected non-nil result")
	}
}

func TestDefaultPruneOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultPruneOptions(1000)

	if opts.MaxTokens != 1000 {
		t.Errorf("expected MaxTokens 1000, got %d", opts.MaxTokens)
	}
	if !opts.PreserveSystemMessage {
		t.Error("expected PreserveSystemMessage to be true")
	}
	if opts.PreserveLastN != 5 {
		t.Errorf("expected PreserveLastN 5, got %d", opts.PreserveLastN)
	}
	if opts.PruneFunc == nil {
		t.Error("expected non-nil PruneFunc")
	}
}

func TestDefaultMessagePrune_EmptyMessages(t *testing.T) {
	t.Parallel()

	messages := []types.Message{}

	pruned, err := DefaultMessagePrune(context.Background(), messages, 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 messages, got %d", len(pruned))
	}
}

func TestDefaultMessagePrune_OnlySystemMessage(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: "system", Content: []types.ContentPart{types.TextContent{Text: "System prompt"}}},
	}

	pruned, err := DefaultMessagePrune(context.Background(), messages, 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message, got %d", len(pruned))
	}
}

func TestDefaultMessagePrune_FewMessages(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: "system", Content: []types.ContentPart{types.TextContent{Text: "System"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "User"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Assistant"}}},
	}

	pruned, err := DefaultMessagePrune(context.Background(), messages, 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should keep all when count <= preserveCount
	if len(pruned) != 3 {
		t.Errorf("expected 3 messages, got %d", len(pruned))
	}
}

func TestDefaultMessagePrune_NonTextContent(t *testing.T) {
	t.Parallel()

	// Message with image content (no text)
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte("fake-image"), MimeType: "image/png"},
		}},
	}

	pruned, err := DefaultMessagePrune(context.Background(), messages, 100)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still work with non-text content
	if len(pruned) != 1 {
		t.Errorf("expected 1 message, got %d", len(pruned))
	}
}

func TestDefaultMessagePrune_AggressivePruning(t *testing.T) {
	t.Parallel()

	// Create many long messages
	messages := make([]types.Message, 15)
	for i := 0; i < 15; i++ {
		messages[i] = types.Message{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: string(make([]byte, 1000))}, // 1000 chars each
			},
		}
	}

	pruned, err := DefaultMessagePrune(context.Background(), messages, 100) // Very low limit

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should aggressively prune when still over limit
	if len(pruned) >= 15 {
		t.Errorf("expected aggressive pruning, got %d messages", len(pruned))
	}
}

func TestPruneMessages_NilPruneFunc(t *testing.T) {
	t.Parallel()

	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
	}

	pruned, err := PruneMessages(context.Background(), messages, PruneOptions{
		MaxTokens: 100,
		PruneFunc: nil, // Should use default
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("expected 1 message, got %d", len(pruned))
	}
}
