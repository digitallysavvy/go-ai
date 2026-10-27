package speechstream

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFrameTypeConstants(t *testing.T) {
	if StartFrameType != "speech-stream.start" || TextDeltaFrameType != "speech-stream.text-delta" || TextDoneFrameType != "speech-stream.text-done" {
		t.Fatal("frame types must use the speech-stream namespace")
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseClientFrame_Start(t *testing.T) {
	min := ParseClientFrame(`{"type":"speech-stream.start"}`)
	if min.Kind != ClientFrameStart || min.Start.Type != StartFrameType || min.Start.Voice != "" {
		t.Fatalf("minimal = %+v", min)
	}

	full := ParseClientFrame(mustJSON(t, map[string]interface{}{
		"type": "speech-stream.start", "voice": "v1", "outputFormat": map[string]interface{}{"type": "audio/pcm", "rate": 24000},
		"instructions": "calm", "speed": 1.1, "language": "en",
		"providerOptions": map[string]interface{}{"elevenlabs": map[string]interface{}{"seed": 1}}, "includeRawChunks": true,
	}))
	s := full.Start
	if full.Kind != ClientFrameStart || s.Voice != "v1" || s.OutputFormat.Type != "audio/pcm" || *s.OutputFormat.Rate != 24000 ||
		s.Instructions != "calm" || *s.Speed != 1.1 || s.Language != "en" || !*s.IncludeRawChunks ||
		s.ProviderOptions["elevenlabs"].(map[string]interface{})["seed"] != float64(1) {
		t.Fatalf("full = %+v", s)
	}

	dropped := ParseClientFrame(`{"type":"speech-stream.start","voice":"v1","headers":{"authorization":"x"},"outputFormat":{"type":"audio/pcm","rate":16000,"extra":true}}`)
	if got := mustJSON(t, dropped.Start); got != `{"type":"speech-stream.start","voice":"v1","outputFormat":{"type":"audio/pcm","rate":16000}}` {
		t.Fatalf("unknown keys not dropped: %s", got)
	}
}

func TestParseClientFrame_InvalidStart(t *testing.T) {
	const providerOptsMsg = "providerOptions must be an object of provider objects when present"
	const formatMsg = "outputFormat must be an audio format object ({ type, rate? }) when present"
	cases := []struct {
		fields string
		msg    string
	}{
		{`"voice":1`, "voice must be a string when present"},
		{`"speed":"1"`, "speed must be a number when present"},
		{`"includeRawChunks":"yes"`, "includeRawChunks must be a boolean when present"},
		{`"providerOptions":[]`, providerOptsMsg},
		{`"providerOptions":{"openai":null}`, providerOptsMsg},
		{`"providerOptions":{"openai":"x"}`, providerOptsMsg},
		{`"outputFormat":{"type":""}`, formatMsg},
		{`"outputFormat":{"type":"audio/pcm","rate":-1}`, formatMsg},
		{`"outputFormat":"pcm_24000"`, formatMsg},
	}
	for _, c := range cases {
		got := ParseClientFrame(`{"type":"speech-stream.start",` + c.fields + `}`)
		if got.Kind != ClientFrameInvalid || got.Message != c.msg {
			t.Errorf("%s: got %+v, want invalid %q", c.fields, got, c.msg)
		}
	}
}

func TestParseClientFrame_TextFrames(t *testing.T) {
	d := ParseClientFrame(`{"type":"speech-stream.text-delta","delta":"Hi 👋"}`)
	if d.Kind != ClientFrameTextDelta || d.Delta != "Hi 👋" || d.Voice != "" || d.ProviderOptions != nil {
		t.Fatalf("delta = %+v", d)
	}
	if ParseClientFrame(`{"type":"speech-stream.text-done"}`).Kind != ClientFrameTextDone {
		t.Fatal("text-done")
	}
	withMeta := ParseClientFrame(`{"type":"speech-stream.text-delta","delta":"Hi ","voice":"v2","providerOptions":{"elevenlabs":{"newTurn":true}}}`)
	if withMeta.Voice != "v2" || withMeta.ProviderOptions["elevenlabs"].(map[string]interface{})["newTurn"] != true {
		t.Fatalf("withMeta = %+v", withMeta)
	}

	cases := []struct{ fields, msg string }{
		{`"voice":2`, "text-delta voice must be a string when present"},
		{`"providerOptions":"x"`, "text-delta providerOptions must be an object of provider objects when present"},
		{`"providerOptions":{"elevenlabs":[]}`, "text-delta providerOptions must be an object of provider objects when present"},
	}
	for _, c := range cases {
		got := ParseClientFrame(`{"type":"speech-stream.text-delta","delta":"Hi",` + c.fields + `}`)
		if got.Kind != ClientFrameInvalid || got.Message != c.msg {
			t.Errorf("%s: got %+v", c.fields, got)
		}
	}
	if got := ParseClientFrame(`{"type":"speech-stream.text-delta","delta":3}`); got.Message != "text-delta frame must have a string delta" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseClientFrame_Classification(t *testing.T) {
	for in, want := range map[string]ClientFrame{
		`{`:                          {Kind: ClientFrameInvalid, Message: "malformed JSON"},
		`[]`:                         {Kind: ClientFrameInvalid, Message: "frame must be a JSON object"},
		`{"type":1}`:                 {Kind: ClientFrameInvalid, Message: "frame type must be a string"},
		`{"type":"speech-stream.x"}`: {Kind: ClientFrameUnknown},
		// Prototype-pollution payloads are rejected like TS secureJsonParse.
		`{"type":"speech-stream.start","__proto__":{"polluted":true}}`:                 {Kind: ClientFrameInvalid, Message: "malformed JSON"},
		`{"type":"speech-stream.start","constructor":{"prototype":{"polluted":true}}}`: {Kind: ClientFrameInvalid, Message: "malformed JSON"},
	} {
		if got := ParseClientFrame(in); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v want %+v", in, got, want)
		}
	}
}

func TestSerializeAndParsePart(t *testing.T) {
	rate := 24000
	text, ok := SerializePart(&provider.SpeechStreamPart{
		Type: "audio", Audio: []byte{1, 2, 3}, AudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
	})
	if !ok || text != `{"type":"audio","audio":"AQID","audioFormat":{"type":"audio/pcm","rate":24000}}` {
		t.Fatalf("serialized = %s", text)
	}
	part, ok := ParsePart(text)
	if !ok || !reflect.DeepEqual(part.Audio, []byte{1, 2, 3}) || part.AudioFormat.Type != "audio/pcm" || *part.AudioFormat.Rate != 24000 {
		t.Fatalf("parsed = %+v", part)
	}

	chars := 12
	finish, _ := SerializePart(&provider.SpeechStreamPart{Type: "finish", Usage: &provider.SpeechStreamUsage{InputCharacters: &chars},
		ProviderMetadata: map[string]interface{}{"gateway": map[string]interface{}{"cost": "0.0005"}}})
	got, ok := ParsePart(finish)
	if !ok || *got.Usage.InputCharacters != 12 || got.ProviderMetadata["gateway"].(map[string]interface{})["cost"] != "0.0005" {
		t.Fatalf("finish = %+v", got)
	}
}

func TestSerializeErrorPayloads(t *testing.T) {
	text, ok := SerializePart(&provider.SpeechStreamPart{Type: "error", Err: errors.New("boom")})
	if !ok || text != `{"type":"error","error":{"message":"boom","name":"Error"}}` {
		t.Fatalf("text = %s", text)
	}
	if _, ok := SerializePart(&provider.SpeechStreamPart{Type: "raw", RawValue: math.Inf(1)}); ok {
		t.Fatal("non-serializable payloads must be dropped")
	}
}

func TestParsePart_ReviveTimestamp(t *testing.T) {
	part, ok := ParsePart(`{"type":"response-metadata","timestamp":"2024-01-01T00:00:00.000Z","modelId":"m"}`)
	if !ok || !part.Timestamp.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) || part.ModelID != "m" {
		t.Fatalf("part = %+v", part)
	}
}

func TestParsePart_DropsInvalid(t *testing.T) {
	for _, in := range []string{
		`{"type":"audio","audio":"AQ=="}`,
		`{"type":"audio","audio":1,"audioFormat":{"type":"audio/mpeg"}}`,
		`{"type":"finish","usage":{"inputCharacters":"1"}}`,
		`{"type":"stream-start","warnings":[{}]}`,
		`{"type":"stream-start","warnings":[{"type":"unsupported"}]}`,
		`{"type":"stream-start","warnings":[{"type":"other","message":1}]}`,
		`{"type":"stream-start","warnings":[{"type":"deprecated","setting":"x"}]}`,
		`{"type":"response-metadata","timestamp":"not a date"}`,
		`{"type":"unknown-part"}`,
		`{"type":"audio","audio":"AQ==","audioFormat":{"type":"audio/mpeg"},"__proto__":{}}`,
	} {
		if part, ok := ParsePart(in); ok {
			t.Errorf("%s should be dropped, got %+v", in, part)
		}
	}
}

func TestParsePart_Warnings(t *testing.T) {
	part, ok := ParsePart(`{"type":"stream-start","warnings":[
		{"type":"unsupported","feature":"speed","details":"ignored"},
		{"type":"compatibility","feature":"format"},
		{"type":"deprecated","setting":"old","message":"use new"},
		{"type":"other","message":"note"},
		{"type":"future-warning","anything":true}]}`)
	want := []types.Warning{
		{Type: "unsupported", Feature: "speed", Details: "ignored"},
		{Type: "compatibility", Feature: "format"},
		{Type: "deprecated", Setting: "old", Message: "use new"},
		{Type: "other", Message: "note"},
	}
	if !ok || !reflect.DeepEqual(part.Warnings, want) {
		t.Fatalf("warnings = %+v", part)
	}
}
