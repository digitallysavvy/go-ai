package alibaba

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ConvertToAlibabaChatMessages converts SDK messages to Alibaba API format
// Alibaba uses OpenAI-compatible message format
func ConvertToAlibabaChatMessages(messages []types.Message) []map[string]interface{} {
	// TODO: Implement in ALI-T08
	// This will convert:
	// - System messages to role: "system"
	// - User messages with text and images
	// - Assistant messages with text and tool calls
	// - Tool result messages
	result := make([]map[string]interface{}, 0, len(messages))

	for _, msg := range messages {
		converted := convertMessage(msg)
		if converted != nil {
			result = append(result, converted)
		}
	}

	return result
}

// convertMessage converts a single message to Alibaba format
func convertMessage(msg types.Message) map[string]interface{} {
	// TODO: Implement message conversion logic
	// Handle: system, user (text + images), assistant (text + tool calls), tool results
	return nil
}
