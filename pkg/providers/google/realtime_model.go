package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const realtimeWebSocketPath = "google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContentConstrained"

type GoogleRealtimeModel struct {
	provider *Provider
	modelID  string
	mapper   *googleRealtimeEventMapper
}

type GoogleRealtimeModelOptions struct{}

func NewRealtimeModel(p *Provider, modelID string) *GoogleRealtimeModel {
	return &GoogleRealtimeModel{provider: p, modelID: modelID, mapper: &googleRealtimeEventMapper{inputAudioRate: 16000}}
}

func (p *Provider) ExperimentalRealtimeModel(modelID string, _ ...GoogleRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return NewRealtimeModel(p, modelID), nil
}

func (p *Provider) RealtimeModel(modelID string, opts ...GoogleRealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	return p.ExperimentalRealtimeModel(modelID, opts...)
}

func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	model, err := p.ExperimentalRealtimeModel(opts.Model)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return model.DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *GoogleRealtimeModel) SpecificationVersion() string { return "v4" }
func (m *GoogleRealtimeModel) Provider() string             { return m.provider.Name() + ".realtime" }
func (m *GoogleRealtimeModel) ModelID() string              { return m.modelID }

func (m *GoogleRealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	apiKey := m.googleRealtimeAPIKey()
	if apiKey == "" {
		return provider.ClientSecretResult{}, fmt.Errorf("Google Generative AI API key is required for realtime token creation.")
	}
	seconds := 60
	if opts.ExpiresAfterSeconds != nil {
		seconds = *opts.ExpiresAfterSeconds
	}
	now := time.Now()
	body := map[string]interface{}{
		"uses":                     0,
		"expireTime":               googleRealtimeISOString(now.Add(time.Duration(seconds)*time.Second + 30*time.Minute)),
		"newSessionExpireTime":     googleRealtimeISOString(now.Add(time.Duration(seconds) * time.Second)),
		"bidiGenerateContentSetup": m.BuildSessionConfig(valueOrZero(opts.SessionConfig)),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleAuthTokenURL(m.provider.config.BaseURL, apiKey), bytes.NewReader(payload))
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.provider.client.HTTPClient().Do(req)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider.ClientSecretResult{}, fmt.Errorf("Google realtime auth token request failed: %d %s", resp.StatusCode, string(respBody))
	}
	var data struct {
		Name       string `json:"name"`
		ExpireTime string `json:"expireTime"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return provider.ClientSecretResult{}, err
	}
	var expiresAt *int64
	if data.ExpireTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, data.ExpireTime); err == nil {
			sec := t.Unix()
			expiresAt = &sec
		}
	}
	return provider.ClientSecretResult{
		Token:     data.Name,
		URL:       googleWebSocketURL(m.provider.config.BaseURL),
		ExpiresAt: expiresAt,
	}, nil
}

func (m *GoogleRealtimeModel) googleRealtimeAPIKey() string {
	headers := map[string]string{"x-goog-api-key": m.provider.APIKey()}
	for k, v := range m.provider.config.Headers {
		headers[k] = v
	}
	return headers["x-goog-api-key"]
}

func (m *GoogleRealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	return provider.WebSocketConfig{URL: wsURL + "?access_token=" + url.QueryEscape(token)}
}

// thinkingLiveModelRe matches Live models that reason in the background
// (e.g. "gemini-3.8-live-extended-thinking"). Google requires exactly one of
// thinkingLevel/thinkingBudget in their setup and rejects thinkingConfig on
// every other Live model (TS isThinkingLiveModel).
var thinkingLiveModelRe = regexp.MustCompile(`^gemini-\d+\.\d+-live\b.*thinking`)

// isThinkingLiveModel reports whether modelID names a background-reasoning
// Live model, matching only the last "/"-separated path segment.
func isThinkingLiveModel(modelID string) bool {
	segments := strings.Split(modelID, "/")
	name := strings.ToLower(segments[len(segments)-1])
	return thinkingLiveModelRe.MatchString(name)
}

func (m *GoogleRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	setup := map[string]interface{}{"model": googleModelPath(m.modelID)}
	generation := map[string]interface{}{}
	if config.OutputModalities != nil {
		modalities := make([]string, len(config.OutputModalities))
		for i, modality := range config.OutputModalities {
			modalities[i] = strings.ToUpper(modality)
		}
		generation["responseModalities"] = modalities
	} else {
		generation["responseModalities"] = []string{"AUDIO"}
	}
	if config.Voice != nil {
		generation["speechConfig"] = map[string]interface{}{
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{"voiceName": *config.Voice},
			},
		}
	}
	if rawGeneration, ok := config.ProviderOptions["generationConfig"].(map[string]interface{}); ok {
		for k, v := range rawGeneration {
			generation[k] = v
		}
	}
	googleOptions, _ := config.ProviderOptions["google"].(map[string]interface{})
	if translationConfig, ok := googleOptions["translationConfig"].(map[string]interface{}); ok {
		generation["translationConfig"] = translationConfig
	}
	setup["generationConfig"] = generation
	if config.Instructions != nil {
		setup["systemInstruction"] = map[string]interface{}{"parts": []map[string]interface{}{{"text": *config.Instructions}}}
	}
	defaultToolBehavior, hasDefaultToolBehavior := googleOptions["defaultToolBehavior"]
	if len(config.Tools) > 0 {
		decls := make([]map[string]interface{}, 0, len(config.Tools))
		for _, tool := range config.Tools {
			decl := map[string]interface{}{"name": tool.Name}
			if params := convertJSONSchemaToOpenAPISchema(tool.Parameters); params != nil {
				decl["parameters"] = params
			}
			if tool.Description != nil {
				decl["description"] = *tool.Description
			}
			if hasDefaultToolBehavior {
				decl["behavior"] = defaultToolBehavior
			}
			decls = append(decls, decl)
		}
		setup["tools"] = []map[string]interface{}{{"functionDeclarations": decls}}
	}
	if config.InputAudioTranscription != nil {
		setup["inputAudioTranscription"] = map[string]interface{}{}
	}
	if config.OutputAudioTranscription != nil {
		setup["outputAudioTranscription"] = map[string]interface{}{}
	}

	// Default to the lowest-latency thinking level so a session on a
	// background-reasoning model works without provider options. Applied
	// last (after providerOptions are merged below) so it survives a raw
	// providerOptions.generationConfig.
	var thinkingConfig interface{}
	if tc, ok := googleOptions["thinkingConfig"]; ok {
		thinkingConfig = tc
	} else if isThinkingLiveModel(m.modelID) {
		thinkingConfig = map[string]interface{}{"thinkingLevel": "low"}
	}
	applyThinkingConfig := func() {
		if thinkingConfig == nil {
			return
		}
		target, ok := setup["generationConfig"].(map[string]interface{})
		if !ok {
			target = generation
		}
		target["thinkingConfig"] = thinkingConfig
		setup["generationConfig"] = target
	}

	if config.ProviderOptions == nil {
		applyThinkingConfig()
		return setup
	}

	for k, v := range config.ProviderOptions {
		if k == "google" || k == "generationConfig" {
			continue
		}
		setup[k] = v
	}

	applyThinkingConfig()
	return setup
}

func (m *GoogleRealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	return m.mapper.parseServerEvent(raw), nil
}

func (m *GoogleRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return m.mapper.serializeClientEvent(event, m.modelID)
}

type googleRealtimeEventMapper struct {
	turnCounter    int
	hasAudio       bool
	hasText        bool
	hasTranscript  bool
	turnClosed     bool
	inputAudioRate int
}

func (m *googleRealtimeEventMapper) responseID() string {
	return fmt.Sprintf("google-resp-%d", m.turnCounter)
}
func (m *googleRealtimeEventMapper) itemID() string {
	return fmt.Sprintf("google-item-%d", m.turnCounter)
}

func (m *googleRealtimeEventMapper) beginTurnIfClosed() {
	if !m.turnClosed {
		return
	}
	m.turnCounter++
	m.hasAudio = false
	m.hasText = false
	m.hasTranscript = false
	m.turnClosed = false
}

func (m *googleRealtimeEventMapper) parseServerEvent(raw json.RawMessage) []provider.RealtimeServerEvent {
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	if value, ok := data["setupComplete"]; ok && value != nil {
		return []provider.RealtimeServerEvent{{Type: "session-created", Raw: raw}}
	}
	if toolCall, ok := data["toolCall"].(map[string]interface{}); ok {
		m.beginTurnIfClosed()
		calls, _ := toolCall["functionCalls"].([]interface{})
		events := make([]provider.RealtimeServerEvent, 0, len(calls)*2)
		for _, rawCall := range calls {
			call, _ := rawCall.(map[string]interface{})
			argsBytes, _ := json.Marshal(valueOrEmptyMap(call["args"]))
			args := string(argsBytes)
			id := googleString(call, "id")
			name := googleString(call, "name")
			events = append(events,
				provider.RealtimeServerEvent{Type: "function-call-arguments-delta", ResponseID: m.responseID(), ItemID: m.itemID(), CallID: id, Delta: args, Raw: raw},
				provider.RealtimeServerEvent{Type: "function-call-arguments-done", ResponseID: m.responseID(), ItemID: m.itemID(), CallID: id, Name: name, Arguments: args, Raw: raw},
			)
		}
		return events
	}
	if value, ok := data["toolCallCancellation"]; ok && value != nil {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: "toolCallCancellation", Raw: raw}}
	}
	if value, ok := data["goAway"]; ok && value != nil {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: "goAway", Raw: raw}}
	}
	if value, ok := data["sessionResumptionUpdate"]; ok && value != nil {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: "sessionResumptionUpdate", Raw: raw}}
	}
	if serverContent, ok := data["serverContent"].(map[string]interface{}); ok {
		return m.parseServerContent(serverContent, raw)
	}
	if inputTranscription, ok := data["inputTranscription"].(map[string]interface{}); ok {
		if rawText, exists := inputTranscription["text"]; exists && rawText != nil {
			text, _ := rawText.(string)
			return []provider.RealtimeServerEvent{{Type: "input-transcription-completed", ItemID: fmt.Sprintf("google-input-%d", m.turnCounter), Transcript: text, Raw: raw}}
		}
	}
	return []provider.RealtimeServerEvent{{Type: "custom", RawType: firstGoogleRawType(raw), Raw: raw}}
}

func (m *googleRealtimeEventMapper) parseServerContent(serverContent map[string]interface{}, raw json.RawMessage) []provider.RealtimeServerEvent {
	var events []provider.RealtimeServerEvent
	if interrupted, _ := serverContent["interrupted"].(bool); interrupted {
		events = append(events, provider.RealtimeServerEvent{Type: "speech-started", Raw: raw})
	}
	if modelTurn, ok := serverContent["modelTurn"].(map[string]interface{}); ok {
		if parts, ok := modelTurn["parts"].([]interface{}); ok {
			m.beginTurnIfClosed()
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]interface{})
				if inline, ok := part["inlineData"].(map[string]interface{}); ok {
					if data := googleString(inline, "data"); data != "" {
						m.hasAudio = true
						events = append(events, provider.RealtimeServerEvent{Type: "audio-delta", ResponseID: m.responseID(), ItemID: m.itemID(), Delta: data, Raw: raw})
					}
				}
				if text := googleString(part, "text"); text != "" {
					m.hasText = true
					events = append(events, provider.RealtimeServerEvent{Type: "text-delta", ResponseID: m.responseID(), ItemID: m.itemID(), Delta: text, Raw: raw})
				}
			}
		}
	}
	if transcription, ok := serverContent["outputTranscription"].(map[string]interface{}); ok {
		if text := googleString(transcription, "text"); text != "" {
			m.hasTranscript = true
			events = append(events, provider.RealtimeServerEvent{Type: "audio-transcript-delta", ResponseID: m.responseID(), ItemID: m.itemID(), Delta: text, Raw: raw})
		}
	}
	if transcription, ok := serverContent["inputTranscription"].(map[string]interface{}); ok {
		if text := googleString(transcription, "text"); text != "" {
			events = append(events, provider.RealtimeServerEvent{Type: "input-transcription-completed", ItemID: fmt.Sprintf("google-input-%d", m.turnCounter), Transcript: text, Raw: raw})
		}
	}
	// generationComplete means generation has stopped, but playback and the
	// turn can remain open. Kept distinct from response-done, which is
	// emitted only when Google sends turnComplete.
	if complete, _ := serverContent["generationComplete"].(bool); complete {
		events = append(events, provider.RealtimeServerEvent{Type: "custom", RawType: "generationComplete", Raw: raw})
	}
	// interactionStatus (IN_PROGRESS | IDLE | WAITING_FOR_INPUT) is the
	// definitive session-activity signal for background-reasoning models:
	// turnComplete no longer implies the model is idle, since asynchronous
	// tool calls and audio may still follow.
	if value, ok := serverContent["interactionStatus"]; ok && value != nil {
		events = append(events, provider.RealtimeServerEvent{Type: "custom", RawType: "interactionStatus", Raw: raw})
	}
	// waitingForInput is the always-on Proactive Audio turn-taking signal:
	// the model has yielded the floor and is not generating because it
	// expects the user to continue.
	if waiting, _ := serverContent["waitingForInput"].(bool); waiting {
		events = append(events, provider.RealtimeServerEvent{Type: "custom", RawType: "waitingForInput", Raw: raw})
	}
	if complete, _ := serverContent["turnComplete"].(bool); complete {
		if m.hasAudio {
			events = append(events, provider.RealtimeServerEvent{Type: "audio-done", ResponseID: m.responseID(), ItemID: m.itemID(), Raw: raw})
		}
		if m.hasText {
			events = append(events, provider.RealtimeServerEvent{Type: "text-done", ResponseID: m.responseID(), ItemID: m.itemID(), Raw: raw})
		}
		if m.hasTranscript {
			events = append(events, provider.RealtimeServerEvent{Type: "audio-transcript-done", ResponseID: m.responseID(), ItemID: m.itemID(), Raw: raw})
		}
		events = append(events, provider.RealtimeServerEvent{Type: "response-done", ResponseID: m.responseID(), Status: "completed", Raw: raw})
		m.turnClosed = true
	}
	if len(events) == 0 {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: "serverContent", Raw: raw}}
	}
	return events
}

func (m *googleRealtimeEventMapper) serializeClientEvent(event provider.RealtimeClientEvent, modelID string) (json.RawMessage, error) {
	var out interface{}
	switch event.Type {
	case "session-update":
		if event.Config.InputAudioFormat != nil && event.Config.InputAudioFormat.Rate != nil {
			m.inputAudioRate = *event.Config.InputAudioFormat.Rate
		}
		setup := (&GoogleRealtimeModel{modelID: modelID}).BuildSessionConfig(event.Config)
		out = map[string]interface{}{"setup": setup}
	case "input-audio-append":
		out = map[string]interface{}{"realtimeInput": map[string]interface{}{"audio": map[string]interface{}{"data": event.Audio, "mimeType": fmt.Sprintf("audio/pcm;rate=%d", m.inputAudioRate)}}}
	case "conversation-item-create":
		switch event.Item.Type {
		case "text-message":
			out = map[string]interface{}{"realtimeInput": map[string]interface{}{"text": event.Item.Text}}
		case "function-call-output":
			response := toFunctionResponseStruct(event.Item.Output)
			functionResponse := map[string]interface{}{"id": event.Item.CallID, "response": response}
			if event.Item.Name != nil {
				functionResponse["name"] = *event.Item.Name
			}
			out = map[string]interface{}{"toolResponse": map[string]interface{}{"functionResponses": []map[string]interface{}{functionResponse}}}
		case "audio-message":
			out = nil
		default:
			out = nil
		}
	case "input-audio-commit", "input-audio-clear", "response-create", "response-cancel", "conversation-item-truncate":
		out = nil
	default:
		out = nil
	}
	if out == nil {
		return json.RawMessage("null"), nil
	}
	return json.Marshal(out)
}

func googleAuthTokenURL(base, apiKey string) string {
	u := googleRealtimeBaseURL(base)
	u.Path = strings.TrimRight(u.Path, "/") + "/v1alpha/auth_tokens"
	q := u.Query()
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func googleWebSocketURL(base string) string {
	baseURL := googleRealtimeBaseURL(base)
	baseURL.Scheme = strings.Replace(baseURL.Scheme, "http", "ws", 1)
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/ws/" + realtimeWebSocketPath
	return baseURL.String()
}

func googleRealtimeBaseURL(base string) *url.URL {
	if base == "" {
		base = DefaultBaseURL
	}
	u, _ := url.Parse(base)
	segments := strings.Split(u.Path, "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if last == "v1beta" || last == "v1alpha" {
			segments = segments[:len(segments)-1]
			u.Path = strings.Join(segments, "/")
			if u.Path == "" {
				u.Path = "/"
			}
		}
	}
	return u
}

// googleModelPath mirrors TS get-model-path.ts getModelPath: a model ID that
// already contains a "/" (e.g. a fully-qualified "tunedModels/{id}") is used
// verbatim as the path segment; otherwise it is prefixed with "models/".
func googleModelPath(modelID string) string {
	if strings.Contains(modelID, "/") {
		return modelID
	}
	return "models/" + modelID
}

func googleRealtimeISOString(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func firstGoogleRawType(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return "undefined"
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return "undefined"
	}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return "undefined"
		}
		if key, ok := token.(string); ok {
			return key
		}
	}
	return "undefined"
}

func googleString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func valueOrEmptyMap(v interface{}) interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

// toFunctionResponseStruct converts a tool call's output text into the value
// Gemini Live accepts for `functionResponse.response`, mirroring TS
// `toFunctionResponseStruct` (google-realtime-event-mapper.ts): the field is
// a `google.protobuf.Struct` (an object and nothing else), so a string,
// number, array, or `null` on the wire is a protocol violation and closes
// the socket with 1007. An object is passed through unchanged; everything
// else (including malformed JSON, preserved as the raw string) is wrapped
// under an "output" key.
func toFunctionResponseStruct(output string) map[string]interface{} {
	var parsed interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return map[string]interface{}{"output": output}
	}
	if m, ok := parsed.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{"output": parsed}
}

func valueOrZero(config *provider.RealtimeSessionConfig) provider.RealtimeSessionConfig {
	if config == nil {
		return provider.RealtimeSessionConfig{}
	}
	return *config
}

func convertJSONSchemaToOpenAPISchema(schema map[string]interface{}) interface{} {
	return convertJSONSchemaToOpenAPISchemaValue(schema, true)
}

func convertJSONSchemaToOpenAPISchemaValue(schema interface{}, isRoot bool) interface{} {
	if schema == nil {
		return nil
	}
	if isEmptyObjectSchema(schema) {
		if isRoot {
			return nil
		}
		if schemaMap, ok := schema.(map[string]interface{}); ok {
			if description, ok := schemaMap["description"].(string); ok && description != "" {
				return map[string]interface{}{"type": "object", "description": description}
			}
		}
		return map[string]interface{}{"type": "object"}
	}
	if _, ok := schema.(bool); ok {
		return map[string]interface{}{"type": "boolean", "properties": map[string]interface{}{}}
	}
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}
	result := map[string]interface{}{}
	if description, ok := schemaMap["description"].(string); ok && description != "" {
		result["description"] = description
	}
	if required, ok := schemaMap["required"]; ok && required != nil {
		result["required"] = required
	}
	if format, ok := schemaMap["format"].(string); ok && format != "" {
		result["format"] = format
	}
	if constValue, ok := schemaMap["const"]; ok {
		result["enum"] = []interface{}{constValue}
	}
	if rawType, ok := schemaMap["type"]; ok && rawType != nil {
		if typeList, ok := stringList(rawType); ok {
			hasNull := false
			nonNull := make([]string, 0, len(typeList))
			for _, t := range typeList {
				if t == "null" {
					hasNull = true
				} else {
					nonNull = append(nonNull, t)
				}
			}
			switch len(nonNull) {
			case 0:
				result["type"] = "null"
			default:
				anyOf := make([]interface{}, 0, len(nonNull))
				for _, t := range nonNull {
					anyOf = append(anyOf, map[string]interface{}{"type": t})
				}
				result["anyOf"] = anyOf
				if hasNull {
					result["nullable"] = true
				}
			}
		} else if typeString, ok := rawType.(string); ok && typeString != "" {
			result["type"] = typeString
		}
	}
	if enumValues, ok := schemaMap["enum"]; ok {
		result["enum"] = enumValues
	}
	if properties, ok := schemaMap["properties"].(map[string]interface{}); ok {
		converted := map[string]interface{}{}
		for key, value := range properties {
			converted[key] = convertJSONSchemaToOpenAPISchemaValue(value, false)
		}
		result["properties"] = converted
	}
	if items, ok := schemaMap["items"]; ok && items != nil {
		if itemList, ok := schemaSlice(items); ok {
			converted := make([]interface{}, 0, len(itemList))
			for _, item := range itemList {
				converted = append(converted, convertJSONSchemaToOpenAPISchemaValue(item, false))
			}
			result["items"] = converted
		} else {
			result["items"] = convertJSONSchemaToOpenAPISchemaValue(items, false)
		}
	}
	if allOf, ok := schemaList(schemaMap["allOf"]); ok {
		result["allOf"] = convertSchemaList(allOf)
	}
	if anyOf, ok := schemaList(schemaMap["anyOf"]); ok {
		nonNull := make([]interface{}, 0, len(anyOf))
		hasNull := false
		for _, item := range anyOf {
			if itemMap, ok := item.(map[string]interface{}); ok && itemMap["type"] == "null" {
				hasNull = true
			} else {
				nonNull = append(nonNull, item)
			}
		}
		if hasNull {
			if len(nonNull) == 1 {
				converted := convertJSONSchemaToOpenAPISchemaValue(nonNull[0], false)
				if convertedMap, ok := converted.(map[string]interface{}); ok {
					result["nullable"] = true
					for key, value := range convertedMap {
						result[key] = value
					}
				}
			} else {
				result["anyOf"] = convertSchemaList(nonNull)
				result["nullable"] = true
			}
		} else {
			result["anyOf"] = convertSchemaList(anyOf)
		}
	}
	if oneOf, ok := schemaList(schemaMap["oneOf"]); ok {
		result["oneOf"] = convertSchemaList(oneOf)
	}
	if minLength, ok := schemaMap["minLength"]; ok {
		result["minLength"] = minLength
	}
	return result
}

func isEmptyObjectSchema(schema interface{}) bool {
	schemaMap, ok := schema.(map[string]interface{})
	if !ok || schemaMap == nil || schemaMap["type"] != "object" {
		return false
	}
	if additional, ok := schemaMap["additionalProperties"]; ok && truthy(additional) {
		return false
	}
	properties, ok := schemaMap["properties"]
	if !ok || properties == nil {
		return true
	}
	if propertyMap, ok := properties.(map[string]interface{}); ok {
		return len(propertyMap) == 0
	}
	return false
}

func truthy(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case nil:
		return false
	default:
		return true
	}
}

func stringList(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func schemaList(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if list, ok := value.([]interface{}); ok {
		return list, true
	}
	return schemaSlice(value)
}

func schemaSlice(value interface{}) ([]interface{}, bool) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out, true
}

func convertSchemaList(values []interface{}) []interface{} {
	converted := make([]interface{}, 0, len(values))
	for _, value := range values {
		converted = append(converted, convertJSONSchemaToOpenAPISchemaValue(value, false))
	}
	return converted
}
