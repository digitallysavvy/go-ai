package cartesia

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r88provcSpeechCapture struct {
	mu     sync.Mutex
	bodies []string
}

func (c *r88provcSpeechCapture) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return ""
	}
	return c.bodies[len(c.bodies)-1]
}

func r88provcSpeech(t *testing.T, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, *r88provcSpeechCapture, error) {
	t.Helper()
	capture := &r88provcSpeechCapture{}
	p := newCartesiaTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capture.mu.Lock()
		capture.bodies = append(capture.bodies, string(b))
		capture.mu.Unlock()
		_, _ = w.Write([]byte("audio"))
	})
	m, _ := p.SpeechModel("sonic-3")
	if opts.Voice == "" {
		opts.Voice = "test-voice-id"
	}
	if opts.Text == "" {
		opts.Text = "Hello, world!"
	}
	res, err := m.DoGenerate(context.Background(), opts)
	return res, capture, err
}

func r88provcCartesiaOpts(o map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"cartesia": o}
}

// TS cartesia-speech-model.test.ts (392d2fa3a4, f10414f247, 47b666b6c7).
func TestR88provcCartesiaSpeechOptions(t *testing.T) {
	speed := 1.2
	tooFast := 2.0
	for _, tc := range []struct {
		name     string
		opts     *provider.SpeechGenerateOptions
		contains string
		absent   []string
		warn     []string
	}{
		{"volume", &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"volume": 1.5})}, `"generation_config":{"volume":1.5}`, nil, nil},
		{"emotion", &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"emotion": "calm"})}, `"generation_config":{"emotion":"calm"}`, nil, nil},
		{"speed volume emotion", &provider.SpeechGenerateOptions{Speed: &speed, ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"volume": 0.8, "emotion": "sad"})}, `"generation_config":{"speed":1.2,"volume":0.8,"emotion":"sad"}`, nil, nil},
		{"volume kept when speed ignored", &provider.SpeechGenerateOptions{Speed: &tooFast, ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"volume": 1.5})}, `"generation_config":{"volume":1.5}`, nil, []string{"speed"}},
		{"pronunciation dictionary", &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"pronunciationDictionaryId": "pdict_test"})}, `"pronunciation_dict_id":"pdict_test"`, nil, nil},
		{"defaults", &provider.SpeechGenerateOptions{}, `"output_format"`, []string{"pronunciation_dict_id", "generation_config", "locale", "normalization"}, nil},
		{"all together in TS key order", &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{
			"locale": "en-GB", "normalization": "off", "pronunciationDictionaryId": "test-dictionary", "volume": 1.2, "emotion": "excited",
		})}, `"locale":"en-GB","normalization":"off","pronunciation_dict_id":"test-dictionary","generation_config":{"volume":1.2,"emotion":"excited"}}`, []string{`"language"`}, nil},
		{"provider language wins over generic", &provider.SpeechGenerateOptions{Language: "en", ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"language": "es"})}, `"language":"es"`, nil, nil},
		{"locale alone", &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"locale": "en-GB"})}, `"locale":"en-GB"`, []string{`"language"`, "normalization"}, nil},
		{"normalization with language", &provider.SpeechGenerateOptions{Language: "en", ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"normalization": "es-MX"})}, `"language":"en","normalization":"es-MX"`, []string{"locale"}, nil},
		{"locale over generic language warns", &provider.SpeechGenerateOptions{Language: "en", ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"locale": "en-GB"})}, `"locale":"en-GB"`, []string{`"language"`}, []string{"language"}},
		{"empty locale counts", &provider.SpeechGenerateOptions{Language: "en", ProviderOptions: r88provcCartesiaOpts(map[string]interface{}{"locale": ""})}, `"locale":""`, []string{`"language"`}, []string{"language"}},
	} {
		res, capture, err := r88provcSpeech(t, tc.opts)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		body := capture.last()
		if !strings.Contains(body, tc.contains) {
			t.Fatalf("%s: body = %s", tc.name, body)
		}
		for _, a := range tc.absent {
			if strings.Contains(body, a) {
				t.Fatalf("%s: %s in body %s", tc.name, a, body)
			}
		}
		var features []string
		for _, w := range res.Warnings {
			features = append(features, w.Feature)
		}
		if !reflect.DeepEqual(features, tc.warn) {
			t.Fatalf("%s: warnings = %#v", tc.name, res.Warnings)
		}
		if tc.name == "locale over generic language warns" && res.Warnings[0] != (types.Warning{Type: "unsupported", Feature: "language", Details: "Cartesia does not support both locale and language. The language option was ignored in favor of providerOptions.cartesia.locale."}) {
			t.Fatalf("warning = %#v", res.Warnings[0])
		}
	}

	for _, bad := range []map[string]interface{}{
		{"volume": 0.4}, {"volume": 2.1}, {"emotion": "banana"}, {"volume": nil}, {"emotion": nil},
		{"pronunciationDictionaryId": nil}, {"locale": nil}, {"normalization": nil},
		{"locale": "en-GB", "language": "en"},
	} {
		_, capture, err := r88provcSpeech(t, &provider.SpeechGenerateOptions{ProviderOptions: r88provcCartesiaOpts(bad)})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || capture.last() != "" {
			t.Fatalf("%v: err = %v, body = %q", bad, err, capture.last())
		}
		if bad["locale"] == "en-GB" && (ia.Field != "providerOptions" || err.Error() != "providerOptions.cartesia.locale cannot be combined with providerOptions.cartesia.language") {
			t.Fatalf("conflict error = %q %q", ia.Field, err.Error())
		}
	}

	// Typed options reach the wire too.
	vol := 1.1
	_, capture, err := r88provcSpeech(t, &provider.SpeechGenerateOptions{ProviderOptions: map[string]interface{}{"cartesia": SpeechModelOptions{Volume: &vol, Emotion: "calm", Locale: "es-MX", PronunciationDictionaryID: "d"}}})
	if err != nil || !strings.Contains(capture.last(), `"locale":"es-MX","pronunciation_dict_id":"d","generation_config":{"volume":1.1,"emotion":"calm"}`) {
		t.Fatalf("typed: %v %s", err, capture.last())
	}
}

