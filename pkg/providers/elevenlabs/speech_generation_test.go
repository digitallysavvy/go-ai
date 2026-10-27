package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type genCall struct {
	path  string
	query map[string]string
	body  map[string]interface{}
}

// genServer records calls and answers with fixed audio.
func genServer(t *testing.T) (*Provider, *[]genCall) {
	t.Helper()
	var calls []genCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		q := map[string]string{}
		for k, v := range r.URL.Query() {
			q[k] = v[0]
		}
		calls = append(calls, genCall{r.URL.Path, q, body})
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte{0xff, 0xfb, 1, 2})
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL}), &calls
}

func gen(t *testing.T, p *Provider, modelID string, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	t.Helper()
	model, _ := p.SpeechModel(modelID)
	return model.DoGenerate(context.Background(), opts)
}

func features(ws []types.Warning) []string {
	out := []string{}
	for _, w := range ws {
		out = append(out, w.Feature)
	}
	return out
}

func el(kv ...interface{}) map[string]interface{} { return m("elevenlabs", m(kv...)) }

var dlgInputs = []interface{}{m("text", "Knock knock", "voiceId", "voice-a"), m("text", "Who is there?", "voiceId", "voice-b")}

func TestSpeechGenerate_ReportsOutputFormatMetadataAndPassesNativeFormats(t *testing.T) {
	p, calls := genServer(t)
	r, err := gen(t, p, "eleven_multilingual_v2", &provider.SpeechGenerateOptions{Text: "Hi", OutputFormat: "pcm_24000"})
	if err != nil || !reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "pcm_24000"))) {
		t.Fatalf("metadata = %#v err = %v", r.ProviderMetadata, err)
	}
	for _, c := range []struct {
		model, format, path string
		o                   map[string]interface{}
	}{
		{"music_v2_5", "auto", "/v1/music", nil},
		{"eleven_text_to_sound_v2", "wav_24000", "/v1/sound-generation", nil},
		{"eleven_v3", "wav_22050", "/v1/text-to-dialogue", el("inputs", dlgInputs)},
	} {
		r, err := gen(t, p, c.model, &provider.SpeechGenerateOptions{Text: "Hello", OutputFormat: c.format, ProviderOptions: c.o})
		if err != nil {
			t.Fatal(err)
		}
		last := (*calls)[len(*calls)-1]
		if last.path != c.path || last.query["output_format"] != c.format ||
			r.ProviderMetadata["elevenlabs"].(map[string]interface{})["outputFormat"] != c.format {
			t.Fatalf("%s: call = %+v meta = %v", c.model, last, r.ProviderMetadata)
		}
	}
}

func TestSpeechGenerate_Dialogue(t *testing.T) {
	p, calls := genServer(t)
	r, err := gen(t, p, "eleven_v3", &provider.SpeechGenerateOptions{
		Text: "Knock knock", Language: "en", OutputFormat: "mp3_22050_32",
		ProviderOptions: el("inputs", dlgInputs, "voiceSettings", m("stability", 0.4, "similarityBoost", 0.8), "nextText", "Lettuce.", "seed", 3.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := m("inputs", []interface{}{m("text", "Knock knock", "voice_id", "voice-a"), m("text", "Who is there?", "voice_id", "voice-b")},
		"model_id", "eleven_v3", "language_code", "en", "settings", m("stability", 0.4, "similarity", 0.8), "future_text", "Lettuce.", "seed", 3.0)
	if got := (*calls)[0]; got.path != "/v1/text-to-dialogue" || got.query["output_format"] != "mp3_22050_32" || !reflect.DeepEqual(got.body, want) {
		t.Fatalf("call = %#v", got)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Type != "other" ||
		r.Warnings[0].Message != "ElevenLabs dialogue uses providerOptions.elevenlabs.inputs; the text prompt was ignored." {
		t.Fatalf("warnings = %+v", r.Warnings)
	}
	if !strings.HasPrefix(r.Request.Body.(string), `{"inputs":[{"text":"Knock knock","voice_id":"voice-a"}`) {
		t.Fatalf("request body = %v", r.Request.Body)
	}
}

func TestSpeechGenerate_DialogueIgnoredOptionsAndModelRequirement(t *testing.T) {
	p, calls := genServer(t)
	speed := 1.1
	r, err := gen(t, p, "eleven_v3", &provider.SpeechGenerateOptions{
		Text: "x", Voice: "voice-a", Speed: &speed,
		ProviderOptions: el("inputs", dlgInputs, "voiceSettings", m("style", 0.2), "durationSeconds", 2.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := (*calls)[0].body["settings"]; has {
		t.Fatal("settings must be omitted")
	}
	want := []string{"providerOptions.elevenlabs.durationSeconds", "", "speed", "voice", "providerOptions.elevenlabs.voiceSettings.style"}
	if got := features(r.Warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("features = %v", got)
	}

	before := len(*calls)
	_, err = gen(t, p, "eleven_multilingual_v2", &provider.SpeechGenerateOptions{Text: "x", ProviderOptions: el("inputs", dlgInputs)})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "ElevenLabs dialogue requires an eleven_v3 or eleven_v4 model; eleven_multilingual_v2 is not one.") || len(*calls) != before {
		t.Fatalf("err = %v calls = %d", err, len(*calls))
	}

	r, err = gen(t, p, "music_v2", &provider.SpeechGenerateOptions{Text: "Lo-fi beat", ProviderOptions: el("inputs", dlgInputs)})
	if err != nil || !containsString(features(r.Warnings), "providerOptions.elevenlabs.inputs") {
		t.Fatalf("warnings = %+v err = %v", r.Warnings, err)
	}
}

func TestSpeechGenerate_Music(t *testing.T) {
	p, calls := genServer(t)
	r, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{
		Text: "An upbeat synthwave intro", OutputFormat: "mp3_22050_32",
		ProviderOptions: el("musicLengthMs", 10000.0, "forceInstrumental", true, "enableLogging", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.path != "/v1/music" || c.query["output_format"] != "mp3_22050_32" || c.query["enable_logging"] != "false" ||
		!reflect.DeepEqual(c.body, m("prompt", "An upbeat synthwave intro", "model_id", "music_v2_5", "music_length_ms", 10000.0, "force_instrumental", true)) {
		t.Fatalf("call = %#v", c)
	}
	if len(r.Warnings) != 0 || !reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "mp3_22050_32", "requestedDurationMs", 10000.0))) {
		t.Fatalf("result = %+v", r)
	}

	r, _ = gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "Lo-fi beat"})
	c = (*calls)[1]
	if c.query["output_format"] != "mp3_44100_128" || !reflect.DeepEqual(c.body, m("prompt", "Lo-fi beat", "model_id", "music_v2_5")) ||
		!reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "mp3_44100_128"))) {
		t.Fatalf("defaults: call = %#v meta = %v", c, r.ProviderMetadata)
	}
}

