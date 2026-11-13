package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsdate"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

const realtimeWebSocketPath = "google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContentConstrained"

// realtimeServerWebSocketPath is the unconstrained Bidi endpoint used by
// server-side connections: a trusted server holds the API key and sends the
// session setup over the socket (TS 892af4dab7).
const realtimeServerWebSocketPath = "google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

type GoogleRealtimeModel struct {
	provider *Provider
	modelID  string
	// mapperMu guards mapper: the model-level ParseServerEvent /
	// SerializeClientEvent share one stateful mapper (TS this.mapper), so
	// concurrent direct callers must not race on it. Sessions should use
	// NewServerEventParser, which gives each connection its own mapper.
	mapperMu sync.Mutex
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
	_ provider.Experimental_RealtimeModelV4     = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeClientSecretCreator      = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeWebSocketConfigProvider  = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeServerEventParserFactory = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeServerConnectionProvider = (*GoogleRealtimeModel)(nil)
	_ provider.RealtimeIntentCodec              = (*GoogleRealtimeModel)(nil)
)

// googleRealtimeAssertConversationIntent mirrors TS assertConversationIntent:
// Gemini Live exposes only the conversational endpoint, so another intent
// fails fast instead of silently downgrading. The empty intent is
// conversation.
func googleRealtimeAssertConversationIntent(intent provider.RealtimeSessionIntent) error {
	if intent != "" && intent != provider.RealtimeSessionIntentConversation {
		return fmt.Errorf("Google realtime does not support the '%s' session intent.", intent) //nolint:staticcheck // matches the TS error text
	}
	return nil
}

// GetServerConnection returns the server-side WebSocket connection (TS
// getServerWebSocketConfig, 892af4dab7): the unconstrained v1beta Bidi
// endpoint with the provider's resolved headers (x-goog-api-key, custom
// headers, user agent). Conversation intent only. ConnectRealtime uses it
// only when RealtimeSessionOptions.Intent is set (coordinator ruling 1).
func (m *GoogleRealtimeModel) GetServerConnection(ctx context.Context, opts provider.RealtimeServerConnectionOptions) (provider.WebSocketConfig, error) {
	if err := googleRealtimeAssertConversationIntent(opts.Intent); err != nil {
		return provider.WebSocketConfig{}, err
	}
	headers, err := m.provider.authHeaders(ctx)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	wsURL, err := googleServerWebSocketURL(m.provider.config.BaseURL)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	return provider.WebSocketConfig{URL: wsURL, Headers: headers}, nil
}

// BuildSessionConfigForIntent is BuildSessionConfig for an intent
// (conversation only).
func (m *GoogleRealtimeModel) BuildSessionConfigForIntent(config provider.RealtimeSessionConfig, intent provider.RealtimeSessionIntent) (any, error) {
	if err := googleRealtimeAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.BuildSessionConfig(config), nil
}

// SerializeClientEventForIntent is SerializeClientEvent for an intent
// (conversation only).
func (m *GoogleRealtimeModel) SerializeClientEventForIntent(event provider.RealtimeClientEvent, intent provider.RealtimeSessionIntent) (json.RawMessage, error) {
	if err := googleRealtimeAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.SerializeClientEvent(event)
}

// NewServerEventParser returns a parser with its own event mapper, so each
// connection keeps its turn and transcription state apart (TS
// createServerEventParser, 892af4dab7).
func (m *GoogleRealtimeModel) NewServerEventParser() func(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	mapper := &googleRealtimeEventMapper{inputAudioRate: 16000}
	return func(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
		if err := googleRealtimeNullEventError(raw); err != nil {
			return nil, err
		}
		return mapper.parseServerEvent(raw), nil
	}
}

