package openai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tests ported from packages/openai/src/responses/
// openai-responses-reasoning-effort-update.test.ts (TS row ca31b890a6):
// message-level reasoningEffortUpdate via providerOptions.openai.
// reasoningEffortUpdate on empty-content system messages becomes a
// positioned `configuration_update` item, with request-level dedup/prepend,
// Azure fallback, and adjacency rejection. (userMsg is defined in
// responses_reasoning_effort_update_test.go, TS row 94d5d6d3e6.)

// updateMsg builds a system-role message carrying
// providerOptions[provider].reasoningEffortUpdate=effort, optionally with
// non-empty content (which TS/Go both reject).
func updateMsg(provider, effort, content string) types.Message {
	return types.Message{
		Role:    types.RoleSystem,
		Content: []types.ContentPart{types.TextContent{Text: content}},
		ProviderOptions: map[string]interface{}{
			provider: map[string]interface{}{"reasoningEffortUpdate": effort},
		},
	}
}

// inputMaps normalizes body["input"] (a mix of typed structs like
// responses.UserMessage and raw map[string]interface{} items like
// configuration_update) into plain maps via a JSON round-trip, so tests can
// inspect wire field values uniformly regardless of the Go representation.
func inputMaps(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, _ := body["input"].([]interface{})
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		data, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("marshal input item: %v", err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal input item: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func asUnsupportedFunctionalityError(t *testing.T, err error) *providererrors.UnsupportedFunctionalityError {
	t.Helper()
	var ufe *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &ufe) {
		t.Fatalf("expected *UnsupportedFunctionalityError, got %T: %v", err, err)
	}
	return ufe
}

// TS: 'retains multiple updates among messages for %s' (gpt-6-astra/sol/luna).
func TestResponsesReasoningEffortUpdate_RetainsMultiplePositionedUpdates(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, modelID := range []string{ModelGPT6Astra, ModelGPT6Sol, ModelGPT6Luna} {
		t.Run(modelID, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, modelID)
			body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					userMsg("Question"),
					updateMsg("openai", "high", ""),
					userMsg("Question"),
					updateMsg("openai", "low", ""),
					userMsg("Question"),
				}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "medium", "store": false},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none", warnings)
			}
			input := inputMaps(t, body)
			if len(input) != 5 {
				t.Fatalf("input = %#v, want 5 items", input)
			}
			if input[1]["type"] != "configuration_update" || input[1]["reasoning"].(map[string]interface{})["effort"] != "high" {
				t.Fatalf("input[1] = %#v, want configuration_update effort high", input[1])
			}
			if input[3]["type"] != "configuration_update" || input[3]["reasoning"].(map[string]interface{})["effort"] != "low" {
				t.Fatalf("input[3] = %#v, want configuration_update effort low", input[3])
			}
			reasoning := body["reasoning"].(map[string]interface{})
			if reasoning["effort"] != "medium" {
				t.Fatalf("reasoning = %#v, want effort medium", reasoning)
			}
		})
	}
}

// TS: 'preserves positioned none updates and the initial reasoning effort'
// (gpt-6-sol/luna).
func TestResponsesReasoningEffortUpdate_PreservesPositionedNoneUpdates(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, modelID := range []string{ModelGPT6Sol, ModelGPT6Luna} {
		t.Run(modelID, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, modelID)
			body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					userMsg("Question"),
					updateMsg("openai", "none", ""),
					userMsg("Question"),
					updateMsg("openai", "low", ""),
					userMsg("Question"),
				}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"reasoningEffort": "low"},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none", warnings)
			}
			input := inputMaps(t, body)
			if input[1]["reasoning"].(map[string]interface{})["effort"] != "none" {
				t.Fatalf("input[1] = %#v, want effort none", input[1])
			}
		})
	}
}

// TS: 'prepends a request-level none update' (gpt-6-sol/luna).
func TestResponsesReasoningEffortUpdate_PrependsRequestLevelNoneUpdate(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Sol)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{userMsg("Question")}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "low", "reasoningEffortUpdate": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 2 || input[0]["type"] != "configuration_update" || input[0]["reasoning"].(map[string]interface{})["effort"] != "none" {
		t.Fatalf("input = %#v, want [configuration_update(none), user]", input)
	}
}

