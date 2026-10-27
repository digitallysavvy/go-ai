package elevenlabs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const (
	defaultSpeechVoiceID      = "21m00Tcm4TlvDq8ikWAM"
	defaultSpeechOutputFormat = "mp3_44100_128"
)

// isDialogueModelID reports whether the model is served by Text-to-Dialogue
// rather than the text-to-speech WebSocket (the v3 and v4 families).
func isDialogueModelID(modelID string) bool {
	return strings.HasPrefix(modelID, "eleven_v3") || strings.HasPrefix(modelID, "eleven_v4")
}

// DoStreamSpeech streams speech for streamed text over an ElevenLabs
// WebSocket: text-to-speech stream-input, or Text-to-Dialogue stream-input
// when providerOptions.elevenlabs.dialogue is set (eleven_v3 and eleven_v4
// models). Mirrors TS ElevenLabsSpeechModel#doStream.
func (m *SpeechModel) DoStreamSpeech(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
	if opts == nil || opts.Text == nil {
		return nil, &providererrors.InvalidArgumentError{Field: "text", Message: "a text stream is required"}
	}
	result, err := m.startStream(ctx, opts)
	if err != nil {
		// The provider owns the text stream once DoStreamSpeech is called.
		opts.Text.Cancel(err)
		return nil, err
	}
	return result, nil
}

