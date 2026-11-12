package openai

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// openAILiveOptions is the parsed form of
// sessionConfig.providerOptions.openai for the Live API, mirroring the
// TypeScript SDK's openaiRealtimeModelLiveOptionsSchema (minus the
// WebRTC-only "client" data-channel permissions, which are rejected
// outright since Go only supports the server-WebSocket transport).
type openAILiveOptions struct {
	HasClient      bool
	Delegation     *string // "client", or nil when absent; explicitly-null clears it
	HasDelegation  bool
	DelegationResp bool // providerOptions.openai.delegation.type === "responses" (always rejected)
	Input          []interface{}
	Store          *bool
	Voice          *string
}

var openAILiveOptionKeys = map[string]bool{
	"client": true, "delegation": true, "input": true, "store": true, "voice": true,
}

var openAILiveInputRoles = map[string]bool{"developer": true, "user": true, "assistant": true}

func parseOpenAILiveOptions(providerOptions map[string]interface{}) (openAILiveOptions, error) {
	var out openAILiveOptions
	raw, ok := providerOptions["openai"]
	if !ok || raw == nil {
		return out, nil
	}
	// Read through the JSON form (TS options are JSON values), so Go-typed
	// literals ([]map[string]interface{} input, map[string]string voice)
	// validate like the map[string]interface{} form.
	m, ok := providerutils.NormalizeProviderOptionsObject(raw)
	if !ok {
		return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai", Message: "must be an object"}
	}
	// TS checks the loose {delegation: {type: 'responses'}} shape before the
	// strict schema (openai-live-session-config.ts:41-49), so it wins over
	// an unknown key.
	if d, ok := m["delegation"].(map[string]interface{}); ok && d["type"] == "responses" {
		out.HasDelegation = true
		out.DelegationResp = true
		return out, nil
	}
	for key := range m {
		if !openAILiveOptionKeys[key] {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai." + key, Message: "unrecognized key"}
		}
	}
	// strictObject members (openai-realtime-model-live-options.ts:17-65).
	strict := func(field string, obj map[string]interface{}, keys ...string) error {
		for k := range obj {
			known := false
			for _, want := range keys {
				known = known || k == want
			}
			if !known {
				return &providererrors.InvalidArgumentError{Field: field + "." + k, Message: "unrecognized key"}
			}
		}
		return nil
	}
	if d, ok := m["delegation"].(map[string]interface{}); ok {
		if err := strict("providerOptions.openai.delegation", d, "type"); err != nil {
			return out, err
		}
	}
	if v, ok := m["voice"].(map[string]interface{}); ok {
		if err := strict("providerOptions.openai.voice", v, "id"); err != nil {
			return out, err
		}
	}
	if items, ok := m["input"].([]interface{}); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if err := strict("providerOptions.openai.input", item, "type", "role", "content"); err != nil {
				return out, err
			}
			if content, ok := item["content"].([]interface{}); ok && len(content) == 1 {
				if part, ok := content[0].(map[string]interface{}); ok {
					if err := strict("providerOptions.openai.input.content", part, "type", "text"); err != nil {
						return out, err
					}
				}
			}
		}
	}

	if client, ok := m["client"]; ok {
		// TS parses the strict client schema before rejecting client
		// permissions outside WebRTC, so a malformed value is a schema error.
		if err := validateOpenAILiveClientOption(client); err != nil {
			return out, err
		}
		out.HasClient = true
	}

	if delegationRaw, ok := m["delegation"]; ok {
		if delegationRaw == nil {
			out.HasDelegation = true
		} else if delegationMap, ok := delegationRaw.(map[string]interface{}); ok {
			t, _ := delegationMap["type"].(string)
			switch t {
			case "responses":
				// Checked against a loose schema before the strict one in
				// TS (buildOpenAILiveSessionConfig), so it's accepted here
				// and rejected later with the friendly "Responses
				// delegation" message rather than a generic schema error.
				out.HasDelegation = true
				out.DelegationResp = true
			case "client":
				out.HasDelegation = true
				typeCopy := t
				out.Delegation = &typeCopy
			default:
				// TS's strict schema only allows {type: 'client'} here
				// (openaiRealtimeModelLiveOptionsSchema); any other type,
				// including an absent/malformed one, fails validation.
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.delegation.type", Message: `must be "client"`}
			}
		} else {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.delegation", Message: "must be an object or null"}
		}
	}

	if inputRaw, ok := m["input"]; ok {
		arr, ok := inputRaw.([]interface{})
		if !ok {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "must be an array"}
		}
		if len(arr) > 128 {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "must contain at most 128 items"}
		}
		for _, itemRaw := range arr {
			item, ok := itemRaw.(map[string]interface{})
			if !ok {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "each item must be an object"}
			}
			if t, _ := item["type"].(string); t != "message" {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: `each item's type must be "message"`}
			}
			role, _ := item["role"].(string)
			if !openAILiveInputRoles[role] {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: `role must be "developer", "user", or "assistant"`}
			}
			// TS's discriminatedUnion('role', ...) also requires content to
			// be a single-element tuple: {type:"input_text"} for
			// developer/user, {type:"text"|"output_text"} for assistant.
			content, ok := item["content"].([]interface{})
			if !ok || len(content) != 1 {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "content must be a single-element array"}
			}
			part, ok := content[0].(map[string]interface{})
			if !ok {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "content[0] must be an object"}
			}
			partType, _ := part["type"].(string)
			wantType := partType == "input_text"
			if role == "assistant" {
				wantType = partType == "text" || partType == "output_text"
			}
			if !wantType {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "content[0].type does not match role"}
			}
			if _, ok := part["text"].(string); !ok {
				return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.input", Message: "content[0].text must be a string"}
			}
		}
		out.Input = arr
	}

	if storeRaw, ok := m["store"]; ok {
		b, ok := storeRaw.(bool)
		if !ok {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.store", Message: "must be a boolean"}
		}
		out.Store = &b
	}

	if voiceRaw, ok := m["voice"]; ok {
		voiceMap, ok := voiceRaw.(map[string]interface{})
		if !ok {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.voice", Message: "must be an object with an id"}
		}
		id, _ := voiceMap["id"].(string)
		if id == "" {
			return out, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.voice.id", Message: "must be a non-empty string"}
		}
		out.Voice = &id
	}

	return out, nil
}

