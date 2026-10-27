package elevenlabs

import (
	"encoding/json"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// singleContextProtocol is the text-to-speech "stream-input" protocol: one
// implicit context and one voice; {"text":""} ends the input.
type singleContextProtocol struct {
	init  map[string]interface{}
	voice string
}

func (p *singleContextProtocol) initMessage() map[string]interface{} { return p.init }

func (p *singleContextProtocol) authFields(apiKey string) map[string]interface{} {
	return map[string]interface{}{"xi-api-key": apiKey}
}

func (p *singleContextProtocol) keepaliveMessage() map[string]interface{} {
	return map[string]interface{}{"text": " "}
}

func (p *singleContextProtocol) resolveVoice(partVoice string) (string, error) {
	if partVoice != "" && partVoice != p.voice {
		return "", &providererrors.InvalidArgumentError{
			Field: "text",
			Message: fmt.Sprintf(`The ElevenLabs text-to-speech WebSocket speaks a single voice per stream ("%s"); a text part requested "%s". Use an eleven_v3 or eleven_v4 model with dialogue options for several voices.`,
				p.voice, partVoice),
		}
	}
	return p.voice, nil
}

func (p *singleContextProtocol) encodeText(segment textSegment) encodedText {
	return encodedText{
		messages:   []map[string]interface{}{{"text": segment.text}},
		characters: countCodePoints(segment.text),
	}
}

func (p *singleContextProtocol) endMessages() encodedText {
	return encodedText{messages: []map[string]interface{}{{"text": ""}}}
}

func (p *singleContextProtocol) decodeEvent(raw map[string]interface{}) streamEvent {
	if text, ok := errorText(raw); ok {
		if _, hasAudio := raw["audio"].(string); !hasAudio {
			return streamEvent{err: text, hasErr: true}
		}
	}
	event := streamEvent{}
	if audio, ok := raw["audio"].(string); ok && len(audio) > 0 {
		event.audio = audio
	}
	if raw["isFinal"] == true || raw["is_final"] == true {
		event.isFinal = true
	}
	metadata := map[string]interface{}{}
	if v := raw["alignment"]; v != nil {
		metadata["alignment"] = v
	}
	if v := raw["normalizedAlignment"]; v != nil {
		metadata["normalizedAlignment"] = v
	}
	if len(metadata) > 0 {
		event.providerMetadata = metadata
	}
	return event
}

// errorText extracts the error message of a server message (TS getErrorText).
func errorText(raw map[string]interface{}) (string, bool) {
	message, hasMessage := raw["message"].(string)
	if raw["error"] == nil && (!hasMessage || raw["isFinal"] == true) {
		return "", false
	}
	if hasMessage {
		return message, true
	}
	if s, ok := raw["error"].(string); ok {
		return s, true
	}
	data, err := json.Marshal(raw["error"])
	if err != nil {
		return fmt.Sprint(raw["error"]), true
	}
	return string(data), true
}

// dialogueProtocol is the Text-to-Dialogue "stream-input" protocol. Each text
// part's voice (default defaultVoice) must be one of voices; a voice change,
// or a part with newTurn, starts a new turn. With incremental false (the v3
// family) all inputs go in one final message.
type dialogueProtocol struct {
	init         map[string]interface{}
	voices       []string
	defaultVoice string
	incremental  bool

	buffered     []map[string]interface{}
	currentVoice string
	hasCurrent   bool
}

func (p *dialogueProtocol) initMessage() map[string]interface{} {
	out := make(map[string]interface{}, len(p.init)+1)
	for k, v := range p.init {
		out[k] = v
	}
	out["voices"] = p.voices
	return out
}

func (p *dialogueProtocol) authFields(apiKey string) map[string]interface{} {
	return map[string]interface{}{"xi_api_key": apiKey}
}

func (p *dialogueProtocol) keepaliveMessage() map[string]interface{} {
	return map[string]interface{}{"keep_alive": true}
}

func (p *dialogueProtocol) resolveVoice(partVoice string) (string, error) {
	voice := partVoice
	if voice == "" {
		voice = p.defaultVoice
	}
	for _, v := range p.voices {
		if v == voice {
			return voice, nil
		}
	}
	return "", &providererrors.InvalidArgumentError{
		Field:   "text",
		Message: fmt.Sprintf(`Dialogue text part voice "%s" is not listed in providerOptions.elevenlabs.dialogue.voices.`, voice),
	}
}

func (p *dialogueProtocol) encodeText(segment textSegment) encodedText {
	newTurn := p.hasCurrent && (segment.voice != p.currentVoice || segment.newTurn)
	p.currentVoice, p.hasCurrent = segment.voice, true
	input := map[string]interface{}{"text": segment.text, "voice_id": segment.voice}
	if newTurn {
		input["new_turn"] = true
	}
	characters := countCodePoints(segment.text)
	if !p.incremental {
		p.buffered = append(p.buffered, input)
		return encodedText{characters: characters}
	}
	return encodedText{messages: []map[string]interface{}{{"inputs": []map[string]interface{}{input}}}, characters: characters}
}

func (p *dialogueProtocol) endMessages() encodedText {
	if p.incremental {
		return encodedText{messages: []map[string]interface{}{{"close_socket": true}}}
	}
	inputs := p.buffered
	if inputs == nil {
		inputs = []map[string]interface{}{}
	}
	return encodedText{messages: []map[string]interface{}{{"inputs": inputs, "close_socket": true}}}
}

func (p *dialogueProtocol) decodeEvent(raw map[string]interface{}) streamEvent {
	if text, ok := errorText(raw); ok {
		return streamEvent{err: text, hasErr: true}
	}
	event := streamEvent{}
	if audio, ok := raw["audio"].(string); ok && len(audio) > 0 {
		event.audio = audio
	}
	if raw["is_final"] == true {
		event.isFinal = true
	}
	alignment := camelizeAlignment(raw["alignment"])
	finalForTurn := raw["is_final_audio_for_turn"] == true
	if alignment != nil || finalForTurn {
		metadata := map[string]interface{}{}
		if alignment != nil {
			metadata["alignment"] = alignment
		}
		if finalForTurn {
			metadata["isFinalAudioForTurn"] = true
		}
		event.providerMetadata = metadata
	}
	return event
}

// camelizeAlignment exposes the snake_case dialogue alignment in the
// text-to-speech (camelCase) shape.
func camelizeAlignment(value interface{}) map[string]interface{} {
	alignment, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	out := map[string]interface{}{}
	if v := alignment["chars"]; v != nil {
		out["chars"] = v
	}
	if v := alignment["char_start_times_ms"]; v != nil {
		out["charStartTimesMs"] = v
	}
	if v := alignment["char_durations_ms"]; v != nil {
		out["charDurationsMs"] = v
	}
	return out
}