// TS: 'deduplicates matching request-level and first positioned none updates'.
func TestResponsesReasoningEffortUpdate_DedupesRequestAndFirstPositionedUpdate(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Sol)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			updateMsg("openai", "none", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffortUpdate": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 2 {
		t.Fatalf("input = %#v, want exactly 2 items (deduped)", input)
	}
}

// TS: 'rejects historical none updates for Astra before sending'.
func TestResponsesReasoningEffortUpdate_RejectsHistoricalUnsupportedEffort(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	_, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			userMsg("Question"),
			updateMsg("openai", "none", ""),
			userMsg("Question"),
		}},
	}, false)
	ufe := asUnsupportedFunctionalityError(t, err)
	if ufe.Functionality != "Message-level reasoningEffortUpdate" {
		t.Errorf("Functionality = %q", ufe.Functionality)
	}
	want := "gpt-6-astra only supports the following reasoning efforts: low, medium, high, xhigh, max"
	if ufe.Message != want {
		t.Errorf("Message = %q, want %q", ufe.Message, want)
	}
}

// TS: 'warns and omits request-level none updates for Astra while retaining
// valid history'.
func TestResponsesReasoningEffortUpdate_WarnsAndOmitsUnsupportedRequestLevelUpdate(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			userMsg("Question"),
			updateMsg("openai", "low", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "medium", "reasoningEffortUpdate": "none"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Feature != "reasoningEffortUpdate" {
		t.Fatalf("warnings = %#v, want single reasoningEffortUpdate warning", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 3 || input[1]["type"] != "configuration_update" || input[1]["reasoning"].(map[string]interface{})["effort"] != "low" {
		t.Fatalf("input = %#v, want the valid historical low update retained and no request-level prepend", input)
	}
}

// TS: 'supports $name on Azure messages' (Azure options, OpenAI options
// fallback, Azure taking precedence, explicit empty Azure options).
func TestResponsesReasoningEffortUpdate_AzureFallback(t *testing.T) {
	newAzureModel := func() *ResponsesLanguageModel {
		p := New(Config{APIKey: "test-key", ResponsesProviderName: "azure.responses"})
		return NewResponsesLanguageModel(p, ModelGPT6Astra)
	}

	t.Run("azure options", func(t *testing.T) {
		m := newAzureModel()
		body, _, _, err := m.buildRequest(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				userMsg("Question"),
				updateMsg("azure", "high", ""),
				userMsg("Question"),
			}},
		}, false)
		if err != nil {
			t.Fatalf("buildRequest failed: %v", err)
		}
		input := inputMaps(t, body)
		if input[1]["type"] != "configuration_update" || input[1]["reasoning"].(map[string]interface{})["effort"] != "high" {
			t.Fatalf("input[1] = %#v, want configuration_update effort high", input[1])
		}
	})

	t.Run("openai options fallback", func(t *testing.T) {
		m := newAzureModel()
		body, _, _, err := m.buildRequest(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				userMsg("Question"),
				updateMsg("openai", "high", ""),
				userMsg("Question"),
			}},
		}, false)
		if err != nil {
			t.Fatalf("buildRequest failed: %v", err)
		}
		input := inputMaps(t, body)
		if input[1]["type"] != "configuration_update" || input[1]["reasoning"].(map[string]interface{})["effort"] != "high" {
			t.Fatalf("input[1] = %#v, want configuration_update effort high via openai fallback", input[1])
		}
	})

	t.Run("azure options take precedence", func(t *testing.T) {
		m := newAzureModel()
		body, _, _, err := m.buildRequest(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				userMsg("Question"),
				{
					Role:    types.RoleSystem,
					Content: []types.ContentPart{types.TextContent{Text: ""}},
					ProviderOptions: map[string]interface{}{
						"azure":  map[string]interface{}{"reasoningEffortUpdate": "low"},
						"openai": map[string]interface{}{"reasoningEffortUpdate": "high"},
					},
				},
				userMsg("Question"),
			}},
		}, false)
		if err != nil {
			t.Fatalf("buildRequest failed: %v", err)
		}
		input := inputMaps(t, body)
		if input[1]["reasoning"].(map[string]interface{})["effort"] != "low" {
			t.Fatalf("input[1] = %#v, want azure's effort low to take precedence", input[1])
		}
	})

	t.Run("explicit empty azure options", func(t *testing.T) {
		m := newAzureModel()
		body, _, _, err := m.buildRequest(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				userMsg("Question"),
				{
					Role:    types.RoleSystem,
					Content: []types.ContentPart{types.TextContent{Text: ""}},
					ProviderOptions: map[string]interface{}{
						"azure":  map[string]interface{}{},
						"openai": map[string]interface{}{"reasoningEffortUpdate": "high"},
					},
				},
				userMsg("Question"),
			}},
		}, false)
		if err != nil {
			t.Fatalf("buildRequest failed: %v", err)
		}
		input := inputMaps(t, body)
		// An explicit (even empty) azure options object does not fall back
		// to openai's, so this becomes an ordinary developer message, not a
		// configuration_update.
		if input[1]["type"] == "configuration_update" {
			t.Fatalf("input[1] = %#v, want an ordinary system message (no fallback), not configuration_update", input[1])
		}
		if input[1]["role"] != "developer" || input[1]["content"] != "" {
			t.Fatalf("input[1] = %#v, want {role: developer, content: \"\"}", input[1])
		}
	})
}

