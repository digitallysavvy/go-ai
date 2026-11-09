package cartesia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR90provcSpeechWarningsQuoteLikeTS: TS resolveOutputFormat
// (cartesia-speech-model.ts 84, 98) interpolates `"${outputFormat}"`, so a
// quote or non-ASCII rune is copied as is, not Go-escaped (%q).
func TestR90provcSpeechWarningsQuoteLikeTS(t *testing.T) {
	var warnings []types.Warning
	resolveOutputFormat(`og"g`, nil, &warnings)
	resolveOutputFormat(`wav_é`, nil, &warnings)
	want := []string{
		`Unknown output format "og"g". Falling back to mp3. Use providerOptions.cartesia to configure container, encoding, and sampleRate directly.`,
		`Unsupported Cartesia sample rate in output format "wav_é". Using 44100 Hz instead.`,
	}
	if len(warnings) != len(want) {
		t.Fatalf("warnings = %+v", warnings)
	}
	for i, w := range warnings {
		if w.Details != want[i] {
			t.Errorf("warning %d = %q, want %q", i, w.Details, want[i])
		}
	}
}

// TestR90provcRealtimeUnsupportedEventQuoteLikeTS: cartesia-realtime-model.ts
// 266 `does not support realtime client event "${event.type}".`.
func TestR90provcRealtimeUnsupportedEventQuoteLikeTS(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	_, _, _, err := m.SerializeClientEventRaw(provider.RealtimeClientEvent{Type: `bad"type`})
	want := `Cartesia Ink 2 does not support realtime client event "bad"type".`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestR90provcBatchSendsEmptyLanguage: TS getArgs
// (cartesia-transcription-model.ts 120) sets the form field when
// `cartesiaOptions.language != null`, so an empty string is sent.
func TestR90provcBatchSendsEmptyLanguage(t *testing.T) {
	var values []string
	var present bool
	p := newCartesiaJSONServer(t, cartesiaTranscriptionFixture, func(r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			return
		}
		values, present = r.MultipartForm.Value["language"]
	})
	model, _ := p.TranscriptionModel(ModelInkWhisper)
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:           cartesiaTestAudio,
		MimeType:        "audio/wav",
		ProviderOptions: map[string]interface{}{"cartesia": map[string]interface{}{"language": ""}},
	})
	if err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	if !present || len(values) != 1 || values[0] != "" {
		t.Fatalf("language form field = %v (present %v), want one empty value", values, present)
	}
}

// TestR90provcStreamRejectsEmptyLanguage: TS doStream
// (cartesia-transcription-model.ts 213-221) throws the InvalidArgumentError
// for `language != null && language !== 'en'`, which an empty string meets,
// before the access token request.
func TestR90provcStreamRejectsEmptyLanguage(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	model := newCartesiaRealtimeTestModel(srv.URL)
	_, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Audio:            newChanAudioStream(),
		ProviderOptions:  map[string]interface{}{"cartesia": map[string]interface{}{"language": ""}},
	})
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) || argErr.Message != "Cartesia Ink 2 currently supports English only." {
		t.Fatalf("err = %v, want the English-only InvalidArgumentError", err)
	}
	if n := atomic.LoadInt32(&requests); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of cartesiaErrorDataSchema (undeclared keys stripped) for a
// matching body and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	want := map[string]interface{}{"error_code": nil, "title": "T", "message": "m", "request_id": "r"}
	if got := data(`{"error_code":null,"title":"T","message":"m","request_id":"r","extra":1}`); !reflect.DeepEqual(got, want) {
		t.Errorf("matched data = %#v, want %#v", got, want)
	}
	if got := data(`{"message":"m","other":true}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}
