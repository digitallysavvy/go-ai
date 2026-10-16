package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
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

// GetRealtimeToken mints a client secret via the concrete
// *GoogleRealtimeModel, which always implements RealtimeClientSecretCreator
// (mirrors TS createRealtimeModel().doCreateClientSecret(), called on the
// concrete class instance rather than through the general RealtimeModelV4
// type).
func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	model := NewRealtimeModel(p, opts.Model)
	return model.DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *GoogleRealtimeModel) SpecificationVersion() string { return "v4" }
func (m *GoogleRealtimeModel) Provider() string             { return m.provider.Name() + ".realtime" }
func (m *GoogleRealtimeModel) ModelID() string              { return m.modelID }

// Compile-time checks that *GoogleRealtimeModel satisfies both
// Experimental_RealtimeModelV4 and the optional client-secret-WebSocket
// capabilities (RealtimeClientSecretCreator / RealtimeWebSocketConfigProvider)
// that DoCreateClientSecret/GetWebSocketConfig moved into (hand-off:
// "realtime optional capabilities"). GetRealtimeToken above calls
// DoCreateClientSecret directly on the concrete type, so losing either
// method would otherwise only surface as a runtime failure elsewhere.
var (
	_ provider.Experimental_RealtimeModelV4    = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeClientSecretCreator     = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeWebSocketConfigProvider = (*GoogleRealtimeModel)(nil)
)

