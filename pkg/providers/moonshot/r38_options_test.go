package moonshot

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func moonshotMsg(role types.MessageRole, opts map[string]interface{}, content ...types.ContentPart) types.Message {
	return types.Message{Role: role, Content: content, ProviderOptions: map[string]interface{}{"moonshotai": opts}}
}

// TS parses message provider options with zod and throws
// InvalidArgumentError "invalid moonshotai provider options" on a mismatch.
func TestMoonshotMessageOptionsAreValidated(t *testing.T) {
	text := types.TextContent{Text: "hi"}
	schema := map[string]interface{}{"type": "object"}
	bad := map[string]types.Message{
		"non-string name":  moonshotMsg(types.RoleUser, map[string]interface{}{"name": 123}, text),
		"null name":        moonshotMsg(types.RoleUser, map[string]interface{}{"name": nil}, text),
		"partial false":    moonshotMsg(types.RoleAssistant, map[string]interface{}{"partial": false}, text),
		"partial string":   moonshotMsg(types.RoleAssistant, map[string]interface{}{"partial": "true"}, text),
		"tools not a list": moonshotMsg(types.RoleSystem, map[string]interface{}{"tools": "x"}),
		"tool missing inputSchema": moonshotMsg(types.RoleSystem, map[string]interface{}{"tools": []interface{}{
			map[string]interface{}{"type": "function", "name": "calc"}}}),
		"tool wrong type": moonshotMsg(types.RoleSystem, map[string]interface{}{"tools": []interface{}{
			map[string]interface{}{"type": "other", "name": "calc", "inputSchema": schema}}}),
		"tool non-bool strict": moonshotMsg(types.RoleSystem, map[string]interface{}{"tools": []interface{}{
			map[string]interface{}{"type": "function", "name": "calc", "inputSchema": schema, "strict": "y"}}}),
	}
	for name, msg := range bad {
		t.Run(name, func(t *testing.T) {
			_, _, err := convertToMoonshotChatMessages("kimi-k3", []types.Message{msg}, "")
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) || invalid.Message != "invalid moonshotai provider options" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// TS rejects `tools != null` on a non-system message, an empty list included.
func TestMoonshotEmptyDynamicToolsOnNonSystemMessageIsRejected(t *testing.T) {
	msg := moonshotMsg(types.RoleUser, map[string]interface{}{"tools": []interface{}{}}, types.TextContent{Text: "hi"})
	if _, _, err := convertToMoonshotChatMessages("kimi-k3", []types.Message{msg}, ""); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMoonshotModelOptionsRejectExplicitNull(t *testing.T) {
	for _, key := range []string{"strictJsonSchema", "logprobs", "topLogprobs", "reasoningEffort", "prediction", "thinking", "reasoningHistory", "promptCacheKey", "safetyIdentifier"} {
		if _, err := parseMoonshotModelOptions(map[string]interface{}{key: nil}); err == nil {
			t.Errorf("%s: null accepted", key)
		}
	}
	for _, k := range []string{"type", "budgetTokens"} {
		if _, err := parseMoonshotModelOptions(map[string]interface{}{"thinking": map[string]interface{}{k: nil}}); err == nil {
			t.Errorf("thinking.%s: null accepted", k)
		}
	}
}