// validateOpenAILiveClientOption is the strict client member of TS
// openaiRealtimeModelLiveOptionsSchema: {dataChannel: {allowedClientEvents?:
// 'all' | string[], allowedServerEvents?: 'all' | {type, responseEvent?}[]}},
// where responseEvent is required exactly for type "response.event".
func validateOpenAILiveClientOption(v interface{}) error {
	invalid := func(field, msg string) error {
		return &providererrors.InvalidArgumentError{Field: "providerOptions.openai.client" + field, Message: msg}
	}
	strictKeys := func(field string, obj map[string]interface{}, keys ...string) error {
		for k := range obj {
			known := false
			for _, want := range keys {
				known = known || k == want
			}
			if !known {
				return invalid(field+"."+k, "unrecognized key")
			}
		}
		return nil
	}
	client, ok := v.(map[string]interface{})
	if !ok {
		return invalid("", "must be an object")
	}
	if err := strictKeys("", client, "dataChannel"); err != nil {
		return err
	}
	dc, ok := client["dataChannel"].(map[string]interface{})
	if !ok {
		return invalid(".dataChannel", "must be an object")
	}
	if err := strictKeys(".dataChannel", dc, "allowedClientEvents", "allowedServerEvents"); err != nil {
		return err
	}
	if ev, present := dc["allowedClientEvents"]; present && ev != "all" {
		list, ok := ev.([]interface{})
		if !ok {
			return invalid(".dataChannel.allowedClientEvents", `must be "all" or an array of strings`)
		}
		for _, item := range list {
			if _, ok := item.(string); !ok {
				return invalid(".dataChannel.allowedClientEvents", `must be "all" or an array of strings`)
			}
		}
	}
	if ev, present := dc["allowedServerEvents"]; present && ev != "all" {
		list, ok := ev.([]interface{})
		if !ok {
			return invalid(".dataChannel.allowedServerEvents", `must be "all" or an array of selectors`)
		}
		for _, item := range list {
			sel, ok := item.(map[string]interface{})
			if !ok {
				return invalid(".dataChannel.allowedServerEvents", "each selector must be an object")
			}
			if err := strictKeys(".dataChannel.allowedServerEvents", sel, "type", "responseEvent"); err != nil {
				return err
			}
			t, ok := sel["type"].(string)
			if !ok {
				return invalid(".dataChannel.allowedServerEvents.type", "must be a string")
			}
			re, hasRE := sel["responseEvent"]
			if hasRE {
				if _, ok := re.(string); !ok {
					return invalid(".dataChannel.allowedServerEvents.responseEvent", "must be a string")
				}
			}
			if (t == "response.event") != hasRE {
				return invalid(".dataChannel.allowedServerEvents", "responseEvent is required for response.event and forbidden for other event types.")
			}
		}
	}
	return nil
}

