package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ---------------------------------------------------------------------------
// Ports of ai/packages/ai/src/ui/validate-ui-messages.test.ts
// ---------------------------------------------------------------------------

func testFooTool() types.Tool {
	return types.Tool{
		Name: "foo",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"foo": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"foo"},
		},
		OutputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"result": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"result"},
		},
	}
}

// ports describe('parameter validation')
func TestValidateUIMessages_ParameterValidation(t *testing.T) {
	t.Run("should throw InvalidArgumentError when messages parameter is null", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: nil})
		require.Error(t, err)
	})

	t.Run("should throw TypeValidationError when messages array is empty", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(`[]`)})
		require.Error(t, err)
	})

	t.Run("should throw TypeValidationError when message has empty parts array", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"user","parts":[]}]`),
		})
		require.Error(t, err)
	})

	t.Run("should validate chat ending with assistant message with empty parts array", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[
				{"id":"1","role":"user","parts":[{"type":"text","text":"hi"}]},
				{"id":"2","role":"assistant","parts":[]}
			]`),
		})
		require.NoError(t, err)
		require.Len(t, msgs, 2)
		assert.Empty(t, msgs[1].Parts)
	})
}

// TestValidateUIMessagesStructure_ToolPartRequiredFields ports the TS
// uiMessagesSchema's per-state requirement that `input` (and, for
// output-available, `output`) is a required key (any value, including
// null, satisfies z.unknown()) for every tool state except input-streaming
// and output-error, where it is optional. This runs at the structural
// (schema) validation layer, before any Tools/DataSchemas are considered.
func TestValidateUIMessagesStructure_ToolPartRequiredFields(t *testing.T) {
	build := func(state, extra string) string {
		return `[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"` + state + `"` + extra + `}]}]`
	}

	requiresInput := []string{"input-available", "approval-requested", "approval-responded", "output-denied"}
	for _, state := range requiresInput {
		state := state
		t.Run("state "+state+" rejects a part with no input key", func(t *testing.T) {
			extra := ""
			switch state {
			case "approval-requested":
				extra = `,"approval":{"id":"a1"}`
			case "approval-responded":
				extra = `,"approval":{"id":"a1","approved":true}`
			case "output-denied":
				extra = `,"approval":{"id":"a1","approved":false}`
			}
			_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(build(state, extra)),
			})
			require.Error(t, err)
		})

		t.Run("state "+state+" accepts an explicit null input", func(t *testing.T) {
			extra := `,"input":null`
			switch state {
			case "approval-requested":
				extra += `,"approval":{"id":"a1"}`
			case "approval-responded":
				extra += `,"approval":{"id":"a1","approved":true}`
			case "output-denied":
				extra += `,"approval":{"id":"a1","approved":false}`
			}
			_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(build(state, extra)),
			})
			require.NoError(t, err)
		})
	}

	t.Run("state output-available rejects a part with no input or output key", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(build("output-available", "")),
		})
		require.Error(t, err)
	})

	t.Run("state output-available rejects a part with input but no output key", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(build("output-available", `,"input":{}`)),
		})
		require.Error(t, err)
	})

	t.Run("state output-available accepts explicit null input and output", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(build("output-available", `,"input":null,"output":null`)),
		})
		require.NoError(t, err)
	})

	t.Run("state input-streaming accepts a part with no input key", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(build("input-streaming", "")),
		})
		require.NoError(t, err)
	})

	t.Run("state output-error accepts a part with no input key", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(build("output-error", `,"errorText":"boom"`)),
		})
		require.NoError(t, err)
	})
}