func TestSpeechGenerate_MusicIgnoredOptions(t *testing.T) {
	p, calls := genServer(t)
	speed := 1.2
	r, _ := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{
		Text: "Lo-fi beat", Voice: "v", Speed: &speed, Language: "en", Instructions: "calm",
		ProviderOptions: el("voiceSettings", m("stability", 0.5), "durationSeconds", 2.0, "loop", true),
	})
	if !reflect.DeepEqual((*calls)[0].body, m("prompt", "Lo-fi beat", "model_id", "music_v2_5")) {
		t.Fatalf("body = %#v", (*calls)[0].body)
	}
	want := []string{"instructions", "language", "speed", "voice", "providerOptions.elevenlabs.voiceSettings",
		"providerOptions.elevenlabs.durationSeconds", "providerOptions.elevenlabs.loop"}
	if got := features(r.Warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("features = %v", got)
	}
}

func TestSpeechGenerate_CompositionPlans(t *testing.T) {
	p, calls := genServer(t)
	chunked := m("chunks", []interface{}{
		m("text", "[Intro]", "duration_ms", 4000.0, "positive_styles", []interface{}{"ambient"}),
		m("song_id", "song-1", "range", m("start_ms", 1000.0, "end_ms", 3500.0)),
	})
	r, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "ignored", ProviderOptions: el("compositionPlan", chunked, "seed", 7.0)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual((*calls)[0].body, m("composition_plan", chunked, "model_id", "music_v2_5", "seed", 7.0)) {
		t.Fatalf("body = %#v", (*calls)[0].body)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Message != "ElevenLabs music uses providerOptions.elevenlabs.compositionPlan; the text prompt was ignored." {
		t.Fatalf("warnings = %+v", r.Warnings)
	}
	if !reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "mp3_44100_128", "requestedDurationMs", 6500.0))) {
		t.Fatalf("meta = %#v", r.ProviderMetadata)
	}

	section := func(d float64) map[string]interface{} {
		return m("section_name", "Verse", "positive_local_styles", []interface{}{"driving"}, "negative_local_styles", []interface{}{}, "lines", []interface{}{"Hello"}, "duration_ms", d)
	}
	v1 := m("positive_global_styles", []interface{}{"rock"}, "negative_global_styles", []interface{}{}, "sections", []interface{}{section(3000), section(5000)})
	r, err = gen(t, p, "music_v1", &provider.SpeechGenerateOptions{Text: "ignored", ProviderOptions: el("compositionPlan", v1)})
	if err != nil || r.ProviderMetadata["elevenlabs"].(map[string]interface{})["requestedDurationMs"] != 8000.0 {
		t.Fatalf("v1 meta = %#v err = %v", r.ProviderMetadata, err)
	}

	r, _ = gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "ignored", ProviderOptions: el("compositionPlan", m("chunks", []interface{}{}))})
	if !reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "mp3_44100_128"))) {
		t.Fatalf("empty plan meta = %#v", r.ProviderMetadata)
	}
}