func liveUnsupported(feature string) error {
	return &providererrors.UnsupportedFunctionalityError{Functionality: feature}
}

// liveAudioFormat validates a RealtimeAudioFormat against OpenAI Live's
// supported combinations: audio/pcm at 16000 or 24000 Hz, or audio/pcma /
// audio/pcmu at 8000 Hz. Mirrors the TypeScript SDK's audioFormatSchema.
func liveAudioFormat(f *provider.RealtimeAudioFormat) (map[string]interface{}, error) {
	if f == nil {
		return nil, nil
	}
	switch f.Type {
	case "audio/pcm":
		if f.Rate == nil || (*f.Rate != 16000 && *f.Rate != 24000) {
			return nil, &providererrors.InvalidArgumentError{Field: "audioFormat.rate", Message: "audio/pcm requires rate 16000 or 24000"}
		}
	case "audio/pcma", "audio/pcmu":
		if f.Rate == nil || *f.Rate != 8000 {
			return nil, &providererrors.InvalidArgumentError{Field: "audioFormat.rate", Message: fmt.Sprintf("%s requires rate 8000", f.Type)}
		}
	default:
		return nil, &providererrors.InvalidArgumentError{Field: "audioFormat.type", Message: "must be audio/pcm, audio/pcma, or audio/pcmu"}
	}
	out := map[string]interface{}{"type": f.Type, "rate": *f.Rate}
	return out, nil
}

// buildOpenAILiveSessionConfig converts a provider.RealtimeSessionConfig
// into the OpenAI Live "session" wire object, over the WebSocket transport
// only (WebRTC is TS-only). Mirrors the TypeScript SDK's
// buildOpenAILiveSessionConfig (transport = 'websocket').
func buildOpenAILiveSessionConfig(config provider.RealtimeSessionConfig, modelID string) (map[string]interface{}, error) {
	// TS rejects any present key (Object.keys(config)), an empty list too.
	if config.OutputModalities != nil {
		return nil, liveUnsupported("OpenAI Live session setting: outputModalities")
	}
	if config.InputAudioTranscription != nil {
		return nil, liveUnsupported("OpenAI Live session setting: inputAudioTranscription")
	}
	if config.OutputAudioTranscription != nil {
		return nil, liveUnsupported("OpenAI Live session setting: outputAudioTranscription")
	}
	if config.TurnDetection != nil {
		return nil, liveUnsupported("OpenAI Live session setting: turnDetection")
	}
	if config.Tools != nil {
		return nil, liveUnsupported("OpenAI Live session setting: tools")
	}

	options, err := parseOpenAILiveOptions(config.ProviderOptions)
	if err != nil {
		return nil, err
	}
	if options.DelegationResp {
		return nil, liveUnsupported("OpenAI Live Responses delegation; only client delegation is supported")
	}
	if options.HasClient {
		return nil, liveUnsupported("OpenAI Live client permissions outside WebRTC startup")
	}
	if options.Voice != nil && config.Voice != nil {
		return nil, providererrors.NewArgumentError("voice", "Choose either voice or providerOptions.openai.voice.", nil)
	}

	inputFormat, err := liveAudioFormat(config.InputAudioFormat)
	if err != nil {
		return nil, err
	}
	outputFormat, err := liveAudioFormat(config.OutputAudioFormat)
	if err != nil {
		return nil, err
	}
	if inputFormat != nil && outputFormat != nil &&
		(inputFormat["type"] != outputFormat["type"] || inputFormat["rate"] != outputFormat["rate"]) {
		return nil, providererrors.NewArgumentError("outputAudioFormat", "OpenAI Live requires the same input and output audio format.", nil)
	}

	session := map[string]interface{}{"model": modelID}
	if config.Instructions != nil {
		session["instructions"] = *config.Instructions
	}

	format := inputFormat
	if format == nil {
		format = outputFormat
	}
	if format == nil {
		format = map[string]interface{}{"type": "audio/pcm", "rate": 24000}
	}
	// providerOptions.openai.voice is a named-voice reference object
	// ({id: "..."}) on the wire; the top-level config.voice is a plain
	// preset-voice string. They are mutually exclusive (checked above).
	var voice interface{} = "marin"
	if options.Voice != nil {
		voice = map[string]interface{}{"id": *options.Voice}
	} else if config.Voice != nil {
		voice = *config.Voice
	}
	session["audio"] = map[string]interface{}{
		"format": format,
		"output": map[string]interface{}{"voice": voice},
	}

	if options.HasDelegation {
		if options.Delegation != nil {
			session["delegation"] = map[string]interface{}{"type": *options.Delegation}
		} else {
			session["delegation"] = nil
		}
	}
	if options.Input != nil {
		session["input"] = options.Input
	}
	if options.Store != nil {
		session["store"] = *options.Store
	}

	return session, nil
}