// ports describe('metadata')
func TestValidateUIMessages_Metadata(t *testing.T) {
	t.Run("should validate a user message with metadata when no metadata schema is provided", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"user","metadata":{"anything":"goes"},"parts":[{"type":"text","text":"hi"}]}]`),
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"anything": "goes"}, msgs[0].Metadata)
	})

	metaSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"userId": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"userId"},
	})

	t.Run("should validate a user message with metadata", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:       json.RawMessage(`[{"id":"1","role":"user","metadata":{"userId":"u1"},"parts":[{"type":"text","text":"hi"}]}]`),
			MetadataSchema: metaSchema,
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]interface{}{"userId": "u1"}, msgs[0].Metadata)
	})

	t.Run("should throw type validation error when metadata is invalid", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:       json.RawMessage(`[{"id":"1","role":"user","metadata":{},"parts":[{"type":"text","text":"hi"}]}]`),
			MetadataSchema: metaSchema,
		})
		require.Error(t, err)
	})
}

// ports describe('text parts') / describe('custom parts')
func TestValidateUIMessages_TextAndCustomParts(t *testing.T) {
	t.Run("should validate a user message with a text part", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"user","parts":[{"type":"text","text":"Hello"}]}]`),
		})
		require.NoError(t, err)
		require.Len(t, msgs[0].Parts, 1)
		assert.Equal(t, "text", msgs[0].Parts[0].UIPartType())
	})

	t.Run("should validate an assistant message with a custom part", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"custom","kind":"provider.custom","providerMetadata":{"p":{"a":1}}}]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*CustomContentUIPart)
		assert.Equal(t, "provider.custom", part.Kind)
	})

	t.Run("should validate an assistant message with a custom part without providerMetadata", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"custom","kind":"provider.custom"}]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*CustomContentUIPart)
		assert.Nil(t, part.ProviderMetadata)
	})
}

// ports describe('data parts')
func TestValidateUIMessages_DataParts(t *testing.T) {
	dataSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"city"},
	})

	t.Run("should validate an assistant message with two data parts", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"data-weather","id":"a","data":{"city":"Tokyo"}},
				{"type":"data-weather","id":"b","data":{"city":"Kyoto"}}
			]}]`),
			DataSchemas: map[string]schema.Schema{"weather": dataSchema},
		})
		require.NoError(t, err)
		require.Len(t, msgs[0].Parts, 2)
	})

	t.Run("should return parsed data", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:    json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"data-weather","id":"a","data":{"city":"Tokyo"}}]}]`),
			DataSchemas: map[string]schema.Schema{"weather": dataSchema},
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*DataUIPart)
		assert.Equal(t, map[string]interface{}{"city": "Tokyo"}, part.Data)
	})

	t.Run("should throw type validation error when data is invalid", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:    json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"data-weather","id":"a","data":{}}]}]`),
			DataSchemas: map[string]schema.Schema{"weather": dataSchema},
		})
		require.Error(t, err)
	})

	t.Run("should throw type validation error when there is no data schema for a data part", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:    json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"data-other","id":"a","data":{}}]}]`),
			DataSchemas: map[string]schema.Schema{"weather": dataSchema},
		})
		require.Error(t, err)
	})
}

// ports describe('dynamic tool parts')
func TestValidateUIMessages_DynamicToolParts(t *testing.T) {
	t.Run("should preserve titles on static and dynamic tool parts in every state", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-streaming","title":"Foo Tool"}
			]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "Foo Tool", part.Title)
	})

	t.Run("input-streaming state", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-streaming","input":{"foo":"bar"}}
			]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, ToolStateInputStreaming, part.State)
	})

	t.Run("output-error state", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"output-error","input":{"foo":"bar"},"errorText":"bad"}
			]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "bad", part.ErrorText)
	})

	t.Run("output-error state when input key is absent", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"output-error","errorText":"bad"}
			]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Nil(t, part.Input)
	})
}

