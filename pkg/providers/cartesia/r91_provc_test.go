package cartesia

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR91provcRealtimeAudioBase64URL (provc-18): input-audio-append accepts
// base64url audio, as TS convertBase64ToUint8Array does.
func TestR91provcRealtimeAudioBase64URL(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	audio, binary, _, err := m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "-_8"})
	if err != nil || !binary || string(audio) != "\xfb\xff" {
		t.Fatalf("audio = %x binary=%v err=%v", audio, binary, err)
	}
}

// TestR91provcRealtimeCombinedRawOrder (provc-19): the combined transcript
// raw is TS {event, transcriptEvents, duration}.
func TestR91provcRealtimeCombinedRawOrder(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	parse(t, m, `{"type":"transcript","request_id":"r","text":"a","is_final":true,"duration":0.5}`)
	parse(t, m, `{"type":"transcript","request_id":"r","text":"b","is_final":true,"duration":0.25}`)
	e := parse(t, m, `{"type":"flush_done","request_id":"r"}`)
	if len(e) != 2 {
		t.Fatalf("events = %+v", e)
	}
	if want := `{"event":{"type":"flush_done","request_id":"r"},"transcriptEvents":[{"type":"transcript","request_id":"r","text":"a","is_final":true,"duration":0.5},{"type":"transcript","request_id":"r","text":"b","is_final":true,"duration":0.25}],"duration":0.75}`; string(e[1].Raw) != want {
		t.Errorf("raw = %s\nwant %s", e[1].Raw, want)
	}
}

// TestR91provcStreamLenientEvents (provc-20): an event with a field of
// another type is still used (TS casts the parsed value), and an error event
// with message "" fails with "" (TS `message ?? error_code`).
func TestR91provcStreamLenientEvents(t *testing.T) {
	server := newCartesiaRealtimeTestServer(t)
	defer server.close()
	model := newCartesiaRealtimeTestModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(16000)},
		ProviderOptions:  map[string]interface{}{"cartesia": TranscriptionModelOptions{Language: "en"}},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.toSend <- map[string]interface{}{"type": "turn.update", "request_id": 7, "transcript": "hi"}
	server.toSend <- map[string]interface{}{"type": "error", "message": "", "error_code": "x"}
	parts, err := drainUntilFinishOrError(t, result.Stream)
	if err == nil || err.Error() != "" {
		t.Fatalf("err = %v, want the empty message", err)
	}
	var sawRaw, sawPartial bool
	for _, p := range parts {
		switch p.Type {
		case provider.TranscriptionStreamPartTypeRaw:
			sawRaw = true
		case provider.TranscriptionStreamPartTypePartial:
			if p.Text == "hi" && p.ID == "7" {
				sawPartial = true
			}
		}
	}
	if !sawRaw || !sawPartial {
		t.Errorf("raw=%v partial=%v parts=%s", sawRaw, sawPartial, r91provcTypes(parts))
	}
}

func r91provcTypes(parts []*provider.TranscriptionStreamPart) string {
	var out []string
	for _, p := range parts {
		out = append(out, string(p.Type))
	}
	return strings.Join(out, ",")
}

// TestR91provcTypedOptionLiterals (typed-option ruling).
func TestR91provcTypedOptionLiterals(t *testing.T) {
	if lang, ok := transcriptionLanguageOption(map[string]interface{}{"cartesia": map[string]string{"language": "fr"}}, nil); !ok || lang != "fr" {
		t.Errorf("language = %q %v", lang, ok)
	}
}
