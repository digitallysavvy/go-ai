package elevenlabs

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func dialogueOpts(voices ...string) map[string]interface{} {
	vs := make([]interface{}, len(voices))
	for i, v := range voices {
		vs[i] = v
	}
	return m("elevenlabs", m("dialogue", m("voices", vs)))
}

func in(text, voice string, newTurn bool) map[string]interface{} {
	input := m("text", text, "voice_id", voice)
	if newTurn {
		input["new_turn"] = true
	}
	return m("inputs", []interface{}{input})
}

func TestDialogueStream_StreamsSpeakerTurnsOverV4(t *testing.T) {
	r24 := 24000
	result, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text: newSpeechText(
			provider.SpeechStreamTextPart{Text: "Hi there. ", Voice: "a"},
			provider.SpeechStreamTextPart{Text: "Hello 👋 ", Voice: "b"},
			provider.SpeechStreamTextPart{Text: "friend.", Voice: "b"},
		),
		OutputFormat: &provider.AudioFormat{Type: "audio/pcm", Rate: &r24},
		ProviderOptions: m("elevenlabs", m(
			"dialogue", m("voices", []interface{}{"a", "b"}, "voiceSettings", m("stability", 0.5)),
			"streaming", m("syncAlignment", true, "inactivityTimeout", 60.0),
		)),
	})
	wantURL := "ws://" + strings.TrimPrefix(server.ts.URL, "http://") + "/v1/text-to-dialogue/stream-input?model_id=eleven_v4&output_format=pcm_24000&sync_alignment=true"
	if got := result.RequestBody.(map[string]interface{})["url"]; got != wantURL {
		t.Fatalf("url = %v", got)
	}
	got := server.waitForCount(t, 5, 2*time.Second)
	want := []map[string]interface{}{
		m("voice_settings", m("stability", 0.5), "voices", []interface{}{"a", "b"}, "xi_api_key", "test-api-key"),
		in("Hi there. ", "a", false), in("Hello 👋 ", "b", true), in("friend.", "b", false),
		m("close_socket", true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v\nwant %#v", got, want)
	}

	server.toSend <- m("audio", "AQI=", "alignment", m("chars", []interface{}{"H"}, "char_start_times_ms", []interface{}{0}, "char_durations_ms", []interface{}{5}))
	server.toSend <- m("is_final_audio_for_turn", true)
	server.toSend <- m("is_final", true)

	parts, err := collectSpeech(t, result.Stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 4 {
		t.Fatalf("parts = %+v", parts)
	}
	if len(parts[0].Warnings) != 1 || parts[0].Warnings[0].Feature != "providerOptions.elevenlabs.streaming.inactivityTimeout" {
		t.Fatalf("warnings = %+v", parts[0].Warnings)
	}
	wantAlign := m("elevenlabs", m("alignment", m("chars", []interface{}{"H"}, "charStartTimesMs", []interface{}{0.0}, "charDurationsMs", []interface{}{5.0})))
	if !reflect.DeepEqual(parts[1].ProviderMetadata, wantAlign) {
		t.Fatalf("alignment = %#v", parts[1].ProviderMetadata)
	}
	// A turn boundary arrives as a metadata-only (empty) audio part.
	if parts[2].Type != "audio" || len(parts[2].Audio) != 0 ||
		!reflect.DeepEqual(parts[2].ProviderMetadata, m("elevenlabs", m("isFinalAudioForTurn", true))) {
		t.Fatalf("turn marker = %+v", parts[2])
	}
	// 'Hi there. ' (10) + 'Hello 👋 ' (8) + 'friend.' (7)
	if *parts[3].Usage.InputCharacters != 25 {
		t.Fatalf("usage = %+v", parts[3].Usage)
	}
}

func TestDialogueStream_FlushesHeldTextOnSpeakerChangeAndHonorsNewTurn(t *testing.T) {
	_, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Voice: "b",
		Text: newSpeechText(
			provider.SpeechStreamTextPart{Text: "Hi the", Voice: "a"},
			provider.SpeechStreamTextPart{Text: "Hello "},
			provider.SpeechStreamTextPart{Text: "Again. ", ProviderOptions: m("elevenlabs", m("newTurn", true))},
			provider.SpeechStreamTextPart{Text: "More."},
		),
		ProviderOptions: dialogueOpts("a", "b"),
	})
	got := server.waitForCount(t, 7, 2*time.Second)[1:]
	want := []map[string]interface{}{
		in("Hi ", "a", false),
		in("the", "a", false), // the held partial word is flushed before the speaker changes
		in("Hello ", "b", true),
		in("Again. ", "b", true),
		in("More.", "b", false),
		m("close_socket", true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDialogueStream_DoesNotSplitHeldTextWhenAPartNamesTheDefaultVoice(t *testing.T) {
	_, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text: newSpeechText(
			provider.SpeechStreamTextPart{Text: "Hel"},
			provider.SpeechStreamTextPart{Text: "lo there.", Voice: "a"},
		),
		ProviderOptions: dialogueOpts("a", "b"),
	})
	got := server.waitForCount(t, 4, 2*time.Second)[1:]
	want := []map[string]interface{}{in("Hello ", "a", false), in("there.", "a", false), m("close_socket", true)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDialogueStream_KeepsANewTurnWhoseTextIsEntirelyHeld(t *testing.T) {
	_, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text: newSpeechText(
			provider.SpeechStreamTextPart{Text: "Hi. "},
			provider.SpeechStreamTextPart{Text: "Ag", ProviderOptions: m("elevenlabs", m("newTurn", true))},
			provider.SpeechStreamTextPart{Text: "ain. "},
		),
		ProviderOptions: dialogueOpts("a"),
	})
	got := server.waitForCount(t, 4, 2*time.Second)[1:]
	want := []map[string]interface{}{in("Hi. ", "a", false), in("Again. ", "a", true), m("close_socket", true)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDialogueStream_UnlistedVoiceErrorsWithoutWaitingForMoreText(t *testing.T) {
	for name, text := range map[string]string{"a held part": "Hel", "an empty part": ""} {
		t.Run(name, func(t *testing.T) {
			src := newSpeechText(provider.SpeechStreamTextPart{Text: text, Voice: "c"})
			src.block = true // never ends: the error must not wait for a flush
			result, _ := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{Text: src, ProviderOptions: dialogueOpts("a")})
			_, err := collectSpeech(t, result.Stream)
			if err == nil || !strings.Contains(err.Error(), "is not listed in") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDialogueStream_WarnsAboutOptionsTextToDialogueIgnores(t *testing.T) {
	speed := 1.2
	result, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text:  speechTexts("Hi."),
		Speed: &speed,
		ProviderOptions: m("elevenlabs", m(
			"dialogue", m("voices", []interface{}{"a"}),
			"voiceSettings", m("stability", 0.3),
			"streaming", m("autoMode", true, "chunkLengthSchedule", []interface{}{50.0}),
		)),
	})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- m("is_final", true)
	parts, _ := collectSpeech(t, result.Stream)
	var features []string
	for _, w := range parts[0].Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"speed", "providerOptions.elevenlabs.voiceSettings", "providerOptions.elevenlabs.streaming.autoMode", "providerOptions.elevenlabs.streaming.chunkLengthSchedule"}
	if !reflect.DeepEqual(features, want) {
		t.Fatalf("features = %v", features)
	}
}

func TestDialogueStream_ErrorsWhenAPartVoiceIsNotListed(t *testing.T) {
	result, _ := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text:            newSpeechText(provider.SpeechStreamTextPart{Text: "Hi ", Voice: "c"}),
		ProviderOptions: dialogueOpts("a", "b"),
	})
	_, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != `Invalid argument for parameter text: Dialogue text part voice "c" is not listed in providerOptions.elevenlabs.dialogue.voices.` {
		t.Fatalf("err = %v", err)
	}
}

