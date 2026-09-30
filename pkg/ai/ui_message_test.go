package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Ports of ai/packages/ai/src/ui/ui-messages.test.ts
// ---------------------------------------------------------------------------

// ports ui-messages.test.ts > describe('getStaticToolName')
func TestGetStaticToolName_Ports(t *testing.T) {
	t.Run("should return the tool name after the tool- prefix", func(t *testing.T) {
		part := &ToolUIPart{
			Type:       "tool-getLocation",
			ToolCallID: "tool1",
			State:      ToolStateOutputAvailable,
			Input:      map[string]interface{}{},
			Output:     "some result",
		}
		assert.Equal(t, "getLocation", GetStaticToolName(part))
	})

	t.Run("should return the tool name for tools that contains a dash", func(t *testing.T) {
		part := &ToolUIPart{
			Type:       "tool-get-location",
			ToolCallID: "tool1",
			State:      ToolStateOutputAvailable,
			Input:      map[string]interface{}{},
			Output:     "some result",
		}
		assert.Equal(t, "get-location", GetStaticToolName(part))
	})
}

// ports ui-messages.test.ts > describe('isCustomContentUIPart')
func TestIsCustomContentUIPart_Ports(t *testing.T) {
	t.Run("should return true for a custom part", func(t *testing.T) {
		part := CustomContentUIPart{
			Kind:             "test-provider.compaction",
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "cmp_123"}},
		}
		assert.True(t, IsCustomContentUIPart(part))
	})

	t.Run("should return true for a custom part without providerMetadata", func(t *testing.T) {
		part := CustomContentUIPart{Kind: "openai.compaction"}
		assert.True(t, IsCustomContentUIPart(part))
	})

	t.Run("should return false for a text part", func(t *testing.T) {
		part := TextUIPart{Text: "some text"}
		assert.False(t, IsCustomContentUIPart(part))
	})
}

// ports ui-messages.test.ts > describe('isDataUIPart')
func TestIsDataUIPart_Ports(t *testing.T) {
	t.Run("should return true if the part is a data part", func(t *testing.T) {
		part := DataUIPart{Type: "data-someDataPart", Data: "some data"}
		assert.True(t, IsDataUIPart(part))
	})

	t.Run("should return false if the part is not a data part", func(t *testing.T) {
		part := TextUIPart{Text: "some text"}
		assert.False(t, IsDataUIPart(part))
	})
}

// ports ui-messages.test.ts > describe('isToolOutputErrorUIPart')
func TestIsToolOutputErrorUIPart_Ports(t *testing.T) {
	t.Run("should return true for a static tool output error part", func(t *testing.T) {
		part := &ToolUIPart{
			Type:       "tool-weather",
			ToolCallID: "tool1",
			State:      ToolStateOutputError,
			Input:      map[string]interface{}{"city": "Berlin"},
			ErrorText:  "Weather service unavailable",
		}
		assert.True(t, IsToolOutputErrorUIPart(part))
	})

	t.Run("should return true for a dynamic tool output error part", func(t *testing.T) {
		part := &ToolUIPart{
			Type:       "dynamic-tool",
			ToolName:   "weather",
			ToolCallID: "tool1",
			State:      ToolStateOutputError,
			Input:      map[string]interface{}{"city": "Berlin"},
			ErrorText:  "Weather service unavailable",
		}
		assert.True(t, IsToolOutputErrorUIPart(part))
	})

	t.Run("should return false for a successful tool output part", func(t *testing.T) {
		part := &ToolUIPart{
			Type:       "tool-weather",
			ToolCallID: "tool1",
			State:      ToolStateOutputAvailable,
			Input:      map[string]interface{}{"city": "Berlin"},
			Output:     map[string]interface{}{"temperature": 18},
		}
		assert.False(t, IsToolOutputErrorUIPart(part))
	})

	t.Run("should return false for a non-tool part", func(t *testing.T) {
		part := TextUIPart{Text: "Weather service unavailable"}
		assert.False(t, IsToolOutputErrorUIPart(part))
	})
}

// Additional guard coverage not in ui-messages.test.ts but exercising the
// remaining exported guards in ui_message.go for parity with ui-messages.ts.
func TestUIMessagePartGuards_Additional(t *testing.T) {
	assert.True(t, IsTextUIPart(TextUIPart{Text: "hi"}))
	assert.False(t, IsTextUIPart(ReasoningUIPart{Text: "hi"}))

	assert.True(t, IsReasoningUIPart(ReasoningUIPart{Text: "thinking"}))
	assert.False(t, IsReasoningUIPart(TextUIPart{Text: "hi"}))

	assert.True(t, IsFileUIPart(FileUIPart{MediaType: "image/png", URL: "https://example.com/a.png"}))
	assert.False(t, IsFileUIPart(TextUIPart{Text: "hi"}))

	assert.True(t, IsReasoningFileUIPart(ReasoningFileUIPart{MediaType: "text/plain", URL: "data:..."}))
	assert.False(t, IsReasoningFileUIPart(FileUIPart{MediaType: "image/png", URL: "https://example.com/a.png"}))

	staticPart := &ToolUIPart{Type: "tool-weather", ToolCallID: "1", State: ToolStateInputStreaming}
	dynamicPart := &ToolUIPart{Type: "dynamic-tool", ToolName: "weather", ToolCallID: "1", State: ToolStateInputStreaming}

	assert.True(t, IsStaticToolUIPart(staticPart))
	assert.False(t, IsStaticToolUIPart(dynamicPart))
	assert.True(t, IsDynamicToolUIPart(dynamicPart))
	assert.False(t, IsDynamicToolUIPart(staticPart))
	assert.True(t, IsToolUIPart(staticPart))
	assert.True(t, IsToolUIPart(dynamicPart))
	assert.False(t, IsToolUIPart(TextUIPart{Text: "hi"}))

	assert.Equal(t, "weather", GetToolName(staticPart))
	assert.Equal(t, "weather", GetToolName(dynamicPart))
	assert.Equal(t, "weather", GetToolOrDynamicToolName(staticPart))
}