// serializeOpenAILiveClientEvent serializes a provider.RealtimeClientEvent
// into the OpenAI Live wire format. Mirrors the TypeScript SDK's
// serializeOpenAILiveClientEvent.
func serializeOpenAILiveClientEvent(event provider.RealtimeClientEvent, modelID string) (json.RawMessage, error) {
	eventID := map[string]interface{}{}
	if event.EventID != "" {
		eventID["event_id"] = event.EventID
	}

	var out map[string]interface{}
	switch event.Type {
	case "session-start":
		session, err := buildOpenAILiveSessionConfig(event.Config, modelID)
		if err != nil {
			return nil, err
		}
		out = map[string]interface{}{"type": "session.start", "session": session}
	case "session-update":
		return nil, liveUnsupported("OpenAI Live session-update; startup settings are immutable; use context-append or input-audio-mute/input-audio-unmute")
	case "session-close":
		out = map[string]interface{}{"type": "session.close"}
	case "input-audio-append":
		out = map[string]interface{}{"type": "session.input_audio.append", "audio": event.Audio}
	case "input-audio-mute":
		out = map[string]interface{}{"type": "session.input_audio.mute"}
	case "input-audio-unmute":
		out = map[string]interface{}{"type": "session.input_audio.unmute"}
	case "context-append":
		// TS: z.strictObject({ channel: z.enum([instructions, thinking,
		// commentary]).optional() }).parse(providerOptions.openai ?? {}) and
		// delegationId: z.string().min(1).nullable().
		channel := "thinking"
		if raw, present := event.ProviderOptions["openai"]; present && raw != nil {
			openaiOpts, ok := providerutils.NormalizeProviderOptionsObject(raw)
			if !ok {
				return nil, &providererrors.InvalidArgumentError{Field: "providerOptions.openai", Message: "must be an object"}
			}
			for key, value := range openaiOpts {
				if key != "channel" {
					return nil, &providererrors.InvalidArgumentError{Field: "providerOptions.openai." + key, Message: "unrecognized key"}
				}
				if value == nil {
					continue
				}
				c, _ := value.(string)
				if c != "instructions" && c != "thinking" && c != "commentary" {
					return nil, &providererrors.InvalidArgumentError{Field: "providerOptions.openai.channel", Message: `must be "instructions", "thinking" or "commentary"`}
				}
				channel = c
			}
		}
		var delegationID interface{}
		if event.DelegationID != nil {
			if *event.DelegationID == "" {
				return nil, &providererrors.InvalidArgumentError{Field: "delegationId", Message: "must be a non-empty string or null"}
			}
			delegationID = *event.DelegationID
		}
		out = map[string]interface{}{
			"type":          "session." + channel + ".append",
			"content":       event.Content,
			"delegation_id": delegationID,
		}
	default:
		return nil, liveUnsupported(fmt.Sprintf("OpenAI Live command: %s; use continuous audio and context-append instead of voice-turn commands", event.Type))
	}

	for k, v := range eventID {
		out[k] = v
	}
	return json.Marshal(out)
}

var openAILiveKnownServerTypes = map[string]bool{
	"session.started": true, "session.closed": true, "session.usage.updated": true,
	"session.output_audio.delta": true, "session.input_transcript.delta": true,
	"session.output_transcript.delta": true, "session.delegation.created": true,
	"session.updated": true, "session.input_audio.muted": true, "session.input_audio.unmuted": true,
	"session.instructions.appended": true, "session.thinking.appended": true,
	"session.commentary.appended": true, "error": true,
}