func (m *GoogleRealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	apiKey, err := m.googleRealtimeAPIKey()
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if apiKey == "" {
		return provider.ClientSecretResult{}, fmt.Errorf("Google Generative AI API key is required for realtime token creation.") //nolint:staticcheck // matches TS SDK's exact error text
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
		return provider.ClientSecretResult{}, fmt.Errorf("Google realtime auth token request failed: %d %s", resp.StatusCode, string(respBody)) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
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

// googleRealtimeAPIKey mirrors TS `this.config.headers()['x-goog-api-key']`:
// the provider's getHeaders loads the key (a missing key is a
// LoadAPIKeyError), and a configured x-goog-api-key header wins.
func (m *GoogleRealtimeModel) googleRealtimeAPIKey() (string, error) {
	headers := m.provider.defaultHeaders()
	extra, err := m.provider.apiKeyHeaders()
	if err != nil {
		return "", err
	}
	for k, v := range extra {
		headers[k] = v
	}
	return headers["x-goog-api-key"], nil
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

// BuildSessionConfig ports TS buildGoogleSessionConfig
// (realtime/google-realtime-event-mapper.ts).
func (m *GoogleRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	setup := map[string]interface{}{"model": gemini.GetModelPath(m.modelID)}
	googleOptions, _ := config.ProviderOptions["google"].(map[string]interface{})

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
	setup["generationConfig"] = generation
	if config.Instructions != nil {
		setup["systemInstruction"] = map[string]interface{}{"parts": []map[string]interface{}{{"text": *config.Instructions}}}
	}
	defaultToolBehavior, hasDefaultToolBehavior := googleOptions["defaultToolBehavior"]
	if len(config.Tools) > 0 {
		decls := make([]map[string]interface{}, 0, len(config.Tools))
		for _, tool := range config.Tools {
			// TS sends the JSON Schema untouched as parametersJsonSchema (it no
			// longer converts it to an OpenAPI schema).
			decl := map[string]interface{}{"name": tool.Name}
			if tool.Description != nil {
				decl["description"] = *tool.Description
			}
			if tool.Parameters != nil {
				decl["parametersJsonSchema"] = tool.Parameters
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

	// Background-reasoning models require a thinkingLevel or thinkingBudget.
	// Default to the lowest-latency level when the effective thinking config
	// (the typed option, else a raw providerOptions.generationConfig.
	// thinkingConfig) sets neither. Applied last (after providerOptions are
	// merged below) so it survives a raw providerOptions.generationConfig,
	// while an explicit level/budget (typed or raw) is never replaced.
	rawGenerationConfig, _ := config.ProviderOptions["generationConfig"].(map[string]interface{})
	googleThinkingConfig, hasGoogleThinkingConfig := googleOptions["thinkingConfig"]
	var explicitThinkingConfig map[string]interface{}
	if hasGoogleThinkingConfig {
		explicitThinkingConfig, _ = googleThinkingConfig.(map[string]interface{})
	} else if rawGenerationConfig != nil {
		explicitThinkingConfig, _ = rawGenerationConfig["thinkingConfig"].(map[string]interface{})
	}
	var thinkingConfig interface{}
	if isThinkingLiveModel(m.modelID) && explicitThinkingConfig["thinkingLevel"] == nil && explicitThinkingConfig["thinkingBudget"] == nil {
		merged := map[string]interface{}{}
		for k, v := range explicitThinkingConfig {
			merged[k] = v
		}
		merged["thinkingLevel"] = "low"
		thinkingConfig = merged
	} else if hasGoogleThinkingConfig {
		thinkingConfig = googleThinkingConfig
	}
	// withGenerationField copies the effective generationConfig (the raw one
	// that replaced the default when it is an object) before adding a field.
	withGenerationField := func(key string, value interface{}) {
		target, ok := setup["generationConfig"].(map[string]interface{})
		if !ok {
			target = generation
		}
		next := make(map[string]interface{}, len(target)+1)
		for k, v := range target {
			next[k] = v
		}
		next[key] = value
		setup["generationConfig"] = next
	}
	applyThinkingConfig := func() {
		if thinkingConfig != nil {
			withGenerationField("thinkingConfig", thinkingConfig)
		}
	}

	if config.ProviderOptions == nil {
		applyThinkingConfig()
		return setup
	}

	// TS Object.assign(setup, restProviderOptions): every provider option
	// except `google` replaces the setup key of the same name, so a raw
	// generationConfig replaces the default one.
	for k, v := range config.ProviderOptions {
		if k == "google" {
			continue
		}
		setup[k] = v
	}

	if translationConfig, ok := googleOptions["translationConfig"]; ok && translationConfig != nil {
		withGenerationField("translationConfig", translationConfig)
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

	// Input transcription accumulates independently of turnCounter (see
	// accumulateInputTranscription), mirroring TS GoogleRealtimeEventMapper.
	inputTranscriptionCounter                    int
	inputTranscriptionBuffer                     string
	inputTranscriptionBoundary                   string // "none" | "interrupted" | "turn-complete"
	preserveInputTranscriptionAcrossTurnComplete bool
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
	// Once the next response starts, its completion must delimit its own
	// input even if the interrupted response never sent a turnComplete.
	m.preserveInputTranscriptionAcrossTurnComplete = false
}

// accumulateInputTranscription concatenates consecutive input-transcription
// fragments for a single user utterance under one stable synthetic id,
// mirroring TS GoogleRealtimeEventMapper.accumulateInputTranscription.
// Google's `finished` signal is the authoritative utterance boundary; for
// older payloads without it, response boundaries (interrupted/turnComplete)
// are a fallback. Returns ok=false when nothing should be emitted (e.g. a
// standalone completion marker with no text).
func (m *googleRealtimeEventMapper) accumulateInputTranscription(text string, finished *bool) (itemID, transcript string, ok bool) {
	if text == "" {
		if finished != nil && *finished {
			m.finishInputTranscription()
		}
		return "", "", false
	}

	hasFinishedSignal := finished != nil
	if m.inputTranscriptionBuffer != "" &&
		(m.inputTranscriptionBoundary == "interrupted" ||
			(m.inputTranscriptionBoundary == "turn-complete" && !hasFinishedSignal)) {
		m.finishInputTranscription()
	}
	m.inputTranscriptionBoundary = "none"
	m.inputTranscriptionBuffer += text
	itemID = fmt.Sprintf("google-input-%d", m.inputTranscriptionCounter)
	transcript = m.inputTranscriptionBuffer
	if finished != nil && *finished {
		m.finishInputTranscription()
	}
	return itemID, transcript, true
}

func (m *googleRealtimeEventMapper) finishInputTranscription() {
	if m.inputTranscriptionBuffer == "" {
		return
	}
	m.inputTranscriptionCounter++
	m.inputTranscriptionBuffer = ""
}

// googleOptionalBool reads an optional boolean field, distinguishing "absent"
// (nil) from an explicit `false`, mirroring TS's `finished?: boolean` which
// accumulateInputTranscription checks with `!= null`.
func googleOptionalBool(m map[string]interface{}, key string) *bool {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
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
			args := jsonStringify(valueOrEmptyMap(call["args"]))
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
		itemID, transcript, emitted := m.accumulateInputTranscription(
			googleString(inputTranscription, "text"),
			googleOptionalBool(inputTranscription, "finished"),
		)
		if emitted {
			return []provider.RealtimeServerEvent{{Type: "input-transcription-completed", ItemID: itemID, Transcript: transcript, Raw: raw}}
		}
	}
	return []provider.RealtimeServerEvent{{Type: "custom", RawType: firstGoogleRawType(raw), Raw: raw}}
}

func (m *googleRealtimeEventMapper) parseServerContent(serverContent map[string]interface{}, raw json.RawMessage) []provider.RealtimeServerEvent {
	var events []provider.RealtimeServerEvent
	if interrupted, _ := serverContent["interrupted"].(bool); interrupted {
		// A barge-in ends the current user utterance; the next input
		// transcription fragment belongs to a fresh utterance. The
		// interrupted response's trailing turnComplete must not split that
		// new utterance.
		m.inputTranscriptionBoundary = "interrupted"
		m.preserveInputTranscriptionAcrossTurnComplete = true
		m.turnClosed = true
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
		itemID, transcript, emitted := m.accumulateInputTranscription(
			googleString(transcription, "text"),
			googleOptionalBool(transcription, "finished"),
		)
		if emitted {
			events = append(events, provider.RealtimeServerEvent{Type: "input-transcription-completed", ItemID: itemID, Transcript: transcript, Raw: raw})
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
		if m.preserveInputTranscriptionAcrossTurnComplete {
			m.preserveInputTranscriptionAcrossTurnComplete = false
		} else {
			// For payloads without `finished`, the completed response
			// remains the fallback signal that the next transcription is a
			// new utterance. A transcription carrying `finished` can still
			// arrive late and close the preceding utterance after this
			// response boundary.
			if m.inputTranscriptionBoundary != "interrupted" {
				m.inputTranscriptionBoundary = "turn-complete"
			}
		}
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
	case "input-audio-commit":
		out = map[string]interface{}{"realtimeInput": map[string]interface{}{"audioStreamEnd": true}}
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
	case "input-audio-clear", "response-create", "response-cancel", "conversation-item-truncate":
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
