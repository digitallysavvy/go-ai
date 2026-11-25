package speechstream

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS serializeSpeechStreamPart is JSON.stringify: HTML characters and U+2028
// stay literal, NaN / Infinity write null, an empty object is written, the
// error payload is {name, message} in that order, and the timestamp is
// Date#toJSON (expanded years; null for an Invalid Date). The expected texts
// are the TS output (bun, provider-utils/src/speech-stream-envelope.ts at
// 2ebc78b654).
func TestR94carrySerializePartIsJSONStringify(t *testing.T) {
	inputTokens := math.NaN()
	outputTokens := 3.0
	cases := []struct {
		name string
		part *provider.SpeechStreamPart
		want string
	}{
		{"error text", &provider.SpeechStreamPart{Type: "error", Err: errors.New("a<b&c ")},
			"{\"type\":\"error\",\"error\":{\"name\":\"Error\",\"message\":\"a<b&c \"}}"},
		{"named error", &provider.SpeechStreamPart{Type: "error", Err: r94carryNamedError{}},
			`{"type":"error","error":{"name":"AI_NoSuchModelError","message":"x"}}`},
		{"non-finite raw", &provider.SpeechStreamPart{Type: "raw", RawValue: map[string]interface{}{"a": math.Inf(1), "b": []interface{}{math.NaN(), 1.0}, "c": "<x>"}},
			`{"type":"raw","rawValue":{"a":null,"b":[null,1],"c":"<x>"}}`},
		{"empty metadata", &provider.SpeechStreamPart{Type: "finish", ProviderMetadata: map[string]interface{}{}},
			`{"type":"finish","providerMetadata":{}}`},
		{"NaN usage", &provider.SpeechStreamPart{Type: "finish", Usage: &provider.SpeechStreamUsage{InputCharacters: &inputTokens, OutputTokens: &outputTokens}},
			`{"type":"finish","usage":{"inputCharacters":null,"outputTokens":3}}`},
		{"expanded year, empty headers", &provider.SpeechStreamPart{Type: "response-metadata", Timestamp: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), Headers: map[string]string{}},
			`{"type":"response-metadata","timestamp":"+010000-01-01T00:00:00.000Z","headers":{}}`},
		{"negative year", &provider.SpeechStreamPart{Type: "response-metadata", Timestamp: time.Date(-1, 1, 1, 2, 3, 4, 5_000_000, time.UTC), ModelID: "m"},
			`{"type":"response-metadata","timestamp":"-000001-01-01T02:03:04.005Z","modelId":"m"}`},
		{"max time", &provider.SpeechStreamPart{Type: "response-metadata", Timestamp: time.UnixMilli(8.64e15)},
			`{"type":"response-metadata","timestamp":"+275760-09-13T00:00:00.000Z"}`},
		{"invalid date", &provider.SpeechStreamPart{Type: "response-metadata", Timestamp: time.UnixMilli(8.64e15 + 1)},
			`{"type":"response-metadata","timestamp":null}`},
		{"audio empty metadata", &provider.SpeechStreamPart{Type: "audio", Audio: []byte{1}, AudioFormat: provider.AudioFormat{Type: "audio/mpeg"}, ProviderMetadata: map[string]interface{}{}},
			`{"type":"audio","audio":"AQ==","audioFormat":{"type":"audio/mpeg"},"providerMetadata":{}}`},
	}
	for _, c := range cases {
		got, ok := SerializePart(c.part)
		if !ok || got != c.want {
			t.Errorf("%s: SerializePart = %q, %v; want %q", c.name, got, ok, c.want)
		}
	}
	// A payload JSON.stringify throws on (TS: BigInt) is dropped.
	if got, ok := SerializePart(&provider.SpeechStreamPart{Type: "raw", RawValue: make(chan int)}); ok {
		t.Errorf("non-serializable raw value = %q, want dropped", got)
	}
}

// JSON.parse reads a number out of the double range as +-Infinity, so the
// frame is kept (TS parseSpeechStreamPart / parseSpeechStreamClientFrame).
func TestR94carryParseOutOfRangeNumbers(t *testing.T) {
	part, ok := ParsePart(`{"type":"finish","usage":{"inputTokens":1e400,"outputTokens":-1e400}}`)
	if !ok || part.Usage == nil || !math.IsInf(*part.Usage.InputTokens, 1) || !math.IsInf(*part.Usage.OutputTokens, -1) {
		t.Fatalf("ParsePart = %+v, %v; want Infinity usage", part, ok)
	}
	frame := ParseClientFrame(`{"type":"speech-stream.start","speed":1e400}`)
	if frame.Kind != ClientFrameStart || frame.Start.Speed == nil || !math.IsInf(*frame.Start.Speed, 1) {
		t.Fatalf("ParseClientFrame = %+v", frame)
	}
	for _, in := range []string{
		`{"type":"raw","rawValue":{"x":1e400,"__proto__":1}}`,
		`{"type":"audio","audio":"AQ==","audioFormat":{"type":"audio/pcm","rate":1e400}}`,
	} {
		if part, ok := ParsePart(in); ok {
			t.Errorf("%s = %+v, want dropped", in, part)
		}
	}
}

type r94carryNamedError struct{}

func (r94carryNamedError) Error() string     { return "x" }
func (r94carryNamedError) ErrorName() string { return "AI_NoSuchModelError" }