// ports describe('tool parts')
func TestValidateUIMessages_ToolParts(t *testing.T) {
	tools := []types.Tool{testFooTool()}

	t.Run("input-streaming state accepts any (unvalidated) input", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"input-streaming","input":{"foo":"bar"},"providerExecuted":true}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.True(t, *part.ProviderExecuted)
	})

	t.Run("should validate tool input when state is input-available", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"input-available","input":{"foo":123}}
			]}]`),
			Tools: tools,
		})
		require.Error(t, err)
	})

	t.Run("should validate tool input when state is output-available", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":123},"output":{"result":"ok"}}
			]}]`),
			Tools: tools,
		})
		require.Error(t, err)
	})

	t.Run("should validate tool output when state is output-available", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":"bar"},"output":{"result":123}}
			]}]`),
			Tools: tools,
		})
		require.Error(t, err)
	})

	t.Run("should preserve result provider metadata when state is output-available", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":"bar"},"output":{"result":"ok"},
				 "resultProviderMetadata":{"p":{"a":1}}}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, map[string]interface{}{"p": map[string]interface{}{"a": float64(1)}}, part.ResultProviderMetadata)
	})

	t.Run("should skip tool input validation when state is output-error and there is no input", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-error","errorText":"bad"}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		assert.Equal(t, "tool-foo", msgs[0].Parts[0].UIPartType())
	})

	t.Run("should preserve rawInput when state is output-error", func(t *testing.T) {
		// also ports the rawInput-deprecation-warning assertion from
		// validate-ui-messages.test.ts.
		buf := setupLogWarnings(t)
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-error","errorText":"bad","rawInput":"legacy"}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "legacy", part.RawInput)
		assert.Contains(t, buf.String(), `Deprecated: "rawInput in output-error UI message parts". Use the "input" field instead. The "rawInput" field will be removed in the next major version.`)
	})

	t.Run("should not log a deprecation warning when rawInput is absent", func(t *testing.T) {
		buf := setupLogWarnings(t)
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-error","errorText":"bad"}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("should throw error when no tool schema is found", func(t *testing.T) {
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-bar","toolCallId":"1","state":"input-available","input":{}}
			]}]`),
			Tools: tools,
		})
		require.Error(t, err)
	})

	t.Run("should represent terminal calls from missing tools as dynamic tool parts", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-bar","toolCallId":"1","state":"output-available","input":{"x":1},"output":"done"}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "dynamic-tool", part.Type)
		assert.Equal(t, "bar", part.ToolName)
	})

	t.Run("should represent terminal calls as dynamic tool parts when agent tools are omitted", func(t *testing.T) {
		// ValidateUIMessagesForAgent converts terminal parts to dynamic even
		// when Tools is nil (agent tools unavailable at validation time).
		msgs, err := ValidateUIMessagesForAgent(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":"bar"},"output":{"result":"ok"}}
			]}]`),
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "dynamic-tool", part.Type)
		assert.Equal(t, "foo", part.ToolName)
	})

	t.Run("should reject stale terminal input for an available agent tool", func(t *testing.T) {
		_, err := ValidateUIMessagesForAgent(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":123},"output":{"result":"ok"}}
			]}]`),
			Tools: tools,
		})
		require.Error(t, err)
	})

	t.Run("should validate automatic approval reasons on output parts", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{"foo":"bar"},"output":{"result":"ok"},
				 "approval":{"id":"a1","approved":true,"isAutomatic":true,"reason":"auto-approved"}}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		require.NotNil(t, part.Approval)
		assert.True(t, *part.Approval.IsAutomatic)
		assert.Equal(t, "auto-approved", part.Approval.Reason)
	})

	t.Run("should validate request and response reasons throughout approval", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"approval-responded","input":{"foo":"bar"},
				 "approval":{"id":"a1","approved":true,"requestReason":"needs review","reason":"looks fine"}}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "needs review", part.Approval.RequestReason)
		assert.Equal(t, "looks fine", part.Approval.Reason)
	})

	t.Run("should preserve approval descriptors", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"approval-requested","input":{"foo":"bar"},
				 "approval":{"id":"a1","descriptor":{"kind":"custom","weight":3}}}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, map[string]interface{}{"kind": "custom", "weight": float64(3)}, part.Approval.Descriptor)
	})

	t.Run("should not validate input in input-streaming state", func(t *testing.T) {
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"input-streaming","input":{"foo":123}}
			]}]`),
			Tools: tools,
		})
		require.NoError(t, err)
		assert.Equal(t, "tool-foo", msgs[0].Parts[0].UIPartType())
	})

	t.Run("should reject transformed approval input that does not match its schema input", func(t *testing.T) {
		countTool := types.Tool{
			Name: "count",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"count"},
			},
		}
		_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":4},
				 "approval":{"id":"approval-1","approved":true,"inputSchemaInput":{"count":"3"}}}
			]}]`),
			Tools: []types.Tool{countTool},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match the output reconstructed from inputSchemaInput")
	})

	t.Run("should compare transformed approval input after applying input refinement", func(t *testing.T) {
		refinedTool := types.Tool{
			Name: "refined",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
				"required":   []interface{}{"value"},
			},
		}
		msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-refined","toolCallId":"call-1","state":"approval-responded","input":{"value":"trimmed"},
				 "approval":{"id":"approval-1","approved":true,"inputSchemaInput":{"value":" trimmed "}}}
			]}]`),
			Tools: []types.Tool{refinedTool},
			ExperimentalRefineToolInput: map[string]ToolInputRefiner{
				"refined": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
					value, _ := opts.ToolCall.Arguments["value"].(string)
					return map[string]interface{}{"value": trimSpace(value)}, nil
				},
			},
		})
		require.NoError(t, err)
		part := msgs[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, map[string]interface{}{"value": "trimmed"}, part.Input)
	})
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

// ports describe('safeValidateUIMessages')
func TestSafeValidateUIMessages(t *testing.T) {
	t.Run("should return success result for valid messages", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"user","parts":[{"type":"text","text":"hi"}]}]`),
		})
		assert.True(t, result.Success)
		assert.Nil(t, result.Error)
		assert.Len(t, result.Data, 1)
	})

	t.Run("should return failure result when messages parameter is null", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: nil})
		assert.False(t, result.Success)
		assert.Error(t, result.Error)
	})

	t.Run("should return failure result when messages array is empty", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(`[]`)})
		assert.False(t, result.Success)
	})

	t.Run("should return failure result when message has empty parts array", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"user","parts":[]}]`),
		})
		assert.False(t, result.Success)
	})

	t.Run("should return success result for chat ending with assistant message with empty parts array", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[]}]`),
		})
		assert.True(t, result.Success)
	})

	t.Run("should return failure result when tool input validation fails", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-foo","toolCallId":"1","state":"input-available","input":{"foo":123}}
			]}]`),
			Tools: []types.Tool{testFooTool()},
		})
		assert.False(t, result.Success)
		assert.Error(t, result.Error)
	})

	t.Run("should return unavailable terminal tools as dynamic tool parts", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
				{"type":"tool-bar","toolCallId":"1","state":"output-available","input":{"x":1},"output":"done"}
			]}]`),
			Tools: []types.Tool{testFooTool()},
		})
		require.True(t, result.Success)
		part := result.Data[0].Parts[0].(*ToolUIPart)
		assert.Equal(t, "dynamic-tool", part.Type)
	})

	t.Run("should return failure result when data schema is missing", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages:    json.RawMessage(`[{"id":"1","role":"assistant","parts":[{"type":"data-other","id":"a","data":{}}]}]`),
			DataSchemas: map[string]schema.Schema{},
		})
		assert.False(t, result.Success)
	})

	t.Run("should return failure result for invalid message structure", func(t *testing.T) {
		result := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
			Messages: json.RawMessage(`[{"id":"1","role":"nope","parts":[]}]`),
		})
		assert.False(t, result.Success)
	})
}