func (m *GoogleRealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	apiKey, err := m.googleRealtimeAPIKey(ctx)
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
	// TS: now = Date.now(); openWindowMs = expiresAfterSeconds * 1000;
	// new Date(now + openWindowMs).toISOString(), then the same + 30 min.
	// JS number arithmetic (no int64 overflow); an out-of-range time is the
	// toISOString RangeError, newSessionExpireTime first.
	now := float64(time.Now().UnixMilli())
	openWindowMs := float64(seconds) * 1000
	newSessionExpireTime, err := googleRealtimeISOString(now + openWindowMs)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	expireTime, err := googleRealtimeISOString(now + openWindowMs + 30*60*1000)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	body := map[string]interface{}{
		"uses":                     0,
		"expireTime":               expireTime,
		"newSessionExpireTime":     newSessionExpireTime,
		"bidiGenerateContentSetup": m.BuildSessionConfig(valueOrZero(opts.SessionConfig)),
	}
	// TS JSON.stringify of the literal: uses, expireTime,
	// newSessionExpireTime, then the setup in buildGoogleSessionConfig order.
	orders := googleRealtimeSetupOrders("bidiGenerateContentSetup")
	orders[""] = []string{"uses", "expireTime", "newSessionExpireTime", "bidiGenerateContentSetup"}
	payload := []byte(providerutils.StringifyOrdered(body, orders))
	authTokenURL, err := googleAuthTokenURL(m.provider.config.BaseURL, apiKey)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authTokenURL, bytes.NewReader(payload))
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.provider.client.HTTPClient().Do(req)
	if err != nil {
		// TS calls fetchFn directly (no handleFetchError), so the rejection
		// is returned as is. The URL carries ?key=<API key>; a TS fetch
		// rejection never names the URL, so strip the query first.
		return provider.ClientSecretResult{}, googleRawFetchError(redactURLErrorQuery(err))
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, googleFilesMaxResponseBytes))
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
	// TS response.json(): plain JSON.parse, not secureJsonParse.
	if err := jsonexact.UnmarshalPlain([]byte(textdecode.UTF8(respBody)), &data); err != nil {
		return provider.ClientSecretResult{}, err
	}
	var expiresAt *int64
	if data.ExpireTime != "" {
		// TS Math.floor(new Date(expireTime).getTime() / 1000); a NaN date
		// has no Go value and leaves expiresAt unset.
		if ms, ok := jsdate.Parse(data.ExpireTime); ok {
			sec := ms / 1000
			if ms%1000 < 0 {
				sec--
			}
			expiresAt = &sec
		}
	}
	wsURL, err := googleWebSocketURL(m.provider.config.BaseURL)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return provider.ClientSecretResult{
		Token:     data.Name,
		URL:       wsURL,
		ExpiresAt: expiresAt,
	}, nil
}

// googleRealtimeAPIKey mirrors TS `this.config.headers()['x-goog-api-key']`:
// the provider's getHeaders loads the key (a missing key is a
// LoadAPIKeyError), and a configured x-goog-api-key header wins.
func (m *GoogleRealtimeModel) googleRealtimeAPIKey(ctx context.Context) (string, error) {
	headers, err := m.provider.authHeaders(ctx)
	if err != nil {
		return "", err
	}
	return headers["x-goog-api-key"], nil
}

func (m *GoogleRealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	// TS `${options.url}?access_token=${encodeURIComponent(options.token)}`.
	return provider.WebSocketConfig{URL: wsURL + "?access_token=" + providerutils.EncodeURIComponent(token)}
}

// thinkingLiveModelRe matches Live models that reason in the background
// (e.g. "gemini-3.8-live-extended-thinking"). Google requires exactly one of
// thinkingLevel/thinkingBudget in their setup and rejects thinkingConfig on
// every other Live model (TS isThinkingLiveModel). The TS `.` does not match
// a line terminator (\r, U+2028, U+2029 as well as \n).
var thinkingLiveModelRe = regexp.MustCompile(`^gemini-\d+\.\d+-live\b[^\n\r\x{2028}\x{2029}]*thinking`)

// isThinkingLiveModel reports whether modelID names a background-reasoning
// Live model, matching only the last "/"-separated path segment.
func isThinkingLiveModel(modelID string) bool {
	segments := strings.Split(modelID, "/")
	name := providerutils.JSToLower(segments[len(segments)-1])
	return thinkingLiveModelRe.MatchString(name)
}

// redactURLErrorQuery drops the query (the API key) from a *url.Error's URL.
func redactURLErrorQuery(err error) error {
	ue, ok := err.(*url.Error)
	if !ok {
		return err
	}
	redacted := *ue
	if u, perr := url.Parse(ue.URL); perr == nil {
		u.RawQuery = ""
		u.ForceQuery = false
		redacted.URL = u.String()
	} else {
		redacted.URL = "<redacted>"
	}
	return &redacted
}