// ---------------------------------------------------------------------------
// JSON round-trip tests (PRD acceptance: "JSON of UIMessage round-trips with
// TS fixtures byte-for-byte (camelCase keys)"). Fixtures are transcribed from
// literals in convert-to-model-messages.test.ts and validate-ui-messages.test.ts.
// ---------------------------------------------------------------------------

// roundTrip unmarshals wantJSON into a UIMessage, marshals it back out, and
// asserts the result is JSON-equal (value equality) to wantJSON. It also
// returns the actually-produced bytes so callers can additionally assert key
// order for representative cases.
func roundTrip(t *testing.T, wantJSON string) []byte {
	t.Helper()
	var msg UIMessage
	require.NoError(t, json.Unmarshal([]byte(wantJSON), &msg))
	got, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.JSONEq(t, wantJSON, string(got))
	return got
}

func TestUIMessageJSONRoundTrip_Text(t *testing.T) {
	// ports convert-to-model-messages.test.ts fixtures using a text part.
	got := roundTrip(t, `{
		"id": "1",
		"role": "user",
		"parts": [{ "type": "text", "text": "hi", "state": "done" }]
	}`)
	// key order: type, text, state (no providerMetadata key present)
	assert.Equal(t, `{"id":"1","role":"user","parts":[{"type":"text","text":"hi","state":"done"}]}`, string(got))
}

func TestUIMessageJSONRoundTrip_Custom(t *testing.T) {
	// ports ui-messages.test.ts isCustomContentUIPart fixture.
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "custom",
			"kind": "test-provider.compaction",
			"providerMetadata": { "openai": { "itemId": "cmp_123" } }
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_Reasoning(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "reasoning",
			"id": "r1",
			"text": "thinking...",
			"state": "done"
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_SourceURL(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "source-url",
			"sourceId": "src1",
			"url": "https://example.com",
			"title": "Example"
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_SourceDocument(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "source-document",
			"sourceId": "src1",
			"mediaType": "application/pdf",
			"title": "Doc",
			"filename": "doc.pdf"
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_File(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "user",
		"parts": [{
			"type": "file",
			"mediaType": "image/png",
			"filename": "a.png",
			"url": "https://example.com/a.png"
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_ReasoningFile(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "reasoning-file",
			"mediaType": "text/plain",
			"url": "data:text/plain;base64,aGVsbG8="
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_StepStart(t *testing.T) {
	got := roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{ "type": "step-start" }]
	}`)
	assert.Equal(t, `{"id":"1","role":"assistant","parts":[{"type":"step-start"}]}`, string(got))
}

func TestUIMessageJSONRoundTrip_Data(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "data-weather",
			"id": "d1",
			"data": { "city": "Berlin", "temp": 18 }
		}]
	}`)
}

func TestUIMessageJSONRoundTrip_DynamicTool(t *testing.T) {
	roundTrip(t, `{
		"id": "1",
		"role": "assistant",
		"parts": [{
			"type": "dynamic-tool",
			"toolName": "weather",
			"toolCallId": "call1",
			"state": "output-available",
			"input": { "city": "Berlin" },
			"output": { "temperature": 18 }
		}]
	}`)
}

// Tool part states, ported from convert-to-model-messages.test.ts /
// validate-ui-messages.test.ts fixtures covering every ToolUIPart state.
func TestUIMessageJSONRoundTrip_ToolPartStates(t *testing.T) {
	cases := map[string]string{
		"input-streaming": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "input-streaming",
				"input": { "city": "Berl" }
			}]
		}`,
		"input-available": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "input-available",
				"input": { "city": "Berlin" }
			}]
		}`,
		"output-available": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "output-available",
				"input": { "city": "Berlin" },
				"output": { "temperature": 18 }
			}]
		}`,
		"output-error": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "output-error",
				"input": { "city": "Berlin" },
				"errorText": "Weather service unavailable"
			}]
		}`,
		"approval-requested": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "approval-requested",
				"input": { "city": "Berlin" },
				"approval": { "id": "approval1", "requestReason": "needs confirmation" }
			}]
		}`,
		"approval-responded-approved": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "approval-responded",
				"input": { "city": "Berlin" },
				"approval": { "id": "approval1", "approved": true, "reason": "looks fine" }
			}]
		}`,
		"output-denied": `{
			"id": "1", "role": "assistant",
			"parts": [{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "output-denied",
				"input": { "city": "Berlin" },
				"approval": { "id": "approval1", "approved": false, "reason": "not allowed" }
			}]
		}`,
	}

	for name, wantJSON := range cases {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, wantJSON)
		})
	}
}

func TestUIMessageJSONRoundTrip_MultiPartMessage(t *testing.T) {
	// A composite message exercising several part kinds together, plus
	// message-level metadata.
	roundTrip(t, `{
		"id": "msg1",
		"role": "assistant",
		"metadata": { "createdAt": 12345 },
		"parts": [
			{ "type": "step-start" },
			{ "type": "text", "text": "Looking that up...", "state": "done" },
			{
				"type": "tool-weather",
				"toolCallId": "call1",
				"state": "output-available",
				"input": { "city": "Berlin" },
				"output": { "temperature": 18 }
			},
			{ "type": "text", "text": "It is 18C in Berlin.", "state": "done" }
		]
	}`)
}
