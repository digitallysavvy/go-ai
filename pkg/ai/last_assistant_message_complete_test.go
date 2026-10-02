package ai

import "testing"

func ptrBool(v bool) *bool { return &v }

// Ported from TS ui/last-assistant-message-is-complete-with-tool-calls.test.ts.
func TestLastAssistantMessageIsCompleteWithToolCalls(t *testing.T) {
	t.Parallel()

	t.Run("false if the last step of a multi-step sequence only has text", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getLocation",
						ToolCallID: "call_CuEdmzpx4ZldCkg5SVr3ikLz",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{},
						Output:     "New York",
					},
					&StepStartUIPart{},
					&TextUIPart{Text: "The current weather in New York is windy.", State: UIPartStateDone},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("true when there is a text part after the last tool result in the last step", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_6iy0GxZ9R4VDI5MKohXxV48y",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "New York"},
						Output:     "windy",
					},
					&TextUIPart{Text: "The current weather in New York is windy.", State: UIPartStateDone},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("true when the tool has an output-error state", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_6iy0GxZ9R4VDI5MKohXxV48y",
						State:      ToolStateOutputError,
						Input:      map[string]interface{}{"city": "New York"},
						ErrorText:  "Unable to get weather information",
					},
					&TextUIPart{Text: "The current weather in New York is windy.", State: UIPartStateDone},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("true when dynamic tool call is complete", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"location": "San Francisco"},
						Output:     "sunny",
					},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("false when a tool output is preliminary", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:        "tool-getWeatherInformation",
						ToolCallID:  "call_1",
						State:       ToolStateOutputAvailable,
						Input:       map[string]interface{}{"city": "New York"},
						Output:      "checking weather station",
						Preliminary: ptrBool(true),
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("false when a dynamic tool output is preliminary", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:        "dynamic-tool",
						ToolName:    "getDynamicWeather",
						ToolCallID:  "call_1",
						State:       ToolStateOutputAvailable,
						Input:       map[string]interface{}{"location": "San Francisco"},
						Output:      "checking weather station",
						Preliminary: ptrBool(true),
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("false when dynamic tool call is still streaming input", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateInputStreaming,
						Input:      map[string]interface{}{"location": "San Francisco"},
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("false when dynamic tool call has input but no output", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateInputAvailable,
						Input:      map[string]interface{}{"location": "San Francisco"},
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("true when dynamic tool call has an error", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateOutputError,
						Input:      map[string]interface{}{"location": "San Francisco"},
						ErrorText:  "Failed to fetch weather data",
					},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("true when mixing regular and dynamic tool calls and all are complete", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_regular_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "New York"},
						Output:     "windy",
					},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"location": "San Francisco"},
						Output:     "sunny",
					},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("false when mixing regular and dynamic tool calls and some are incomplete", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_regular_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "New York"},
						Output:     "windy",
					},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_123",
						State:      ToolStateInputAvailable, // incomplete
						Input:      map[string]interface{}{"location": "San Francisco"},
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("true for multi-step sequence where last step has complete dynamic tool calls", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getLocation",
						ToolCallID: "call_location_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{},
						Output:     "New York",
					},
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_456",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"location": "New York"},
						Output:     "cloudy",
					},
					&TextUIPart{Text: "The current weather in New York is cloudy.", State: UIPartStateDone},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("false for multi-step sequence where last step has incomplete dynamic tool calls", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getLocation",
						ToolCallID: "call_location_123",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{},
						Output:     "New York",
					},
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "dynamic-tool",
						ToolName:   "getDynamicWeather",
						ToolCallID: "call_dynamic_456",
						State:      ToolStateInputStreaming, // incomplete
						Input:      map[string]interface{}{"location": "New York"},
					},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("false for complete provider executed tool calls", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:             "tool-web_search",
						Input:            map[string]interface{}{"query": "New York weather"},
						State:            ToolStateOutputAvailable,
						Output:           []interface{}{},
						ToolCallID:       "srvtoolu_01KSMqkKSbgKhCwGZHQDaV48",
						ProviderExecuted: ptrBool(true),
					},
					&TextUIPart{Text: "The current weather in New York is windy.", State: UIPartStateDone},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		if LastAssistantMessageIsCompleteWithToolCalls(nil) != false {
			t.Error("expected false for empty messages")
		}
	})

	t.Run("last message is a user message", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{ID: "1", Role: UIMessageRoleUser, Parts: []UIMessagePart{}},
		})
		if got != false {
			t.Error("expected false for a user message")
		}
	})

	// Ported from TS 9941f32ab9 (#21622): a completed backend tool output
	// followed by terminal text lacking a completed stream state must not
	// be treated as a resumption signal.
	t.Run("false when trailing text has no completed stream state", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_6iy0GxZ9R4VDI5MKohXxV48y",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "New York"},
						Output:     map[string]interface{}{"success": true, "queryResult": "large result"},
					},
					&TextUIPart{Text: "Prompt is too long"},
				},
			},
		})
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("true when text precedes the last completed tool call", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithToolCalls([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&TextUIPart{Text: "I will check the weather.", State: UIPartStateDone},
					&ToolUIPart{
						Type:       "tool-getWeatherInformation",
						ToolCallID: "call_1",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "New York"},
						Output:     "windy",
					},
				},
			},
		})
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})
}

