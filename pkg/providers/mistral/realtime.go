package mistral

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// RealtimeSession holds the credentials for one browser voice conversation
// with a Mistral managed voice agent (TS Experimental_MistralRealtimeSession,
// 0912e0b1b9).
type RealtimeSession struct {
	ID               string `json:"id"`
	ParticipantToken string `json:"participantToken"`
	ServerURL        string `json:"serverUrl"`
}

// RealtimeCreateSessionOptions are the options of
// (*RealtimeSessions).CreateSession.
type RealtimeCreateSessionOptions struct {
	// Model is the Mistral chat model that drives the voice agent.
	Model string
	// SessionConfig is the realtime session configuration (instructions,
	// tools, voice, input transcription, output modalities and
	// providerOptions.mistral; see the Mistral provider docs).
	SessionConfig *provider.RealtimeSessionConfig
}

// RealtimeSessions is the server half of the TS Mistral realtime factory
// (provider.experimental_realtime): it creates and closes voice sessions with
// the Mistral API key. The browser transport (LiveKit WebRTC) is not part of
// the Go SDK; a Go server hands the returned credentials to a browser client.
//
// Experimental: may change in patch releases.
type RealtimeSessions struct {
	provider *Provider
}

// ExperimentalRealtime returns the Mistral voice session helpers (TS
// experimental_realtime createSession / closeSession). Server-only: the
// requests carry the provider's API key.
func (p *Provider) ExperimentalRealtime() *RealtimeSessions {
	return &RealtimeSessions{provider: p}
}

// CreateSession creates a voice session: POST /v1/audio/sessions with the
// session configuration built from opts (TS createSession).
func (r *RealtimeSessions) CreateSession(ctx context.Context, opts RealtimeCreateSessionOptions) (*RealtimeSession, error) {
	if providerutils.JSTrim(opts.Model) == "" {
		return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "Mistral voice sessions require a model ID."}
	}
	config := provider.RealtimeSessionConfig{}
	if opts.SessionConfig != nil {
		config = *opts.SessionConfig
	}
	body, err := BuildRealtimeSessionConfig(config, opts.Model, false)
	if err != nil {
		return nil, err
	}
	resp, err := r.provider.client.Do(ctx, internalhttp.Request{Method: http.MethodPost, Path: "/v1/audio/sessions", Body: body})
	if err != nil {
		return nil, providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, internalhttp.StatusProviderError("mistral", resp.StatusCode, resp.Headers, resp.Body)
	}
	text := []byte(textdecode.UTF8(resp.Body))
	var value struct {
		ID               interface{} `json:"id"`
		ParticipantToken interface{} `json:"participant_token"`
		ServerURL        interface{} `json:"server_url"`
	}
	if err := json.Unmarshal(text, &value); err != nil {
		return nil, providerutils.InvalidJSONResponseError("mistral", resp.StatusCode, resp.Headers, text, err)
	}
	id, ok1 := value.ID.(string)
	token, ok2 := value.ParticipantToken.(string)
	serverURL, ok3 := value.ServerURL.(string)
	if !ok1 || !ok2 || !ok3 || id == "" || token == "" || serverURL == "" {
		return nil, providerutils.InvalidJSONResponseError("mistral", resp.StatusCode, resp.Headers, text,
			fmt.Errorf("expected non-empty id, participant_token and server_url"))
	}
	return &RealtimeSession{ID: id, ParticipantToken: token, ServerURL: serverURL}, nil
}

