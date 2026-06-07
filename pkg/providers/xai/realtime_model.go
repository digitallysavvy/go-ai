package xai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

type XAIRealtimeModel struct {
	provider *Provider
	modelID  string
}

type XAIRealtimeModelOptions struct{}

func NewRealtimeModel(p *Provider, modelID string) *XAIRealtimeModel {
	return &XAIRealtimeModel{provider: p, modelID: modelID}
}

func (p *Provider) ExperimentalRealtimeModel(modelID string, _ ...XAIRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return NewRealtimeModel(p, modelID), nil
}

func (p *Provider) RealtimeModel(modelID string, opts ...XAIRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return p.ExperimentalRealtimeModel(modelID, opts...)
}

func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	model, err := p.ExperimentalRealtimeModel(opts.Model)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return model.DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *XAIRealtimeModel) SpecificationVersion() string { return "v4" }
func (m *XAIRealtimeModel) Provider() string             { return "xai.realtime" }
func (m *XAIRealtimeModel) ModelID() string              { return m.modelID }

func (m *XAIRealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	if m.provider.config.APIKey == "" {
		return provider.ClientSecretResult{}, fmt.Errorf("xAI API key API key is missing. Pass it using the 'apiKey' parameter or the XAI_API_KEY environment variable.")
	}
	body := map[string]interface{}{}
	if opts.ExpiresAfterSeconds != nil {
		body["expires_after"] = map[string]interface{}{"seconds": *opts.ExpiresAfterSeconds}
	}
	client := internalhttp.NewClient(internalhttp.Config{
		BaseURL: m.provider.realtimeBaseURL,
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Authorization": "Bearer " + m.provider.config.APIKey,
			"Content-Type":  "application/json",
		}, m.provider.config.Headers),
		HTTPClient: m.provider.client.HTTPClient(),
	})
	resp, err := client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/realtime/client_secrets",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider.ClientSecretResult{}, fmt.Errorf("xAI realtime client secret request failed: %d %s", resp.StatusCode, string(resp.Body))
	}
	var data struct {
		Value     string `json:"value"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		return provider.ClientSecretResult{}, err
	}
	u, _ := url.Parse(m.provider.realtimeBaseURL)
	return provider.ClientSecretResult{
		Token:     data.Value,
		URL:       fmt.Sprintf("wss://%s/v1/realtime?model=%s", u.Host, url.QueryEscape(m.modelID)),
		ExpiresAt: data.ExpiresAt,
	}, nil
}

func (m *XAIRealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	return provider.WebSocketConfig{URL: wsURL, Protocols: []string{"xai-client-secret." + token}}
}

func (m *XAIRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	session := map[string]interface{}{}
	if config.Instructions != nil {
		session["instructions"] = *config.Instructions
	}
	if config.Voice != nil {
		session["voice"] = *config.Voice
	}
	audio := map[string]interface{}{}
	if config.InputAudioFormat != nil {
		audio["input"] = map[string]interface{}{"format": xaiAudioFormatMap(config.InputAudioFormat)}
	}
	if config.OutputAudioFormat != nil {
		audio["output"] = map[string]interface{}{"format": xaiAudioFormatMap(config.OutputAudioFormat)}
	}
	if len(audio) > 0 {
		session["audio"] = audio
	}
	if config.TurnDetection != nil && config.TurnDetection.Type == "disabled" {
		session["turn_detection"] = nil
	} else if config.TurnDetection != nil {
		session["turn_detection"] = xaiTurnDetectionMap(config.TurnDetection)
	}
	if len(config.Tools) > 0 {
		session["tools"] = xaiRealtimeTools(config.Tools)
	}
	if config.ProviderOptions != nil {
		if extraTools, ok := xaiSliceToInterfaces(config.ProviderOptions["tools"]); ok {
			existing, _ := session["tools"].([]map[string]interface{})
			combined := make([]interface{}, 0, len(existing)+len(extraTools))
			for _, tool := range existing {
				combined = append(combined, tool)
			}
			combined = append(combined, extraTools...)
			session["tools"] = combined
		}
		for k, v := range config.ProviderOptions {
			if k != "tools" {
				session[k] = v
			}
		}
	}
	return session
}

func (m *XAIRealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	return []provider.RealtimeServerEvent{xaiParseServerEvent(raw)}, nil
}

func (m *XAIRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return xaiSerializeClientEvent(event, m.BuildSessionConfig)
}

func xaiParseServerEvent(raw json.RawMessage) provider.RealtimeServerEvent {
	var event map[string]interface{}
	_ = json.Unmarshal(raw, &event)
	rawType, _ := event["type"].(string)
	e := provider.RealtimeServerEvent{Raw: raw}
	switch rawType {
	case "session.created":
		e.Type = "session-created"
		e.SessionID = xaiNestedString(event, "session", "id")
	case "session.updated":
		e.Type = "session-updated"
	case "input_audio_buffer.speech_started":
		e.Type = "speech-started"
		e.ItemID = xaiStringField(event, "item_id")
	case "input_audio_buffer.speech_stopped":
		e.Type = "speech-stopped"
		e.ItemID = xaiStringField(event, "item_id")
	case "input_audio_buffer.committed":
		e.Type = "audio-committed"
		e.ItemID = xaiStringField(event, "item_id")
		e.PreviousItemID = xaiStringField(event, "previous_item_id")
	case "conversation.item.added":
		e.Type = "conversation-item-added"
		if itemID, ok := xaiNestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = xaiStringField(event, "item_id")
		}
		e.Item = event["item"]
	case "conversation.item.input_audio_transcription.completed":
		e.Type = "input-transcription-completed"
		e.ItemID = xaiStringField(event, "item_id")
		e.Transcript = xaiStringField(event, "transcript")
	case "response.created":
		e.Type = "response-created"
		if responseID, ok := xaiNestedStringField(event, "response", "id"); ok {
			e.ResponseID = responseID
		} else {
			e.ResponseID = xaiStringField(event, "response_id")
		}
	case "response.done":
		e.Type = "response-done"
		if responseID, ok := xaiNestedStringField(event, "response", "id"); ok {
			e.ResponseID = responseID
		} else {
			e.ResponseID = xaiStringField(event, "response_id")
		}
		if status, ok := xaiNestedStringField(event, "response", "status"); ok {
			e.Status = status
		} else {
			e.Status = "completed"
		}
	case "response.output_item.added":
		e.Type = "output-item-added"
		e.ResponseID = xaiStringField(event, "response_id")
		if itemID, ok := xaiNestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = xaiStringField(event, "item_id")
		}
	case "response.output_item.done":
		e.Type = "output-item-done"
		e.ResponseID = xaiStringField(event, "response_id")
		if itemID, ok := xaiNestedStringField(event, "item", "id"); ok {
			e.ItemID = itemID
		} else {
			e.ItemID = xaiStringField(event, "item_id")
		}
	case "response.content_part.added":
		e.Type = "content-part-added"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
	case "response.content_part.done":
		e.Type = "content-part-done"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
	case "response.output_audio.delta":
		e.Type = "audio-delta"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.Delta = xaiStringField(event, "delta")
	case "response.output_audio.done":
		e.Type = "audio-done"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
	case "response.output_audio_transcript.delta":
		e.Type = "audio-transcript-delta"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.Delta = xaiStringField(event, "delta")
	case "response.output_audio_transcript.done":
		e.Type = "audio-transcript-done"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.Transcript = xaiStringField(event, "transcript")
	case "response.text.delta":
		e.Type = "text-delta"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.Delta = xaiStringField(event, "delta")
	case "response.text.done":
		e.Type = "text-done"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.Text = xaiStringField(event, "text")
	case "response.function_call_arguments.delta":
		e.Type = "function-call-arguments-delta"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.CallID = xaiStringField(event, "call_id")
		e.Delta = xaiStringField(event, "delta")
	case "response.function_call_arguments.done":
		e.Type = "function-call-arguments-done"
		e.ResponseID = xaiStringField(event, "response_id")
		e.ItemID = xaiStringField(event, "item_id")
		e.CallID = xaiStringField(event, "call_id")
		e.Name = xaiStringField(event, "name")
		e.Arguments = xaiStringField(event, "arguments")
	case "error":
		e.Type = "error"
		if message, ok := xaiNestedStringField(event, "error", "message"); ok {
			e.Message = message
		} else if message, ok := xaiStringFieldPresent(event, "message"); ok {
			e.Message = message
		} else {
			e.Message = "Unknown error"
		}
		if code, ok := xaiNestedStringField(event, "error", "code"); ok {
			e.Code = code
		} else {
			e.Code = xaiStringField(event, "code")
		}
	default:
		e.Type = "custom"
		e.RawType = rawType
	}
	return e
}

func xaiSerializeClientEvent(event provider.RealtimeClientEvent, buildSession func(provider.RealtimeSessionConfig) any) (json.RawMessage, error) {
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
		item, err := xaiConversationItem(event.Item)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		out = map[string]interface{}{"type": "conversation.item.create", "item": item}
	case "conversation-item-truncate":
		return nil, nil
	case "response-create":
		m := map[string]interface{}{"type": "response.create"}
		if event.Options != nil {
			resp := map[string]interface{}{}
			if event.Options.Modalities != nil {
				resp["modalities"] = event.Options.Modalities
			}
			if event.Options.Instructions != nil {
				resp["instructions"] = *event.Options.Instructions
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

func xaiConversationItem(item provider.RealtimeConversationItem) (map[string]interface{}, error) {
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

func xaiAudioFormatMap(f *provider.RealtimeAudioFormat) map[string]interface{} {
	out := map[string]interface{}{"type": f.Type}
	if f.Rate != nil {
		out["rate"] = *f.Rate
	}
	return out
}

func xaiTurnDetectionMap(td *provider.RealtimeTurnDetection) map[string]interface{} {
	out := map[string]interface{}{"type": "server_vad"}
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

func xaiRealtimeTools(tools []provider.RealtimeToolDefinition) []map[string]interface{} {
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

func xaiSliceToInterfaces(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if values, ok := value.([]interface{}); ok {
		return values, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	values := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		values = append(values, rv.Index(i).Interface())
	}
	return values, true
}

func xaiStringField(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func xaiStringFieldPresent(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func xaiNestedString(m map[string]interface{}, key, nested string) string {
	child, _ := m[key].(map[string]interface{})
	v, _ := child[nested].(string)
	return v
}

func xaiNestedStringField(m map[string]interface{}, key, nested string) (string, bool) {
	child, ok := m[key].(map[string]interface{})
	if !ok || child == nil {
		return "", false
	}
	return xaiStringFieldPresent(child, nested)
}