// Ported from TS ui/last-assistant-message-is-complete-with-approval-responses.test.ts.
func TestLastAssistantMessageIsCompleteWithApprovalResponses(t *testing.T) {
	t.Parallel()

	t.Run("false if messages is empty", func(t *testing.T) {
		if LastAssistantMessageIsCompleteWithApprovalResponses(nil) != false {
			t.Error("expected false")
		}
	})

	t.Run("false if last message is a user message", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{ID: "1", Role: UIMessageRoleUser, Parts: []UIMessagePart{}},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("false if there are no tool invocations in the last step", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&TextUIPart{Text: "Hello", State: UIPartStateDone},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("false if no tool has approval-responded state", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalRequested,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1"},
					},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("false if some tools still have approval-requested state", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_2",
						State:      ToolStateApprovalRequested,
						Input:      map[string]interface{}{"city": "Paris"},
						Approval:   &ToolUIPartApproval{ID: "approval_2"},
					},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("true when a non-provider-executed tool has approval-responded", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
				},
			},
		})
		if got != true {
			t.Error("expected true")
		}
	})

	t.Run("true when a provider-executed tool has approval-responded", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:             "dynamic-tool",
						ToolName:         "mcp.shorten_url",
						ToolCallID:       "call_1",
						State:            ToolStateApprovalResponded,
						Input:            map[string]interface{}{"url": "https://ai-sdk.dev/"},
						Approval:         &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
						ProviderExecuted: ptrBool(true),
					},
				},
			},
		})
		if got != true {
			t.Error("expected true")
		}
	})

	t.Run("true when all tools have a terminal state and at least one is approval-responded", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_2",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "Paris"},
						Output:     map[string]interface{}{"temperature": 20, "weather": "cloudy"},
					},
				},
			},
		})
		if got != true {
			t.Error("expected true")
		}
	})

	t.Run("false when a tool output is preliminary", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&ToolUIPart{
						Type:        "tool-getWeather",
						ToolCallID:  "call_2",
						State:       ToolStateOutputAvailable,
						Input:       map[string]interface{}{"city": "Paris"},
						Output:      map[string]interface{}{"progress": 50},
						Preliminary: ptrBool(true),
					},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("false when a dynamic tool output is preliminary", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&ToolUIPart{
						Type:        "dynamic-tool",
						ToolName:    "getDynamicWeather",
						ToolCallID:  "call_2",
						State:       ToolStateOutputAvailable,
						Input:       map[string]interface{}{"city": "Paris"},
						Output:      map[string]interface{}{"progress": 50},
						Preliminary: ptrBool(true),
					},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("true when a tool output is denied and another approval has responded", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&ToolUIPart{
						Type:       "tool-deleteCalendarEvent",
						ToolCallID: "call_2",
						State:      ToolStateOutputDenied,
						Input:      map[string]interface{}{"eventId": "event_1"},
						Approval:   &ToolUIPartApproval{ID: "approval_2", Approved: ptrBool(false)},
					},
				},
			},
		})
		if got != true {
			t.Error("expected true")
		}
	})

	t.Run("true mixing provider-executed (approval-responded) and regular (output-available)", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:             "dynamic-tool",
						ToolName:         "mcp.shorten_url",
						ToolCallID:       "call_1",
						State:            ToolStateApprovalResponded,
						Input:            map[string]interface{}{"url": "https://ai-sdk.dev/"},
						Approval:         &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
						ProviderExecuted: ptrBool(true),
					},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_2",
						State:      ToolStateOutputAvailable,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Output:     map[string]interface{}{"temperature": 25, "weather": "sunny"},
					},
				},
			},
		})
		if got != true {
			t.Error("expected true")
		}
	})

	t.Run("false when provider-executed tool is approval-responded but regular tool is still approval-requested", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:             "dynamic-tool",
						ToolName:         "mcp.shorten_url",
						ToolCallID:       "call_1",
						State:            ToolStateApprovalResponded,
						Input:            map[string]interface{}{"url": "https://ai-sdk.dev/"},
						Approval:         &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
						ProviderExecuted: ptrBool(true),
					},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_2",
						State:      ToolStateApprovalRequested,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_2"},
					},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})

	t.Run("only considers the last step in a multi-step message", func(t *testing.T) {
		got := LastAssistantMessageIsCompleteWithApprovalResponses([]UIMessage{
			{
				ID:   "1",
				Role: UIMessageRoleAssistant,
				Parts: []UIMessagePart{
					&StepStartUIPart{},
					&ToolUIPart{
						Type:       "tool-getWeather",
						ToolCallID: "call_1",
						State:      ToolStateApprovalResponded,
						Input:      map[string]interface{}{"city": "Tokyo"},
						Approval:   &ToolUIPartApproval{ID: "approval_1", Approved: ptrBool(true)},
					},
					&StepStartUIPart{},
					&TextUIPart{Text: "Done.", State: UIPartStateDone},
				},
			},
		})
		if got != false {
			t.Error("expected false")
		}
	})
}
