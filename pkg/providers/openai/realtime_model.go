package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type OpenAIRealtimeModel struct {
	provider *Provider
	modelID  string
}

type OpenAIRealtimeModelOptions struct{}

func NewRealtimeModel(p *Provider, modelID string) *OpenAIRealtimeModel {
	return &OpenAIRealtimeModel{provider: p, modelID: modelID}
}

func (p *Provider) ExperimentalRealtimeModel(modelID string, _ ...OpenAIRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return NewRealtimeModel(p, modelID), nil
}

func (p *Provider) RealtimeModel(modelID string, opts ...OpenAIRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return p.ExperimentalRealtimeModel(modelID, opts...)
}

func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	model, err := p.ExperimentalRealtimeModel(opts.Model)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return model.DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *OpenAIRealtimeModel) SpecificationVersion() string { return "v4" }
func (m *OpenAIRealtimeModel) Provider() string             { return m.provider.Name() + ".realtime" }
func (m *OpenAIRealtimeModel) ModelID() string              { return m.modelID }

func (m *OpenAIRealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	if m.provider.config.APIKey == "" {
		return provider.ClientSecretResult{}, fmt.Errorf("OpenAI API key is missing. Pass it using the 'apiKey' parameter or the OPENAI_API_KEY environment variable.")
	}
	session := map[string]interface{}{"type": "realtime", "model": m.modelID}
	if opts.SessionConfig != nil {
		if built, ok := m.BuildSessionConfig(*opts.SessionConfig).(map[string]interface{}); ok {
			session = built
		}
	}
	body := map[string]interface{}{"session": session}
	if opts.ExpiresAfterSeconds != nil {
		body["expires_after"] = map[string]interface{}{"anchor": "created_at", "seconds": *opts.ExpiresAfterSeconds}
	}
	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/realtime/client_secrets",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider.ClientSecretResult{}, fmt.Errorf("OpenAI realtime client secret request failed: %d %s", resp.StatusCode, string(resp.Body))
	}
	var data struct {
		Value     string `json:"value"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		return provider.ClientSecretResult{}, err
	}
	base := m.provider.config.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, _ := url.Parse(base)
	return provider.ClientSecretResult{
		Token:     data.Value,
		URL:       fmt.Sprintf("wss://%s/v1/realtime?model=%s", u.Host, url.QueryEscape(m.modelID)),
		ExpiresAt: data.ExpiresAt,
	}, nil
}

func (m *OpenAIRealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	return provider.WebSocketConfig{URL: wsURL, Protocols: []string{"realtime", "openai-insecure-api-key." + token}}
}

func (m *OpenAIRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	session := map[string]interface{}{"type": "realtime", "model": m.modelID}
	addOpenAIRealtimeSessionFields(session, config)
	return session
}

func (m *OpenAIRealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	return []provider.RealtimeServerEvent{parseOpenAICompatibleRealtimeServerEvent(raw, false)}, nil
}

func (m *OpenAIRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return serializeOpenAICompatibleRealtimeClientEvent(event, func(cfg provider.RealtimeSessionConfig) any {
		return m.BuildSessionConfig(cfg)
	}, true)
}

func addOpenAIRealtimeSessionFields(session map[string]interface{}, config provider.RealtimeSessionConfig) {
	if config.Instructions != nil {
		session["instructions"] = *config.Instructions
	}
	if config.OutputModalities != nil {
		session["output_modalities"] = config.OutputModalities
	}
	audio := map[string]interface{}{}
	if config.InputAudioFormat != nil || config.InputAudioTranscription != nil || config.TurnDetection != nil {
		input := map[string]interface{}{}
		if config.InputAudioFormat != nil {
			input["format"] = audioFormatMap(config.InputAudioFormat)
		}
		if config.TurnDetection != nil && config.TurnDetection.Type == "disabled" {
			input["turn_detection"] = nil
		} else if config.TurnDetection != nil {
			input["turn_detection"] = turnDetectionMap(config.TurnDetection, true)
		}
		if config.InputAudioTranscription != nil {
			model := "gpt-realtime-whisper"
			if config.InputAudioTranscription.Model != nil {
				model = *config.InputAudioTranscription.Model
			}
			if model == "" {
				model = ""
			}
			transcription := map[string]interface{}{"model": model}
			if config.InputAudioTranscription.Language != nil {
				transcription["language"] = *config.InputAudioTranscription.Language
			}
			if config.InputAudioTranscription.Prompt != nil {
				transcription["prompt"] = *config.InputAudioTranscription.Prompt
			}
			input["transcription"] = transcription
		}
		audio["input"] = input
	}
	if config.OutputAudioFormat != nil || config.Voice != nil {
		output := map[string]interface{}{}
		if config.OutputAudioFormat != nil {
			output["format"] = audioFormatMap(config.OutputAudioFormat)
		}
		if config.Voice != nil {
			output["voice"] = *config.Voice
		}
		audio["output"] = output
	}
	if len(audio) > 0 {
		session["audio"] = audio
	}
	if len(config.Tools) > 0 {
		session["tools"] = realtimeTools(config.Tools)
		session["tool_choice"] = "auto"
	}
	for k, v := range config.ProviderOptions {
		session[k] = v
	}
}

func audioFormatMap(f *provider.RealtimeAudioFormat) map[string]interface{} {
	out := map[string]interface{}{"type": f.Type}
	if f.Rate != nil {
		out["rate"] = *f.Rate
	}
	return out
}

func turnDetectionMap(td *provider.RealtimeTurnDetection, openaiNames bool) map[string]interface{} {
	t := td.Type
	if openaiNames {
		switch t {
		case "server-vad":
			t = "server_vad"
		case "semantic-vad":
			t = "semantic_vad"
		}
	} else {
		t = "server_vad"
	}
	out := map[string]interface{}{"type": t}
	if td.Threshold != nil {
		out["threshold"] = *td.Threshold
	}
	if td.SilenceDurationMs != nil {
		out["silence_duration_ms"] = *td.SilenceDurationMs
	}
	if td.PrefixPaddingMs != nil {
		out["prefix_padding_ms"] = *td.PrefixPaddingMs
	}
	return out
}

func realtimeTools(tools []provider.RealtimeToolDefinition) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		t := map[string]interface{}{"type": "function", "name": tool.Name, "parameters": tool.Parameters}
		if tool.Type != "" {
			t["type"] = tool.Type
		}
		if tool.Description != nil {
			t["description"] = *tool.Description
		}
		out = append(out, t)
	}
	return out
}

func parseOpenAICompatibleRealtimeServerEvent(raw json.RawMessage, xai bool) provider.RealtimeServerEvent {
	var event map[string]interface{}
	_ = json.Unmarshal(raw, &event)
	rawType, _ := event["type"].(string)
	e := provider.RealtimeServerEvent{Raw: raw}
	switch rawType {
	case "session.created":
		e.Type = "session-created"
		e.SessionID = nestedString(event, "session", "id")
	case "session.updated":
		e.Type = "session-updated"
	case "input_audio_buffer.speech_started":
		e.Type = "speech-started"
		e.ItemID = stringField(event, "item_id")
	case "input_audio_buffer.speech_stopped":
		e.Type = "speech-stopped"
		e.ItemID = stringField(event, "item_id")
	case "input_audio_buffer.committed":
		e.Type = "audio-committed"
		e.ItemID = stringField(event, "item_id")
		e.PreviousItemID = stringField(event, "previous_item_id")
	case "conversation.item.added":
		e.Type = "conversation-item-added"
		if itemID, ok := nestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = stringField(event, "item_id")
		}
		e.Item = event["item"]
	case "conversation.item.input_audio_transcription.completed":
		e.Type = "input-transcription-completed"
		e.ItemID = stringField(event, "item_id")
		e.Transcript = stringField(event, "transcript")
	case "response.created":
		e.Type = "response-created"
		if responseID, ok := nestedStringField(event, "response", "id"); ok {
			e.ResponseID = responseID
		} else {
			e.ResponseID = stringField(event, "response_id")
		}
	case "response.done":
		e.Type = "response-done"
		if responseID, ok := nestedStringField(event, "response", "id"); ok {
			e.ResponseID = responseID
		} else {
			e.ResponseID = stringField(event, "response_id")
		}
		if status, ok := nestedStringField(event, "response", "status"); ok {
			e.Status = status
		} else {
			e.Status = "completed"
		}
	case "response.output_item.added":
		e.Type = "output-item-added"
		e.ResponseID = stringField(event, "response_id")
		if itemID, ok := nestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = stringField(event, "item_id")
		}
	case "response.output_item.done":
		e.Type = "output-item-done"
		e.ResponseID = stringField(event, "response_id")
		if itemID, ok := nestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = stringField(event, "item_id")
		}
	case "response.content_part.added":
		e.Type = "content-part-added"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
	case "response.content_part.done":
		e.Type = "content-part-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
	case "response.output_audio.delta":
		e.Type = "audio-delta"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Delta = stringField(event, "delta")
	case "response.output_audio.done":
		e.Type = "audio-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
	case "response.output_audio_transcript.delta":
		e.Type = "audio-transcript-delta"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Delta = stringField(event, "delta")
	case "response.output_audio_transcript.done":
		e.Type = "audio-transcript-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Transcript = stringField(event, "transcript")
	case "response.output_text.delta":
		if xai {
			e.Type = "custom"
			e.RawType = rawType
			return e
		}
		e.Type = "text-delta"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Delta = stringField(event, "delta")
	case "response.output_text.done":
		if xai {
			e.Type = "custom"
			e.RawType = rawType
			return e
		}
		e.Type = "text-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Text = stringField(event, "text")
	case "response.text.delta":
		if !xai {
			e.Type = "custom"
			e.RawType = rawType
			return e
		}
		e.Type = "text-delta"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Delta = stringField(event, "delta")
	case "response.text.done":
		if !xai {
			e.Type = "custom"
			e.RawType = rawType
			return e
		}
		e.Type = "text-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.Text = stringField(event, "text")
	case "response.function_call_arguments.delta":
		e.Type = "function-call-arguments-delta"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.CallID = stringField(event, "call_id")
		e.Delta = stringField(event, "delta")
	case "response.function_call_arguments.done":
		e.Type = "function-call-arguments-done"
		e.ResponseID = stringField(event, "response_id")
		e.ItemID = stringField(event, "item_id")
		e.CallID = stringField(event, "call_id")
		e.Name = stringField(event, "name")
		e.Arguments = stringField(event, "arguments")
	case "error":
		e.Type = "error"
		if message, ok := nestedStringField(event, "error", "message"); ok {
			e.Message = message
		} else if message, ok := stringFieldPresent(event, "message"); ok {
			e.Message = message
		} else {
			e.Message = "Unknown error"
		}
		if code, ok := nestedStringField(event, "error", "code"); ok {
			e.Code = code
		} else {
			e.Code = stringField(event, "code")
		}
	default:
		e.Type = "custom"
		e.RawType = rawType
	}
	return e
}

func serializeOpenAICompatibleRealtimeClientEvent(event provider.RealtimeClientEvent, buildSession func(provider.RealtimeSessionConfig) any, openai bool) (json.RawMessage, error) {
	var out interface{}
	switch event.Type {
	case "session-update":
		out = map[string]interface{}{"type": "session.update", "session": buildSession(event.Config)}
	case "input-audio-append":
		out = map[string]interface{}{"type": "input_audio_buffer.append", "audio": event.Audio}
	case "input-audio-commit":
		out = map[string]interface{}{"type": "input_audio_buffer.commit"}
	case "input-audio-clear":
		out = map[string]interface{}{"type": "input_audio_buffer.clear"}
	case "conversation-item-create":
		item, err := openAIConversationItem(event.Item)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		out = map[string]interface{}{"type": "conversation.item.create", "item": item}
	case "conversation-item-truncate":
		if !openai {
			return json.RawMessage("null"), nil
		}
		out = map[string]interface{}{"type": "conversation.item.truncate", "item_id": event.ItemID, "content_index": event.ContentIndex, "audio_end_ms": event.AudioEndMs}
	case "response-create":
		m := map[string]interface{}{"type": "response.create"}
		if event.Options != nil {
			resp := map[string]interface{}{}
			if event.Options.Modalities != nil {
				if openai {
					resp["output_modalities"] = event.Options.Modalities
				} else {
					resp["modalities"] = event.Options.Modalities
				}
			}
			if event.Options.Instructions != nil {
				resp["instructions"] = *event.Options.Instructions
			}
			if openai && event.Options.Metadata != nil {
				resp["metadata"] = event.Options.Metadata
			}
			m["response"] = resp
		}
		out = m
	case "response-cancel":
		out = map[string]interface{}{"type": "response.cancel"}
	default:
		return nil, nil
	}
	return json.Marshal(out)
}

func openAIConversationItem(item provider.RealtimeConversationItem) (map[string]interface{}, error) {
	switch item.Type {
	case "text-message":
		return map[string]interface{}{"type": "message", "role": item.Role, "content": []map[string]interface{}{{"type": "input_text", "text": item.Text}}}, nil
	case "audio-message":
		return map[string]interface{}{"type": "message", "role": item.Role, "content": []map[string]interface{}{{"type": "input_audio", "audio": item.Audio}}}, nil
	case "function-call-output":
		return map[string]interface{}{"type": "function_call_output", "call_id": item.CallID, "output": item.Output}, nil
	default:
		return nil, nil
	}
}

func stringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func stringFieldPresent(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func nestedString(m map[string]interface{}, key, nested string) string {
	child, _ := m[key].(map[string]interface{})
	v, _ := child[nested].(string)
	return v
}

func nestedStringField(m map[string]interface{}, key, nested string) (string, bool) {
	child, ok := m[key].(map[string]interface{})
	if !ok || child == nil {
		return "", false
	}
	return stringFieldPresent(child, nested)
}