// CloseSession releases a voice session after the browser disconnects:
// DELETE /v1/audio/sessions/{id} (TS closeSession).
func (r *RealtimeSessions) CloseSession(ctx context.Context, sessionID string) error {
	if providerutils.JSTrim(sessionID) == "" || sessionID == "." || sessionID == ".." {
		return &providererrors.InvalidArgumentError{Field: "sessionId", Message: "Mistral voice sessions require a valid session ID."}
	}
	resp, err := r.provider.client.Do(ctx, internalhttp.Request{
		Method: http.MethodDelete,
		Path:   "/v1/audio/sessions/" + providerutils.EncodeURIComponent(sessionID),
	})
	if err != nil {
		return providererrors.NewProviderError("mistral", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return internalhttp.StatusProviderError("mistral", resp.StatusCode, resp.Headers, resp.Body)
	}
	return nil
}

var mistralZeroInt = 0

// mistralStrictObject is z.strictObject(schema): unknown keys are rejected.
func mistralStrictObject(schema providerutils.OptionSchema) providerutils.OptionCheck {
	object := providerutils.OptObject(schema)
	return func(path string, v interface{}) error {
		if err := object(path, v); err != nil {
			return err
		}
		if m, ok := v.(map[string]interface{}); ok {
			if err := mistralUnknownKeys(path+".", m, schema); err != nil {
				return err
			}
		}
		return nil
	}
}

func mistralUnknownKeys(prefix string, m map[string]interface{}, schema providerutils.OptionSchema) error {
	var unknown []string
	for k := range m {
		if _, ok := schema[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unrecognized key(s) in object: %s (at %s)", strings.Join(unknown, ", "), strings.TrimSuffix(prefix, "."))
}

func mistralIntRange(lo, hi *int) providerutils.OptionCheck { return providerutils.OptInt(lo, hi) }

// mistralRealtimeOptionsSchema is mistralRealtimeModelOptions (TS
// mistral-realtime-model-options.ts).
var mistralRealtimeOptionsSchema = func() providerutils.OptionSchema {
	lo, hi := 80, 1200
	nonEmpty := providerutils.OptStringLen(1, -1)
	unit := providerutils.OptRange(0, 1)
	nonNeg := mistralIntRange(&mistralZeroInt, nil)
	return providerutils.OptionSchema{
		"stt": mistralStrictObject(providerutils.OptionSchema{
			"model":                  nonEmpty,
			"language":               nonEmpty,
			"contextBias":            providerutils.OptArray(nonEmpty, 0, -1),
			"targetStreamingDelayMs": mistralIntRange(&lo, &hi),
			"turnDetection": mistralStrictObject(providerutils.OptionSchema{
				"startThreshold": unit, "eagerEndThreshold": unit, "endThreshold": unit,
				"minSilenceMs": nonNeg, "endTimeoutMs": nonNeg,
			}),
		}),
		"tts": mistralStrictObject(providerutils.OptionSchema{
			"model":        nonEmpty,
			"voiceId":      nonEmpty,
			"phonemeHints": providerutils.OptRecord(nil, providerutils.OptArray(nonEmpty, 1, -1)),
		}),
		"turn": mistralStrictObject(providerutils.OptionSchema{
			"allowInterruptions": providerutils.OptBool,
			"inactivity": mistralStrictObject(providerutils.OptionSchema{
				"timeoutMs":    nonNeg,
				"maxReminders": nonNeg,
				"messages":     providerutils.OptArray(providerutils.OptString, 0, -1),
			}),
		}),
		"welcomeMessage": providerutils.OptString,
		"agentId":        nonEmpty,
		"agentVersion":   nonNeg,
	}
}()

func mistralRealtimeUnsupported(setting string, update bool) error {
	kind := "setting"
	if update {
		kind = "live update of"
	}
	return &providererrors.UnsupportedFunctionalityError{Functionality: "Mistral voice agent " + kind + " " + setting}
}

// BuildRealtimeSessionConfig builds the Mistral voice session configuration
// from a realtime session config (TS buildMistralRealtimeSessionConfig): the
// REST body of CreateSession, or with update the subset a live session.update
// accepts. modelID, when not empty, becomes llm.model.
//
// Experimental: may change in patch releases.
func BuildRealtimeSessionConfig(config provider.RealtimeSessionConfig, modelID string, update bool) (map[string]interface{}, error) {
	type setting struct {
		key string
		set bool
	}
	for _, s := range []setting{
		{"voice", config.Voice != nil},
		{"outputModalities", config.OutputModalities != nil},
		{"inputAudioFormat", config.InputAudioFormat != nil},
		{"inputAudioTranscription", config.InputAudioTranscription != nil},
		{"outputAudioTranscription", config.OutputAudioTranscription != nil},
		{"outputAudioFormat", config.OutputAudioFormat != nil},
		{"turnDetection", config.TurnDetection != nil},
	} {
		allowed := !update && (s.key == "voice" || s.key == "inputAudioTranscription" || s.key == "outputAudioTranscription" || s.key == "outputModalities")
		if s.set && !allowed {
			return nil, mistralRealtimeUnsupported(s.key, update)
		}
	}
	if t := config.InputAudioTranscription; t != nil && t.Prompt != nil {
		return nil, mistralRealtimeUnsupported("inputAudioTranscription.prompt", update)
	}
	if t := config.OutputAudioTranscription; t != nil && (t.Model != nil || t.Language != nil || t.Prompt != nil) {
		return nil, mistralRealtimeUnsupported("outputAudioTranscription settings", update)
	}
	if mods := config.OutputModalities; mods != nil {
		hasAudio, other := false, false
		for _, m := range mods {
			switch m {
			case "audio":
				hasAudio = true
			case "text":
			default:
				other = true
			}
		}
		if !hasAudio || other {
			return nil, mistralRealtimeUnsupported("outputModalities without audio", update)
		}
	}

	var options map[string]interface{}
	if raw, ok := config.ProviderOptions["mistral"]; ok && raw != nil {
		normalized := providerutils.NormalizeOptionsJSON(map[string]interface{}{"mistral": raw})
		parsed, err := providerutils.ParseProviderOptions("mistral", normalized, mistralRealtimeOptionsSchema)
		if err != nil {
			return nil, err
		}
		if parsed != nil {
			if err := mistralUnknownKeys("", parsed, mistralRealtimeOptionsSchema); err != nil {
				return nil, &providererrors.InvalidArgumentError{Field: "providerOptions", Message: "invalid mistral provider options", Cause: err}
			}
		}
		options = parsed
	}
	if update {
		keys := make([]string, 0, len(options))
		for k, v := range options {
			if v != nil && k != "turn" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			return nil, mistralRealtimeUnsupported("providerOptions.mistral."+keys[0], true)
		}
	}
	if options["agentVersion"] != nil && options["agentId"] == nil {
		return nil, &providererrors.InvalidArgumentError{Field: "providerOptions.mistral.agentVersion", Message: "Mistral agentVersion requires agentId."}
	}

	obj := func(v interface{}) map[string]interface{} { m, _ := v.(map[string]interface{}); return m }
	// put sets key when value is not nil (JSON.stringify drops undefined).
	put := func(m map[string]interface{}, key string, value interface{}) {
		if value != nil {
			m[key] = value
		}
	}
	stt, tts, turn := obj(options["stt"]), obj(options["tts"]), obj(options["turn"])

	out := map[string]interface{}{}
	if modelID != "" {
		out["llm"] = map[string]interface{}{"model": modelID}
	}
	if config.Instructions != nil {
		out["instructions"] = *config.Instructions
	}
	if config.Tools != nil {
		tools := make([]interface{}, 0, len(config.Tools))
		for _, t := range config.Tools {
			fn := map[string]interface{}{"name": t.Name, "parameters": t.Parameters}
			if t.Description != nil {
				fn["description"] = *t.Description
			}
			tools = append(tools, map[string]interface{}{"type": "function", "function": fn})
		}
		out["tools"] = tools
	}
	if stt != nil || config.InputAudioTranscription != nil {
		s := map[string]interface{}{}
		model, language := stt["model"], stt["language"]
		if t := config.InputAudioTranscription; t != nil {
			if t.Model != nil {
				model = *t.Model
			}
			if t.Language != nil {
				language = *t.Language
			}
		}
		put(s, "model", model)
		put(s, "language", language)
		put(s, "context_bias", stt["contextBias"])
		put(s, "target_streaming_delay_ms", stt["targetStreamingDelayMs"])
		if td := obj(stt["turnDetection"]); td != nil {
			d := map[string]interface{}{}
			put(d, "start_threshold", td["startThreshold"])
			put(d, "eager_end_threshold", td["eagerEndThreshold"])
			put(d, "end_threshold", td["endThreshold"])
			put(d, "min_silence_ms", td["minSilenceMs"])
			put(d, "end_timeout_ms", td["endTimeoutMs"])
			s["turn_detection"] = d
		}
		out["stt"] = s
	}
	if tts != nil || config.Voice != nil {
		s := map[string]interface{}{}
		put(s, "model", tts["model"])
		voice := tts["voiceId"]
		if config.Voice != nil {
			voice = *config.Voice
		}
		put(s, "voice_id", voice)
		put(s, "phoneme_hints", tts["phonemeHints"])
		out["tts"] = s
	}
	if turn != nil {
		s := map[string]interface{}{}
		put(s, "allow_interruptions", turn["allowInterruptions"])
		if in := obj(turn["inactivity"]); in != nil {
			i := map[string]interface{}{}
			put(i, "timeout_ms", in["timeoutMs"])
			put(i, "max_reminders", in["maxReminders"])
			put(i, "messages", in["messages"])
			s["inactivity"] = i
		}
		out["turn"] = s
	}
	put(out, "welcome_message", options["welcomeMessage"])
	put(out, "agent_id", options["agentId"])
	put(out, "agent_version", options["agentVersion"])
	return out, nil
}