// TS: 'rejects unsupported Azure configurations when using OpenAI message options'.
func TestResponsesReasoningEffortUpdate_RejectsUnsupportedAzureConfiguration(t *testing.T) {
	p := New(Config{APIKey: "test-key", ResponsesProviderName: "azure.responses"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	_, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			userMsg("Question"),
			updateMsg("openai", "high", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"truncation": "auto"},
		},
	}, false)
	ufe := asUnsupportedFunctionalityError(t, err)
	if ufe.Functionality != "Message-level reasoningEffortUpdate" {
		t.Errorf("Functionality = %q", ufe.Functionality)
	}
}

// TS: 'keeps the request-level update prepended and historical updates positioned'.
func TestResponsesReasoningEffortUpdate_RequestLevelPrependedAndHistoryKept(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			userMsg("Question"),
			updateMsg("openai", "high", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "low", "reasoningEffortUpdate": "medium"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 4 {
		t.Fatalf("input = %#v, want 4 items", input)
	}
	if input[0]["reasoning"].(map[string]interface{})["effort"] != "medium" {
		t.Fatalf("input[0] = %#v, want prepended effort medium", input[0])
	}
	if input[2]["reasoning"].(map[string]interface{})["effort"] != "high" {
		t.Fatalf("input[2] = %#v, want historical effort high retained", input[2])
	}
}

// TS: 'uses an identical first historical update without prepending a duplicate'.
func TestResponsesReasoningEffortUpdate_IdenticalFirstHistoricalUpdateNotDuplicated(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			updateMsg("openai", "high", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffort": "low", "reasoningSummary": "concise", "reasoningEffortUpdate": "high"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 2 {
		t.Fatalf("input = %#v, want 2 items (deduped)", input)
	}
}

// TS: 'emits controls independently of systemMessageMode=%s'.
func TestResponsesReasoningEffortUpdate_IndependentOfSystemMessageMode(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, mode := range []string{"system", "developer", "remove"} {
		t.Run(mode, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, ModelGPT6Astra)
			body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					updateMsg("openai", "high", ""),
					userMsg("Question"),
				}},
				ProviderOptions: map[string]interface{}{
					"openai": map[string]interface{}{"systemMessageMode": mode},
				},
			}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none", warnings)
			}
			input := inputMaps(t, body)
			if len(input) != 2 || input[0]["type"] != "configuration_update" {
				t.Fatalf("input = %#v, want [configuration_update, user]", input)
			}
		})
	}
}

// TS: 'preserves ordinary empty system messages'.
func TestResponsesReasoningEffortUpdate_PreservesOrdinaryEmptySystemMessages(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: ""}}},
			userMsg("Question"),
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	input := inputMaps(t, body)
	if len(input) != 2 || input[0]["role"] != "developer" || input[0]["content"] != "" {
		t.Fatalf("input = %#v, want [{role: developer, content: \"\"}, user]", input)
	}
}