func TestDialogueStream_KeepsAliveEvenWhenInactivityTimeoutIsSet(t *testing.T) {
	withKeepalive(t, 40*time.Millisecond)
	src := speechTexts()
	src.block = true
	_, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{
		Text:            src,
		ProviderOptions: m("elevenlabs", m("dialogue", m("voices", []interface{}{"a"}), "streaming", m("inactivityTimeout", 60.0))),
	})
	got := server.waitForCount(t, 2, 2*time.Second)[1:]
	if !reflect.DeepEqual(got[0], m("keep_alive", true)) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDialogueStream_SendsTheWholeScriptInOneMessageForV3(t *testing.T) {
	_, server := startSpeech(t, "eleven_v3_conversational", &provider.SpeechStreamOptions{
		Text:            speechTexts("Hello ", "world."),
		ProviderOptions: dialogueOpts("a"),
	})
	got := server.waitForCount(t, 2, 2*time.Second)
	want := []map[string]interface{}{
		m("voices", []interface{}{"a"}, "xi_api_key", "test-api-key"),
		m("inputs", []interface{}{m("text", "Hello ", "voice_id", "a"), m("text", "world.", "voice_id", "a")}, "close_socket", true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v", got)
	}
}

func TestDialogueStream_ServerErrorFrames(t *testing.T) {
	result, server := startSpeech(t, "eleven_v4", &provider.SpeechStreamOptions{Text: speechTexts("Hi"), ProviderOptions: dialogueOpts("a", "b")})
	server.waitForCount(t, 3, 2*time.Second)
	server.toSend <- m("message", "eleven_v3_conversational only supports a single voice.", "error", "invalid_argument", "code", 1008)
	_, err := collectSpeech(t, result.Stream)
	if err == nil || err.Error() != "ElevenLabs speech stream error: eleven_v3_conversational only supports a single voice." {
		t.Fatalf("err = %v", err)
	}
}

func TestDialogueStream_SendsKeepAliveWhileABufferedV3ScriptWaits(t *testing.T) {
	withKeepalive(t, 40*time.Millisecond)
	src := newSpeechText(provider.SpeechStreamTextPart{Text: "Hello there. "})
	src.block = true
	_, server := startSpeech(t, "eleven_v3", &provider.SpeechStreamOptions{Text: src, ProviderOptions: dialogueOpts("a")})
	got := server.waitForCount(t, 2, 2*time.Second)[1:]
	if !reflect.DeepEqual(got[0], m("keep_alive", true)) {
		t.Fatalf("messages = %#v", got)
	}
	close(src.release)
	all := server.waitForCount(t, 3, 2*time.Second)
	want := m("inputs", []interface{}{m("text", "Hello there. ", "voice_id", "a")}, "close_socket", true)
	if !reflect.DeepEqual(all[len(all)-1], want) {
		// a keepalive can race ahead of the final message
		for _, msg := range server.all() {
			if reflect.DeepEqual(msg, want) {
				return
			}
		}
		t.Fatalf("final message missing: %#v", all)
	}
}

func TestDialogueStream_TypedOptionsAreValidated(t *testing.T) {
	server := newRealtimeTestServer(t)
	defer server.close()
	text := speechTexts("Hi")
	_, err := newSpeechTestModel(t, server.ts.URL, "eleven_v4").DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{
		Text:            text,
		ProviderOptions: m("elevenlabs", SpeechModelOptions{Dialogue: &DialogueOptions{}}),
	})
	if err == nil || !text.wasCancelled() {
		t.Fatalf("err = %v cancelled = %v", err, text.wasCancelled())
	}
}