// parseOpenAILiveServerEvent parses one OpenAI Live server event. Mirrors
// the TypeScript SDK's parseOpenAILiveServerEvent (envelope pre-check for
// unknown types, then strict per-type field validation).
func parseOpenAILiveServerEvent(raw json.RawMessage) []provider.RealtimeServerEvent {
	var envelope map[string]interface{}
	hasEnvelope := json.Unmarshal(raw, &envelope) == nil && envelope != nil
	rawType, hasType := "", false
	if hasEnvelope {
		rawType, hasType = envelope["type"].(string)
	}
	if hasType && !openAILiveKnownServerTypes[rawType] {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: rawType, Raw: raw}}
	}
	if !hasType {
		return invalidLiveServerEvent(raw)
	}

	e := provider.RealtimeServerEvent{Raw: raw}
	switch rawType {
	case "session.started":
		id, ok := reqLiveString(envelope, "session", "id", 1)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "session-started"
		e.SessionID = id
		e.DelegationMode = "client"
		if session, ok := envelope["session"].(map[string]interface{}); ok {
			if delegationRaw, present := session["delegation"]; present && delegationRaw != nil {
				delegation, ok := delegationRaw.(map[string]interface{})
				if !ok {
					return invalidLiveServerEvent(raw)
				}
				t, ok := delegation["type"].(string)
				if !ok || (t != "client" && t != "responses") {
					return invalidLiveServerEvent(raw)
				}
				if t == "responses" {
					e.DelegationMode = "provider"
				}
			}
		}
	case "session.closed":
		usage, ok := reqLiveUsage(envelope)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		reason, ok := reqLiveTopString(envelope, "reason", 0)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		if sessionRaw, present := envelope["session"]; present && sessionRaw != nil {
			id, ok := reqLiveString(envelope, "session", "id", 1)
			if !ok {
				return invalidLiveServerEvent(raw)
			}
			e.SessionID = id
		}
		e.Type = "session-closed"
		e.Usage = usage
		e.Reason = reason
	case "session.usage.updated":
		usage, ok := reqLiveUsage(envelope)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "session-usage"
		e.Usage = usage
		if cwRaw, present := envelope["context_window"]; present && cwRaw != nil {
			cw, ok := cwRaw.(map[string]interface{})
			if !ok {
				return invalidLiveServerEvent(raw)
			}
			ratio, ok := cw["usage_ratio"].(float64)
			if !ok || ratio < 0 || ratio > 1 {
				return invalidLiveServerEvent(raw)
			}
			e.ContextWindowUsageRatio = &ratio
		}
	case "session.output_audio.delta":
		delta, ok := reqLiveTopString(envelope, "delta", 0)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "audio-chunk"
		e.Delta = delta
	case "session.input_transcript.delta", "session.output_transcript.delta":
		delta, ok := reqLiveTopString(envelope, "delta", 0)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		startMs, endMs, ok, intervalOK := reqLiveInterval(envelope)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		if !intervalOK {
			return invalidLiveEventTimeInterval(raw)
		}
		e.Type = "transcript-fragment"
		if rawType == "session.input_transcript.delta" {
			e.Speaker = "user"
		} else {
			e.Speaker = "assistant"
		}
		e.Delta = delta
		start, end := int(startMs), int(endMs)
		e.StartMs, e.EndMs = &start, &end
	case "session.delegation.created":
		delegation, ok := envelope["delegation"].(map[string]interface{})
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		id, ok := delegation["id"].(string)
		if !ok || id == "" {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "delegation-created"
		e.DelegationID = id
		if targetRaw, present := delegation["target"]; present && targetRaw != nil {
			target, ok := targetRaw.(string)
			if !ok || (target != "client" && target != "responses") {
				return invalidLiveServerEvent(raw)
			}
			if target == "responses" {
				e.Target = "provider"
			} else {
				e.Target = target
			}
		}
		if responseIDRaw, present := delegation["response_id"]; present && responseIDRaw != nil {
			responseID, ok := responseIDRaw.(string)
			if !ok || responseID == "" {
				return invalidLiveServerEvent(raw)
			}
			e.ResponseID = responseID
		}
		if offsetRaw, present := envelope["offset_ms"]; present && offsetRaw != nil {
			offset, ok := offsetRaw.(float64)
			if !ok || offset < 0 {
				return invalidLiveServerEvent(raw)
			}
			off := int(offset)
			e.OffsetMs = &off
		}
	case "error":
		errObj, ok := envelope["error"].(map[string]interface{})
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		message, ok := errObj["message"].(string)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		code, ok := reqLiveOptionalString(errObj, "code")
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		clientEventID, ok := reqLiveOptionalString(errObj, "client_event_id")
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "error"
		e.Message = message
		e.Code = code
		e.ClientEventID = clientEventID
	case "session.updated":
		if _, ok := reqLiveString(envelope, "session", "id", 1); !ok {
			return invalidLiveServerEvent(raw)
		}
		clientEventID, ok := reqLiveTopOptionalString(envelope, "client_event_id")
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "command-acknowledged"
		e.Command = "session.update"
		e.ClientEventID = clientEventID
	case "session.input_audio.muted", "session.input_audio.unmuted":
		clientEventID, ok := reqLiveTopOptionalString(envelope, "client_event_id")
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		e.Type = "command-acknowledged"
		e.Command = rawType[:len(rawType)-1]
		e.ClientEventID = clientEventID
	case "session.instructions.appended", "session.thinking.appended", "session.commentary.appended":
		_, _, ok, intervalOK := reqLiveInterval(envelope)
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		clientEventID, ok := reqLiveTopOptionalString(envelope, "client_event_id")
		if !ok {
			return invalidLiveServerEvent(raw)
		}
		if !intervalOK {
			return invalidLiveEventTimeInterval(raw)
		}
		e.Type = "command-acknowledged"
		e.Command = rawType[:len(rawType)-2]
		e.ClientEventID = clientEventID
	}
	return []provider.RealtimeServerEvent{e}
}