// Ported from validate-ui-messages.test.ts "should warn when rawInput is
// present in output-error state" (issue #51): ValidateUIMessages logs a
// deprecation warning via LogWarnings when an output-error tool part still
// carries the deprecated rawInput field. The global warning logger is
// process state, so this test is not parallel (see log_warnings_test.go).
func TestValidateUIMessages_WarnsOnDeprecatedRawInput(t *testing.T) {
	buf := setupLogWarnings(t)
	tools := []types.Tool{testFooTool()}
	_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-foo","toolCallId":"1","state":"output-error","errorText":"bad","rawInput":{"foo":"bar"}}
		]}]`),
		Tools: tools,
	})
	require.NoError(t, err)
	lines := logLines(buf)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], `rawInput in output-error UI message parts`)
	assert.Contains(t, lines[1], `Use the "input" field instead`)
}

// A rawInput-free output-error part, or a non-output-error part carrying
// rawInput, must not trigger the warning.
func TestValidateUIMessages_NoWarningWithoutDeprecatedRawInput(t *testing.T) {
	buf := setupLogWarnings(t)
	tools := []types.Tool{testFooTool()}
	_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-foo","toolCallId":"1","state":"output-error","errorText":"bad"}
		]}]`),
		Tools: tools,
	})
	require.NoError(t, err)
	assert.Empty(t, logLines(buf))
}