func (m *SpeechModel) startStream(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
	if isMusicModelID(m.modelID) || isSoundEffectsModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming speech with " + m.modelID,
			Message:       fmt.Sprintf("ElevenLabs %s does not support streaming; use generateSpeech instead.", m.modelID),
		}
	}
	currentDate := time.Now()
	if err := validateSpeechProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	elOpts, _ := extractSpeechModelOptions(opts.ProviderOptions)
	dialogue := elOpts.Dialogue
	streaming := elOpts.Streaming

	if dialogue != nil && !isDialogueModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming dialogue with " + m.modelID,
			Message:       fmt.Sprintf("ElevenLabs streaming dialogue requires an eleven_v3 or eleven_v4 model; %s is not one.", m.modelID),
		}
	}
	if dialogue == nil && isDialogueModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming speech with " + m.modelID,
			Message:       fmt.Sprintf("The ElevenLabs text-to-speech WebSocket does not serve %s. Set providerOptions.elevenlabs.dialogue to stream it as dialogue.", m.modelID),
		}
	}
	if dialogue != nil && (len(dialogue.Voices) == 0 || len(dialogue.Voices) > 10) {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions.elevenlabs.dialogue.voices",
			Message: fmt.Sprintf("dialogue.voices must list 1 to 10 voices; received %d.", len(dialogue.Voices)),
		}
	}
	if dialogue != nil && m.modelID == SpeechModelV3Conversational && len(dialogue.Voices) != 1 {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions.elevenlabs.dialogue.voices",
			Message: fmt.Sprintf("eleven_v3_conversational supports exactly one dialogue voice; received %d.", len(dialogue.Voices)),
		}
	}
	// The WebSocket API rejects locators without a version, unlike HTTP.
	for _, locator := range elOpts.PronunciationDictionaryLocators {
		if locator.VersionID == "" {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions.elevenlabs.pronunciationDictionaryLocators",
				Message: fmt.Sprintf(`Streaming speech requires a versionId for pronunciation dictionary "%s".`, locator.PronunciationDictionaryID),
			}
		}
	}

	warnings := streamSpeechWarnings(opts, elOpts)

	outputFormat := ""
	if streaming != nil {
		outputFormat = streaming.OutputFormat
	}
	if outputFormat == "" {
		var err error
		if outputFormat, err = toElevenLabsStreamOutputFormat(opts.OutputFormat); err != nil {
			return nil, err
		}
	}

	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = defaultSpeechVoiceID
	}
	path := "/v1/text-to-speech/" + providerutils.EncodeURIComponent(voiceID) + "/stream-input"
	if dialogue != nil {
		path = "/v1/text-to-dialogue/stream-input"
	}
	wsURL, err := toWebSocketURL(m.provider.config.BaseURL, path)
	if err != nil {
		return nil, err
	}

	language := opts.Language
	if language == "" {
		language = elOpts.LanguageCode
	}
	var q queryBuilder
	q.set("model_id", m.modelID)
	q.set("output_format", outputFormat)
	if language != "" {
		q.set("language_code", language)
	}
	if elOpts.EnableLogging != nil {
		q.set("enable_logging", strconv.FormatBool(*elOpts.EnableLogging))
	}
	// Text-to-Dialogue has a fixed inactivity timeout and no auto/SSML mode.
	if dialogue == nil && streaming != nil && streaming.InactivityTimeout != nil {
		q.set("inactivity_timeout", strconv.Itoa(*streaming.InactivityTimeout))
	}
	if streaming != nil && streaming.SyncAlignment != nil {
		q.set("sync_alignment", strconv.FormatBool(*streaming.SyncAlignment))
	}
	if dialogue == nil && streaming != nil && streaming.AutoMode != nil {
		q.set("auto_mode", strconv.FormatBool(*streaming.AutoMode))
	}
	if elOpts.ApplyTextNormalization != "" {
		q.set("apply_text_normalization", elOpts.ApplyTextNormalization)
	}
	if elOpts.Seed != nil {
		q.set("seed", formatFloatParam(*elOpts.Seed))
	}
	if dialogue == nil && streaming != nil && streaming.EnableSSMLParsing != nil {
		q.set("enable_ssml_parsing", strconv.FormatBool(*streaming.EnableSSMLParsing))
	}
	wsURL.RawQuery = q.encode()

	var protocol streamProtocol
	if dialogue != nil {
		if opts.Voice != "" && !containsString(dialogue.Voices, opts.Voice) {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "voice",
				Message: fmt.Sprintf(`ElevenLabs streaming dialogue voice "%s" is not listed in providerOptions.elevenlabs.dialogue.voices.`, opts.Voice),
			}
		}
		defaultVoice := opts.Voice
		if defaultVoice == "" {
			defaultVoice = dialogue.Voices[0]
		}
		protocol = &dialogueProtocol{
			init:         dialogueInitMessage(dialogue, elOpts),
			voices:       dialogue.Voices,
			defaultVoice: defaultVoice,
			// The v3 family accepts a single message per session.
			incremental: strings.HasPrefix(m.modelID, "eleven_v4"),
		}
	} else {
		protocol = &singleContextProtocol{init: streamInitMessage(opts, elOpts), voice: voiceID}
	}

	headers, apiKey, err := m.streamHeaders(opts.Headers)
	if err != nil {
		return nil, err
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}
	stream := newElevenLabsSpeechStream(abortCtx, speechStreamConfig{
		url:         wsURL,
		headers:     headers,
		apiKey:      apiKey,
		audioFormat: getElevenLabsAudioFormat(outputFormat),
		// Text-to-Dialogue has a fixed 20 s inactivity timeout, so it always
		// keeps alive; text-to-speech honors an explicit inactivityTimeout
		// instead.
		keepalive:         dialogue != nil || streaming == nil || streaming.InactivityTimeout == nil,
		keepaliveInterval: speechKeepaliveInterval,
		includeRawChunks:  opts.IncludeRawChunks,
		protocol:          protocol,
		text:              opts.Text,
		warnings:          warnings,
	})

	// The API key is kept out of the reported request body.
	return &provider.SpeechStreamResult{
		Stream:      stream,
		RequestBody: map[string]interface{}{"url": wsURL.String(), "initMessage": protocol.initMessage()},
		Response:    &provider.SpeechStreamResponseMetadata{Timestamp: currentDate, ModelID: m.modelID},
	}, nil
}