func invalidLiveServerEvent(raw json.RawMessage) []provider.RealtimeServerEvent {
	return []provider.RealtimeServerEvent{{Type: "error", Code: "invalid_server_event", Message: "Invalid OpenAI Live server event.", Raw: raw}}
}

// invalidLiveEventTimeInterval mirrors the TypeScript SDK's generic
// `'start_ms' in event && event.end_ms < event.start_ms` check in
// parseServerEvent, which runs after per-type schema validation succeeds and
// applies to every event carrying start_ms/end_ms (transcript deltas and the
// three "*.appended" command acknowledgments).
func invalidLiveEventTimeInterval(raw json.RawMessage) []provider.RealtimeServerEvent {
	return []provider.RealtimeServerEvent{{Type: "error", Code: "invalid_server_event", Message: "Invalid OpenAI Live event time interval.", Raw: raw}}
}

// reqLiveInterval requires start_ms and end_ms as top-level nonnegative
// numbers. ok is false when either field is missing/malformed (schema
// failure -> invalidLiveServerEvent); when ok is true, intervalOK reports
// whether end_ms >= start_ms (false -> invalidLiveEventTimeInterval).
func reqLiveInterval(envelope map[string]interface{}) (startMs, endMs float64, ok, intervalOK bool) {
	startMs, ok = reqLiveFloat(envelope, "start_ms")
	if !ok {
		return 0, 0, false, false
	}
	endMs, ok = reqLiveFloat(envelope, "end_ms")
	if !ok {
		return 0, 0, false, false
	}
	return startMs, endMs, true, endMs >= startMs
}

func reqLiveUsage(envelope map[string]interface{}) (*provider.RealtimeUsage, bool) {
	usageMap, ok := envelope["usage"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	seconds, ok := usageMap["seconds"].(float64)
	if !ok || seconds < 0 {
		return nil, false
	}
	return &provider.RealtimeUsage{Seconds: seconds}, true
}

// reqLiveString requires envelope[key] to be an object with a string field
// named nested of at least minLen characters.
func reqLiveString(envelope map[string]interface{}, key, nested string, minLen int) (string, bool) {
	child, ok := envelope[key].(map[string]interface{})
	if !ok {
		return "", false
	}
	v, ok := child[nested].(string)
	if !ok || len(v) < minLen {
		return "", false
	}
	return v, true
}

func reqLiveTopString(envelope map[string]interface{}, key string, minLen int) (string, bool) {
	v, ok := envelope[key].(string)
	if !ok || len(v) < minLen {
		return "", false
	}
	return v, true
}

func reqLiveFloat(envelope map[string]interface{}, key string) (float64, bool) {
	v, ok := envelope[key].(float64)
	if !ok || v < 0 {
		return 0, false
	}
	return v, true
}

// reqLiveTopOptionalString allows key to be absent or a string; any other
// present value (including a non-string) is invalid.
func reqLiveTopOptionalString(envelope map[string]interface{}, key string) (string, bool) {
	v, present := envelope[key]
	if !present || v == nil {
		return "", true
	}
	s, ok := v.(string)
	return s, ok
}

// reqLiveOptionalString allows key to be absent, null, or a string.
func reqLiveOptionalString(m map[string]interface{}, key string) (string, bool) {
	v, present := m[key]
	if !present || v == nil {
		return "", true
	}
	s, ok := v.(string)
	return s, ok
}
