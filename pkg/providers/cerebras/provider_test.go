package cerebras

import "testing"

func TestRenameReasoningContentMatchesTopLevelAssistantMessages(t *testing.T) {
	payload := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":              "assistant",
				"content":           "done",
				"reasoning_content": "think",
			},
			map[string]interface{}{
				"role":              "user",
				"reasoning_content": "leave-user-alone",
			},
			map[string]interface{}{
				"role":              "assistant",
				"reasoning":         "existing",
				"reasoning_content": "do-not-overwrite",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"role":              "assistant",
						"reasoning_content": "nested-untouched",
					},
				},
			},
		},
	}

	renameReasoningContent(payload)

	messages := payload["messages"].([]interface{})
	first := messages[0].(map[string]interface{})
	if first["reasoning"] != "think" {
		t.Fatalf("first reasoning = %v, want think", first["reasoning"])
	}
	if _, ok := first["reasoning_content"]; ok {
		t.Fatalf("first reasoning_content should be removed: %#v", first)
	}

	user := messages[1].(map[string]interface{})
	if user["reasoning_content"] != "leave-user-alone" {
		t.Fatalf("user reasoning_content = %v, want untouched", user["reasoning_content"])
	}

	third := messages[2].(map[string]interface{})
	if third["reasoning"] != "existing" {
		t.Fatalf("existing reasoning overwritten: %#v", third)
	}
	if _, ok := third["reasoning_content"]; ok {
		t.Fatalf("third reasoning_content should be removed: %#v", third)
	}
	toolCalls := third["tool_calls"].([]interface{})
	nested := toolCalls[0].(map[string]interface{})
	if nested["reasoning_content"] != "nested-untouched" {
		t.Fatalf("nested reasoning_content = %v, want untouched", nested["reasoning_content"])
	}
}