// streamHeaders builds the WebSocket handshake headers like the HTTP client's
// (provider headers, user agent), with per-call headers winning. The API key
// is split out: it travels in the init message so native WebSockets work.
func (m *SpeechModel) streamHeaders(perCall map[string]string) (headers map[string]string, apiKey string, err error) {
	cfg := m.provider.config
	authHeaders, err := providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "ELEVENLABS_API_KEY", "ElevenLabs", "xi-api-key", "")
	if err != nil {
		return nil, "", err
	}
	merged := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"xi-api-key": cfg.APIKey,
	}, authHeaders, cfg.Headers), version.ProviderUserAgent("elevenlabs"))
	// Per-call headers win over the provider headers (header names compare
	// case-insensitively).
	for k, v := range perCall {
		for existing := range merged {
			if strings.EqualFold(existing, k) {
				delete(merged, existing)
			}
		}
		merged[k] = v
	}
	headers = make(map[string]string, len(merged))
	for k, v := range merged {
		if strings.EqualFold(k, "xi-api-key") {
			apiKey = v
			continue
		}
		headers[k] = v
	}
	return headers, apiKey, nil
}

// toWebSocketURL upgrades the configured HTTP(S) base URL plus path to a
// WS(S) URL. The host always comes from the configured base URL.
func toWebSocketURL(baseURL, path string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	return u, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func locatorMessages(locators []PronunciationDictionaryLocator) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(locators))
	for _, locator := range locators {
		out = append(out, map[string]interface{}{
			"pronunciation_dictionary_id": locator.PronunciationDictionaryID,
			"version_id":                  locator.VersionID,
		})
	}
	return out
}

func dialogueInitMessage(dialogue *DialogueOptions, elOpts SpeechModelOptions) map[string]interface{} {
	msg := map[string]interface{}{}
	if dialogue.VoiceSettings != nil && dialogue.VoiceSettings.Stability != nil {
		msg["voice_settings"] = map[string]interface{}{"stability": *dialogue.VoiceSettings.Stability}
	}
	if elOpts.PronunciationDictionaryLocators != nil {
		msg["pronunciation_dictionary_locators"] = locatorMessages(elOpts.PronunciationDictionaryLocators)
	}
	return msg
}

func streamInitMessage(opts *provider.SpeechStreamOptions, elOpts SpeechModelOptions) map[string]interface{} {
	voiceSettings := map[string]interface{}{}
	if vs := elOpts.VoiceSettings; vs != nil {
		if vs.Stability != nil {
			voiceSettings["stability"] = *vs.Stability
		}
		if vs.SimilarityBoost != nil {
			voiceSettings["similarity_boost"] = *vs.SimilarityBoost
		}
		if vs.Style != nil {
			voiceSettings["style"] = *vs.Style
		}
		if vs.UseSpeakerBoost != nil {
			voiceSettings["use_speaker_boost"] = *vs.UseSpeakerBoost
		}
	}
	if opts.Speed != nil {
		voiceSettings["speed"] = *opts.Speed
	}
	// The protocol requires a single space as the first message text.
	msg := map[string]interface{}{"text": " "}
	if len(voiceSettings) > 0 {
		msg["voice_settings"] = voiceSettings
	}
	if elOpts.Streaming != nil && elOpts.Streaming.ChunkLengthSchedule != nil {
		msg["generation_config"] = map[string]interface{}{"chunk_length_schedule": elOpts.Streaming.ChunkLengthSchedule}
	}
	if elOpts.PronunciationDictionaryLocators != nil {
		msg["pronunciation_dictionary_locators"] = locatorMessages(elOpts.PronunciationDictionaryLocators)
	}
	return msg
}