// googleRealtimeSetupOrders is the JS insertion order of the
// buildGoogleSessionConfig object at path prefix (for StringifyOrdered):
// model, generationConfig, systemInstruction, tools, the transcription
// configs, then the provider option keys Object.assign appends (sorted: a Go
// map has no order). generationConfig adds translationConfig and then
// thinkingConfig after its own keys; a function declaration is name,
// description, parametersJsonSchema, behavior.
func googleRealtimeSetupOrders(prefix string) map[string][]string {
	return map[string][]string{
		prefix:                                 {"model", "generationConfig", "systemInstruction", "tools", "inputAudioTranscription", "outputAudioTranscription"},
		prefix + ".generationConfig":           {"responseModalities", "speechConfig", "translationConfig", "thinkingConfig"},
		prefix + ".tools.functionDeclarations": {"name", "description", "parametersJsonSchema", "behavior"},
	}
}

// BuildSessionConfig ports TS buildGoogleSessionConfig
// (realtime/google-realtime-event-mapper.ts).
func (m *GoogleRealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	setup := map[string]interface{}{"model": gemini.GetModelPath(m.modelID)}
	// Typed-option ruling: Go-typed literals are read in their JSON form.
	config.ProviderOptions = providerutils.NormalizeOptionsJSON(config.ProviderOptions)
	googleOptions, _ := config.ProviderOptions["google"].(map[string]interface{})

	generation := map[string]interface{}{}
	if config.OutputModalities != nil {
		modalities := make([]string, len(config.OutputModalities))
		for i, modality := range config.OutputModalities {
			modalities[i] = providerutils.JSToUpper(modality)
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
	// TS `defaultToolBehavior != null`.
	defaultToolBehavior := googleOptions["defaultToolBehavior"]
	hasDefaultToolBehavior := defaultToolBehavior != nil
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
	// TS `googleOptions?.thinkingConfig ?? raw...`: a null option is absent.
	googleThinkingConfig := googleOptions["thinkingConfig"]
	hasGoogleThinkingConfig := googleThinkingConfig != nil
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
	if err := googleRealtimeNullEventError(raw); err != nil {
		return nil, err
	}
	m.mapperMu.Lock()
	defer m.mapperMu.Unlock()
	return m.mapper.parseServerEvent(raw), nil
}

// googleRealtimeNullEventError is the TypeError TS parseEvent throws reading
// `data.setupComplete` of a JSON null server message; the realtime event
// channel treats it as fatal.
func googleRealtimeNullEventError(raw json.RawMessage) error {
	if string(bytes.TrimSpace(raw)) == "null" {
		return errors.New("Cannot read properties of null (reading 'setupComplete')") //nolint:staticcheck // TS V8 TypeError text
	}
	return nil
}

func (m *GoogleRealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	m.mapperMu.Lock()
	defer m.mapperMu.Unlock()
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

// parseServerEvent ports TS parseServerEvent (892af4dab7): the frame's
// usageMetadata goes on the response-done event of the same frame, or else
// into a custom "usageMetadata" event (only a shared frame associates usage
// with a completion).
func (m *googleRealtimeEventMapper) parseServerEvent(raw json.RawMessage) []provider.RealtimeServerEvent {
	events := m.parseEvent(raw)
	var data struct {
		UsageMetadata *googleRealtimeUsageMetadata `json:"usageMetadata"`
	}
	_ = jsonexact.Unmarshal(raw, &data)
	usage := mapGoogleRealtimeUsage(data.UsageMetadata)
	if usage == nil {
		return events
	}
	hasDone := false
	for _, ev := range events {
		if ev.Type == "response-done" {
			hasDone = true
		}
	}
	if hasDone {
		out := make([]provider.RealtimeServerEvent, len(events))
		for i, ev := range events {
			if ev.Type == "response-done" {
				u := *usage
				ev.Usage = &u
			}
			out[i] = ev
		}
		return out
	}
	update := provider.RealtimeServerEvent{Type: "custom", RawType: "usageMetadata", Usage: usage, Raw: raw}
	if len(events) == 1 && events[0].Type == "custom" && events[0].RawType == "usageMetadata" {
		return []provider.RealtimeServerEvent{update}
	}
	return append(events, update)
}

type googleRealtimeTokenDetail struct {
	Modality   *string  `json:"modality"`
	TokenCount *float64 `json:"tokenCount"`
}

// googleRealtimeUsageMetadata is the Gemini Live usageMetadata (Live uses
// responseTokenCount / responseTokensDetails for output, not the candidates*
// fields of generateContent).
type googleRealtimeUsageMetadata struct {
	PromptTokenCount        *float64                    `json:"promptTokenCount"`
	ResponseTokenCount      *float64                    `json:"responseTokenCount"`
	ToolUsePromptTokenCount *float64                    `json:"toolUsePromptTokenCount"`
	ThoughtsTokenCount      *float64                    `json:"thoughtsTokenCount"`
	CachedContentTokenCount *float64                    `json:"cachedContentTokenCount"`
	PromptTokensDetails     []googleRealtimeTokenDetail `json:"promptTokensDetails"`
	ResponseTokensDetails   []googleRealtimeTokenDetail `json:"responseTokensDetails"`
}

// mapGoogleRealtimeUsage ports TS mapGoogleUsage: gross totals include tool
// use prompt tokens (input) and thoughts (output); per-modality TEXT / AUDIO
// sums; nil when nothing is observable. A text/audio split is never guessed.
func mapGoogleRealtimeUsage(u *googleRealtimeUsageMetadata) *provider.RealtimeUsage {
	if u == nil {
		return nil
	}
	count := func(v float64) *float64 { return &v } // TS keeps the JS number
	usage := &provider.RealtimeUsage{}
	seen := false
	if u.PromptTokenCount != nil {
		v := *u.PromptTokenCount
		if u.ToolUsePromptTokenCount != nil {
			v += *u.ToolUsePromptTokenCount
		}
		usage.InputTokens, seen = count(v), true
	}
	if u.ResponseTokenCount != nil {
		v := *u.ResponseTokenCount
		if u.ThoughtsTokenCount != nil {
			v += *u.ThoughtsTokenCount
		}
		usage.OutputTokens, seen = count(v), true
	}
	if u.CachedContentTokenCount != nil {
		usage.CachedInputTokens, seen = count(*u.CachedContentTokenCount), true
	}
	sum := func(details []googleRealtimeTokenDetail) (text, audio *float64) {
		var t, a float64
		hasT, hasA := false, false
		for _, d := range details {
			if d.TokenCount == nil {
				continue
			}
			modality := ""
			if d.Modality != nil {
				modality = providerutils.JSToUpper(*d.Modality)
			}
			switch modality {
			case "AUDIO":
				a += *d.TokenCount
				hasA = true
			case "TEXT":
				t += *d.TokenCount
				hasT = true
			}
		}
		if hasT {
			text = count(t)
		}
		if hasA {
			audio = count(a)
		}
		return text, audio
	}
	usage.InputTextTokens, usage.InputAudioTokens = sum(u.PromptTokensDetails)
	usage.OutputTextTokens, usage.OutputAudioTokens = sum(u.ResponseTokensDetails)
	if usage.InputTextTokens != nil || usage.InputAudioTokens != nil || usage.OutputTextTokens != nil || usage.OutputAudioTokens != nil {
		seen = true
	}
	if !seen {
		return nil
	}
	return usage
}

func (m *googleRealtimeEventMapper) parseEvent(raw json.RawMessage) []provider.RealtimeServerEvent {
	var data map[string]interface{}
	_ = jsonexact.Unmarshal(raw, &data)
	if value, ok := data["setupComplete"]; ok && value != nil {
		return []provider.RealtimeServerEvent{{Type: "session-created", Raw: raw}}
	}
	if toolCall, ok := data["toolCall"].(map[string]interface{}); ok {
		m.beginTurnIfClosed()
		calls, _ := toolCall["functionCalls"].([]interface{})
		// TS JSON.stringify(functionCall.args ?? {}) keeps the wire key order.
		var ordered struct {
			ToolCall struct {
				FunctionCalls []struct {
					Args json.RawMessage `json:"args"`
				} `json:"functionCalls"`
			} `json:"toolCall"`
		}
		_ = jsonexact.Unmarshal(raw, &ordered)
		events := make([]provider.RealtimeServerEvent, 0, len(calls)*2)
		for i, rawCall := range calls {
			call, _ := rawCall.(map[string]interface{})
			args := jsonStringify(valueOrEmptyMap(call["args"]))
			if i < len(ordered.ToolCall.FunctionCalls) {
				if v, ok := jsonorder.Parse(ordered.ToolCall.FunctionCalls[i].Args); ok && v != nil {
					args = jsonorder.Stringify(v)
				}
			}
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
		// TS JSON.stringify keeps the buildGoogleSessionConfig key order.
		return json.RawMessage(providerutils.StringifyOrdered(map[string]interface{}{"setup": setup}, googleRealtimeSetupOrders("setup"))), nil
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
	return jsonstr.Marshal(out)
}

// googleAuthTokenURL is TS getAuthTokensURL plus the key query:
// `${getAuthTokensURL(baseURL)}?key=${encodeURIComponent(apiKey)}`.
func googleAuthTokenURL(base, apiKey string) (string, error) {
	u, err := googleRealtimeBaseURL(base)
	if err != nil {
		return "", err
	}
	u.SetPathname(strings.TrimSuffix(u.Pathname(), "/") + "/v1alpha/auth_tokens") // TS replace(/\/$/, '')
	return u.String() + "?key=" + providerutils.EncodeURIComponent(apiKey), nil
}

// googleWebSocketURL is TS getWebSocketURL (the constrained endpoint).
func googleWebSocketURL(base string) (string, error) {
	u, err := googleRealtimeWebSocketURL(base, realtimeWebSocketPath)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// googleServerWebSocketURL is TS getServerWebSocketURL.
func googleServerWebSocketURL(base string) (string, error) {
	u, err := googleRealtimeWebSocketURL(base, realtimeServerWebSocketPath)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// googleRealtimeWebSocketURL is TS getRealtimeWebSocketURL:
// url.protocol = protocol === 'https:' ? 'wss:' : 'ws:', then
// url.pathname = `${pathname.replace(/\/$/, ”)}/ws/${webSocketPath}`.
func googleRealtimeWebSocketURL(base, webSocketPath string) (*urlnorm.URL, error) {
	u, err := googleRealtimeBaseURL(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.SetProtocol("wss")
	} else {
		u.SetProtocol("ws")
	}
	u.SetPathname(strings.TrimSuffix(u.Pathname(), "/") + "/ws/" + webSocketPath)
	return u, nil
}

// googleRealtimeBaseURL is TS getRealtimeBaseURL: new URL(baseURL) (WHATWG,
// "Invalid URL" for a base URL it rejects), with a trailing v1beta or
// v1alpha path segment removed through the pathname setter.
func googleRealtimeBaseURL(base string) (*urlnorm.URL, error) {
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := urlnorm.Parse(base)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(u.Pathname(), "/")
	if last := segments[len(segments)-1]; last == "v1beta" || last == "v1alpha" {
		joined := strings.Join(segments[:len(segments)-1], "/")
		if joined == "" {
			joined = "/"
		}
		u.SetPathname(joined)
	}
	return u, nil
}

// googleRealtimeISOString is new Date(ms).toISOString(): TimeClip (an
// integral time value, truncated toward zero), jsdate.ISOString (expanded
// years outside 0..9999), and RangeError "Invalid time value" outside the
// ECMAScript time range.
func googleRealtimeISOString(ms float64) (string, error) {
	if !math.IsNaN(ms) && !math.IsInf(ms, 0) && math.Abs(ms) <= 8.64e15 {
		if s, ok := jsdate.ISOString(time.UnixMilli(int64(ms))); ok {
			return s, nil
		}
	}
	return "", errors.New("Invalid time value") //nolint:staticcheck // TS RangeError text
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
func toFunctionResponseStruct(output string) interface{} {
	// TS safeParseJSON keeps the object's key order (and rejects
	// __proto__ / constructor.prototype keys, like securejson).
	var probe interface{}
	if securejson.Unmarshal([]byte(output), &probe) != nil {
		return map[string]interface{}{"output": output}
	}
	parsed, ok := jsonorder.Parse([]byte(output))
	if !ok {
		return map[string]interface{}{"output": output}
	}
	if obj, isObj := parsed.(*jsonorder.Object); isObj {
		return obj
	}
	wrapped := jsonorder.New()
	wrapped.Set("output", parsed)
	return wrapped
}

func valueOrZero(config *provider.RealtimeSessionConfig) provider.RealtimeSessionConfig {
	if config == nil {
		return provider.RealtimeSessionConfig{}
	}
	return *config
}