func TestSpeechGenerate_MusicRejections(t *testing.T) {
	p, calls := genServer(t)
	for name, o := range map[string]map[string]interface{}{
		"chunk without a length": el("compositionPlan", m("chunks", []interface{}{m("text", "[Intro]")})),
		"length out of range":    el("musicLengthMs", 1000.0),
		"plan with length":       el("compositionPlan", m("chunks", []interface{}{}), "musicLengthMs", 5000.0),
		"plan with instrumental": el("compositionPlan", m("chunks", []interface{}{}), "forceInstrumental", false),
	} {
		if _, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "x", ProviderOptions: o}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	_, err := gen(t, p, "music_v2_5", &provider.SpeechGenerateOptions{Text: "x", ProviderOptions: el("compositionPlan", m("chunks", []interface{}{}), "musicLengthMs", 5000.0)})
	if err == nil || !strings.Contains(err.Error(), "ElevenLabs music musicLengthMs cannot be combined with compositionPlan.") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("rejections must not call the API; got %d calls", len(*calls))
	}
}

func TestSpeechGenerate_SoundEffects(t *testing.T) {
	p, calls := genServer(t)
	r, _ := gen(t, p, "eleven_text_to_sound_v2", &provider.SpeechGenerateOptions{
		Text: "Glass shattering", OutputFormat: "pcm_24000",
		ProviderOptions: el("durationSeconds", 2.0, "promptInfluence", 0.6, "loop", true),
	})
	c := (*calls)[0]
	if c.path != "/v1/sound-generation" || !reflect.DeepEqual(c.body, m("text", "Glass shattering", "model_id", "eleven_text_to_sound_v2", "duration_seconds", 2.0, "prompt_influence", 0.6, "loop", true)) {
		t.Fatalf("call = %#v", c)
	}
	if len(r.Warnings) != 0 || !reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "pcm_24000", "requestedDurationMs", 2000.0))) {
		t.Fatalf("result = %+v", r)
	}

	r, _ = gen(t, p, "eleven_text_to_sound_v2", &provider.SpeechGenerateOptions{Text: "Door creak"})
	if !reflect.DeepEqual((*calls)[1].body, m("text", "Door creak", "model_id", "eleven_text_to_sound_v2")) ||
		!reflect.DeepEqual(r.ProviderMetadata, m("elevenlabs", m("outputFormat", "mp3_44100_128"))) {
		t.Fatalf("defaults: %#v %v", (*calls)[1].body, r.ProviderMetadata)
	}

	r, _ = gen(t, p, "eleven_text_to_sound_v2", &provider.SpeechGenerateOptions{
		Text: "Door creak", ProviderOptions: el("musicLengthMs", 5000.0, "seed", 1.0, "previousText", "x"),
	})
	want := []string{"providerOptions.elevenlabs.previousText", "providerOptions.elevenlabs.musicLengthMs", "providerOptions.elevenlabs.seed"}
	if got := features(r.Warnings); !reflect.DeepEqual(got, want) {
		t.Fatalf("features = %v", got)
	}
}

func TestSpeechStream_RejectsMusicAndSoundEffectsAndCancelsText(t *testing.T) {
	for _, id := range []string{"music_v2_5", "eleven_text_to_sound_v2"} {
		text := speechTexts("Hi")
		model := NewSpeechModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), id)
		_, err := model.DoStreamSpeech(context.Background(), &provider.SpeechStreamOptions{Text: text})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "ElevenLabs "+id+" does not support streaming; use generateSpeech instead.") || !text.wasCancelled() {
			t.Fatalf("%s: err = %v", id, err)
		}
	}
}

func TestSpeechStream_WarnsAboutHTTPOnlyGenerationOptions(t *testing.T) {
	for name, c := range map[string]struct {
		model string
		extra map[string]interface{}
	}{
		"single-voice": {"eleven_flash_v2_5", nil},
		"dialogue":     {"eleven_v4", m("dialogue", m("voices", []interface{}{"voice-a"}))},
	} {
		t.Run(name, func(t *testing.T) {
			o := m("inputs", []interface{}{m("text", "Knock knock", "voiceId", "voice-a")}, "musicLengthMs", 5000.0, "durationSeconds", 2.0, "loop", true)
			for k, v := range c.extra {
				o[k] = v
			}
			result, server := startSpeech(t, c.model, &provider.SpeechStreamOptions{Text: speechTexts("Hi"), ProviderOptions: m("elevenlabs", o)})
			part, _ := result.Stream.Next()
			want := []string{"providerOptions.elevenlabs.inputs", "providerOptions.elevenlabs.musicLengthMs", "providerOptions.elevenlabs.durationSeconds", "providerOptions.elevenlabs.loop"}
			if got := features(part.Warnings); !reflect.DeepEqual(got, want) {
				t.Fatalf("features = %v", got)
			}
			for _, msg := range server.waitForCount(t, 1, 2e9) {
				if strings.Contains(toJSON(t, msg), "Knock") {
					t.Fatal("inputs must not be sent over the WebSocket")
				}
			}
		})
	}
}