// streamSpeechWarnings are the stream-start warnings for a streaming call.
func streamSpeechWarnings(opts *provider.SpeechStreamOptions, elOpts SpeechModelOptions) []types.Warning {
	warnings := []types.Warning{}
	unsupported := func(feature, details string) {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature, Details: details})
	}
	if opts.Instructions != "" {
		unsupported("instructions", "ElevenLabs speech models do not support instructions. Instructions parameter was ignored.")
	}
	// HTTP-only options: neither WebSocket protocol carries them.
	for _, option := range streamHTTPOnlyOptions(elOpts) {
		unsupported("providerOptions.elevenlabs."+option, fmt.Sprintf("ElevenLabs streaming speech does not support %s.", option))
	}
	if elOpts.Dialogue != nil {
		// Text-to-Dialogue takes only dialogue.voiceSettings.stability, and
		// has a fixed inactivity timeout and no chunking, auto, or SSML
		// modes.
		var ignored []string
		if opts.Speed != nil {
			ignored = append(ignored, "speed")
		}
		if elOpts.VoiceSettings != nil {
			ignored = append(ignored, "providerOptions.elevenlabs.voiceSettings")
		}
		if s := elOpts.Streaming; s != nil {
			if s.AutoMode != nil {
				ignored = append(ignored, "providerOptions.elevenlabs.streaming.autoMode")
			}
			if s.ChunkLengthSchedule != nil {
				ignored = append(ignored, "providerOptions.elevenlabs.streaming.chunkLengthSchedule")
			}
			if s.EnableSSMLParsing != nil {
				ignored = append(ignored, "providerOptions.elevenlabs.streaming.enableSsmlParsing")
			}
			if s.InactivityTimeout != nil {
				ignored = append(ignored, "providerOptions.elevenlabs.streaming.inactivityTimeout")
			}
		}
		for _, feature := range ignored {
			unsupported(feature, fmt.Sprintf("ElevenLabs streaming dialogue does not support %s; it was ignored.", feature))
		}
	}
	return warnings
}

// streamHTTPOnlyOptions lists the set options that only the HTTP endpoints
// read, in TS order.
func streamHTTPOnlyOptions(elOpts SpeechModelOptions) []string {
	var set []string
	if elOpts.ApplyLanguageTextNormalization != nil {
		set = append(set, "applyLanguageTextNormalization")
	}
	if elOpts.Inputs != nil {
		set = append(set, "inputs")
	}
	if elOpts.NextRequestIDs != nil {
		set = append(set, "nextRequestIds")
	}
	if elOpts.NextText != "" {
		set = append(set, "nextText")
	}
	if elOpts.PreviousRequestIDs != nil {
		set = append(set, "previousRequestIds")
	}
	if elOpts.PreviousText != "" {
		set = append(set, "previousText")
	}
	for _, option := range append(append([]string{}, musicOnlyOptions...), soundEffectsOnlyOptions...) {
		if elOpts.isSet(option) {
			set = append(set, option)
		}
	}
	return set
}

// toElevenLabsStreamOutputFormat maps a portable output format to an
// ElevenLabs output_format. Exact native formats (for example other MP3
// bitrates) use providerOptions.elevenlabs.streaming.outputFormat.
func toElevenLabsStreamOutputFormat(format *provider.AudioFormat) (string, error) {
	if format == nil {
		return defaultSpeechOutputFormat, nil
	}
	// MIME types are case-insensitive and may carry parameters.
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(format.Type, ";", 2)[0]))
	rate := format.Rate
	switch mediaType {
	case "audio/mpeg", "audio/mp3":
		if rate == nil || *rate == 44100 {
			return "mp3_44100_128", nil
		}
		if *rate == 22050 {
			return "mp3_22050_32", nil
		}
	case "audio/pcm", "audio/l16":
		pcmRate := 24000
		if rate != nil {
			pcmRate = *rate
		}
		switch pcmRate {
		case 8000, 16000, 22050, 24000, 44100, 48000:
			return "pcm_" + strconv.Itoa(pcmRate), nil
		}
	case "audio/pcmu", "audio/basic":
		if rate == nil || *rate == 8000 {
			return "ulaw_8000", nil
		}
	case "audio/pcma":
		if rate == nil || *rate == 8000 {
			return "alaw_8000", nil
		}
	case "audio/opus", "audio/ogg":
		if rate == nil || *rate == 48000 {
			return "opus_48000_64", nil
		}
	}
	desc := fmt.Sprintf(`{"type":%q`, format.Type)
	if rate != nil {
		desc += fmt.Sprintf(`,"rate":%d`, *rate)
	}
	desc += "}"
	return "", &providererrors.InvalidArgumentError{
		Field:   "outputFormat",
		Message: fmt.Sprintf("ElevenLabs streaming speech does not support the output format %s. Use providerOptions.elevenlabs.streaming.outputFormat for a native ElevenLabs format.", desc),
	}
}

var _ provider.SpeechStreamer = (*SpeechModel)(nil)
