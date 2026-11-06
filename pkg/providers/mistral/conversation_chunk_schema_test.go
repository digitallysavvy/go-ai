package mistral

import "testing"

func TestConversationRequiredFieldsPresent(t *testing.T) {
	for _, c := range []struct {
		typ, data string
		ok        bool
	}{
		{"conversation.response.started", `{"type":"conversation.response.started","conversation_id":"c"}`, true},
		{"conversation.response.started", `{"type":"conversation.response.started"}`, false},
		{"conversation.response.done", `{"type":"conversation.response.done"}`, true},
		{"conversation.response.error", `{"message":"m","code":500}`, true},
		{"conversation.response.error", `{"message":"m"}`, false},
		{"message.output.delta", `{"id":"i","content":"x"}`, true},
		{"message.output.delta", `{"id":"i"}`, false},
		{"function.call.delta", `{"tool_call_id":"t","name":"n","arguments":""}`, true},
		{"function.call.delta", `{"name":"n","arguments":""}`, false},
		{"tool.execution.started", `{"id":"i","name":"n"}`, false},
		{"tool.execution.done", `{"id":"i","name":"n"}`, true},
	} {
		if _, ok := conversationRequiredFieldsPresent(c.data, c.typ); ok != c.ok {
			t.Errorf("%s %s: ok = %v, want %v", c.typ, c.data, ok, c.ok)
		}
	}
}