// TS: 'allows explicit compaction with standard mode and disabled truncation'.
func TestResponsesReasoningEffortUpdate_AllowsExplicitCompactionWithStandardMode(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			updateMsg("openai", "high", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"reasoningMode":     "standard",
				"truncation":        "disabled",
				"compactionTrigger": true,
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	input := inputMaps(t, body)
	if len(input) != 3 || input[0]["type"] != "configuration_update" || input[2]["type"] != "compaction_trigger" {
		t.Fatalf("input = %#v, want [configuration_update, user, compaction_trigger]", input)
	}
}

// TS: 'rejects mixed text %j before sending'.
func TestResponsesReasoningEffortUpdate_RejectsNonEmptyContent(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	for _, content := range []string{"Instructions", " "} {
		t.Run(content, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, ModelGPT6Astra)
			_, _, _, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					updateMsg("openai", "high", content),
					userMsg("Question"),
				}},
			}, false)
			ufe := asUnsupportedFunctionalityError(t, err)
			if ufe.Functionality != "Message-level reasoningEffortUpdate" {
				t.Errorf("Functionality = %q", ufe.Functionality)
			}
			want := "Message-level reasoningEffortUpdate requires empty system message content."
			if ufe.Message != want {
				t.Errorf("Message = %q, want %q", ufe.Message, want)
			}
		})
	}
}

// TS: 'rejects adjacent serialized updates before sending' (subset of cases).
func TestResponsesReasoningEffortUpdate_RejectsAdjacentUpdates(t *testing.T) {
	p := New(Config{APIKey: "test-key"})

	cases := []struct {
		name     string
		messages []types.Message
		opts     map[string]interface{}
	}{
		{
			name: "two adjacent historical updates",
			messages: []types.Message{
				updateMsg("openai", "high", ""),
				updateMsg("openai", "low", ""),
				userMsg("Question"),
			},
		},
		{
			name: "same-effort adjacent historical updates",
			messages: []types.Message{
				updateMsg("openai", "high", ""),
				updateMsg("openai", "high", ""),
				userMsg("Question"),
			},
		},
		{
			name: "request-level prepend creates adjacency",
			messages: []types.Message{
				updateMsg("openai", "high", ""),
				userMsg("Question"),
			},
			opts: map[string]interface{}{"reasoningEffortUpdate": "low"},
		},
		{
			name: "removed system message exposes adjacency",
			messages: []types.Message{
				updateMsg("openai", "high", ""),
				{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Removed"}}},
				updateMsg("openai", "low", ""),
				userMsg("Question"),
			},
			opts: map[string]interface{}{"systemMessageMode": "remove"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewResponsesLanguageModel(p, ModelGPT6Astra)
			providerOpts := map[string]interface{}{}
			if tc.opts != nil {
				providerOpts = tc.opts
			}
			_, _, _, err := m.buildRequest(&provider.GenerateOptions{
				Prompt:          types.Prompt{Messages: tc.messages},
				ProviderOptions: map[string]interface{}{"openai": providerOpts},
			}, false)
			ufe := asUnsupportedFunctionalityError(t, err)
			if ufe.Functionality != "Adjacent reasoning effort configuration updates" {
				t.Errorf("Functionality = %q", ufe.Functionality)
			}
		})
	}
}

// TS: 'checks the first item after system text has been removed'.
func TestResponsesReasoningEffortUpdate_ChecksFirstItemAfterSystemTextRemoved(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Removed"}}},
			updateMsg("openai", "high", ""),
			userMsg("Question"),
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffortUpdate": "high", "systemMessageMode": "remove"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequest failed: %v", err)
	}
	input := inputMaps(t, body)
	if len(input) != 2 || input[0]["type"] != "configuration_update" {
		t.Fatalf("input = %#v, want [configuration_update, user] (deduped, no adjacency)", input)
	}
	if len(warnings) != 1 || warnings[0].Type != "other" || warnings[0].Message != "system messages are removed for this model" {
		t.Fatalf("warnings = %#v, want a single 'system messages are removed' warning", warnings)
	}
}

