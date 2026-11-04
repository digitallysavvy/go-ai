package elevenlabs

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// MusicWordTimestamp is one lyric word of a music track with timestamps
// (providerMetadata.elevenlabs.wordTimestamps, TS 48ab95d62a).
type MusicWordTimestamp struct {
	Word    string  `json:"word"`
	StartMs float64 `json:"startMs"`
	EndMs   float64 `json:"endMs"`
}

// musicTimestampsResponse is the parsed /v1/music/detailed response (TS
// ElevenLabsMusicTimestampsResponse).
type musicTimestampsResponse struct {
	audio          []byte
	wordTimestamps []MusicWordTimestamp
	hasWords       bool
	// raw is the parsed JSON metadata part (TS rawValue, reported as
	// response.body).
	raw interface{}
}

// The TS regexps with JS semantics: \s is the JS class, and the /m ^ of the
// part content type also starts a line after \r, U+2028 or U+2029.
var (
	musicMultipartPattern = regexp.MustCompile(`(?i)^multipart/mixed\b`)
	musicBoundaryPattern  = regexp.MustCompile(`(?i)(?:^|;)` + providerutils.JSRegexpSpace + `*boundary=(?:"([^"]+)"|([^;` + providerutils.JSRegexpSpaceChars + `]+))`)
	musicPartTypePattern  = regexp.MustCompile(`(?i)(?:^|[\n\r\x{2028}\x{2029}])content-type:` + providerutils.JSRegexpSpace + `*([^;\r\n]+)`)
)

// musicMetadataSchema mirrors TS musicMetadataSchema.
var musicMetadataSchema = zodlite.Object(
	zodlite.F("words_timestamps", zodlite.Nullish(zodlite.Array(zodlite.Object(
		zodlite.F("word", zodlite.String),
		zodlite.F("start_ms", zodlite.Number),
		zodlite.F("end_ms", zodlite.Number),
	)))),
)

// parseMusicTimestampsResponse ports TS createMusicTimestampsResponseHandler
// (elevenlabs-music-response.ts): /v1/music/detailed returns JSON metadata
// and binary audio as multipart/mixed.
func parseMusicTimestampsResponse(contentType string, body []byte) (*musicTimestampsResponse, error) {
	boundary := musicBoundaryPattern.FindStringSubmatch(contentType)
	if !musicMultipartPattern.MatchString(contentType) || boundary == nil {
		return nil, providererrors.NewInvalidResponseDataError(contentType, "ElevenLabs music response is missing a multipart boundary.")
	}
	b := boundary[1]
	if b == "" {
		b = boundary[2]
	}
	delimiter := []byte("--" + b)
	separator := []byte("\r\n\r\n")

	var audio []byte
	var metadata *musicTimestampsResponse
	hasMetadata := false
	position := findMusicBoundary(body, delimiter, 0)
	closed := false
	for position != -1 {
		start := position + len(delimiter)
		if start+1 < len(body) && body[start] == '-' && body[start+1] == '-' {
			closed = true
			break
		}
		next := findMusicBoundary(body, delimiter, start+2)
		if next == -1 {
			break
		}
		end := next - 2 // Remove only the CRLF framing this part.
		headersEnd := findMusicBytes(body, separator, start+2)
		if headersEnd == -1 || headersEnd+len(separator) > end {
			return nil, providererrors.NewInvalidResponseDataError(contentType, "ElevenLabs music response contains malformed part headers.")
		}
		headers := textdecode.UTF8(body[start+2 : headersEnd])
		partType := ""
		if m := musicPartTypePattern.FindStringSubmatch(headers); m != nil {
			partType = strings.ToLower(providerutils.JSTrim(m[1]))
		}
		part := body[headersEnd+len(separator) : end]
		switch {
		case partType == "application/json":
			if hasMetadata {
				return nil, providererrors.NewInvalidResponseDataError(contentType, "ElevenLabs music response contains multiple metadata parts.")
			}
			parsed, err := parseMusicMetadata(textdecode.UTF8(part))
			if err != nil {
				return nil, err
			}
			metadata, hasMetadata = parsed, true
		case strings.HasPrefix(partType, "audio/"):
			if audio != nil {
				return nil, providererrors.NewInvalidResponseDataError(contentType, "ElevenLabs music response contains multiple audio parts.")
			}
			audio = append([]byte{}, part...)
		}
		position = next
	}

	if !closed || audio == nil || !hasMetadata {
		return nil, providererrors.NewInvalidResponseDataError(contentType, "ElevenLabs music response must contain complete metadata and audio parts.")
	}
	metadata.audio = audio
	return metadata, nil
}

// parseMusicMetadata is TS safeParseJSON with musicMetadataSchema: invalid
// JSON is a JSONParseError, a schema mismatch a type validation error.
func parseMusicMetadata(text string) (*musicTimestampsResponse, error) {
	var raw interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, &providererrors.JSONParseError{Text: text, Cause: err}
	}
	if message := zodlite.ValidationMessage(musicMetadataSchema, text); message != "" {
		return nil, providererrors.NewValidationErrorWithContext(zodlite.CompactJSON(text), zodlite.IssueText(message), nil, &providererrors.ValidationContext{})
	}
	var decoded struct {
		Words []struct {
			Word    string  `json:"word"`
			StartMs float64 `json:"start_ms"`
			EndMs   float64 `json:"end_ms"`
		} `json:"words_timestamps"`
	}
	_ = json.Unmarshal([]byte(text), &decoded)
	out := &musicTimestampsResponse{raw: raw}
	if m, ok := raw.(map[string]interface{}); ok && m["words_timestamps"] != nil {
		out.hasWords = true
		out.wordTimestamps = make([]MusicWordTimestamp, 0, len(decoded.Words))
		for _, w := range decoded.Words {
			out.wordTimestamps = append(out.wordTimestamps, MusicWordTimestamp{Word: w.Word, StartMs: w.StartMs, EndMs: w.EndMs})
		}
	}
	return out, nil
}

func findMusicBytes(data, pattern []byte, from int) int {
	if from < 0 {
		from = 0
	}
	if from > len(data) {
		return -1
	}
	if i := bytes.Index(data[from:], pattern); i >= 0 {
		return from + i
	}
	return -1
}

// findMusicBoundary finds a delimiter that starts a line and is followed by
// CRLF or "--" (TS findBoundary).
func findMusicBoundary(data, pattern []byte, from int) int {
	for i := findMusicBytes(data, pattern, from); i != -1; i = findMusicBytes(data, pattern, i+1) {
		end := i + len(pattern)
		lineStart := i == 0 || (i >= 2 && data[i-2] == '\r' && data[i-1] == '\n')
		lineEnd := end+1 < len(data) && ((data[end] == '\r' && data[end+1] == '\n') || (data[end] == '-' && data[end+1] == '-'))
		if lineStart && lineEnd {
			return i
		}
	}
	return -1
}
