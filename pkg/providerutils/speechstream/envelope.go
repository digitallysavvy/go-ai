// Package speechstream implements the experimental speech-stream WebSocket
// envelope (v1): the standard serialization of provider.SpeechStreamer over a
// WebSocket (TS provider-utils speech-stream-envelope.ts). Clients such as
// the gateway provider encode with this package and servers decode with it,
// so the two sides cannot drift.
//
// Envelope rules:
//
//  1. The client sends exactly one "speech-stream.start" TEXT frame first.
//  2. Input text rides "speech-stream.text-delta" TEXT frames, in order. A
//     frame may carry its text part's voice and providerOptions. A long part
//     may span several frames: voice is repeated on each and providerOptions
//     appear on the first only, so they must describe an event at the start
//     of the part (for example a turn break), not a setting. An empty delta
//     is valid and carries only part metadata; it does not end the input.
//  3. The client signals the end of the input text with the
//     "speech-stream.text-done" TEXT frame; a plain close without it is an
//     abort.
//  4. Every server to client TEXT frame is one JSON-serialized speech stream
//     part (flattened, no wrapper). Audio payloads are base64 strings.
//     Timestamps are ISO 8601 strings. Error payloads serialize as
//     {name, message}; servers may instead send {type, message}, where type
//     names an error class the client can map to a typed error.
//  5. The server closes with code 1000 after the finish part. Over this
//     envelope an error part is terminal: the server sends it on failure and
//     then closes non-1000. A close without a prior finish is an error.
//  6. Unknown frame and part types are ignored in both directions.
//  7. Servers may enforce a maximum frame size (the AI Gateway rejects
//     frames over 256 KiB); clients should keep text-delta frames under
//     64 KiB.
//  8. Connection establishment (URL, auth) is transport-specific and out of
//     scope.
//
// The envelope validates frame shape only; server policy layers on top.
package speechstream

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsdate"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// Frame types.
const (
	// StartFrameType is the type of the first client TEXT frame.
	StartFrameType = "speech-stream.start"
	// TextDeltaFrameType carries one input text delta.
	TextDeltaFrameType = "speech-stream.text-delta"
	// TextDoneFrameType signals the end of the input text.
	TextDoneFrameType = "speech-stream.text-done"
)

// AudioFormat is the wire shape of an audio format.
type AudioFormat struct {
	Type string   `json:"type"`
	Rate *float64 `json:"rate,omitempty"`
}

// StartFrame is the client's session start frame. Optional keys are omitted
// when unset.
type StartFrame struct {
	Type             string                 `json:"type"`
	Voice            string                 `json:"voice,omitempty"`
	OutputFormat     *AudioFormat           `json:"outputFormat,omitempty"`
	Instructions     string                 `json:"instructions,omitempty"`
	Speed            *float64               `json:"speed,omitempty"`
	Language         string                 `json:"language,omitempty"`
	ProviderOptions  map[string]interface{} `json:"providerOptions,omitempty"`
	IncludeRawChunks *bool                  `json:"includeRawChunks,omitempty"`
}