// TS cartesia-transcription-model.test.ts (467b299146): Ink 2 keyterms and
// turn thresholds.
func TestR88provcCartesiaStreamingKeytermsAndTurnThresholds(t *testing.T) {
	start := func(t *testing.T, streaming map[string]interface{}) (*provider.TranscriptionStreamResult, error) {
		t.Helper()
		server := newCartesiaRealtimeTestServer(t)
		t.Cleanup(server.close)
		model := newCartesiaRealtimeTestModel(server.ts.URL)
		return model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newChanAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: rate(24000)},
			ProviderOptions:  map[string]interface{}{"cartesia": map[string]interface{}{"streaming": streaming}},
		})
	}
	firstWarnings := func(t *testing.T, res *provider.TranscriptionStreamResult) []types.Warning {
		t.Helper()
		defer res.Stream.Close() //nolint:errcheck
		first, err := res.Stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		return first.Warnings
	}

	t.Run("invalid options fail before connecting", func(t *testing.T) {
		terms := make([]interface{}, 101)
		for i := range terms {
			terms[i] = "term"
		}
		for _, streaming := range []map[string]interface{}{{"keyterms": terms}, {"turnEndTimeoutMs": 800.5}, {"turnStartThreshold": nil}} {
			_, err := start(t, streaming)
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) {
				t.Fatalf("%v: err = %v", streaming, err)
			}
		}
	})

	t.Run("keyterms are repeated query parameters on both endpoints", func(t *testing.T) {
		res, err := start(t, map[string]interface{}{"keyterms": []interface{}{"Cartesia", "Sonic 3"}})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := res.RequestBody.(string)
		if !strings.Contains(body, "/stt/turns/websocket?") || !strings.HasSuffix(body, "&keyterm=Cartesia&keyterm=Sonic+3") {
			t.Fatalf("url = %s", body)
		}
		_ = res.Stream.Close()
		res, err = start(t, map[string]interface{}{"turnDetection": false, "keyterms": []interface{}{"Cartesia"}})
		if err != nil {
			t.Fatal(err)
		}
		body, _ = res.RequestBody.(string)
		if !strings.Contains(body, "/stt/websocket?") || !strings.HasSuffix(body, "&keyterm=Cartesia") {
			t.Fatalf("url = %s", body)
		}
		_ = res.Stream.Close()
	})

	thresholds := map[string]interface{}{"turnStartThreshold": 0.5, "turnEagerEndThreshold": 0.4, "turnEndThreshold": 0.3, "turnEndTimeoutMs": 800}

	t.Run("thresholds are sent with turn detection", func(t *testing.T) {
		res, err := start(t, thresholds)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := res.RequestBody.(string)
		if !strings.HasSuffix(body, "&turn_start_threshold=0.5&turn_eager_end_threshold=0.4&turn_end_threshold=0.3&turn_end_timeout_ms=800") {
			t.Fatalf("url = %s", body)
		}
		if w := firstWarnings(t, res); len(w) != 0 {
			t.Fatalf("warnings = %#v", w)
		}
	})

	t.Run("thresholds are omitted and warned without turn detection", func(t *testing.T) {
		opts := map[string]interface{}{"turnDetection": false}
		for k, v := range thresholds {
			opts[k] = v
		}
		res, err := start(t, opts)
		if err != nil {
			t.Fatal(err)
		}
		if body, _ := res.RequestBody.(string); strings.Contains(body, "turn_") {
			t.Fatalf("url = %s", body)
		}
		var want []types.Warning
		for _, f := range []string{"turnStartThreshold", "turnEagerEndThreshold", "turnEndThreshold", "turnEndTimeoutMs"} {
			want = append(want, types.Warning{Type: "unsupported", Feature: "providerOptions.cartesia.streaming." + f, Details: "Turn thresholds require turnDetection to be enabled."})
		}
		if w := firstWarnings(t, res); !reflect.DeepEqual(w, want) {
			t.Fatalf("warnings = %#v", w)
		}
	})
}