// TS: 'message-level continuation with $field' describe.each over
// previousResponseId/conversation -- reasoning already stored server-side is
// filtered out of history, which can leave a positioned update either
// correctly isolated or newly adjacent to another update.
func TestResponsesReasoningEffortUpdate_ContinuationFiltersStoredReasoning(t *testing.T) {
	previousReasoning := types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentPart{types.ReasoningContent{
			Text: "Earlier reasoning",
			ProviderOptions: map[string]interface{}{
				"openai": map[string]interface{}{"itemId": "rs_previous"},
			},
		}},
	}

	cases := []struct {
		name  string
		field string
		opts  map[string]interface{}
	}{
		{name: "previousResponseId", field: "previous_response_id", opts: map[string]interface{}{"previousResponseId": "resp_previous"}},
		{name: "conversation", field: "conversation", opts: map[string]interface{}{"conversation": "conv_test"}},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/preserves an update after filtering reasoning already stored in history", func(t *testing.T) {
			p := New(Config{APIKey: "test-key"})
			m := NewResponsesLanguageModel(p, ModelGPT6Astra)
			openaiOpts := map[string]interface{}{"reasoningEffort": "low"}
			for k, v := range tc.opts {
				openaiOpts[k] = v
			}
			body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					previousReasoning,
					updateMsg("openai", "high", ""),
					userMsg("Question"),
				}},
				ProviderOptions: map[string]interface{}{"openai": openaiOpts},
			}, false)
			if err != nil {
				t.Fatalf("buildRequest failed: %v", err)
			}
			if got := body[tc.field]; got != tc.opts[continuationOptKey(tc.name)] {
				t.Fatalf("body[%q] = %#v, want %#v", tc.field, got, tc.opts[continuationOptKey(tc.name)])
			}
			input := inputMaps(t, body)
			if len(input) != 2 || input[0]["type"] != "configuration_update" || input[0]["reasoning"].(map[string]interface{})["effort"] != "high" {
				t.Fatalf("input = %#v, want [configuration_update(high), user]", input)
			}
			if input[1]["role"] != "user" {
				t.Fatalf("input[1] = %#v, want user message", input[1])
			}
			if len(warnings) != 0 {
				t.Fatalf("warnings = %#v, want none", warnings)
			}
		})

		t.Run(tc.name+"/rejects updates made adjacent by filtering stored reasoning", func(t *testing.T) {
			p := New(Config{APIKey: "test-key"})
			m := NewResponsesLanguageModel(p, ModelGPT6Astra)
			_, _, _, err := m.buildRequest(&provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					updateMsg("openai", "high", ""),
					previousReasoning,
					updateMsg("openai", "low", ""),
					userMsg("Question"),
				}},
				ProviderOptions: map[string]interface{}{"openai": tc.opts},
			}, false)
			ufe := asUnsupportedFunctionalityError(t, err)
			if ufe.Functionality != "Adjacent reasoning effort configuration updates" {
				t.Errorf("Functionality = %q", ufe.Functionality)
			}
		})
	}
}

// TS: 'rejects minimal message-level updates even on Luna' -- reasoningEffortUpdate
// is validated against a fixed schema enum independently of, and before, any
// per-model SupportedReasoningEfforts check, so an out-of-enum value like
// "minimal" is rejected the same way even on a model (Luna) that supports "none".
func TestResponsesReasoningEffortUpdate_RejectsInvalidMessageLevelValue(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Luna)
	_, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			updateMsg("openai", "minimal", ""),
			userMsg("Question"),
		}},
	}, false)
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

// TS: 'rejects minimal request-level updates even on Luna'.
func TestResponsesReasoningEffortUpdate_RejectsInvalidRequestLevelValue(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Luna)
	_, _, _, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{userMsg("Question")}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"reasoningEffortUpdate": "minimal"},
		},
	}, false)
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

// continuationOptKey maps a continuation case name to its providerOptions
// key, so the "preserves" subtest above can assert the wire field echoes the
// caller-supplied value regardless of which continuation option was used.
func continuationOptKey(name string) string {
	if name == "previousResponseId" {
		return "previousResponseId"
	}
	return "conversation"
}