// TextDeltaFrame is a client frame carrying one input text delta, that is one
// text part (or a slice of one).
type TextDeltaFrame struct {
	Type            string                 `json:"type"`
	Delta           string                 `json:"delta"`
	Voice           string                 `json:"voice,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// Kinds of a parsed client frame.
const (
	ClientFrameStart     = "start"
	ClientFrameTextDelta = "text-delta"
	ClientFrameTextDone  = "text-done"
	// ClientFrameInvalid is malformed JSON or a recognized frame with an
	// invalid shape; Message says why.
	ClientFrameInvalid = "invalid"
	// ClientFrameUnknown is an unrecognized frame type; ignore it for
	// forward compatibility.
	ClientFrameUnknown = "unknown"
)

// ClientFrame is the server-side classification of a client TEXT frame.
type ClientFrame struct {
	Kind            string
	Start           *StartFrame
	Delta           string
	Voice           string
	ProviderOptions map[string]interface{}
	Message         string
}

func invalid(message string) ClientFrame {
	return ClientFrame{Kind: ClientFrameInvalid, Message: message}
}

// decodeSecure parses JSON and rejects payloads carrying a "__proto__" key or
// a "constructor" object with a "prototype" key, like TS secureJsonParse.
// A number out of the double range reads as +-Infinity (0 when it
// underflows), as JSON.parse reads it, instead of failing the frame.
func decodeSecure(text string) (interface{}, error) {
	if !json.Valid([]byte(text)) {
		return nil, fmt.Errorf("invalid JSON")
	}
	var value interface{}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	value = jsNumbers(value)
	if hasForbiddenKey(value) {
		return nil, fmt.Errorf("Object contains forbidden prototype property") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return value, nil
}

// jsNumbers replaces each json.Number in a decoded tree with its JS double.
func jsNumbers(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		f, _ := strconv.ParseFloat(string(v), 64) // a range error gives +-Inf or 0
		return f
	case map[string]interface{}:
		for k, child := range v {
			v[k] = jsNumbers(child)
		}
	case []interface{}:
		for i, child := range v {
			v[i] = jsNumbers(child)
		}
	}
	return value
}

func hasForbiddenKey(value interface{}) bool {
	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v["__proto__"]; ok {
			return true
		}
		if c, ok := v["constructor"].(map[string]interface{}); ok {
			if _, has := c["prototype"]; has {
				return true
			}
		}
		for _, child := range v {
			if hasForbiddenKey(child) {
				return true
			}
		}
	case []interface{}:
		for _, child := range v {
			if hasForbiddenKey(child) {
				return true
			}
		}
	}
	return false
}

func isRecord(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

func isProviderOptions(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	for _, entry := range m {
		if !isRecord(entry) {
			return false
		}
	}
	return true
}

func isAudioFormat(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	if t, ok := m["type"].(string); !ok || t == "" {
		return false
	}
	if rate, present := m["rate"]; present {
		n, ok := rate.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
			return false
		}
	}
	return true
}

// ParseClientFrame classifies one client TEXT frame. It validates envelope
// shape only and never panics.
func ParseClientFrame(text string) ClientFrame {
	value, err := decodeSecure(text)
	if err != nil {
		return invalid("malformed JSON")
	}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return invalid("frame must be a JSON object")
	}
	frameType, ok := obj["type"].(string)
	if !ok {
		return invalid("frame type must be a string")
	}

	switch frameType {
	case StartFrameType:
		return parseStartFrame(obj)

	case TextDeltaFrameType:
		delta, ok := obj["delta"].(string)
		if !ok {
			return invalid("text-delta frame must have a string delta")
		}
		frame := ClientFrame{Kind: ClientFrameTextDelta, Delta: delta}
		if v, present := obj["voice"]; present {
			voice, ok := v.(string)
			if !ok {
				return invalid("text-delta voice must be a string when present")
			}
			frame.Voice = voice
		}
		if v, present := obj["providerOptions"]; present {
			if !isProviderOptions(v) {
				return invalid("text-delta providerOptions must be an object of provider objects when present")
			}
			frame.ProviderOptions = v.(map[string]interface{})
		}
		return frame

	case TextDoneFrameType:
		return ClientFrame{Kind: ClientFrameTextDone}

	default:
		return ClientFrame{Kind: ClientFrameUnknown}
	}
}

func parseStartFrame(obj map[string]interface{}) ClientFrame {
	checks := []struct{ field, kind string }{
		{"instructions", "string"}, {"language", "string"}, {"speed", "number"},
		{"voice", "string"}, {"includeRawChunks", "boolean"},
	}
	for _, c := range checks {
		v, present := obj[c.field]
		if !present {
			continue
		}
		okType := false
		switch c.kind {
		case "string":
			_, okType = v.(string)
		case "number":
			_, okType = v.(float64)
		case "boolean":
			_, okType = v.(bool)
		}
		if !okType {
			return invalid(fmt.Sprintf("%s must be a %s when present", c.field, c.kind))
		}
	}
	if v, present := obj["outputFormat"]; present && !isAudioFormat(v) {
		return invalid("outputFormat must be an audio format object ({ type, rate? }) when present")
	}
	if v, present := obj["providerOptions"]; present && !isProviderOptions(v) {
		return invalid("providerOptions must be an object of provider objects when present")
	}

	// Only known keys are passed on, so servers never see client extras.
	frame := &StartFrame{Type: StartFrameType}
	frame.Voice, _ = obj["voice"].(string)
	frame.Instructions, _ = obj["instructions"].(string)
	frame.Language, _ = obj["language"].(string)
	if n, ok := obj["speed"].(float64); ok {
		frame.Speed = &n
	}
	if b, ok := obj["includeRawChunks"].(bool); ok {
		frame.IncludeRawChunks = &b
	}
	if of, ok := obj["outputFormat"].(map[string]interface{}); ok {
		format := &AudioFormat{Type: of["type"].(string)}
		if rate, ok := of["rate"].(float64); ok {
			format.Rate = &rate
		}
		frame.OutputFormat = format
	}
	if po, ok := obj["providerOptions"].(map[string]interface{}); ok {
		frame.ProviderOptions = po
	}
	return ClientFrame{Kind: ClientFrameStart, Start: frame}
}

type wireAudioFormat struct {
	Type string   `json:"type"`
	Rate *float64 `json:"rate,omitempty"`
}

func toWireFormat(f provider.AudioFormat) wireAudioFormat {
	out := wireAudioFormat{Type: f.Type}
	if f.Rate != nil {
		rate := float64(*f.Rate)
		out.Rate = &rate
	}
	return out
}

// SerializePart serializes a speech stream part as one TEXT frame, as TS
// JSON.stringify writes it: HTML characters stay literal, NaN and Infinity
// write null, and a present map is written even when empty. Audio is
// base64-encoded, the timestamp is written like Date#toJSON, and error
// payloads that are Go errors serialize as {name, message}. It returns false
// for payloads that are not JSON-serializable; callers drop the frame.
func SerializePart(part *provider.SpeechStreamPart) (string, bool) {
	v, ok := serializablePart(part)
	if !ok {
		return "", false
	}
	// jsonstr writes NaN and Infinity as null, as JSON.stringify does.
	data, err := jsonstr.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// errorPayload is TS { name: error.name, message: error.message }.
type errorPayload struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// jsErrorName is the TS error.name of a Go error: a named SDK error
// (ErrorName) keeps its TS class name; any other error is "Error".
func jsErrorName(err error) string {
	if named, ok := err.(interface{ ErrorName() string }); ok && named.ErrorName() != "" {
		return named.ErrorName()
	}
	return "Error"
}

func serializablePart(part *provider.SpeechStreamPart) (interface{}, bool) {
	switch part.Type {
	case provider.SpeechStreamPartTypeStreamStart:
		warnings := part.Warnings
		if warnings == nil {
			warnings = []types.Warning{}
		}
		return struct {
			Type     string          `json:"type"`
			Warnings []types.Warning `json:"warnings"`
		}{part.Type, warnings}, true

	case provider.SpeechStreamPartTypeAudio:
		metadata := mapOrNil(part.ProviderMetadata)
		return struct {
			Type             string          `json:"type"`
			ID               string          `json:"id,omitempty"`
			Audio            string          `json:"audio"`
			AudioFormat      wireAudioFormat `json:"audioFormat"`
			ProviderMetadata interface{}     `json:"providerMetadata,omitempty"`
		}{part.Type, part.ID, base64.StdEncoding.EncodeToString(part.Audio), toWireFormat(part.AudioFormat), metadata}, true

	case provider.SpeechStreamPartTypeResponseMetadata:
		// Date#toJSON: the ISO string, or null for a time outside the JS
		// date range (an Invalid Date).
		var ts interface{}
		if !part.Timestamp.IsZero() {
			ts = json.RawMessage("null")
			if iso, ok := jsdate.ISOString(part.Timestamp); ok {
				ts = iso
			}
		}
		body := part.Body
		return struct {
			Type      string      `json:"type"`
			Timestamp interface{} `json:"timestamp,omitempty"`
			ModelID   string      `json:"modelId,omitempty"`
			Headers   interface{} `json:"headers,omitempty"`
			Body      interface{} `json:"body,omitempty"`
		}{part.Type, ts, part.ModelID, mapOrNil(part.Headers), body}, true

	case provider.SpeechStreamPartTypeFinish:
		var usage interface{}
		if part.Usage != nil {
			usage = part.Usage
		}
		return struct {
			Type             string      `json:"type"`
			Usage            interface{} `json:"usage,omitempty"`
			ProviderMetadata interface{} `json:"providerMetadata,omitempty"`
		}{part.Type, usage, mapOrNil(part.ProviderMetadata)}, true

	case provider.SpeechStreamPartTypeRaw:
		return struct {
			Type     string      `json:"type"`
			RawValue interface{} `json:"rawValue"`
		}{part.Type, part.RawValue}, true

	case provider.SpeechStreamPartTypeError:
		payload := part.Err
		if err, ok := payload.(error); ok {
			payload = errorPayload{Name: jsErrorName(err), Message: err.Error()}
		}
		return struct {
			Type  string      `json:"type"`
			Error interface{} `json:"error"`
		}{part.Type, payload}, true
	}
	return nil, false
}

// mapOrNil returns a present map, even an empty one, as a non-nil interface
// value, which JSON.stringify writes ({}); a nil map gives nil, omitted like
// undefined.
func mapOrNil[V any](m map[string]V) interface{} {
	if m == nil {
		return nil
	}
	return m
}

func isString(v interface{}) bool { _, ok := v.(string); return ok }

func isOptional(m map[string]interface{}, key string, check func(interface{}) bool) bool {
	v, present := m[key]
	return !present || check(v)
}

func isNumber(v interface{}) bool { _, ok := v.(float64); return ok }

var knownWarningTypes = map[string]bool{"compatibility": true, "deprecated": true, "other": true, "unsupported": true}

func isWarning(m map[string]interface{}) bool {
	switch m["type"] {
	case "compatibility", "unsupported":
		return isString(m["feature"]) && isOptional(m, "details", isString)
	case "deprecated":
		return isString(m["setting"]) && isString(m["message"])
	case "other":
		return isString(m["message"])
	}
	return false
}

// ParsePart parses one server TEXT frame into a speech stream part. It
// returns false for malformed or unsafe JSON, unknown part types, and known
// part types whose fields are missing or mistyped. Unknown warning types in
// a stream-start part are dropped (forward compatibility). Audio is decoded
// from base64 (padded or unpadded, standard or URL-safe alphabet).
func ParsePart(text string) (*provider.SpeechStreamPart, bool) {
	part, ok, err := ParsePartStrict(text)
	if err != nil {
		return nil, false
	}
	return part, ok
}

// ParsePartStrict is ParsePart for a consumer that reports what TS reports
// for an audio part whose base64 string cannot be decoded. TS
// parseSpeechStreamPart keeps such a part (audio is any string), and
// streamSpeech then fails the stream at it with the error of
// convertBase64ToUint8Array (atob's InvalidCharacterError). ParsePartStrict
// returns that error for the frame; every other frame gives ParsePart's
// result and a nil error.
func ParsePartStrict(text string) (*provider.SpeechStreamPart, bool, error) {
	value, err := decodeSecure(text)
	if err != nil {
		return nil, false, nil
	}
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil, false, nil
	}
	partType, _ := raw["type"].(string)
	part := &provider.SpeechStreamPart{Type: partType}

	switch partType {
	case provider.SpeechStreamPartTypeStreamStart:
		list, ok := raw["warnings"].([]interface{})
		if !ok {
			return nil, false, nil
		}
		part.Warnings = []types.Warning{}
		for _, item := range list {
			w, isMap := item.(map[string]interface{})
			if isMap {
				if t, isStr := w["type"].(string); isStr && !knownWarningTypes[t] {
					continue
				}
			}
			if !isMap || !isWarning(w) {
				return nil, false, nil
			}
			warning := types.Warning{}
			warning.Type, _ = w["type"].(string)
			warning.Feature, _ = w["feature"].(string)
			warning.Setting, _ = w["setting"].(string)
			warning.Details, _ = w["details"].(string)
			warning.Message, _ = w["message"].(string)
			part.Warnings = append(part.Warnings, warning)
		}

	case provider.SpeechStreamPartTypeAudio:
		encoded, ok := raw["audio"].(string)
		if !ok || !isAudioFormat(raw["audioFormat"]) ||
			!isOptional(raw, "id", isString) || !isOptional(raw, "providerMetadata", isRecord) {
			return nil, false, nil
		}
		audio, err := types.DecodeFileDataString(encoded)
		if err != nil {
			return nil, false, &InvalidCharacterError{Message: atobErrorMessage(encoded)}
		}
		part.Audio = audio
		part.ID, _ = raw["id"].(string)
		format := raw["audioFormat"].(map[string]interface{})
		part.AudioFormat.Type = format["type"].(string)
		if rate, ok := format["rate"].(float64); ok {
			// Truncated to the v0.5.2 *int (known difference "Audio sample
			// rate is an int"); a rate beyond int is clamped, because an
			// out-of-range float conversion is implementation-defined in Go
			// (negative on amd64 and 386).
			r := math.MaxInt
			if rate < float64(math.MaxInt) {
				r = int(rate)
			}
			part.AudioFormat.Rate = &r
		}
		part.ProviderMetadata, _ = raw["providerMetadata"].(map[string]interface{})

	case provider.SpeechStreamPartTypeFinish:
		usageOK := true
		if u, present := raw["usage"]; present {
			um, isMap := u.(map[string]interface{})
			usageOK = isMap
			if isMap {
				for _, key := range []string{"inputCharacters", "inputTokens", "outputTokens"} {
					if !isOptional(um, key, isNumber) {
						usageOK = false
					}
				}
			}
		}
		if !usageOK || !isOptional(raw, "providerMetadata", isRecord) {
			return nil, false, nil
		}
		if um, ok := raw["usage"].(map[string]interface{}); ok {
			usage := &provider.SpeechStreamUsage{}
			usage.InputCharacters = numberField(um, "inputCharacters")
			usage.InputTokens = numberField(um, "inputTokens")
			usage.OutputTokens = numberField(um, "outputTokens")
			if r, ok := um["raw"].(map[string]interface{}); ok {
				usage.Raw = r
			}
			part.Usage = usage
		}
		part.ProviderMetadata, _ = raw["providerMetadata"].(map[string]interface{})

	case provider.SpeechStreamPartTypeResponseMetadata:
		if !isOptional(raw, "modelId", isString) || !isOptional(raw, "headers", isRecord) {
			return nil, false, nil
		}
		if ts := raw["timestamp"]; ts != nil {
			s, ok := ts.(string)
			if !ok {
				return nil, false, nil
			}
			// TS new Date(timestamp); a NaN date makes the part invalid.
			ms, ok := jsdate.Parse(s)
			if !ok {
				return nil, false, nil
			}
			part.Timestamp = time.UnixMilli(ms)
		}
		part.ModelID, _ = raw["modelId"].(string)
		if h, ok := raw["headers"].(map[string]interface{}); ok {
			part.Headers = make(map[string]string, len(h))
			for k, hv := range h {
				if s, ok := hv.(string); ok {
					part.Headers[k] = s
				}
			}
		}
		part.Body = raw["body"]

	case provider.SpeechStreamPartTypeRaw:
		rv, ok := raw["rawValue"]
		if !ok {
			return nil, false, nil
		}
		part.RawValue = rv

	case provider.SpeechStreamPartTypeError:
		ev, ok := raw["error"]
		if !ok {
			return nil, false, nil
		}
		part.Err = ev

	default:
		return nil, false, nil
	}
	return part, true, nil
}

// InvalidCharacterError is the DOMException atob throws (name
// InvalidCharacterError) for a string TS convertBase64ToUint8Array cannot
// decode; Error is its message, as Node writes it.
type InvalidCharacterError struct{ Message string }

func (e *InvalidCharacterError) Error() string { return e.Message }

// ErrorName is the DOMException name.
func (e *InvalidCharacterError) ErrorName() string { return "InvalidCharacterError" }

// atobErrorMessage is the message Node's atob throws for s after TS
// convertBase64ToUint8Array maps "-" and "_" to "+" and "/": "Invalid
// character" for a character outside the base64 alphabet (ASCII whitespace
// is skipped), an "=" that is not trailing, more than two "=", or padding
// that does not complete a 4-character group; "The string to be decoded is
// not correctly encoded." when the data characters leave one extra
// character. Matched against Node 24 atob on 300,000 generated strings.
func atobErrorMessage(s string) string {
	const invalid = "Invalid character"
	alpha, eq := 0, 0
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r':
			continue
		case r == '=':
			eq++
			if eq > 2 {
				return invalid
			}
			continue
		case r == '-' || r == '_' || r == '+' || r == '/' ||
			(r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
		default:
			return invalid
		}
		if eq > 0 {
			return invalid
		}
		alpha++
	}
	if alpha%4 == 1 {
		return "The string to be decoded is not correctly encoded."
	}
	return invalid
}

// numberField reads an optional JS number as is (no truncation).
func numberField(m map[string]interface{}, key string) *float64 {
	if f, ok := m[key].(float64); ok {
		return &f
	}
	return nil
}
